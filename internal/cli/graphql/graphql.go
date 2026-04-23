package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/api"
	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/output"
)

type opts struct {
	auth          string
	noAuth        bool
	query         string
	variables     string
	operationName string
	timeoutMS     int
	maxBytes      int64
	format        string
}

func Register(root *cobra.Command, globals shared.Globals) {
	o := &opts{}
	cmd := &cobra.Command{
		Use:   "graphql <endpoint>",
		Short: "Authenticated GraphQL POST",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(args[0], globals(), o)
		},
	}
	cmd.Flags().StringVar(&o.auth, "auth", "", "Credential alias")
	cmd.Flags().BoolVar(&o.noAuth, "no-auth", false, "Skip auth even if a credential matches")
	cmd.Flags().StringVar(&o.query, "query", "", "GraphQL document (required; @file, @- for stdin)")
	cmd.Flags().StringVar(&o.variables, "variables", "", "JSON variables (string, @file, or @-)")
	cmd.Flags().StringVar(&o.operationName, "operation-name", "", "Operation name")
	cmd.Flags().IntVar(&o.timeoutMS, "timeout", 0, "Request timeout in ms")
	cmd.Flags().Int64Var(&o.maxBytes, "max-size", 0, "Max response body size in bytes")
	cmd.Flags().StringVar(&o.format, "format", "", "Output format: json, raw, text")

	cmd.AddCommand(&cobra.Command{
		Use:   "llm-help",
		Short: "Show detailed reference for graphql",
		Run:   func(cmd *cobra.Command, args []string) { fmt.Print(usageText) },
	})

	root.AddCommand(cmd)
}

// gqlError mirrors the shape of a single GraphQL error in the response
// envelope. Declared once so the authenticated-retry classification path
// doesn't re-stringify an anonymous struct literal.
type gqlError struct {
	Message    string         `json:"message"`
	Path       []any          `json:"path"`
	Extensions map[string]any `json:"extensions"`
}

type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []gqlError      `json:"errors"`
}

func run(endpoint string, g *shared.GlobalFlags, o *opts) error {
	if strings.TrimSpace(o.query) == "" {
		return shared.Fail(agenterrors.New("--query is required", agenterrors.FixableByAgent).
			WithHint("Pass the GraphQL document as a string, @file, or @- for stdin"))
	}

	authName := shared.FirstNonEmpty(o.auth, g.Auth)
	var auth *credential.Resolved
	if !o.noAuth {
		a, err := shared.ResolveAuth(endpoint, authName)
		if err != nil {
			return shared.Fail(err)
		}
		auth = a
	}
	// Anonymous requests must opt in via --no-auth; ResolveAuth already
	// errors when no credential matches, so reaching here means the caller
	// either picked one explicitly or asked for anonymous on purpose.

	body, err := buildGraphQLPayload(o)
	if err != nil {
		return shared.Fail(err)
	}

	timeout, maxBytes := shared.ResolveLimits(o.timeoutMS, o.maxBytes, g)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	resp, err := api.Do(ctx, api.Request{
		Method:  "POST",
		URL:     endpoint,
		Headers: map[string]string{"Content-Type": "application/json", "Accept": "application/json"},
		Body:    bytes.NewReader(body),
		Auth:    auth,
	}, api.ClientOptions{
		Timeout:         timeout,
		MaxBytes:        maxBytes,
		FollowRedirects: true,
	})

	envelope, parsed := buildGraphQLEnvelope(endpoint, auth, resp)
	output.PrintJSON(envelope)

	if err != nil {
		return shared.Fail(err)
	}
	// 2xx HTTP but GraphQL-level errors — classify the first one.
	if resp != nil && resp.Status < 400 && len(parsed.Errors) > 0 {
		return shared.Fail(classifyGraphQL(parsed.Errors[0].Message, parsed.Errors[0].Extensions))
	}
	return nil
}

// buildGraphQLPayload assembles the {query, variables?, operationName?} JSON
// body from the flag state. Fails fast with fixable_by:agent on bad input.
func buildGraphQLPayload(o *opts) ([]byte, error) {
	query, err := loadInline(o.query)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{"query": string(query)}
	if o.operationName != "" {
		payload["operationName"] = o.operationName
	}
	if o.variables != "" {
		v, err := loadInline(o.variables)
		if err != nil {
			return nil, err
		}
		var vm any
		if err := json.Unmarshal(v, &vm); err != nil {
			return nil, agenterrors.Newf(agenterrors.FixableByAgent,
				"--variables is not valid JSON: %s", err.Error())
		}
		payload["variables"] = vm
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}
	return body, nil
}

// buildGraphQLEnvelope produces the top-level JSON envelope emitted to
// stdout and returns the parsed response so the caller can inspect errors
// without re-unmarshalling.
func buildGraphQLEnvelope(endpoint string, auth *credential.Resolved, resp *api.Response) (map[string]any, gqlResponse) {
	envelope := map[string]any{
		"endpoint":  endpoint,
		"status":    nil,
		"truncated": false,
		"data":      nil,
		"errors":    nil,
		"auth":      nil,
	}
	if auth != nil {
		envelope["auth"] = auth.Name
	}
	var parsed gqlResponse
	if resp == nil {
		return envelope, parsed
	}
	envelope["status"] = resp.Status
	envelope["truncated"] = resp.Truncated
	_ = json.Unmarshal(resp.Body, &parsed)
	if len(parsed.Data) > 0 {
		var dataAny any
		_ = json.Unmarshal(parsed.Data, &dataAny)
		envelope["data"] = dataAny
	}
	if len(parsed.Errors) > 0 {
		envelope["errors"] = parsed.Errors
	}
	return envelope, parsed
}

func classifyGraphQL(message string, extensions map[string]any) *agenterrors.APIError {
	code, _ := extensions["code"].(string)
	upper := strings.ToUpper(code)
	if upper == "UNAUTHENTICATED" || upper == "FORBIDDEN" {
		return agenterrors.Newf(agenterrors.FixableByHuman, "GraphQL error (%s): %s", upper, message).
			WithHint("Credentials were rejected by the GraphQL server. Ask the user to verify the stored credential.")
	}
	return agenterrors.Newf(agenterrors.FixableByAgent, "GraphQL error: %s", message).
		WithHint("Check the query, variables, and field selection")
}

func loadInline(spec string) ([]byte, error) {
	if spec == "@-" {
		return io.ReadAll(os.Stdin)
	}
	if strings.HasPrefix(spec, "@") {
		b, err := os.ReadFile(spec[1:])
		if err != nil {
			return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent).
				WithHint("Check the path and ensure the file is readable")
		}
		return b, nil
	}
	return []byte(spec), nil
}

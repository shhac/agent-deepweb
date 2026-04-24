package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/api"
	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/output"
)

type opts struct {
	profile       string
	cookieJar     string
	query         string
	variables     string
	operationName string
	timeoutMS     int
	maxBytes      int64
	format        string
	track         bool
	hideRequest   bool
	hideResponse  bool
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
	cmd.Flags().StringVar(&o.profile, "profile", "", "Profile name, or 'none' for explicit anonymous")
	cmd.Flags().StringVar(&o.cookieJar, "cookiejar", "", "Bring-your-own cookie jar (plaintext JSON file)")
	cmd.Flags().StringVar(&o.query, "query", "", "GraphQL document (required; @file, @- for stdin)")
	cmd.Flags().StringVar(&o.variables, "variables", "", "JSON variables (string, @file, or @-)")
	cmd.Flags().StringVar(&o.operationName, "operation-name", "", "Operation name")
	cmd.Flags().IntVar(&o.timeoutMS, "timeout", 0, "Request timeout in ms")
	cmd.Flags().Int64Var(&o.maxBytes, "max-size", 0, "Max response body size in bytes")
	cmd.Flags().StringVar(&o.format, "format", "", "Output format: json, raw, text")
	cmd.Flags().BoolVar(&o.track, "track", false, "Persist a full-fidelity record of this request/response; retrieve later with 'agent-deepweb audit show <id>'")
	cmd.Flags().BoolVar(&o.hideRequest, "hide-request", false, "Omit the 'request' field from the envelope")
	cmd.Flags().BoolVar(&o.hideResponse, "hide-response", false, "Omit response headers/body from the envelope")

	shared.LLMHelp(cmd, "graphql", usageText)
	registerImportSchema(cmd)

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

	profileName := shared.FirstNonEmpty(o.profile, g.Profile)
	auth, err := shared.ResolveProfile(endpoint, profileName)
	if err != nil {
		return shared.Fail(err)
	}
	// Anonymous requests must opt in via `--profile none`; ResolveProfile
	// errors when no profile matches and the flag is empty, so reaching
	// here means the caller picked one explicitly or asked for anonymous.

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
		JarPath: o.cookieJar,
		Track:   o.track,
	}, api.ClientOptions{
		Timeout:         timeout,
		MaxBytes:        maxBytes,
		FollowRedirects: true,
	})

	envelope, parsed := buildGraphQLEnvelope(endpoint, auth, resp, o.hideRequest, o.hideResponse)
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
	query, err := shared.LoadInlineSpec(o.query)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{"query": string(query)}
	if o.operationName != "" {
		payload["operationName"] = o.operationName
	}
	if o.variables != "" {
		v, err := shared.LoadInlineSpec(o.variables)
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
// without re-unmarshalling. hideRequest/hideResponse mirror the same
// flags on fetch — token-saving opt-outs for the LLM.
func buildGraphQLEnvelope(endpoint string, auth *credential.Resolved, resp *api.Response, hideRequest, hideResponse bool) (map[string]any, gqlResponse) {
	envelope := map[string]any{
		"endpoint": endpoint,
		"status":   nil,
		"profile":  nil,
	}
	if auth != nil {
		envelope["profile"] = auth.Name
	}
	if resp != nil && resp.AuditID != "" {
		envelope["audit_id"] = resp.AuditID
	}
	if !hideRequest && resp != nil && resp.Sent.Method != "" {
		envelope["request"] = map[string]any{
			"method":     resp.Sent.Method,
			"url":        resp.Sent.URL,
			"headers":    resp.Sent.Headers,
			"body_bytes": resp.Sent.BodyBytes,
		}
	}
	if !hideResponse {
		envelope["truncated"] = false
		envelope["data"] = nil
		envelope["errors"] = nil
	}
	var parsed gqlResponse
	if resp == nil {
		return envelope, parsed
	}
	envelope["status"] = resp.Status
	_ = json.Unmarshal(resp.Body, &parsed)
	if !hideResponse {
		envelope["truncated"] = resp.Truncated
		if len(parsed.Data) > 0 {
			var dataAny any
			_ = json.Unmarshal(parsed.Data, &dataAny)
			envelope["data"] = dataAny
		}
		if len(parsed.Errors) > 0 {
			envelope["errors"] = parsed.Errors
		}
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


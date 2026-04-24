// Package jsonrpc implements the `jsonrpc` command — the JSON-RPC 2.0
// analog of `graphql`. Same profile/jar/track plumbing; a POST body
// shaped {"jsonrpc":"2.0","method":...,"params":...,"id":N}; response
// classified into result vs error.{code,message,data} with standard
// error codes mapped to fixable_by.
//
// File layout:
//
//	jsonrpc.go   Register + run + buildPayload + buildEnvelope
//	classify.go  standard error codes → fixable_by
//	usage.go     llm-help reference card
package jsonrpc

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
	profile      string
	cookieJar    string
	method       string
	params       string
	id           string
	notify       bool
	timeoutMS    int
	maxBytes     int64
	format       string
	track        bool
	hideRequest  bool
	hideResponse bool
}

func Register(root *cobra.Command, globals shared.Globals) {
	o := &opts{}
	cmd := &cobra.Command{
		Use:   "jsonrpc <endpoint>",
		Short: "Authenticated JSON-RPC 2.0 POST",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(args[0], globals(), o)
		},
	}
	cmd.Flags().StringVar(&o.profile, "profile", "", "Profile name, or 'none' for explicit anonymous")
	cmd.Flags().StringVar(&o.cookieJar, "cookiejar", "", "Bring-your-own cookie jar (plaintext JSON file)")
	cmd.Flags().StringVar(&o.method, "method", "", "JSON-RPC method name (required, e.g. eth_blockNumber)")
	cmd.Flags().StringVar(&o.params, "params", "", "JSON-encoded params (array or object, or @file, @-)")
	cmd.Flags().StringVar(&o.id, "id", "1", "Request id (string or integer; default '1'). Ignored when --notify.")
	cmd.Flags().BoolVar(&o.notify, "notify", false, "Send as a notification (no id, server does not reply)")
	cmd.Flags().IntVar(&o.timeoutMS, "timeout", 0, "Request timeout in ms")
	cmd.Flags().Int64Var(&o.maxBytes, "max-size", 0, "Max response body size in bytes")
	cmd.Flags().StringVar(&o.format, "format", "", "Output format: json, raw, text")
	cmd.Flags().BoolVar(&o.track, "track", false, "Persist a full-fidelity record; retrieve later with 'agent-deepweb audit show <id>'")
	cmd.Flags().BoolVar(&o.hideRequest, "hide-request", false, "Omit the 'request' field from the envelope")
	cmd.Flags().BoolVar(&o.hideResponse, "hide-response", false, "Omit response headers/body from the envelope")

	shared.LLMHelp(cmd, "jsonrpc", usageText)

	root.AddCommand(cmd)
}

// rpcError mirrors the JSON-RPC 2.0 error object shape.
type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	ID      any             `json:"id"`
}

func run(endpoint string, g *shared.GlobalFlags, o *opts) error {
	if strings.TrimSpace(o.method) == "" {
		return shared.Fail(agenterrors.New("--method is required", agenterrors.FixableByAgent).
			WithHint("Pass the RPC method name, e.g. --method eth_blockNumber"))
	}

	profileName := shared.FirstNonEmpty(o.profile, g.Profile)
	auth, err := shared.ResolveProfile(endpoint, profileName)
	if err != nil {
		return shared.Fail(err)
	}

	body, err := buildPayload(o)
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

	envelope, parsed := buildEnvelope(endpoint, auth, resp, o.hideRequest, o.hideResponse)
	output.PrintJSON(envelope)

	if err != nil {
		return shared.Fail(err)
	}
	// 2xx HTTP but RPC-level error — classify via the standard codes.
	if resp != nil && resp.Status < 400 && parsed.Error != nil {
		return shared.Fail(classifyRPC(parsed.Error.Code, parsed.Error.Message))
	}
	return nil
}

// buildPayload assembles {"jsonrpc":"2.0","method":...,"params":...,"id":...}
// from the flag state. Fails fast with fixable_by:agent on bad input.
// --notify omits the id (per spec: notifications have no id and no reply).
func buildPayload(o *opts) ([]byte, error) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  o.method,
	}
	if o.params != "" {
		raw, err := shared.LoadInlineSpec(o.params)
		if err != nil {
			return nil, err
		}
		var pm any
		if err := json.Unmarshal(raw, &pm); err != nil {
			return nil, agenterrors.Newf(agenterrors.FixableByAgent,
				"--params is not valid JSON: %s", err.Error()).
				WithHint("Pass a JSON array or object (e.g. '[\"0x123\",true]' or '{\"key\":\"val\"}')")
		}
		payload["params"] = pm
	}
	if !o.notify {
		// Try numeric id first so `--id 123` becomes the number 123 rather
		// than the string "123" — some servers are strict about this.
		payload["id"] = coerceID(o.id)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}
	return body, nil
}

// coerceID returns the id as a JSON number when it parses as an integer,
// otherwise as a string. Matches what most servers expect when a client
// says `--id 1`.
func coerceID(s string) any {
	var n int64
	if err := json.Unmarshal([]byte(s), &n); err == nil {
		return n
	}
	return s
}

func buildEnvelope(endpoint string, auth *credential.Resolved, resp *api.Response, hideRequest, hideResponse bool) (map[string]any, rpcResponse) {
	envelope := output.BuildBaseEnvelope(baseIn(auth, resp, hideRequest))
	envelope["endpoint"] = endpoint
	if !hideResponse {
		envelope["truncated"] = false
		envelope["result"] = nil
		envelope["error"] = nil
	}
	var parsed rpcResponse
	if resp == nil {
		envelope["status"] = nil
		return envelope, parsed
	}
	_ = json.Unmarshal(resp.Body, &parsed)
	if !hideResponse {
		envelope["truncated"] = resp.Truncated
		if len(parsed.Result) > 0 {
			var resAny any
			_ = json.Unmarshal(parsed.Result, &resAny)
			envelope["result"] = resAny
		}
		if parsed.Error != nil {
			envelope["error"] = parsed.Error
		}
	}
	return envelope, parsed
}

// baseIn is the shared shim both this verb and graphql use to assemble
// a BaseEnvelopeIn from an api.Response. Pulls the Sent snapshot into
// the base envelope's request fields; a nil resp degrades to
// "no request visible yet" (status will be nil in the envelope).
func baseIn(auth *credential.Resolved, resp *api.Response, hideRequest bool) output.BaseEnvelopeIn {
	in := output.BaseEnvelopeIn{
		Auth:        auth,
		HideRequest: hideRequest,
	}
	if resp != nil {
		in.Status = resp.Status
		in.AuditID = resp.AuditID
		in.RequestMethod = resp.Sent.Method
		in.RequestURL = resp.Sent.URL
		in.RequestHeaders = resp.Sent.Headers
		in.RequestBodyBytes = resp.Sent.BodyBytes
	}
	return in
}

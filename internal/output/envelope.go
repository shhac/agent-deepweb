package output

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/shhac/agent-deepweb/internal/credential"
)

// EnvelopeIn carries the fields fetch/tpl both want in their JSON output.
// Declared here (rather than importing api) so this package stays
// dependency-free of the HTTP layer.
type EnvelopeIn struct {
	URL         string
	Auth        *credential.Resolved
	Status      int
	StatusText  string
	Headers     http.Header
	ContentType string
	Body        []byte
	Truncated   bool

	// Request info surfaced alongside the response. Populated by the CLI
	// layer from api.Response.Sent. Included in the envelope by default;
	// suppressed when HideRequest is true (--hide-request CLI flag).
	RequestMethod    string
	RequestURL       string
	RequestHeaders   http.Header
	RequestBodyBytes int

	// AuditID is set when the request was run with --track. Empty
	// otherwise; omitted from the envelope when empty.
	AuditID string

	// Visibility toggles driven by --hide-request / --hide-response.
	// Default: both false (all fields included).
	HideRequest  bool
	HideResponse bool
}

// BaseEnvelopeIn is the minimal set of fields every verb's response
// envelope carries: the status code, the resolved profile, an optional
// audit_id (populated when --track was set), and a snapshot of the
// request that went out. The URL / endpoint key is NOT included here
// because the key name varies by verb ("url" for fetch, "endpoint"
// for graphql + jsonrpc); callers insert that one field themselves.
type BaseEnvelopeIn struct {
	Auth             *credential.Resolved
	Status           int
	AuditID          string
	RequestMethod    string
	RequestURL       string
	RequestHeaders   http.Header
	RequestBodyBytes int
	HideRequest      bool
}

// BuildBaseEnvelope returns an envelope carrying the fields shared by
// every verb: status, profile, optional audit_id, and (unless
// HideRequest) a redacted snapshot of the request. Verbs layer their
// own response-shaped fields on top.
//
// Invariant: the returned map is freshly-allocated — callers can add
// and mutate without risking aliasing with a future call.
func BuildBaseEnvelope(in BaseEnvelopeIn) map[string]any {
	env := map[string]any{
		"status": in.Status,
	}
	if in.Auth != nil {
		env["profile"] = in.Auth.Name
	} else {
		env["profile"] = nil
	}
	if in.AuditID != "" {
		env["audit_id"] = in.AuditID
	}
	if !in.HideRequest && in.RequestMethod != "" {
		env["request"] = map[string]any{
			"method":     in.RequestMethod,
			"url":        in.RequestURL,
			"headers":    in.RequestHeaders,
			"body_bytes": in.RequestBodyBytes,
		}
	}
	return env
}

// BuildHTTPEnvelope returns the LLM-facing map for fetch/tpl responses.
// Shape is stable — documented in fetch/usage.go.
//
// By default includes:
//   - response: status, status_text, url, headers, content_type, truncated, body
//   - request: method, url, headers, body_bytes
//   - profile: the resolved profile name or "none"/nil
//   - audit_id: when --track was set
//
// --hide-request drops the "request" field (save tokens when the LLM
// only cares about the response). --hide-response drops everything
// response-shaped except status/url/profile/audit_id (save tokens when
// the LLM only cares about "did it work").
func BuildHTTPEnvelope(in EnvelopeIn) map[string]any {
	env := BuildBaseEnvelope(BaseEnvelopeIn{
		Auth:             in.Auth,
		Status:           in.Status,
		AuditID:          in.AuditID,
		RequestMethod:    in.RequestMethod,
		RequestURL:       in.RequestURL,
		RequestHeaders:   in.RequestHeaders,
		RequestBodyBytes: in.RequestBodyBytes,
		HideRequest:      in.HideRequest,
	})
	env["url"] = in.URL
	if !in.HideResponse {
		env["status_text"] = in.StatusText
		env["headers"] = in.Headers
		env["content_type"] = in.ContentType
		env["truncated"] = in.Truncated
		env["body"] = RenderBody(in.ContentType, in.Body)
	}
	return env
}

// RenderResponse handles the output-format switch shared by the request
// verbs. The format string is the global --format value (already validated
// command-aware by NewRoot via AllowFormats, so it is one of
// json|yaml|jsonl|raw|text or empty). Bails on a nil resp; otherwise writes
// one of:
//   - format=raw          → response body bytes directly to stdout
//   - format=text         → "HTTP <status> <text>\n\n" + body bytes
//   - json | yaml | jsonl → envelope built via BuildHTTPEnvelope, rendered via
//     out.Print (json pretty, jsonl one compact line, yaml via the registered
//     encoder), with `extras` merged in (e.g. {"new_cookies": ...} from fetch,
//     or {"template": <name>} from tpl).
//
// Centralising this means the envelope shape, the text-format preamble, and the
// raw-bytes fallback evolve in one place.
//
// When --track was used, the audit ID is also written to stderr so
// it's visible in raw/text modes where the envelope isn't printed.
func RenderResponse(in EnvelopeIn, status int, statusText string, body []byte, format string, extras map[string]any) {
	if PrintBody(format, status, statusText, body, in.AuditID) {
		return
	}
	env := BuildHTTPEnvelope(in)
	for k, v := range extras {
		env[k] = v
	}
	PrintEnvelope(env, format)
}

// RenderBody decodes JSON bodies into native values so the envelope stays
// a single coherent JSON document; falls back to a string for non-JSON.
func RenderBody(contentType string, body []byte) any {
	if strings.Contains(strings.ToLower(contentType), "json") {
		var v any
		if err := json.Unmarshal(body, &v); err == nil {
			return v
		}
	}
	return string(body)
}

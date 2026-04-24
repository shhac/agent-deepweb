package output

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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
	env := map[string]any{
		"url":    in.URL,
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
	if !in.HideResponse {
		env["status_text"] = in.StatusText
		env["headers"] = in.Headers
		env["content_type"] = in.ContentType
		env["truncated"] = in.Truncated
		env["body"] = RenderBody(in.ContentType, in.Body)
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

// RenderResponse handles the raw/text/json output-format switch shared
// by fetch and tpl. Bails on a nil resp; otherwise writes one of:
//   - format=raw   → response body bytes directly to stdout
//   - format=text  → "HTTP <status> <text>\n\n" + body bytes
//   - format=json  → JSON envelope built via BuildHTTPEnvelope, with
//     `extras` merged in (e.g. {"new_cookies": ...} from fetch, or
//     {"template": <name>} from tpl).
//
// Centralising this means the JSON envelope shape, the text-format
// preamble, and the raw-bytes fallback evolve in one place.
//
// When --track was used, the audit ID is also written to stderr so
// it's visible in raw/text modes where the envelope isn't printed.
func RenderResponse(in EnvelopeIn, status int, statusText string, body []byte, format string, extras map[string]any) {
	f, _ := ParseFormat(format)
	switch f {
	case FormatRaw:
		_, _ = os.Stdout.Write(body)
		if in.AuditID != "" {
			fmt.Fprintln(os.Stderr, "audit_id:", in.AuditID)
		}
		return
	case FormatText:
		fmt.Printf("HTTP %d %s\n\n", status, statusText)
		_, _ = os.Stdout.Write(body)
		if in.AuditID != "" {
			fmt.Fprintln(os.Stderr, "audit_id:", in.AuditID)
		}
		return
	}
	env := BuildHTTPEnvelope(in)
	for k, v := range extras {
		env[k] = v
	}
	PrintJSON(env)
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

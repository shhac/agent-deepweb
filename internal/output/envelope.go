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
}

// BuildHTTPEnvelope returns the LLM-facing map for fetch/tpl responses.
// Shape is stable — documented in fetch/usage.go.
func BuildHTTPEnvelope(in EnvelopeIn) map[string]any {
	env := map[string]any{
		"status":       in.Status,
		"status_text":  in.StatusText,
		"url":          in.URL,
		"headers":      in.Headers,
		"content_type": in.ContentType,
		"truncated":    in.Truncated,
		"body":         RenderBody(in.ContentType, in.Body),
	}
	if in.Auth != nil {
		env["profile"] = in.Auth.Name
	} else {
		env["profile"] = nil
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
func RenderResponse(in EnvelopeIn, status int, statusText string, body []byte, format string, extras map[string]any) {
	f, _ := ParseFormat(format)
	switch f {
	case FormatRaw:
		_, _ = os.Stdout.Write(body)
		return
	case FormatText:
		fmt.Printf("HTTP %d %s\n\n", status, statusText)
		_, _ = os.Stdout.Write(body)
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

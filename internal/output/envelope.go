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

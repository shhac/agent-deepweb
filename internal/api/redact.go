package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/shhac/agent-deepweb/internal/credential"
)

// headerRedactPattern matches response headers whose values we never print,
// because a misbehaving (or compromised) upstream might echo back tokens,
// cookies, or keys.
var headerRedactPattern = regexp.MustCompile(
	`(?i)^(authorization|cookie|set-cookie|x-[a-z0-9-]*(?:token|auth|key)|api[-_]?key)$`,
)

// bodyFieldRedactPattern matches JSON field names that typically carry
// secret material. Used by RedactJSONBody. We match as a substring so e.g.
// "clientSecret" and "client_secret" both hit.
var bodyFieldRedactPattern = regexp.MustCompile(
	`(?i)(authorization|cookie|access_token|refresh_token|id_token|api[-_]?key|client_secret|password|bearer|secret|token)`,
)

// RedactHeaders returns a copy of h with sensitive headers replaced by "<redacted>".
func RedactHeaders(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for k, vs := range h {
		if headerRedactPattern.MatchString(k) {
			redacted := make([]string, len(vs))
			for i := range vs {
				redacted[i] = "<redacted>"
			}
			out[k] = redacted
			continue
		}
		out[k] = append([]string(nil), vs...)
	}
	return out
}

// RedactJSONBody walks the body as JSON. If it's an object (or nested),
// any field whose name matches bodyFieldRedactPattern has its string value
// replaced with "<redacted>". Returns the original bytes unchanged if the
// body isn't valid JSON or isn't JSON-y (likely HTML).
func RedactJSONBody(body []byte, contentType string) []byte {
	ct := strings.ToLower(contentType)
	if !strings.Contains(ct, "json") {
		return body
	}
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return body
	}
	redacted := redactValue(decoded)
	out, err := json.MarshalIndent(redacted, "", "  ")
	if err != nil {
		return body
	}
	return out
}

// RedactSecretEcho walks the raw bytes and replaces any substring that exactly
// matches a secret value held by the Resolved credential. This is belt-and-
// braces defense: even if a server echoes a token in a field we don't
// recognise by name, the literal value gets masked on the way out.
// Only values longer than 4 bytes are considered (skip empty / trivial strings).
func RedactSecretEcho(body []byte, resolved *credential.Resolved) []byte {
	if resolved == nil {
		return body
	}
	mask := []byte("<redacted>")
	needles := []string{
		resolved.Secrets.Token,
		resolved.Secrets.Password,
		resolved.Secrets.Cookie,
	}
	for _, v := range resolved.Secrets.Headers {
		needles = append(needles, v)
	}
	// Also mask any live session token (form-auth) and sensitive cookies.
	if resolved.Type == credential.AuthForm {
		if sess, err := credential.ReadSession(resolved.Name); err == nil {
			if sess.Token != "" {
				needles = append(needles, sess.Token)
			}
			for _, c := range sess.Cookies {
				if c.Sensitive {
					needles = append(needles, c.Value)
				}
			}
		}
	}
	for _, n := range needles {
		if len(n) <= 4 {
			continue
		}
		body = bytes.ReplaceAll(body, []byte(n), mask)
	}
	return body
}

func redactValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, child := range val {
			if bodyFieldRedactPattern.MatchString(k) {
				if _, ok := child.(string); ok {
					out[k] = "<redacted>"
					continue
				}
			}
			out[k] = redactValue(child)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, child := range val {
			out[i] = redactValue(child)
		}
		return out
	default:
		return v
	}
}

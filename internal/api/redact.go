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

// RedactSecretEcho walks the raw bytes and replaces any substring that
// exactly matches a secret value held by the Resolved credential. This is
// belt-and-braces defense: even if a server echoes a token in a field we
// don't recognise by name, the literal value gets masked on the way out.
// Only needles longer than 4 bytes are considered — trivial values would
// false-positive on common body text.
//
// Also masks any sensitive cookie values stored in the profile's jar
// (any profile type can have a jar in v2), and — for form-auth — the
// session-acquired bearer token.
func RedactSecretEcho(body []byte, resolved *credential.Resolved) []byte {
	if resolved == nil {
		return body
	}
	needles := gatherNeedles(resolved)
	if len(needles) == 0 {
		return body
	}
	mask := []byte("<redacted>")
	for _, n := range needles {
		body = bytes.ReplaceAll(body, []byte(n), mask)
	}
	return body
}

// gatherNeedles collects the secret values from resolved.Secrets and
// the profile's jar that are long enough to redact safely. Short values
// (≤ 4 bytes) are skipped to avoid false positives.
func gatherNeedles(resolved *credential.Resolved) []string {
	var needles []string
	add := func(s string) {
		if len(s) > 4 {
			needles = append(needles, s)
		}
	}
	add(resolved.Secrets.Token)
	add(resolved.Secrets.Password)
	add(resolved.Secrets.Cookie)
	for _, v := range resolved.Secrets.Headers {
		add(v)
	}
	if jarState, err := credential.ReadJar(resolved.Name); err == nil {
		add(jarState.Token)
		for _, c := range jarState.Cookies {
			if c.Sensitive {
				add(c.Value)
			}
		}
	}
	return needles
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

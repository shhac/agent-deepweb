package importers

import (
	"github.com/shhac/agent-deepweb/internal/template"

	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

// importhelpers.go — cross-importer utilities shared by the Postman,
// OpenAPI / Swagger v2, HAR, .http file, curl, and GraphQL-schema
// importers. None of these helpers have any format-specific
// knowledge; they exist at this layer because every importer ends
// up needing to split a query string, sanitise an identifier, or
// scan a string for `{{placeholders}}`.
//
// Pure functions only. Anything with I/O or mutation of global state
// belongs in the format-specific file or in template.go.

// splitQuery lifts a `?k=v&k2=v2` suffix off a URL into a map, so
// template.Template.Query can carry per-key placeholders independently of the
// URL. Returns the base URL (suffix stripped) and the map; map is
// nil when there's no query string.
//
// Not round-trip-safe for multi-value queries (duplicate keys collapse
// to the last value) — callers that need multi-value semantics should
// use url.ParseQuery directly.
func splitQuery(u string) (string, map[string]string) {
	i := strings.IndexByte(u, '?')
	if i < 0 {
		return u, nil
	}
	base := u[:i]
	qs := u[i+1:]
	out := map[string]string{}
	for _, pair := range strings.Split(qs, "&") {
		if pair == "" {
			continue
		}
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) != 2 {
			continue
		}
		out[kv[0]] = kv[1]
	}
	if len(out) == 0 {
		return base, nil
	}
	return base, out
}

// sanitiseIdentifier lowercases and replaces anything outside
// [a-z0-9._-] with a single underscore, collapsing runs and stripping
// trailing punctuation. Produces a stable, shell-safe leaf name for
// `<prefix>.<leaf>` template names.
func sanitiseIdentifier(s string) string {
	var b strings.Builder
	s = strings.ToLower(s)
	prevUnderscore := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= '0' && r <= '9',
			r == '.', r == '-':
			b.WriteRune(r)
			prevUnderscore = false
		default:
			if !prevUnderscore && b.Len() > 0 {
				b.WriteRune('_')
				prevUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_-.")
}

var pathParamRe = regexp.MustCompile(`\{([^{}]+)\}`)

// pathToPlaceholders rewrites OpenAPI/Swagger `{id}` placeholders to
// our engine's `{{id}}` form. Type hints (`{id:int}`) are stripped.
func pathToPlaceholders(path string) string {
	return pathParamRe.ReplaceAllStringFunc(path, func(match string) string {
		inner := match[1 : len(match)-1]
		if i := strings.IndexByte(inner, ':'); i != -1 {
			inner = inner[:i]
		}
		return "{{" + inner + "}}"
	})
}

// collectPlaceholdersInto walks s looking for `{{name}}` and writes
// each unique name it finds into acc. Nested braces aren't expected
// in the formats we import, so we stop at the first matching `}}`.
func collectPlaceholdersInto(s string, acc map[string]bool) {
	for {
		i := strings.Index(s, "{{")
		if i < 0 {
			return
		}
		s = s[i+2:]
		j := strings.Index(s, "}}")
		if j < 0 {
			return
		}
		name := strings.TrimSpace(s[:j])
		if name != "" {
			acc[name] = true
		}
		s = s[j+2:]
	}
}

// looksLikeJSON returns true for strings that look structurally JSON:
// trimmed string starts with `{` or `[`. Used by body-format sniffers
// when the Content-Type header is absent or untrusted.
func looksLikeJSON(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")
}

// looksLikeForm returns true for k=v&k2=v2 shaped bodies — a
// conservative sniff that lets a body without an explicit
// Content-Type still land as body_format=form when that's clearly
// what it is.
func looksLikeForm(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, "{}[]\n") {
		return false
	}
	if !strings.Contains(s, "=") {
		return false
	}
	for _, pair := range strings.Split(s, "&") {
		if strings.Count(pair, "=") < 1 {
			return false
		}
	}
	return true
}

// copyMap returns a shallow copy of a string-to-string map. Used by
// importers that need to layer folder-scoped variables over an
// inherited set without mutating the parent.
func copyMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// anyAncestorMatches is a case-insensitive substring match over a
// folder-path ancestor list. Used by importers that accept a --folder
// filter flag.
func anyAncestorMatches(ancestors []string, needle string) bool {
	n := strings.ToLower(needle)
	for _, a := range ancestors {
		if strings.Contains(strings.ToLower(a), n) {
			return true
		}
	}
	return false
}

// collectTemplateRefs walks the URL, query map, headers map, and body
// template looking for `{{placeholder}}` tokens and returns the set
// of unique names. Every importer needs this: a template.Template's declared
// parameters must exactly cover the placeholders it references
// (otherwise run-time substitution leaves `{{foo}}` in the wire
// bytes, which is a silent correctness bug).
func collectTemplateRefs(urlBase string, query, headers map[string]string, body []byte) map[string]bool {
	refs := map[string]bool{}
	collectPlaceholdersInto(urlBase, refs)
	for k, v := range query {
		collectPlaceholdersInto(k, refs)
		collectPlaceholdersInto(v, refs)
	}
	for k, v := range headers {
		collectPlaceholdersInto(k, refs)
		collectPlaceholdersInto(v, refs)
	}
	if len(body) > 0 {
		collectPlaceholdersInto(string(body), refs)
	}
	return refs
}

// uniquifyName returns a deduplicated template name using a shared
// counter map. The first time a base is seen it's returned as-is;
// subsequent occurrences gain a numeric suffix (`base_2`, `base_3`,
// …) so importers that re-emit the same (method, path) shape produce
// distinctly-named templates rather than overwriting each other.
//
// Counter maps are owned by the caller so independent import runs
// don't collide across calls.
func uniquifyName(base string, counters map[string]int) string {
	counters[base]++
	if counters[base] == 1 {
		return base
	}
	return base + "_" + itoaPositive(counters[base])
}

// itoaPositive formats a positive int into decimal without pulling in
// strconv. Callers guarantee n > 0 (counters only grow).
func itoaPositive(n int) string {
	if n <= 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// sniffBody picks a (body_format, body_template) pair based on the
// provided body text + Content-Type. Every plain-text importer
// (curl, .http file, HAR) needed the same three-branch dispatch —
// this is its canonical form.
//
// Decision rules:
//
//	ct starts with application/json OR body looksLikeJSON
//	  → body_format="json", body_template=the raw JSON (validated by
//	    json.Unmarshal). Malformed JSON falls back to "raw".
//	ct starts with application/x-www-form-urlencoded OR looksLikeForm
//	  → body_format="form", body_template={k:v} from parsed query
//	    pairs.
//	otherwise
//	  → body_format="raw", body_template=the body as a JSON-escaped
//	    string (so substitute is a no-op on it).
//
// Empty body returns ("", nil) — callers already strip that case
// before reaching us, but belt + braces.
func sniffBody(body, contentType string) (string, json.RawMessage) {
	if strings.TrimSpace(body) == "" {
		return "", nil
	}
	ct := strings.ToLower(contentType)
	switch {
	case strings.HasPrefix(ct, "application/json"), looksLikeJSON(body):
		var probe any
		if err := json.Unmarshal([]byte(body), &probe); err == nil {
			return "json", json.RawMessage(body)
		}
		// JSON-shaped but invalid → fall through to raw.
		s, _ := json.Marshal(body)
		return "raw", s
	case strings.HasPrefix(ct, "application/x-www-form-urlencoded"), looksLikeForm(body):
		obj := parseFormBody(body)
		raw, _ := json.Marshal(obj)
		return "form", raw
	}
	s, _ := json.Marshal(body)
	return "raw", s
}

// parseFormBody turns a `k=v&k2=v2` string into a map. Single-value
// only — matching the rest of our importers' query-string handling.
// Uses url.ParseQuery so `%20`-style escapes decode correctly, and
// falls back to a hand-rolled split if that fails (which should be
// impossible but keeps the function total).
func parseFormBody(body string) map[string]string {
	out := map[string]string{}
	if vals, err := url.ParseQuery(body); err == nil {
		for k, vs := range vals {
			if len(vs) > 0 {
				out[k] = vs[0]
			}
		}
		return out
	}
	for _, pair := range strings.Split(body, "&") {
		if pair == "" {
			continue
		}
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			out[kv[0]] = kv[1]
		}
	}
	return out
}

// paramsFromRefs builds a template.ParamSpec map from a set of placeholder
// names. Each ref becomes a string-typed parameter; if the ref name
// appears in defaults, its Default is set (and it's not required).
// Refs with no default are marked Required — importers supply
// whatever notion of "default" they have (Postman variables,
// .http `@var` declarations) and names without one fall through.
//
// Pass defaults=nil for importers that don't carry defaults (e.g.
// curl and HAR).
func paramsFromRefs(refs map[string]bool, defaults map[string]string) map[string]template.ParamSpec {
	if len(refs) == 0 {
		return nil
	}
	out := make(map[string]template.ParamSpec, len(refs))
	for name := range refs {
		spec := template.ParamSpec{Type: "string"}
		if def, ok := defaults[name]; ok {
			// Defaults present → parameter is optional. Empty-string
			// default is still a default (the user opted into one).
			if def != "" {
				spec.Default = def
			}
		} else {
			spec.Required = true
		}
		out[name] = spec
	}
	return out
}

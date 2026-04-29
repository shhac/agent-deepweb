package template

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// placeholderRE matches {{param_name}} with optional whitespace inside.
var placeholderRE = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

// SubstituteString replaces {{name}} placeholders with the string form of
// the typed parameter value. Used for URL path/query/header values.
// If `urlEscape` is true, each value is URL-path-escaped.
func SubstituteString(s string, params map[string]any, urlEscape bool) (string, error) {
	var missErr error
	out := placeholderRE.ReplaceAllStringFunc(s, func(match string) string {
		key := placeholderRE.FindStringSubmatch(match)[1]
		v, ok := params[key]
		if !ok {
			if missErr == nil {
				missErr = fmt.Errorf("no value for placeholder %q", key)
			}
			return match
		}
		str := formatScalar(v)
		if urlEscape {
			return url.PathEscape(str)
		}
		return str
	})
	return out, missErr
}

// SubstituteBody walks a JSON body template. Leaf string values that match
// exactly "{{name}}" are replaced with the typed parameter value (preserving
// its type — int stays int, bool stays bool, arrays stay arrays). This is
// how a template author writes `{"priority": "{{priority}}"}` and the
// integer parameter ends up as a JSON number, not a string.
//
// Strings that contain a placeholder *within* other text undergo normal
// string substitution ({{x}}-like embeds).
func SubstituteBody(tmpl json.RawMessage, params map[string]any) (json.RawMessage, error) {
	if len(tmpl) == 0 {
		return nil, nil
	}
	var decoded any
	if err := json.Unmarshal(tmpl, &decoded); err != nil {
		return nil, fmt.Errorf("body_template is not valid JSON: %w", err)
	}
	replaced, err := walkBody(decoded, params)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(replaced)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func walkBody(v any, params map[string]any) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, child := range x {
			r, err := walkBody(child, params)
			if err != nil {
				return nil, err
			}
			out[k] = r
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, child := range x {
			r, err := walkBody(child, params)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	case string:
		return substituteStringValue(x, params)
	default:
		return v, nil
	}
}

// substituteStringValue is the load-bearing rule of body templating:
// a string that IS a single {{placeholder}} gets the typed value (so an
// int param becomes a JSON number, an array param a JSON array); a
// string with embedded placeholders is rendered as a string. This split
// is what lets templates produce well-typed JSON bodies.
func substituteStringValue(x string, params map[string]any) (any, error) {
	if m := placeholderRE.FindStringSubmatch(x); m != nil && m[0] == strings.TrimSpace(x) {
		key := m[1]
		p, ok := params[key]
		if !ok {
			return nil, fmt.Errorf("no value for placeholder %q", key)
		}
		return p, nil
	}
	return SubstituteString(x, params, false)
}

func formatScalar(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(v)
	}
}

// ExpandURL takes a template URL ("https://host/users/{{id}}") and a
// Query map ("{"page": "{{p}}"}"), and produces a fully-formed URL with
// path placeholders URL-escaped and query params appended.
func ExpandURL(urlTpl string, queryTpl map[string]string, params map[string]any) (string, error) {
	expanded, err := SubstituteString(urlTpl, params, true)
	if err != nil {
		return "", err
	}
	if len(queryTpl) == 0 {
		return expanded, nil
	}
	values := url.Values{}
	for k, vTpl := range queryTpl {
		v, err := SubstituteString(vTpl, params, false)
		if err != nil {
			return "", err
		}
		values.Add(k, v)
	}
	sep := "?"
	if strings.Contains(expanded, "?") {
		sep = "&"
	}
	return expanded + sep + values.Encode(), nil
}

// ExpandHeaders runs substitution across each header value.
func ExpandHeaders(headersTpl map[string]string, params map[string]any) (map[string]string, error) {
	if len(headersTpl) == 0 {
		return nil, nil
	}
	out := map[string]string{}
	for k, v := range headersTpl {
		s, err := SubstituteString(v, params, false)
		if err != nil {
			return nil, err
		}
		out[k] = s
	}
	return out, nil
}

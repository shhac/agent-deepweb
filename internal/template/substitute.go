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
		// Whole-string placeholder → type-preserving substitution.
		if m := placeholderRE.FindStringSubmatch(x); m != nil && m[0] == strings.TrimSpace(x) {
			key := m[1]
			p, ok := params[key]
			if !ok {
				return nil, fmt.Errorf("no value for placeholder %q", key)
			}
			return p, nil
		}
		// Inline embed → string substitution.
		s, err := SubstituteString(x, params, false)
		return s, err
	default:
		return v, nil
	}
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

// DeclaredPlaceholders returns all {{name}} references in a template
// (URL, query values, header values, body_template). Used by Validate to
// catch missing ParamSpecs at import time.
func (t *Template) DeclaredPlaceholders() []string {
	seen := map[string]struct{}{}
	collect := func(s string) {
		for _, m := range placeholderRE.FindAllStringSubmatch(s, -1) {
			seen[m[1]] = struct{}{}
		}
	}
	collect(t.URL)
	for _, v := range t.Query {
		collect(v)
	}
	for _, v := range t.Headers {
		collect(v)
	}
	if len(t.BodyTemplate) > 0 {
		collect(string(t.BodyTemplate))
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

// Lint reports template-definition problems: unknown body format, params
// referenced but not declared, params declared but unreferenced.
func (t *Template) Lint() []string {
	var issues []string
	if t.URL == "" {
		issues = append(issues, "url is empty")
	}
	if t.Method == "" {
		issues = append(issues, "method is empty")
	}
	switch strings.ToLower(t.BodyFormat) {
	case "", "json", "form", "raw":
	default:
		issues = append(issues, fmt.Sprintf("unknown body_format %q (use json|form|raw)", t.BodyFormat))
	}
	declared := t.DeclaredPlaceholders()
	specMap := map[string]struct{}{}
	for n := range t.Parameters {
		specMap[n] = struct{}{}
	}
	declaredMap := map[string]struct{}{}
	for _, d := range declared {
		declaredMap[d] = struct{}{}
		if _, ok := specMap[d]; !ok {
			issues = append(issues, fmt.Sprintf("placeholder {{%s}} used but no parameter declared", d))
		}
	}
	for n := range specMap {
		if _, ok := declaredMap[n]; !ok {
			issues = append(issues, fmt.Sprintf("parameter %q declared but never referenced", n))
		}
	}
	return issues
}

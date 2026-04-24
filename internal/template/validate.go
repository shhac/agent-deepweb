package template

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// CoerceParam converts the raw string value (from `--param name=value`) to
// the declared parameter type, and validates against Enum if set. Returns
// the typed value (any) or a classification-carrying error.
func CoerceParam(spec ParamSpec, raw string) (any, error) {
	var v any
	switch spec.Type {
	case "", "string":
		v = raw
	case "int":
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("expected int, got %q", raw)
		}
		v = n
	case "number":
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("expected number, got %q", raw)
		}
		v = n
	case "bool":
		switch strings.ToLower(raw) {
		case "true", "1", "yes":
			v = true
		case "false", "0", "no":
			v = false
		default:
			return nil, fmt.Errorf("expected bool (true/false), got %q", raw)
		}
	case "string-array":
		// Comma-separated; empty → empty slice.
		if raw == "" {
			v = []string{}
		} else {
			v = strings.Split(raw, ",")
		}
	case "object":
		// Accept a JSON object so imports from OpenAPI / Postman /
		// HAR that surface a requestBody as one opaque 'body' param
		// stay runnable. The body_template substitution path is
		// type-preserving, so the object lands as a JSON object not
		// a string.
		var obj map[string]any
		if err := json.Unmarshal([]byte(raw), &obj); err != nil {
			return nil, fmt.Errorf("expected JSON object, got %q: %v", raw, err)
		}
		v = obj
	case "array":
		// Matching "object" — accept a JSON array for parameters
		// declared with type:array. Distinct from string-array which
		// is a CLI ergonomics shortcut for comma-separated strings.
		var arr []any
		if err := json.Unmarshal([]byte(raw), &arr); err != nil {
			return nil, fmt.Errorf("expected JSON array, got %q: %v", raw, err)
		}
		v = arr
	default:
		return nil, fmt.Errorf("unknown parameter type %q", spec.Type)
	}

	if len(spec.Enum) > 0 {
		ok := false
		for _, e := range spec.Enum {
			if paramEqual(e, v) {
				ok = true
				break
			}
		}
		if !ok {
			return nil, fmt.Errorf("value %v is not in enum %v", v, spec.Enum)
		}
	}
	return v, nil
}

func paramEqual(a, b any) bool {
	// JSON numbers come back as float64; normalise before comparing.
	if af, aOK := toFloat(a); aOK {
		if bf, bOK := toFloat(b); bOK {
			return af == bf
		}
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}

// Validate checks that:
//   - every required parameter has a value (after defaults),
//   - every provided parameter name is declared,
//   - types coerce cleanly,
//
// and returns a map of typed values keyed by parameter name.
func (t *Template) Validate(rawParams map[string]string) (map[string]any, error) {
	out := map[string]any{}
	// Required & declared
	for name, spec := range t.Parameters {
		rawValue, provided := rawParams[name]
		if !provided {
			if spec.Default != nil {
				out[name] = spec.Default
				continue
			}
			if spec.Required {
				return nil, fmt.Errorf("missing required parameter %q", name)
			}
			continue
		}
		v, err := CoerceParam(spec, rawValue)
		if err != nil {
			return nil, fmt.Errorf("parameter %q: %w", name, err)
		}
		out[name] = v
	}
	// Unknown params
	for name := range rawParams {
		if _, ok := t.Parameters[name]; !ok {
			return nil, fmt.Errorf("unknown parameter %q", name)
		}
	}
	return out, nil
}

// DeclaredPlaceholders returns all {{name}} references in a template
// (URL, query values, header values, body_template). Used by Lint to
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

// Lint reports template-definition problems: missing url/method, unknown
// body format, params referenced but not declared, params declared but
// unreferenced.
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

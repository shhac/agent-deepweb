package template

import (
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

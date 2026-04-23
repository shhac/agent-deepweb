package template

import (
	"strings"
	"testing"
)

func TestValidate_RequiredAndDefaults(t *testing.T) {
	tpl := &Template{
		Parameters: map[string]ParamSpec{
			"name":  {Type: "string", Required: true},
			"limit": {Type: "int", Default: float64(10)}, // float64 because that's how JSON would decode it
		},
	}
	// Missing required
	if _, err := tpl.Validate(map[string]string{}); err == nil {
		t.Error("expected error for missing required param")
	}
	// Only required supplied → default applied
	got, err := tpl.Validate(map[string]string{"name": "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if got["name"] != "alice" {
		t.Errorf("name: %v", got["name"])
	}
	if got["limit"] != float64(10) {
		t.Errorf("limit default not applied: %v", got["limit"])
	}
}

func TestValidate_UnknownParamRejected(t *testing.T) {
	tpl := &Template{Parameters: map[string]ParamSpec{"a": {Type: "string"}}}
	_, err := tpl.Validate(map[string]string{"a": "ok", "b": "evil"})
	if err == nil || !strings.Contains(err.Error(), "unknown parameter") {
		t.Errorf("expected unknown-parameter error, got %v", err)
	}
}

func TestCoerceParam_Types(t *testing.T) {
	cases := []struct {
		spec ParamSpec
		raw  string
		want any
		err  bool
	}{
		{ParamSpec{Type: "string"}, "hi", "hi", false},
		{ParamSpec{Type: "int"}, "42", int64(42), false},
		{ParamSpec{Type: "int"}, "not-int", nil, true},
		{ParamSpec{Type: "number"}, "3.14", float64(3.14), false},
		{ParamSpec{Type: "bool"}, "true", true, false},
		{ParamSpec{Type: "bool"}, "false", false, false},
		{ParamSpec{Type: "bool"}, "maybe", nil, true},
		{ParamSpec{Type: "string-array"}, "a,b,c", []string{"a", "b", "c"}, false},
		{ParamSpec{Type: "string-array"}, "", []string{}, false},
	}
	for _, tc := range cases {
		got, err := CoerceParam(tc.spec, tc.raw)
		if tc.err {
			if err == nil {
				t.Errorf("spec=%v raw=%q expected error, got %v", tc.spec, tc.raw, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("spec=%v raw=%q unexpected err: %v", tc.spec, tc.raw, err)
			continue
		}
		// Slice compare
		if a, ok := tc.want.([]string); ok {
			b, _ := got.([]string)
			if len(a) != len(b) {
				t.Errorf("slice len: %v vs %v", a, b)
				continue
			}
			for i := range a {
				if a[i] != b[i] {
					t.Errorf("slice[%d]: %v vs %v", i, a[i], b[i])
				}
			}
			continue
		}
		if got != tc.want {
			t.Errorf("spec=%v raw=%q got %v(%T), want %v(%T)", tc.spec, tc.raw, got, got, tc.want, tc.want)
		}
	}
}

func TestCoerceParam_Enum(t *testing.T) {
	spec := ParamSpec{Type: "string", Enum: []any{"open", "closed"}}
	if _, err := CoerceParam(spec, "open"); err != nil {
		t.Errorf("valid enum rejected: %v", err)
	}
	if _, err := CoerceParam(spec, "flagged"); err == nil {
		t.Error("invalid enum accepted")
	}
}

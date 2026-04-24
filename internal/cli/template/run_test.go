package templatecli

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/shhac/agent-deepweb/internal/template"
)

// readAll is a tiny convenience for asserting on io.Reader contents.
func readAll(t *testing.T, r io.Reader) string {
	t.Helper()
	if r == nil {
		return ""
	}
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestBuildTemplateBody_JSON — JSON format substitutes {{param}}
// placeholders with type-preserving values (int stays as number, not
// "42"), and stamps Content-Type: application/json.
func TestBuildTemplateBody_JSON(t *testing.T) {
	tpl := &template.Template{
		Name:         "t",
		BodyFormat:   "json",
		BodyTemplate: json.RawMessage(`{"id": "{{id}}", "count": "{{count}}"}`),
	}
	typed := map[string]any{"id": "x", "count": 42}
	r, ct, err := buildTemplateBody(tpl, typed)
	if err != nil {
		t.Fatal(err)
	}
	if ct != "application/json" {
		t.Errorf("content-type: %q", ct)
	}
	body := readAll(t, r)
	// Type preservation: int must appear as number, not "42".
	if !strings.Contains(body, `"count":42`) && !strings.Contains(body, `"count": 42`) {
		t.Errorf("body should preserve int type; got %q", body)
	}
	if !strings.Contains(body, `"id":"x"`) && !strings.Contains(body, `"id": "x"`) {
		t.Errorf("body should substitute string; got %q", body)
	}
}

// TestBuildTemplateBody_Form — form format renders as
// application/x-www-form-urlencoded with each field substituted.
func TestBuildTemplateBody_Form(t *testing.T) {
	tpl := &template.Template{
		Name:         "t",
		BodyFormat:   "form",
		BodyTemplate: json.RawMessage(`{"grant_type":"password","scope":"{{scope}}"}`),
	}
	typed := map[string]any{"scope": "read write"}
	r, ct, err := buildTemplateBody(tpl, typed)
	if err != nil {
		t.Fatal(err)
	}
	if ct != "application/x-www-form-urlencoded" {
		t.Errorf("content-type: %q", ct)
	}
	body := readAll(t, r)
	// Form encoding: spaces become + (or %20). Either form is acceptable.
	if !strings.Contains(body, "grant_type=password") {
		t.Errorf("missing grant_type: %q", body)
	}
	if !strings.Contains(body, "scope=read") {
		t.Errorf("missing scope substitution: %q", body)
	}
}

// TestBuildTemplateBody_Raw — raw format emits the substituted string
// as-is with no Content-Type (caller's responsibility — the template
// author may set one explicitly in Headers).
func TestBuildTemplateBody_Raw(t *testing.T) {
	tpl := &template.Template{
		Name:         "t",
		BodyFormat:   "raw",
		BodyTemplate: json.RawMessage(`"hello {{name}}"`),
	}
	typed := map[string]any{"name": "world"}
	r, ct, err := buildTemplateBody(tpl, typed)
	if err != nil {
		t.Fatal(err)
	}
	if ct != "" {
		t.Errorf("raw should not stamp content-type, got %q", ct)
	}
	if got := readAll(t, r); got != "hello world" {
		t.Errorf("body: %q", got)
	}
}

// TestBuildTemplateBody_NoneMeansNilBody — an empty body_format (or
// empty body_template) produces (nil, "", nil). runTemplate relies on
// body==nil to default the method to GET.
func TestBuildTemplateBody_NoneMeansNilBody(t *testing.T) {
	cases := []struct {
		name string
		tpl  *template.Template
	}{
		{"no format", &template.Template{Name: "t", BodyTemplate: json.RawMessage(`{"a":1}`)}},
		{"no template", &template.Template{Name: "t", BodyFormat: "json"}},
		{"neither", &template.Template{Name: "t"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, ct, err := buildTemplateBody(tc.tpl, nil)
			if err != nil {
				t.Fatal(err)
			}
			if r != nil || ct != "" {
				t.Errorf("want (nil, \"\"), got (%v, %q)", r, ct)
			}
		})
	}
}

// TestBuildTemplateBody_UnknownFormat — human-fixable: the template
// author misspelled body_format. Fix requires re-importing, so human
// not agent.
func TestBuildTemplateBody_UnknownFormat(t *testing.T) {
	tpl := &template.Template{
		Name:         "bad",
		BodyFormat:   "xml",
		BodyTemplate: json.RawMessage(`{}`),
	}
	_, _, err := buildTemplateBody(tpl, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `"xml"`) {
		t.Errorf("error should quote the bad format: %q", err.Error())
	}
}

// TestChooseMethod — --method takes precedence, else POST when there's
// a body, else GET.
func TestChooseMethod(t *testing.T) {
	cases := []struct {
		flag    string
		hasBody bool
		want    string
	}{
		{"", false, "GET"},
		{"", true, "POST"},
		{"patch", false, "PATCH"},
		{"PUT", true, "PUT"},
	}
	for _, tc := range cases {
		if got := chooseMethod(tc.flag, tc.hasBody); got != tc.want {
			t.Errorf("chooseMethod(%q, %v) = %q; want %q", tc.flag, tc.hasBody, got, tc.want)
		}
	}
}

// TestPrepareRequest_StampsContentTypeFromBody — prepareRequest adds
// Content-Type when the body has one but the template didn't set one
// explicitly. Confirms the "template author can override" path AND
// the default path.
func TestPrepareRequest_StampsContentTypeFromBody(t *testing.T) {
	t.Run("default from body_format", func(t *testing.T) {
		tpl := &template.Template{
			Name:         "t",
			URL:          "https://api.example.com/items",
			BodyFormat:   "json",
			BodyTemplate: json.RawMessage(`{"x":1}`),
		}
		_, headers, body, err := prepareRequest(tpl, nil)
		if err != nil {
			t.Fatal(err)
		}
		if body == nil {
			t.Fatal("body should be non-nil for json format")
		}
		if headers["Content-Type"] != "application/json" {
			t.Errorf("Content-Type: %q", headers["Content-Type"])
		}
	})
	t.Run("explicit header wins", func(t *testing.T) {
		tpl := &template.Template{
			Name:         "t",
			URL:          "https://api.example.com/items",
			Headers:      map[string]string{"Content-Type": "application/vnd.api+json"},
			BodyFormat:   "json",
			BodyTemplate: json.RawMessage(`{"x":1}`),
		}
		_, headers, _, err := prepareRequest(tpl, nil)
		if err != nil {
			t.Fatal(err)
		}
		if headers["Content-Type"] != "application/vnd.api+json" {
			t.Errorf("template header should not be overwritten by body default; got %q", headers["Content-Type"])
		}
	})
}

package template

import (
	"strings"
	"testing"

	"github.com/shhac/agent-deepweb/internal/config"
)

const minimalSpec = `{
  "openapi": "3.0.3",
  "servers": [{"url": "https://api.example.com/v1"}],
  "paths": {
    "/users/{id}": {
      "get": {
        "operationId": "getUser",
        "summary": "Fetch one user",
        "tags": ["users"],
        "parameters": [
          {"name":"id","in":"path","required":true,"schema":{"type":"integer"}},
          {"name":"include","in":"query","schema":{"type":"string","enum":["posts","photos"]}},
          {"name":"X-Trace-ID","in":"header","schema":{"type":"string"}}
        ]
      }
    },
    "/users": {
      "post": {
        "operationId": "createUser",
        "tags": ["users","admin"],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {"type":"object"}
            }
          }
        }
      }
    },
    "/health": {
      "get": {
        "operationId": "healthz",
        "tags": ["ops"]
      }
    }
  }
}`

// TestImportOpenAPI_CoreShapes exercises the happy path: every
// operation becomes a template, path placeholders convert, query +
// header parameters land in the right slot, and profile/prefix are
// honoured.
func TestImportOpenAPI_CoreShapes(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	imported, err := ImportOpenAPI([]byte(minimalSpec), ImportOpenAPIOptions{
		Prefix:  "myapi",
		Profile: "myapi-prof",
	})
	if err != nil {
		t.Fatalf("ImportOpenAPI: %v", err)
	}
	if len(imported) != 3 {
		t.Errorf("want 3 imports (healthz + getUser + createUser), got %d: %v", len(imported), imported)
	}

	// Placeholder rewrite: /users/{id} → /users/{{id}}
	got, err := Get("myapi.getuser")
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://api.example.com/v1/users/{{id}}" {
		t.Errorf("URL: %q", got.URL)
	}
	if got.Method != "GET" {
		t.Errorf("method: %q", got.Method)
	}
	if got.Profile != "myapi-prof" {
		t.Errorf("profile: %q", got.Profile)
	}

	// Path parameter typed as int (integer → int).
	p, ok := got.Parameters["id"]
	if !ok || p.Type != "int" || !p.Required {
		t.Errorf("id param: %+v", p)
	}

	// Query parameter goes into Query as a {{placeholder}} AND into Parameters.
	if got.Query["include"] != "{{include}}" {
		t.Errorf("query include placeholder: %q", got.Query["include"])
	}
	incl, ok := got.Parameters["include"]
	if !ok || len(incl.Enum) != 2 {
		t.Errorf("include param: %+v", incl)
	}

	// Header parameter goes into Headers + Parameters.
	if got.Headers["X-Trace-ID"] != "{{X-Trace-ID}}" {
		t.Errorf("X-Trace-ID header placeholder: %q", got.Headers["X-Trace-ID"])
	}
}

// TestImportOpenAPI_RequestBodyBecomesObjectParam — application/json
// body translates to body_format=json + body_template pass-through +
// a single "body" object param. The template can then accept the
// full body via --param body='{...}'.
func TestImportOpenAPI_RequestBodyBecomesObjectParam(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	if _, err := ImportOpenAPI([]byte(minimalSpec), ImportOpenAPIOptions{Prefix: "x"}); err != nil {
		t.Fatal(err)
	}
	created, err := Get("x.createuser")
	if err != nil {
		t.Fatal(err)
	}
	if created.BodyFormat != "json" {
		t.Errorf("body_format: %q", created.BodyFormat)
	}
	body, ok := created.Parameters["body"]
	if !ok {
		t.Fatal("body param missing")
	}
	if body.Type != "object" || !body.Required {
		t.Errorf("body param: %+v", body)
	}
}

// TestImportOpenAPI_TagFilter_NarrowsImport — with --tag limiting to
// "ops", only the healthz endpoint should survive.
func TestImportOpenAPI_TagFilter_NarrowsImport(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	imported, err := ImportOpenAPI([]byte(minimalSpec), ImportOpenAPIOptions{
		Prefix:    "api",
		TagFilter: []string{"ops"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) != 1 || imported[0] != "api.healthz" {
		t.Errorf("want [api.healthz], got %v", imported)
	}
}

// TestImportOpenAPI_ServerOverride_ReplacesSpecURL — --server wins
// over the first servers[] entry, which is how a staging import
// targets a different host.
func TestImportOpenAPI_ServerOverride_ReplacesSpecURL(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	if _, err := ImportOpenAPI([]byte(minimalSpec), ImportOpenAPIOptions{
		Prefix:         "staging",
		ServerOverride: "https://staging.example.com/",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := Get("staging.healthz")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.URL, "https://staging.example.com/") {
		t.Errorf("server override not applied: %q", got.URL)
	}
	// Trailing slashes on the override are trimmed so we don't get // in the middle.
	if strings.Contains(got.URL, "//health") {
		t.Errorf("double slash between server and path: %q", got.URL)
	}
}

// TestImportOpenAPI_RejectsYAML — YAML specs are refused with a hint
// pointing at the yq one-liner, rather than crashing the JSON parser
// on the first `-` or unquoted key.
func TestImportOpenAPI_RejectsYAML(t *testing.T) {
	yamlSpec := "openapi: 3.0.3\npaths: {}\n"
	_, err := ImportOpenAPI([]byte(yamlSpec), ImportOpenAPIOptions{Prefix: "x"})
	if err == nil {
		t.Fatal("expected YAML refusal")
	}
	if !strings.Contains(err.Error(), "YAML") {
		t.Errorf("error should mention YAML: %q", err.Error())
	}
}

// TestImportOpenAPI_AcceptsSwaggerV2 — v2 specs route through the
// v2-to-v3 transformer. Minimal smoke test that path+query+body params
// all land in the right slots.
func TestImportOpenAPI_AcceptsSwaggerV2(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	v2 := `{
      "swagger": "2.0",
      "host": "api.example.com",
      "basePath": "/v1",
      "schemes": ["https"],
      "paths": {
        "/pets/{id}": {
          "get": {
            "operationId": "getPet",
            "parameters": [
              {"name":"id","in":"path","required":true,"type":"integer"},
              {"name":"verbose","in":"query","type":"boolean"}
            ]
          }
        },
        "/pets": {
          "post": {
            "operationId": "createPet",
            "consumes": ["application/json"],
            "parameters": [
              {"name":"body","in":"body","required":true,"schema":{"type":"object"}}
            ]
          }
        }
      }
    }`
	imported, err := ImportOpenAPI([]byte(v2), ImportOpenAPIOptions{Prefix: "v2api"})
	if err != nil {
		t.Fatalf("ImportOpenAPI(v2): %v", err)
	}
	if len(imported) != 2 {
		t.Errorf("want 2 imports, got %v", imported)
	}
	got, err := Get("v2api.getpet")
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://api.example.com/v1/pets/{{id}}" {
		t.Errorf("URL not composed from host+basePath+path: %q", got.URL)
	}
	if got.Parameters["id"].Type != "int" || !got.Parameters["id"].Required {
		t.Errorf("id param: %+v", got.Parameters["id"])
	}
	if got.Parameters["verbose"].Type != "bool" {
		t.Errorf("verbose: %+v", got.Parameters["verbose"])
	}
	// createPet routes through requestBody → body object param.
	create, err := Get("v2api.createpet")
	if err != nil {
		t.Fatal(err)
	}
	if create.BodyFormat != "json" {
		t.Errorf("body_format: %q", create.BodyFormat)
	}
	body := create.Parameters["body"]
	if body.Type != "object" || !body.Required {
		t.Errorf("body param: %+v", body)
	}
}

// TestImportOpenAPI_RejectsUnknownVersion — neither openapi 3.x nor
// swagger 2.x → clear refusal.
func TestImportOpenAPI_RejectsUnknownVersion(t *testing.T) {
	doc := `{"swagger":"1.2","paths":{}}`
	_, err := ImportOpenAPI([]byte(doc), ImportOpenAPIOptions{Prefix: "x"})
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("want unsupported-version error, got %v", err)
	}
}

// TestImportOpenAPI_RequiresPrefix — without a prefix every imported
// template would collide on the next import of a different spec.
func TestImportOpenAPI_RequiresPrefix(t *testing.T) {
	_, err := ImportOpenAPI([]byte(minimalSpec), ImportOpenAPIOptions{})
	if err == nil || !strings.Contains(err.Error(), "--prefix") {
		t.Errorf("want prefix-required error, got %v", err)
	}
}

// TestImportOpenAPI_FallsBackOperationIdToSluggedMethodPath — specs
// that omit operationId still produce usable names. Important for
// specs scraped/generated without authorship discipline.
func TestImportOpenAPI_FallsBackOperationIdToSluggedMethodPath(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	spec := `{
      "openapi":"3.0.0",
      "servers":[{"url":"https://x"}],
      "paths":{"/items/{id}":{"get":{"tags":["items"]}}}
    }`
	imported, err := ImportOpenAPI([]byte(spec), ImportOpenAPIOptions{Prefix: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) != 1 {
		t.Fatalf("want 1 imported, got %v", imported)
	}
	// The slug form should be something like "get_items_id" — no spaces,
	// no braces, lowercase.
	name := imported[0]
	if !strings.HasPrefix(name, "p.") {
		t.Errorf("name should carry prefix: %q", name)
	}
	if strings.ContainsAny(name, "{} ") {
		t.Errorf("slug should not contain braces or spaces: %q", name)
	}
}

// TestPathToPlaceholders covers the OpenAPI-path → template-url
// conversion in isolation.
func TestPathToPlaceholders(t *testing.T) {
	cases := map[string]string{
		"/users/{id}":               "/users/{{id}}",
		"/users/{id}/posts/{postId}": "/users/{{id}}/posts/{{postId}}",
		"/health":                    "/health",
		"/items/{id:int}":            "/items/{{id}}", // type hints stripped
	}
	for in, want := range cases {
		if got := pathToPlaceholders(in); got != want {
			t.Errorf("pathToPlaceholders(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSanitiseIdentifier covers the name-slug rules in isolation.
func TestSanitiseIdentifier(t *testing.T) {
	cases := map[string]string{
		"getUser":             "getuser",
		"GET /users/{id}":     "get_users_id",
		"create.user":         "create.user",
		"__weird__name__":     "weird_name",
		"items-v2":            "items-v2",
	}
	for in, want := range cases {
		if got := sanitiseIdentifier(in); got != want {
			t.Errorf("sanitiseIdentifier(%q) = %q, want %q", in, got, want)
		}
	}
}

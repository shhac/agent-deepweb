package importers

import (
	"github.com/shhac/agent-deepweb/internal/template"

	"strings"
	"testing"

	"github.com/shhac/agent-deepweb/internal/config"
)

const postmanMinimal = `{
  "info": { "name": "my-collection", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json" },
  "variable": [
    { "key": "baseUrl", "value": "https://api.example.com" },
    { "key": "apiVersion", "value": "v1" }
  ],
  "item": [
    {
      "name": "Users",
      "item": [
        {
          "name": "Get user",
          "request": {
            "method": "GET",
            "url": "{{baseUrl}}/{{apiVersion}}/users/{{userId}}?expand={{expand}}",
            "header": [{"key":"Accept","value":"application/json"}]
          }
        },
        {
          "name": "Create user",
          "request": {
            "method": "POST",
            "url": {
              "raw": "{{baseUrl}}/{{apiVersion}}/users",
              "protocol": "https",
              "host": ["api","example","com"],
              "path": ["{{apiVersion}}","users"]
            },
            "header": [{"key":"Content-Type","value":"application/json"}],
            "body": {
              "mode": "raw",
              "raw": "{\"name\":\"{{name}}\"}",
              "options": { "raw": { "language": "json" } }
            }
          }
        }
      ]
    },
    {
      "name": "Health",
      "request": {
        "method": "GET",
        "url": { "raw": "{{baseUrl}}/healthz" }
      }
    }
  ]
}`

// TestImportPostman_CoreShapes — happy path: nested folder walked,
// both url-string and url-object shapes normalize, query lifted from
// ?k=v into Query map, headers preserved, JSON raw body detected.
func TestImportPostman_CoreShapes(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	imported, err := ImportPostman([]byte(postmanMinimal), ImportPostmanOptions{
		Prefix:  "mc",
		Profile: "mc-prof",
	})
	if err != nil {
		t.Fatalf("ImportPostman: %v", err)
	}
	if len(imported) != 3 {
		t.Errorf("want 3 imports, got %d: %v", len(imported), imported)
	}

	// Nested folder flattens into the name.
	getUser, err := template.Get("mc.users_get_user")
	if err != nil {
		t.Fatalf("mc.users_get_user: %v", err)
	}
	if getUser.Method != "GET" {
		t.Errorf("method: %q", getUser.Method)
	}
	if getUser.Profile != "mc-prof" {
		t.Errorf("profile: %q", getUser.Profile)
	}
	// URL string form: query lifted, base trimmed.
	if !strings.HasPrefix(getUser.URL, "{{baseUrl}}") || strings.Contains(getUser.URL, "?") {
		t.Errorf("url shape: %q", getUser.URL)
	}
	if getUser.Query["expand"] != "{{expand}}" {
		t.Errorf("query lift: %+v", getUser.Query)
	}
	if getUser.Headers["Accept"] != "application/json" {
		t.Errorf("headers: %+v", getUser.Headers)
	}

	// template.ParamSpec emitted for every {{placeholder}} seen across URL/headers/body.
	wantParams := []string{"baseUrl", "apiVersion", "userId", "expand"}
	for _, p := range wantParams {
		if _, ok := getUser.Parameters[p]; !ok {
			t.Errorf("missing param %q: %+v", p, getUser.Parameters)
		}
	}
	// Defaults inherited from collection-level variables.
	if getUser.Parameters["baseUrl"].Default != "https://api.example.com" {
		t.Errorf("baseUrl default not inherited: %+v", getUser.Parameters["baseUrl"])
	}
	// userId has no default → required.
	if !getUser.Parameters["userId"].Required {
		t.Errorf("userId without default should be Required")
	}
}

// TestImportPostman_UrlObjectShape — the url-object form (raw +
// protocol + host + path) is accepted and round-trips back to raw.
func TestImportPostman_UrlObjectShape(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	if _, err := ImportPostman([]byte(postmanMinimal), ImportPostmanOptions{Prefix: "x"}); err != nil {
		t.Fatal(err)
	}
	create, err := template.Get("x.users_create_user")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(create.URL, "{{baseUrl}}/{{apiVersion}}/users") {
		t.Errorf("url-object raw not preserved: %q", create.URL)
	}
	if create.BodyFormat != "json" {
		t.Errorf("JSON raw body not detected: %q", create.BodyFormat)
	}
}

// TestImportPostman_FolderFilter — --folder narrows to matching subtree.
func TestImportPostman_FolderFilter(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	imported, err := ImportPostman([]byte(postmanMinimal), ImportPostmanOptions{
		Prefix:     "x",
		FolderPath: "users",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Only 2 items (Get user, Create user) — Health is at the top level and
	// doesn't have a folder ancestor at all.
	if len(imported) != 2 {
		t.Errorf("want 2 filtered imports, got %d: %v", len(imported), imported)
	}
	for _, n := range imported {
		if !strings.Contains(n, "users_") {
			t.Errorf("filter leaked non-matching item: %q", n)
		}
	}
}

// TestImportPostman_BodyModes — the four supported body modes all
// produce sensible body_format / body_template pairs.
func TestImportPostman_BodyModes(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	spec := `{
      "info": { "name": "t", "schema": "...v2.1.0..." },
      "item": [
        {
          "name": "raw-text",
          "request": {
            "method": "POST",
            "url": "https://x/raw",
            "body": { "mode": "raw", "raw": "hello world", "options": {"raw":{"language":"text"}} }
          }
        },
        {
          "name": "urlencoded",
          "request": {
            "method": "POST",
            "url": "https://x/form",
            "body": { "mode": "urlencoded", "urlencoded": [
              {"key":"a","value":"1"},
              {"key":"b","value":"2","disabled":true}
            ]}
          }
        },
        {
          "name": "graphql",
          "request": {
            "method": "POST",
            "url": "https://x/gql",
            "body": { "mode": "graphql", "graphql": { "query": "query { me { id } }" } }
          }
        }
      ]
    }`

	if _, err := ImportPostman([]byte(spec), ImportPostmanOptions{Prefix: "b"}); err != nil {
		t.Fatal(err)
	}

	t.Run("raw text → body_format=raw", func(t *testing.T) {
		got, _ := template.Get("b.raw-text")
		if got.BodyFormat != "raw" {
			t.Errorf("body_format: %q (raw text should use raw format)", got.BodyFormat)
		}
	})
	t.Run("urlencoded → body_format=form, disabled kv dropped", func(t *testing.T) {
		got, _ := template.Get("b.urlencoded")
		if got.BodyFormat != "form" {
			t.Errorf("body_format: %q", got.BodyFormat)
		}
		if strings.Contains(string(got.BodyTemplate), `"b"`) {
			t.Errorf("disabled kv leaked through: %s", got.BodyTemplate)
		}
	})
	t.Run("graphql → body_format=json with query wrapper", func(t *testing.T) {
		got, _ := template.Get("b.graphql")
		if got.BodyFormat != "json" {
			t.Errorf("body_format: %q", got.BodyFormat)
		}
		// Store round-trips through MarshalIndent so the body becomes
		// pretty-printed; match on the structural pieces rather than exact bytes.
		body := string(got.BodyTemplate)
		if !strings.Contains(body, `"query"`) || !strings.Contains(body, `query { me { id } }`) {
			t.Errorf("graphql body not wrapped: %s", body)
		}
	})
}

// TestImportPostman_RejectsNonV2 — a collection without a v2.x schema
// tag is refused loudly.
func TestImportPostman_RejectsNonV2(t *testing.T) {
	doc := `{"info":{"schema":"https://schema.getpostman.com/json/collection/v1/collection.json","name":"old"},"item":[]}`
	_, err := ImportPostman([]byte(doc), ImportPostmanOptions{Prefix: "x"})
	if err == nil || !strings.Contains(err.Error(), "v2") {
		t.Errorf("want v2 schema refusal, got %v", err)
	}
}

// TestImportPostman_RequiresPrefix — same contract as OpenAPI.
func TestImportPostman_RequiresPrefix(t *testing.T) {
	_, err := ImportPostman([]byte(postmanMinimal), ImportPostmanOptions{})
	if err == nil || !strings.Contains(err.Error(), "--prefix") {
		t.Errorf("want prefix-required error, got %v", err)
	}
}

// TestCollectPlaceholders — the placeholder scanner in isolation.
func TestCollectPlaceholders(t *testing.T) {
	acc := map[string]bool{}
	collectPlaceholdersInto(`{{a}}/{{b}}?q={{a}}`, acc)
	if !acc["a"] || !acc["b"] || len(acc) != 2 {
		t.Errorf("expected {a,b}: %v", acc)
	}
}

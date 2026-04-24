package importers

import (
	"github.com/shhac/agent-deepweb/internal/template"

	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestImport_AllFormats_E2E drives each importer against mockdeep to
// prove the full pipeline (parse → store → expand → api.Do) works
// end-to-end, not just at the parser boundary. Every subtest:
//
//  1. constructs a fixture whose URL references the current mockdeep
//     test server (so the test can actually execute the template),
//  2. imports it through the format-specific Import*,
//  3. fetches the stored template and runs it through runTemplate
//     (the shared harness from openapi_e2e_test.go),
//  4. asserts mockdeep's reply.
//
// The shared server + anonProfile + runTemplate helpers live in
// openapi_e2e_test.go to keep this file focused on fixture shapes.
func TestImport_AllFormats_E2E(t *testing.T) {
	base := setupMockdeep(t)
	auth := anonProfile(t, base)

	t.Run("postman", func(t *testing.T) {
		collection := fmt.Sprintf(`{
          "info": {"name": "e2e", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
          "item": [
            {
              "name": "ping",
              "request": {
                "method": "GET",
                "url": "%s/healthz"
              }
            }
          ]
        }`, base)
		imported, err := ImportPostman([]byte(collection), ImportPostmanOptions{Prefix: "pm"})
		if err != nil {
			t.Fatalf("ImportPostman: %v", err)
		}
		if len(imported) != 1 {
			t.Fatalf("want 1 import, got %v", imported)
		}
		tpl, _ := template.Get(imported[0])
		resp := runTemplate(t, tpl, nil, auth)
		if resp.Status != 200 {
			t.Errorf("postman template run status: %d body=%s", resp.Status, resp.Body)
		}
	})

	t.Run("har", func(t *testing.T) {
		har := fmt.Sprintf(`{
          "log": {
            "entries": [
              {
                "request": {
                  "method": "GET",
                  "url": "%s/healthz",
                  "headers": [
                    {"name":"Authorization","value":"Bearer should-be-stripped"}
                  ]
                }
              }
            ]
          }
        }`, base)
		imported, err := ImportHAR([]byte(har), ImportHAROptions{Prefix: "har"})
		if err != nil {
			t.Fatalf("ImportHAR: %v", err)
		}
		tpl, _ := template.Get(imported[0])
		// The stale Authorization should NOT be carried forward.
		if _, leaked := tpl.Headers["Authorization"]; leaked {
			t.Errorf("HAR import leaked auth header: %+v", tpl.Headers)
		}
		resp := runTemplate(t, tpl, nil, auth)
		if resp.Status != 200 {
			t.Errorf("har template run status: %d", resp.Status)
		}
	})

	t.Run(".http file", func(t *testing.T) {
		httpDoc := fmt.Sprintf(`### ping
GET %s/healthz
Accept: application/json
`, base)
		imported, err := ImportHTTPText(httpDoc, ImportHTTPFileOptions{Prefix: "hh"})
		if err != nil {
			t.Fatalf("ImportHTTPText: %v", err)
		}
		tpl, _ := template.Get(imported[0])
		resp := runTemplate(t, tpl, nil, auth)
		if resp.Status != 200 {
			t.Errorf(".http template run status: %d", resp.Status)
		}
	})

	t.Run("curl", func(t *testing.T) {
		cmd := fmt.Sprintf(`curl -L -H "Accept: application/json" %s/healthz`, base)
		_, err := ImportCurl(cmd, ImportCurlOptions{Name: "cc.ping"})
		if err != nil {
			t.Fatalf("ImportCurl: %v", err)
		}
		tpl, _ := template.Get("cc.ping")
		resp := runTemplate(t, tpl, nil, auth)
		if resp.Status != 200 {
			t.Errorf("curl template run status: %d", resp.Status)
		}
	})

	// POST paths with bodies — exercises the body_format/body_template
	// round trip for each format, not just the simple GET case.
	t.Run("postman POST JSON body", func(t *testing.T) {
		collection := fmt.Sprintf(`{
          "info": {"name":"e2e","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
          "item": [
            {
              "name": "echo-it",
              "request": {
                "method": "POST",
                "url": "%s/echo",
                "header": [{"key":"Content-Type","value":"application/json"}],
                "body": {
                  "mode": "raw",
                  "raw": "{\"payload\":\"{{value}}\"}",
                  "options": {"raw":{"language":"json"}}
                }
              }
            }
          ]
        }`, base)
		imported, err := ImportPostman([]byte(collection), ImportPostmanOptions{Prefix: "pm2"})
		if err != nil {
			t.Fatalf("ImportPostman: %v", err)
		}
		tpl, _ := template.Get(imported[0])
		resp := runTemplate(t, tpl, map[string]any{"value": "hi"}, auth)
		if resp.Status != 200 {
			t.Fatalf("postman POST status: %d", resp.Status)
		}
		if !strings.Contains(string(resp.Body), "hi") {
			t.Errorf("postman substituted value missing from echo: %s", resp.Body)
		}
	})

	t.Run("curl POST JSON body", func(t *testing.T) {
		// --json implies POST + Content-Type: application/json
		cmd := fmt.Sprintf(`curl %s/echo --json '{"payload":"fromcurl"}'`, base)
		if _, err := ImportCurl(cmd, ImportCurlOptions{Name: "cc.echo"}); err != nil {
			t.Fatalf("ImportCurl: %v", err)
		}
		tpl, _ := template.Get("cc.echo")
		resp := runTemplate(t, tpl, nil, auth)
		if resp.Status != 200 {
			t.Fatalf("curl POST status: %d", resp.Status)
		}
		if !strings.Contains(string(resp.Body), "fromcurl") {
			t.Errorf("curl body missing from echo: %s", resp.Body)
		}
	})
}

// TestImport_FromFile_E2E writes a fixture to disk (rather than passing
// bytes directly) to exercise the ImportXFile wrappers — belt + braces
// over the os.ReadFile path. Single subtest; one file variant is enough
// to cover the wrapper, which is otherwise trivial.
func TestImport_FromFile_E2E(t *testing.T) {
	base := setupMockdeep(t)
	auth := anonProfile(t, base)

	dir := t.TempDir()
	path := filepath.Join(dir, "spec.http")
	content := fmt.Sprintf(`### health
GET %s/healthz
`, base)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	imported, err := ImportHTTPFile(path, ImportHTTPFileOptions{Prefix: "f"})
	if err != nil {
		t.Fatal(err)
	}
	tpl, _ := template.Get(imported[0])
	resp := runTemplate(t, tpl, nil, auth)
	if resp.Status != 200 {
		t.Errorf("file import status: %d", resp.Status)
	}
}

package importers

import (
	"github.com/shhac/agent-deepweb/internal/template"

	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shhac/agent-deepweb/internal/api"
	"github.com/shhac/agent-deepweb/internal/config"
	"github.com/shhac/agent-deepweb/internal/credential"
	"github.com/shhac/agent-deepweb/internal/mockdeep"
)

// fetchSpec pulls /openapi.json off the mockdeep server under test.
// Used to exercise the full path: server serves spec → we import it →
// we execute the imported template against the same server.
func fetchSpec(t *testing.T, base string) []byte {
	t.Helper()
	resp, err := http.Get(base + "/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("GET /openapi.json → %d: %s", resp.StatusCode, b)
	}
	return b
}

// runTemplate is the minimal "execute a stored template.Template via api.Do"
// path — equivalent to what `template run` does, minus the cobra +
// stdout layer. It expands URL/query/headers/body via the exported
// template helpers, then hands off to api.Do. Kept in the e2e test
// file (rather than shipped to the core package) because production
// callers go through the CLI's prepareRequest — this is just a
// test harness.
func runTemplate(t *testing.T, tpl *template.Template, params map[string]any, auth *credential.Resolved) *api.Response {
	t.Helper()
	typed, err := tpl.Validate(stringifyParams(params))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	expandedURL, err := template.ExpandURL(tpl.URL, tpl.Query, typed)
	if err != nil {
		t.Fatalf("expand url: %v", err)
	}
	headers, err := template.ExpandHeaders(tpl.Headers, typed)
	if err != nil {
		t.Fatalf("expand headers: %v", err)
	}
	if headers == nil {
		headers = map[string]string{}
	}
	var body io.Reader
	if len(tpl.BodyTemplate) > 0 && tpl.BodyFormat == "json" {
		b, err := template.SubstituteBody(tpl.BodyTemplate, typed)
		if err != nil {
			t.Fatalf("substitute body: %v", err)
		}
		body = bytes.NewReader(b)
		headers["Content-Type"] = "application/json"
	}
	method := tpl.Method
	if method == "" {
		method = "GET"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := api.Do(ctx, api.Request{
		Method:  method,
		URL:     expandedURL,
		Headers: headers,
		Body:    body,
		Auth:    auth,
	}, api.ClientOptions{Timeout: 5 * time.Second, MaxBytes: 1 << 20, FollowRedirects: true})
	if err != nil && resp == nil {
		t.Fatalf("api.Do: %v", err)
	}
	return resp
}

// stringifyParams converts typed values back to strings so Validate
// (which expects flag-origin strings) can re-coerce them. Lets tests
// express params naturally (map[string]any) while going through the
// same validation path as the real CLI.
func stringifyParams(params map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range params {
		switch tv := v.(type) {
		case string:
			out[k] = tv
		case int:
			out[k] = itoa(tv)
		case int64:
			out[k] = itoa(int(tv))
		case bool:
			if tv {
				out[k] = "true"
			} else {
				out[k] = "false"
			}
		default:
			b, _ := json.Marshal(tv)
			out[k] = string(b)
		}
	}
	return out
}

func itoa(n int) string { return intToString(n) }

func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// setupMockdeep is the shared test fixture: a fresh httptest.Server
// wrapping mockdeep + an isolated config dir for audit sinks.
func setupMockdeep(t *testing.T) string {
	t.Helper()
	config.SetConfigDir(t.TempDir())
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	srv := httptest.NewServer(mockdeep.New())
	t.Cleanup(srv.Close)
	return srv.URL
}

// anonProfile builds an agent-deepweb profile scoped to the test
// server's host with allow_http=true (httptest is plain http). No
// real auth — the OpenAPI paths we exercise here are public.
func anonProfile(_ *testing.T, base string) *credential.Resolved {
	host := strings.TrimPrefix(strings.TrimPrefix(base, "http://"), "https://")
	return &credential.Resolved{
		Credential: credential.Credential{
			Name:      "anon-e2e",
			Type:      credential.AuthBearer,
			Domains:   []string{host},
			AllowHTTP: true,
		},
		// Bearer token deliberately bogus — mockdeep's covered routes
		// (healthz, echo, status, headers) don't require auth.
		Secrets: credential.Secrets{Token: "unused"},
	}
}

// TestOpenAPI_E2E_ImportAndRun — the flagship e2e. Fetch the spec
// from mockdeep, import it, then execute each imported template
// against mockdeep and sanity-check the response. Exercises path
// parameter substitution, JSON request bodies, header parameters.
func TestOpenAPI_E2E_ImportAndRun(t *testing.T) {
	base := setupMockdeep(t)
	spec := fetchSpec(t, base)

	imported, err := ImportOpenAPI(spec, ImportOpenAPIOptions{Prefix: "md"})
	if err != nil {
		t.Fatalf("ImportOpenAPI: %v", err)
	}
	if len(imported) < 3 {
		t.Fatalf("want ≥3 templates imported, got %v", imported)
	}

	auth := anonProfile(t, base)

	t.Run("healthz round-trips", func(t *testing.T) {
		tpl, err := template.Get("md.healthz")
		if err != nil {
			t.Fatal(err)
		}
		resp := runTemplate(t, tpl, nil, auth)
		if resp.Status != 200 {
			t.Fatalf("status %d (body=%s)", resp.Status, resp.Body)
		}
		if !strings.Contains(string(resp.Body), `"ok"`) || !strings.Contains(string(resp.Body), `true`) {
			t.Errorf("body: %s", resp.Body)
		}
	})

	t.Run("status path param substitutes", func(t *testing.T) {
		tpl, err := template.Get("md.status")
		if err != nil {
			t.Fatal(err)
		}
		// mockdeep returns the requested HTTP status code. 204 has no
		// body so there's nothing to redact (keeps the test quiet).
		resp := runTemplate(t, tpl, map[string]any{"code": 204}, auth)
		if resp.Status != 204 {
			t.Errorf("expected 204, got %d (body=%q)", resp.Status, resp.Body)
		}
	})

	t.Run("echo POST carries body through", func(t *testing.T) {
		tpl, err := template.Get("md.echo")
		if err != nil {
			t.Fatal(err)
		}
		// The imported requestBody param is typed object → pass a JSON
		// object and make sure it comes back in mockdeep's echo reply.
		resp := runTemplate(t, tpl, map[string]any{
			"body": map[string]any{"hello": "world"},
		}, auth)
		if resp.Status != 200 {
			t.Fatalf("status %d", resp.Status)
		}
		// mockdeep's echo handler reflects the raw request body inside a
		// JSON envelope, so the inner JSON is escape-stringified
		// (\"hello\":\"world\"). Search for the identifying tokens
		// without caring about quote escaping.
		s := string(resp.Body)
		if !strings.Contains(s, "hello") || !strings.Contains(s, "world") {
			t.Errorf("echo body missing substituted value: %s", s)
		}
	})
}

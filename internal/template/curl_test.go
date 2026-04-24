package template

import (
	"strings"
	"testing"

	"github.com/shhac/agent-deepweb/internal/config"
)

// TestImportCurl_GETSimple — the minimum-viable curl: `curl https://...`
// produces a GET template with no body.
func TestImportCurl_GETSimple(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	_, err := ImportCurl(`curl https://api.example.com/items`, ImportCurlOptions{Name: "x.list"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := Get("x.list")
	if got.Method != "GET" || got.URL != "https://api.example.com/items" {
		t.Errorf("template: %+v", got)
	}
}

// TestImportCurl_POSTWithJSONBody — the workhorse case. -X POST +
// -H Content-Type: application/json + -d '{"k":"v"}' should produce
// a POST template with body_format=json.
func TestImportCurl_POSTWithJSONBody(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	cmd := `curl -X POST https://api.example.com/items \
  -H "Content-Type: application/json" \
  -H "Accept: application/json" \
  -d '{"name":"x","qty":3}'`
	_, err := ImportCurl(cmd, ImportCurlOptions{Name: "x.create"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := Get("x.create")
	if got.Method != "POST" {
		t.Errorf("method: %q", got.Method)
	}
	if got.BodyFormat != "json" {
		t.Errorf("body_format: %q", got.BodyFormat)
	}
	if got.Headers["Content-Type"] != "application/json" {
		t.Errorf("Content-Type: %q", got.Headers["Content-Type"])
	}
	if got.Headers["Accept"] != "application/json" {
		t.Errorf("Accept: %q", got.Headers["Accept"])
	}
}

// TestImportCurl_DefaultsMethodToPOSTWithBody — `-d 'body'` without
// `-X` implies POST, matching curl's own behaviour AND our fetch rule.
func TestImportCurl_DefaultsMethodToPOSTWithBody(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	_, err := ImportCurl(`curl https://x/items -d '{"a":1}'`, ImportCurlOptions{Name: "y.post"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := Get("y.post")
	if got.Method != "POST" {
		t.Errorf("method should default to POST when body present; got %q", got.Method)
	}
}

// TestImportCurl_IgnoresNoisyFlags — browser "Copy as cURL" emits
// lots of flags that are irrelevant for a stored template. Confirm
// the importer drops them without choking.
func TestImportCurl_IgnoresNoisyFlags(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	cmd := `curl -L -v --compressed --http2 --max-time 30 \
  -b 'session=abc' -A 'Mozilla/5.0' \
  -k \
  https://api.example.com/ping`
	_, err := ImportCurl(cmd, ImportCurlOptions{Name: "z.ping"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := Get("z.ping")
	if got.Method != "GET" || got.URL != "https://api.example.com/ping" {
		t.Errorf("template: %+v", got)
	}
	if _, ok := got.Headers["Cookie"]; ok {
		t.Error("-b Cookie should not land as a Header")
	}
}

// TestImportCurl_JsonFlag — curl 7.82+ --json implies
// Content-Type: application/json AND POST method.
func TestImportCurl_JsonFlag(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	_, err := ImportCurl(`curl https://x/items --json '{"a":1}'`, ImportCurlOptions{Name: "j.create"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := Get("j.create")
	if got.Method != "POST" {
		t.Errorf("--json should imply POST; got %q", got.Method)
	}
	if got.Headers["Content-Type"] != "application/json" {
		t.Errorf("--json should set Content-Type: %q", got.Headers["Content-Type"])
	}
}

// TestImportCurl_UrlencodedBody — k=v pairs via -d produce a form body.
func TestImportCurl_UrlencodedBody(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	_, err := ImportCurl(`curl https://x/login -d 'u=alice&p=secret'`, ImportCurlOptions{Name: "f.login"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := Get("f.login")
	if got.BodyFormat != "form" {
		t.Errorf("k=v body should sniff as form: %q", got.BodyFormat)
	}
	body := string(got.BodyTemplate)
	if !strings.Contains(body, `"u"`) || !strings.Contains(body, `"alice"`) {
		t.Errorf("form body: %s", body)
	}
}

// TestImportCurl_QueryStringLift — ?k=v lifts to Query so it's
// overridable at template-run time.
func TestImportCurl_QueryStringLift(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	_, err := ImportCurl(`curl 'https://x/search?q=foo&page=2'`, ImportCurlOptions{Name: "q.search"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := Get("q.search")
	if strings.Contains(got.URL, "?") {
		t.Errorf("URL should not carry query: %q", got.URL)
	}
	if got.Query["q"] != "foo" || got.Query["page"] != "2" {
		t.Errorf("query lift: %+v", got.Query)
	}
}

// TestImportCurl_RequiresName — there's no filename to derive from so
// --name is hard-required at CLI boundary and at pure-func level.
func TestImportCurl_RequiresName(t *testing.T) {
	_, err := ImportCurl(`curl https://x/`, ImportCurlOptions{})
	if err == nil || !strings.Contains(err.Error(), "--name") {
		t.Errorf("want --name required, got %v", err)
	}
}

// TestImportCurl_UnterminatedQuote — broken input surfaces a clear
// lexer error, not a silent malformed template.
func TestImportCurl_UnterminatedQuote(t *testing.T) {
	_, err := ImportCurl(`curl https://x -d 'broken`, ImportCurlOptions{Name: "z"})
	if err == nil || !strings.Contains(err.Error(), "unterminated") {
		t.Errorf("want quote error, got %v", err)
	}
}

// TestShellLex covers the core tokenizer rules.
func TestShellLex(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{`curl https://x`, []string{"curl", "https://x"}},
		{`curl "a b" 'c d'`, []string{"curl", "a b", "c d"}},
		{`curl -H "X: Y"`, []string{"curl", "-H", "X: Y"}},
		{`a \"escaped\" b`, []string{`a`, `"escaped"`, `b`}},
		{"curl \\\n  https://x", []string{"curl", "https://x"}},                 // line-continuation
		{`curl -d "{\"k\":\"v\"}"`, []string{"curl", "-d", `{"k":"v"}`}},        // escaped quotes inside double quotes
		{`curl -d '{"k":"v"}'`, []string{"curl", "-d", `{"k":"v"}`}},            // raw inside single quotes
	}
	for _, tc := range cases {
		got, err := shellLex(tc.in)
		if err != nil {
			t.Errorf("shellLex(%q) error: %v", tc.in, err)
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("shellLex(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("shellLex(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

// TestImportCurl_EqualsFlagForm — `--header=K: V` (the `=` form) must
// be recognised as equivalent to `--header K: V`.
func TestImportCurl_EqualsFlagForm(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	_, err := ImportCurl(`curl https://x/a --header='X-Trace: 1'`, ImportCurlOptions{Name: "eq.flag"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := Get("eq.flag")
	if got.Headers["X-Trace"] != "1" {
		t.Errorf("equals-form header: %+v", got.Headers)
	}
}

package importers

import (
	"github.com/shhac/agent-deepweb/internal/template"

	"strings"
	"testing"

	"github.com/shhac/agent-deepweb/internal/config"
)

const harSample = `{
  "log": {
    "entries": [
      {
        "request": {
          "method": "GET",
          "url": "https://api.example.com/items?page=1",
          "headers": [
            {"name":"Accept","value":"application/json"},
            {"name":"Authorization","value":"Bearer real-session-token"},
            {"name":"Cookie","value":"session=xyz"},
            {"name":"User-Agent","value":"Mozilla/5.0"}
          ],
          "queryString": [
            {"name":"page","value":"1"}
          ]
        }
      },
      {
        "request": {
          "method": "POST",
          "url": "https://api.example.com/items",
          "headers": [
            {"name":"Content-Type","value":"application/json"}
          ],
          "postData": {
            "mimeType": "application/json",
            "text": "{\"name\":\"x\"}"
          }
        }
      },
      {
        "request": {
          "method": "POST",
          "url": "https://api.example.com/login",
          "headers": [{"name":"Content-Type","value":"application/x-www-form-urlencoded"}],
          "postData": {
            "mimeType": "application/x-www-form-urlencoded",
            "params": [
              {"name":"u","value":"alice"},
              {"name":"p","value":"secret"}
            ]
          }
        }
      }
    ]
  }
}`

// TestImportHAR_StripsAuthHeaders — the load-bearing security
// guarantee: authorization + cookie + user-agent from the capture
// MUST NOT land in the stored template. The user's real session
// would otherwise leak into every future template run.
func TestImportHAR_StripsAuthHeaders(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	imported, err := ImportHAR([]byte(harSample), ImportHAROptions{Prefix: "cap"})
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) != 3 {
		t.Fatalf("want 3 imports, got %d: %v", len(imported), imported)
	}

	tpl, err := template.Get(imported[0]) // cap.get_items (sorted first)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"Authorization", "authorization", "Cookie", "cookie", "User-Agent", "user-agent"} {
		if _, leaked := tpl.Headers[forbidden]; leaked {
			t.Errorf("auth header %q leaked into template: %+v", forbidden, tpl.Headers)
		}
	}
	if got := tpl.Headers["Accept"]; got != "application/json" {
		t.Errorf("non-auth header dropped: Accept=%q", got)
	}
}

// TestImportHAR_QueryLift — ?page=1 becomes a Query entry (not baked
// into URL), so the user can override at run time.
func TestImportHAR_QueryLift(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	imported, err := ImportHAR([]byte(harSample), ImportHAROptions{Prefix: "cap"})
	if err != nil {
		t.Fatal(err)
	}

	// Find the GET (method=GET, not POST).
	var getTpl *template.Template
	for _, name := range imported {
		t2, _ := template.Get(name)
		if t2.Method == "GET" {
			getTpl = t2
			break
		}
	}
	if getTpl == nil {
		t.Fatal("GET template missing")
	}
	if strings.Contains(getTpl.URL, "?") {
		t.Errorf("URL should not carry query: %q", getTpl.URL)
	}
	if getTpl.Query["page"] != "1" {
		t.Errorf("page query not lifted: %+v", getTpl.Query)
	}
}

// TestImportHAR_BodyFormats — json → json+pass-through, form-urlencoded → form.
func TestImportHAR_BodyFormats(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	if _, err := ImportHAR([]byte(harSample), ImportHAROptions{Prefix: "cap"}); err != nil {
		t.Fatal(err)
	}

	postItems, _ := template.Get("cap.post_items")
	if postItems.BodyFormat != "json" {
		t.Errorf("application/json postData should become body_format=json: %q", postItems.BodyFormat)
	}
	body := string(postItems.BodyTemplate)
	if !strings.Contains(body, `"name"`) || !strings.Contains(body, `"x"`) {
		t.Errorf("json body not preserved: %s", body)
	}

	login, _ := template.Get("cap.post_login")
	if login.BodyFormat != "form" {
		t.Errorf("form-urlencoded → body_format=form, got %q", login.BodyFormat)
	}
	formBody := string(login.BodyTemplate)
	if !strings.Contains(formBody, `"u"`) || !strings.Contains(formBody, `"alice"`) {
		t.Errorf("form body not preserved: %s", formBody)
	}
}

// TestImportHAR_UrlContains_Filter — narrow imports to entries whose
// URL matches a substring. Common for extracting just the API calls
// from a noisy capture that includes CDN/analytics.
func TestImportHAR_UrlContains_Filter(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	imported, err := ImportHAR([]byte(harSample), ImportHAROptions{Prefix: "cap", URLContains: "/login"})
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) != 1 || !strings.Contains(imported[0], "login") {
		t.Errorf("url-contains filter: %v", imported)
	}
}

// TestImportHAR_Dedupe — duplicates of the same (method,url,body)
// collapse to one template when --dedupe is set.
func TestImportHAR_Dedupe(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	dup := `{"log":{"entries":[
      {"request":{"method":"GET","url":"https://x/a","headers":[]}},
      {"request":{"method":"GET","url":"https://x/a","headers":[]}},
      {"request":{"method":"GET","url":"https://x/a","headers":[]}}
    ]}}`

	imported, err := ImportHAR([]byte(dup), ImportHAROptions{Prefix: "d", Dedupe: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) != 1 {
		t.Errorf("dedupe should collapse to 1, got %v", imported)
	}
}

// TestImportHAR_NameDisambiguation — without --dedupe, three hits to
// the same URL produce three distinctly-named templates.
func TestImportHAR_NameDisambiguation(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	dup := `{"log":{"entries":[
      {"request":{"method":"GET","url":"https://x/a","headers":[]}},
      {"request":{"method":"GET","url":"https://x/a","headers":[]}},
      {"request":{"method":"GET","url":"https://x/a","headers":[]}}
    ]}}`

	imported, err := ImportHAR([]byte(dup), ImportHAROptions{Prefix: "d"})
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) != 3 {
		t.Errorf("want 3 distinctly named, got %v", imported)
	}
	// Names should be all different.
	seen := map[string]bool{}
	for _, n := range imported {
		if seen[n] {
			t.Errorf("duplicate name: %q", n)
		}
		seen[n] = true
	}
}

// TestImportHAR_RejectsEmptyCapture — a HAR with no entries is a
// user error, not a silent no-op.
func TestImportHAR_RejectsEmptyCapture(t *testing.T) {
	_, err := ImportHAR([]byte(`{"log":{"entries":[]}}`), ImportHAROptions{Prefix: "x"})
	if err == nil || !strings.Contains(err.Error(), "no log.entries") {
		t.Errorf("want empty-capture error, got %v", err)
	}
}

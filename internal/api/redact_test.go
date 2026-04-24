package api

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/shhac/agent-deepweb/internal/config"
	"github.com/shhac/agent-deepweb/internal/credential"
)

func TestRedactHeaders_PatternMatches(t *testing.T) {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("Authorization", "Bearer xxx")
	h.Set("Set-Cookie", "session=abc")
	h.Set("X-API-Key", "k")
	h.Set("X-Custom-Token", "t")
	h.Set("User-Agent", "agent-deepweb/0.1")

	out := RedactHeaders(h, nil)
	if out.Get("Content-Type") != "application/json" {
		t.Error("Content-Type should be untouched")
	}
	if out.Get("User-Agent") != "agent-deepweb/0.1" {
		t.Error("User-Agent should be untouched")
	}
	if out.Get("Authorization") != "<redacted>" {
		t.Errorf("Authorization not redacted: %q", out.Get("Authorization"))
	}
	if out.Get("Set-Cookie") != "<redacted>" {
		t.Errorf("Set-Cookie not redacted: %q", out.Get("Set-Cookie"))
	}
	if out.Get("X-Api-Key") != "<redacted>" {
		t.Errorf("X-Api-Key not redacted: %q", out.Get("X-Api-Key"))
	}
	if out.Get("X-Custom-Token") != "<redacted>" {
		t.Errorf("X-Custom-Token not redacted: %q", out.Get("X-Custom-Token"))
	}
}

func TestRedactJSONBody_FieldPatterns(t *testing.T) {
	body := []byte(`{"name":"alice","access_token":"abc","data":{"refresh_token":"def","theme":"dark"}, "password":"pw"}`)
	out := RedactJSONBody(body, "application/json")
	s := string(out)
	if !strings.Contains(s, `"name": "alice"`) {
		t.Errorf("non-secret field lost: %s", s)
	}
	if strings.Contains(s, `"abc"`) || strings.Contains(s, `"def"`) || strings.Contains(s, `"pw"`) {
		t.Errorf("secret value leaked: %s", s)
	}
	if !strings.Contains(s, `"theme": "dark"`) {
		t.Errorf("non-secret nested field lost: %s", s)
	}
}

func TestRedactJSONBody_NonJSONUntouched(t *testing.T) {
	body := []byte("<html>password=xxx</html>")
	out := RedactJSONBody(body, "text/html")
	if !bytes.Equal(out, body) {
		t.Errorf("html body should be untouched: %s", out)
	}
}

func TestRedactSecretEcho_MasksLiteralValues(t *testing.T) {
	resolved := &credential.Resolved{
		Credential: credential.Credential{Type: credential.AuthBearer, Name: "c"},
		Secrets: credential.Secrets{
			Token:    "abc-super-secret-xyz",
			Password: "wonderland",
			Headers:  map[string]string{"X-K": "another-real-secret-1234"},
		},
	}
	// Body contains the literal token in a field we don't redact by name.
	body := []byte(`{"echo": "the token was abc-super-secret-xyz yes"}`)
	out := RedactSecretEcho(body, resolved)
	if bytes.Contains(out, []byte("abc-super-secret-xyz")) {
		t.Errorf("literal token not masked: %s", out)
	}
	if !bytes.Contains(out, []byte("<redacted>")) {
		t.Errorf("expected <redacted> marker: %s", out)
	}

	// Short/empty values must not be treated as needles (they'd false-positive).
	resolved.Secrets.Token = "x"
	body = []byte(`{"message": "x marks the spot"}`)
	out = RedactSecretEcho(body, resolved)
	if !bytes.Contains(out, []byte("x marks")) {
		t.Errorf("short needle (len<=4) should have been skipped: %s", out)
	}
}

// TestRedactHeaders_PerProfileOverrides covers the two override lists
// on the profile: SensitiveHeaders (force-redact even when the default
// pattern doesn't match) and VisibleHeaders (force-show even when it
// does).
func TestRedactHeaders_PerProfileOverrides(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer xxx")                  // default-sensitive
	h.Set("X-Correlation-ID", "visible-by-default")       // default-visible
	h.Set("X-My-Weird-Secret", "custom-field-we-want-hidden") // LLM-unfriendly name

	resolved := &credential.Resolved{
		Credential: credential.Credential{
			SensitiveHeaders: []string{"X-My-Weird-Secret"},
			VisibleHeaders:   []string{"Authorization"},
		},
	}

	out := RedactHeaders(h, resolved)
	if out.Get("Authorization") != "Bearer xxx" {
		t.Errorf("VisibleHeaders override should un-redact Authorization: got %q", out.Get("Authorization"))
	}
	if out.Get("X-My-Weird-Secret") != "<redacted>" {
		t.Errorf("SensitiveHeaders override should redact X-My-Weird-Secret: got %q", out.Get("X-My-Weird-Secret"))
	}
	if out.Get("X-Correlation-ID") != "visible-by-default" {
		t.Errorf("Unrelated header should be untouched: got %q", out.Get("X-Correlation-ID"))
	}
}

// TestRedactSecretEcho_MasksJarTokenAndSensitiveCookies covers the
// jar-sourced needles added in v2: form-auth bearer token + any
// jar cookie flagged Sensitive. A regression here means a live
// session token can echo verbatim in response bodies.
func TestRedactSecretEcho_MasksJarTokenAndSensitiveCookies(t *testing.T) {
	dir := t.TempDir()
	setConfigDir(t, dir)

	// Register a form profile — the only type whose jar carries a
	// separate Token field.
	if _, err := credential.Store(
		credential.Credential{Name: "p", Type: credential.AuthForm, Domains: []string{"a.example.com"}},
		credential.Secrets{Username: "u", Password: "pw"},
	); err != nil {
		t.Fatal(err)
	}
	if err := credential.WriteJar(&credential.Jar{
		Name:  "p",
		Token: "jar-session-token-long",
		Cookies: []credential.PersistedCookie{
			{Name: "sid", Value: "sid-value-looong", Sensitive: true},
			{Name: "theme", Value: "dark-theme-value", Sensitive: false},
		},
	}); err != nil {
		t.Fatal(err)
	}
	resolved, _ := credential.Resolve("p")

	body := []byte(`echo: jar-session-token-long AND sid-value-looong AND dark-theme-value`)
	masked := RedactSecretEcho(body, resolved)

	if bytes.Contains(masked, []byte("jar-session-token-long")) {
		t.Errorf("jar token should be redacted: %s", masked)
	}
	if bytes.Contains(masked, []byte("sid-value-looong")) {
		t.Errorf("sensitive cookie value should be redacted: %s", masked)
	}
	// Non-sensitive cookie must NOT be redacted.
	if !bytes.Contains(masked, []byte("dark-theme-value")) {
		t.Errorf("visible cookie value should have been preserved: %s", masked)
	}
}

// setConfigDir is a local helper — the api test package can't rely on
// the one in do_test.go since it's in the same package but uses a
// different name. Keeps this test file self-contained.
func setConfigDir(t *testing.T, dir string) {
	t.Helper()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })
}

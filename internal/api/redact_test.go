package api

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

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

	out := RedactHeaders(h)
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

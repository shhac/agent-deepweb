package output

import (
	"net/http"
	"testing"

	"github.com/shhac/agent-deepweb/internal/credential"
)

func TestRenderBody(t *testing.T) {
	t.Run("valid JSON is decoded", func(t *testing.T) {
		got := RenderBody("application/json", []byte(`{"x":1}`))
		m, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("want map, got %T", got)
		}
		if m["x"].(float64) != 1 {
			t.Errorf("bad decode: %v", m)
		}
	})

	t.Run("non-JSON content-type stays string", func(t *testing.T) {
		got := RenderBody("text/html", []byte("<html/>"))
		if got != "<html/>" {
			t.Errorf("got %v", got)
		}
	})

	t.Run("malformed JSON falls back to string", func(t *testing.T) {
		got := RenderBody("application/json", []byte(`{not json`))
		if got != `{not json` {
			t.Errorf("got %v", got)
		}
	})

	t.Run("empty body", func(t *testing.T) {
		got := RenderBody("application/json", nil)
		if got != "" {
			t.Errorf("got %v", got)
		}
	})
}

func TestBuildHTTPEnvelope(t *testing.T) {
	hdr := http.Header{"Content-Type": []string{"application/json"}}
	auth := &credential.Resolved{Credential: credential.Credential{Name: "c"}}

	env := BuildHTTPEnvelope(EnvelopeIn{
		URL:         "https://example.com/p",
		Auth:        auth,
		Status:      200,
		StatusText:  "200 OK",
		Headers:     hdr,
		ContentType: "application/json",
		Body:        []byte(`{"ok":true}`),
		Truncated:   false,
	})

	if env["status"] != 200 {
		t.Errorf("status: %v", env["status"])
	}
	if env["url"] != "https://example.com/p" {
		t.Errorf("url: %v", env["url"])
	}
	if env["profile"] != "c" {
		t.Errorf("profile: %v", env["profile"])
	}
	body, ok := env["body"].(map[string]any)
	if !ok || body["ok"] != true {
		t.Errorf("body: %v", env["body"])
	}
}

func TestBuildHTTPEnvelope_NilAuth(t *testing.T) {
	env := BuildHTTPEnvelope(EnvelopeIn{
		URL:    "https://example.com/",
		Auth:   nil,
		Status: 200,
	})
	if env["profile"] != nil {
		t.Errorf("profile should be nil, got %v", env["profile"])
	}
}

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

// TestBuildHTTPEnvelope_HideRequest drops the "request" block from the
// envelope — the LLM's "save tokens, I sent this, I don't need to see
// it again" knob.
func TestBuildHTTPEnvelope_HideRequest(t *testing.T) {
	env := BuildHTTPEnvelope(EnvelopeIn{
		URL:              "https://x/",
		Status:           200,
		Headers:          http.Header{"Content-Type": []string{"application/json"}},
		ContentType:      "application/json",
		Body:             []byte(`{}`),
		RequestMethod:    "POST",
		RequestURL:       "https://x/",
		RequestHeaders:   http.Header{"Content-Type": []string{"application/json"}},
		RequestBodyBytes: 42,
		HideRequest:      true,
	})
	if _, ok := env["request"]; ok {
		t.Error("--hide-request should drop the 'request' key")
	}
	// Response fields must still be present.
	if env["status"] != 200 {
		t.Errorf("status should remain: %v", env["status"])
	}
	if env["body"] == nil {
		t.Error("body should still be included when only --hide-request is set")
	}
}

// TestBuildHTTPEnvelope_HideResponse drops the verbose response fields
// while keeping status/url/profile/audit_id. Used for "did it work?"
// calls where the body would burn tokens.
func TestBuildHTTPEnvelope_HideResponse(t *testing.T) {
	env := BuildHTTPEnvelope(EnvelopeIn{
		URL:           "https://x/",
		Status:        201,
		StatusText:    "201 Created",
		Headers:       http.Header{"X-Req-ID": []string{"abc"}},
		ContentType:   "application/json",
		Body:          []byte(`{"big":"response"}`),
		Truncated:     false,
		RequestMethod: "POST",
		RequestURL:    "https://x/",
		AuditID:       "20260424T1200-abcd",
		HideResponse:  true,
	})
	// Kept:
	for _, k := range []string{"status", "url", "profile", "audit_id", "request"} {
		if _, ok := env[k]; !ok {
			t.Errorf("--hide-response should keep %q, got env=%+v", k, env)
		}
	}
	// Dropped:
	for _, k := range []string{"status_text", "headers", "content_type", "truncated", "body"} {
		if _, ok := env[k]; ok {
			t.Errorf("--hide-response should drop %q, got %v", k, env[k])
		}
	}
}

// TestBuildHTTPEnvelope_AuditIDAppearsOnlyWhenSet keeps the envelope
// clean for untracked calls — an empty "audit_id": "" key would force
// every LLM to treat it as a valid string.
func TestBuildHTTPEnvelope_AuditIDAppearsOnlyWhenSet(t *testing.T) {
	envNoID := BuildHTTPEnvelope(EnvelopeIn{URL: "https://x/", Status: 200})
	if _, ok := envNoID["audit_id"]; ok {
		t.Error("audit_id key should be absent when AuditID is empty")
	}

	envWithID := BuildHTTPEnvelope(EnvelopeIn{URL: "https://x/", Status: 200, AuditID: "20260424T1200-beef"})
	if envWithID["audit_id"] != "20260424T1200-beef" {
		t.Errorf("audit_id: %v", envWithID["audit_id"])
	}
}

// TestBuildHTTPEnvelope_RequestBlockPresentByDefault — the envelope
// MUST include request info by default; regressing this removes
// symmetry between what the LLM sent and what came back.
func TestBuildHTTPEnvelope_RequestBlockPresentByDefault(t *testing.T) {
	env := BuildHTTPEnvelope(EnvelopeIn{
		URL:              "https://x/y",
		Status:           200,
		RequestMethod:    "PUT",
		RequestURL:       "https://x/y",
		RequestHeaders:   http.Header{"Content-Type": []string{"text/plain"}},
		RequestBodyBytes: 128,
	})
	req, ok := env["request"].(map[string]any)
	if !ok {
		t.Fatalf("request block missing; env=%+v", env)
	}
	if req["method"] != "PUT" || req["body_bytes"] != 128 {
		t.Errorf("request block: %+v", req)
	}
}

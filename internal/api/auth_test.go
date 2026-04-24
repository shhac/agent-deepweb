package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shhac/agent-deepweb/internal/credential"
)

// newReq is a test helper that builds an *http.Request with a stock
// URL so ApplyAuth has a header map to mutate.
func newReq(t *testing.T) *http.Request {
	t.Helper()
	req, err := http.NewRequest("GET", "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

// TestApplyAuth_NilResolvedIsNoop — anonymous requests (`--profile
// none`) pass a nil Resolved. ApplyAuth must not panic or mutate
// the request headers.
func TestApplyAuth_NilResolvedIsNoop(t *testing.T) {
	req := newReq(t)
	ApplyAuth(req, nil)
	if len(req.Header) != 0 {
		t.Errorf("nil resolved should leave headers untouched: %+v", req.Header)
	}
}

// TestApplyAuth_UnknownTypeIsNoop — a Resolved with an unrecognised
// Type (pre-v2 schema, accidentally malformed) must not panic; it's
// a silent no-op. The profile-validation layer catches bad types
// before they reach ApplyAuth.
func TestApplyAuth_UnknownTypeIsNoop(t *testing.T) {
	req := newReq(t)
	r := &credential.Resolved{Credential: credential.Credential{Type: "not-a-real-type"}}
	ApplyAuth(req, r)
	if len(req.Header) != 0 {
		t.Errorf("unknown type should leave headers untouched: %+v", req.Header)
	}
}

func TestApplyAuth_Bearer(t *testing.T) {
	t.Run("default header + prefix", func(t *testing.T) {
		req := newReq(t)
		ApplyAuth(req, &credential.Resolved{
			Credential: credential.Credential{Type: credential.AuthBearer},
			Secrets:    credential.Secrets{Token: "tok-xyz"},
		})
		if got := req.Header.Get("Authorization"); got != "Bearer tok-xyz" {
			t.Errorf("Authorization: %q", got)
		}
	})
	t.Run("custom header and prefix", func(t *testing.T) {
		req := newReq(t)
		ApplyAuth(req, &credential.Resolved{
			Credential: credential.Credential{Type: credential.AuthBearer},
			Secrets: credential.Secrets{
				Token:  "tok",
				Header: "X-Token",
				Prefix: "Token ",
			},
		})
		if got := req.Header.Get("X-Token"); got != "Token tok" {
			t.Errorf("X-Token: %q", got)
		}
		if auth := req.Header.Get("Authorization"); auth != "" {
			t.Errorf("Authorization should be empty when header overridden: %q", auth)
		}
	})
	t.Run("empty prefix with non-Authorization header", func(t *testing.T) {
		// Only Authorization gets the auto-Bearer prefix; other header
		// names go through verbatim (no implicit prefix).
		req := newReq(t)
		ApplyAuth(req, &credential.Resolved{
			Credential: credential.Credential{Type: credential.AuthBearer},
			Secrets:    credential.Secrets{Token: "raw-tok", Header: "X-Api-Key"},
		})
		if got := req.Header.Get("X-Api-Key"); got != "raw-tok" {
			t.Errorf("X-Api-Key: %q (should not get Bearer prefix)", got)
		}
	})
	t.Run("empty token is a no-op", func(t *testing.T) {
		req := newReq(t)
		ApplyAuth(req, &credential.Resolved{
			Credential: credential.Credential{Type: credential.AuthBearer},
			Secrets:    credential.Secrets{Token: ""},
		})
		if len(req.Header) != 0 {
			t.Errorf("empty token should not set headers: %+v", req.Header)
		}
	})
	t.Run("overrides existing Authorization", func(t *testing.T) {
		// The http.Client applies ApplyAuth AFTER caller-supplied
		// headers; any Authorization header the user set via --header
		// gets replaced. This is the documented precedence.
		req := newReq(t)
		req.Header.Set("Authorization", "Bearer stale")
		ApplyAuth(req, &credential.Resolved{
			Credential: credential.Credential{Type: credential.AuthBearer},
			Secrets:    credential.Secrets{Token: "fresh"},
		})
		if got := req.Header.Get("Authorization"); got != "Bearer fresh" {
			t.Errorf("Authorization: %q", got)
		}
	})
}

func TestApplyAuth_Basic(t *testing.T) {
	t.Run("user+pass → base64", func(t *testing.T) {
		req := newReq(t)
		ApplyAuth(req, &credential.Resolved{
			Credential: credential.Credential{Type: credential.AuthBasic},
			Secrets:    credential.Secrets{Username: "alice", Password: "wonder"},
		})
		got := req.Header.Get("Authorization")
		// alice:wonder → YWxpY2U6d29uZGVy
		if got != "Basic YWxpY2U6d29uZGVy" {
			t.Errorf("Authorization: %q", got)
		}
	})
	t.Run("empty user+pass is a no-op", func(t *testing.T) {
		req := newReq(t)
		ApplyAuth(req, &credential.Resolved{
			Credential: credential.Credential{Type: credential.AuthBasic},
		})
		if len(req.Header) != 0 {
			t.Errorf("no creds should not set headers: %+v", req.Header)
		}
	})
	t.Run("user only (no password) still attaches", func(t *testing.T) {
		// Some APIs authenticate "username:" (empty password). Our
		// implementation should honour it — not short-circuit because
		// password is empty.
		req := newReq(t)
		ApplyAuth(req, &credential.Resolved{
			Credential: credential.Credential{Type: credential.AuthBasic},
			Secrets:    credential.Secrets{Username: "api-key-as-user"},
		})
		if got := req.Header.Get("Authorization"); !strings.HasPrefix(got, "Basic ") {
			t.Errorf("username-only should still produce Basic header; got %q", got)
		}
	})
}

func TestApplyAuth_Cookie(t *testing.T) {
	t.Run("Cookie header set", func(t *testing.T) {
		req := newReq(t)
		ApplyAuth(req, &credential.Resolved{
			Credential: credential.Credential{Type: credential.AuthCookie},
			Secrets:    credential.Secrets{Cookie: "session=abc; csrf=xyz"},
		})
		if got := req.Header.Get("Cookie"); got != "session=abc; csrf=xyz" {
			t.Errorf("Cookie: %q", got)
		}
	})
	t.Run("empty cookie is a no-op", func(t *testing.T) {
		req := newReq(t)
		ApplyAuth(req, &credential.Resolved{
			Credential: credential.Credential{Type: credential.AuthCookie},
		})
		if len(req.Header) != 0 {
			t.Error("empty cookie should not set headers")
		}
	})
}

func TestApplyAuth_Custom(t *testing.T) {
	t.Run("multiple headers applied", func(t *testing.T) {
		req := newReq(t)
		ApplyAuth(req, &credential.Resolved{
			Credential: credential.Credential{Type: credential.AuthCustom},
			Secrets: credential.Secrets{Headers: map[string]string{
				"X-Api-Key": "k-1",
				"X-Env":     "prod",
			}},
		})
		if req.Header.Get("X-Api-Key") != "k-1" || req.Header.Get("X-Env") != "prod" {
			t.Errorf("custom headers: %+v", req.Header)
		}
	})
	t.Run("empty Headers map is a no-op", func(t *testing.T) {
		req := newReq(t)
		ApplyAuth(req, &credential.Resolved{
			Credential: credential.Credential{Type: credential.AuthCustom},
		})
		if len(req.Header) != 0 {
			t.Error("empty custom-headers should not set headers")
		}
	})
	t.Run("Token field ignored on custom type", func(t *testing.T) {
		// Custom auth is "attach these headers, nothing else" — the
		// bearer-like Token/Header/Prefix trio must NOT be applied
		// (otherwise a custom profile with a Token field set could
		// silently attach an Authorization header).
		req := newReq(t)
		ApplyAuth(req, &credential.Resolved{
			Credential: credential.Credential{Type: credential.AuthCustom},
			Secrets: credential.Secrets{
				Token:   "should-not-be-attached",
				Header:  "Authorization",
				Prefix:  "Bearer ",
				Headers: map[string]string{"X-Api-Key": "k"},
			},
		})
		if req.Header.Get("Authorization") != "" {
			t.Errorf("Authorization should not be set by custom type: %q", req.Header.Get("Authorization"))
		}
	})
}

// TestApplyAuth_End2End exercises ApplyAuth against a real test
// server to confirm the outgoing request really carries what we set.
// Not strictly necessary given the unit tests above — added as a
// defence-in-depth guard in case a future refactor of the api.Do
// pipeline changes the header-ordering rules.
func TestApplyAuth_End2End(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL, nil)
	ApplyAuth(req, &credential.Resolved{
		Credential: credential.Credential{Type: credential.AuthBearer},
		Secrets:    credential.Secrets{Token: "end2end-token"},
	})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if gotAuth != "Bearer end2end-token" {
		t.Errorf("server saw %q, want Bearer end2end-token", gotAuth)
	}
}

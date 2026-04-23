package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/shhac/agent-deepweb/internal/audit"
	"github.com/shhac/agent-deepweb/internal/config"
	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

// setup points the config directory (and therefore the audit log and
// session files) at a tempdir, and enables auditing. Returns the dir so
// tests can inspect files directly.
func setup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir("") })
	t.Setenv("AGENT_DEEPWEB_AUDIT", "on")
	t.Setenv("AGENT_DEEPWEB_MODE", "")
	return dir
}

// testResolved is a helper for building a Resolved credential scoped to
// the httptest server's host so allowlist checks pass.
func testResolved(t *testing.T, authType string, serverURL string, secrets credential.Secrets) *credential.Resolved {
	t.Helper()
	u, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	return &credential.Resolved{
		Credential: credential.Credential{
			Name:      "c",
			Type:      authType,
			Domains:   []string{u.Host},
			AllowHTTP: true, // httptest is http://
		},
		Secrets: secrets,
	}
}

func TestDo_OKWritesAuditEntry(t *testing.T) {
	setup(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-xyz-long" {
			t.Errorf("auth header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	resolved := testResolved(t, credential.AuthBearer, srv.URL, credential.Secrets{Token: "token-xyz-long"})
	resp, err := Do(context.Background(), Request{
		Method: "GET",
		URL:    srv.URL + "/thing",
		Auth:   resolved,
	}, ClientOptions{Timeout: 5 * time.Second, MaxBytes: 1024})

	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if resp.Status != 200 {
		t.Fatalf("status %d", resp.Status)
	}

	entries, _ := audit.Tail(10)
	if len(entries) != 1 {
		t.Fatalf("want 1 audit entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Outcome != "ok" || e.Status != 200 || e.Credential != "c" || e.Method != "GET" {
		t.Errorf("audit entry: %+v", e)
	}
}

func TestDo_HTTPErrorClassified(t *testing.T) {
	setup(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	resolved := testResolved(t, credential.AuthBearer, srv.URL, credential.Secrets{Token: "token-xyz-long"})
	resp, err := Do(context.Background(), Request{URL: srv.URL + "/missing", Auth: resolved},
		ClientOptions{Timeout: 5 * time.Second, MaxBytes: 1024})

	if resp == nil || resp.Status != 404 {
		t.Fatalf("want 404 response, got %+v", resp)
	}
	var ae *agenterrors.APIError
	if !agenterrors.As(err, &ae) {
		t.Fatalf("err is not APIError: %v", err)
	}
	if ae.FixableBy != agenterrors.FixableByAgent {
		t.Errorf("404 should be agent-fixable, got %s", ae.FixableBy)
	}

	entries, _ := audit.Tail(10)
	if len(entries) != 1 || entries[0].Outcome != "error" || entries[0].FixableBy != "agent" {
		t.Errorf("audit of 404: %+v", entries)
	}
}

func TestDo_SchemeRefusalAuditsError(t *testing.T) {
	setup(t)
	// No AllowHTTP → http:// to a non-loopback should refuse.
	resolved := &credential.Resolved{
		Credential: credential.Credential{Name: "c", Type: credential.AuthBearer, Domains: []string{"example.com"}},
		Secrets:    credential.Secrets{Token: "t-long-enough-to-redact"},
	}
	resp, err := Do(context.Background(), Request{URL: "http://example.com/", Auth: resolved},
		ClientOptions{Timeout: 2 * time.Second, MaxBytes: 1024})
	if resp != nil {
		t.Errorf("scheme refusal should not return a response, got %+v", resp)
	}
	if err == nil || !strings.Contains(err.Error(), "http://") {
		t.Fatalf("expected scheme refusal error, got %v", err)
	}

	entries, _ := audit.Tail(10)
	if len(entries) != 1 || entries[0].Outcome != "error" || entries[0].FixableBy != "human" {
		t.Errorf("scheme-refusal audit: %+v", entries)
	}
}

func TestDo_ResponseTokenEcho_Redacted(t *testing.T) {
	setup(t)
	// Server echoes the token in a body field we DON'T redact by field name.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"saw abc-super-secret-long-value here"}`))
	}))
	defer srv.Close()

	resolved := testResolved(t, credential.AuthBearer, srv.URL, credential.Secrets{Token: "abc-super-secret-long-value"})
	resp, err := Do(context.Background(), Request{URL: srv.URL, Auth: resolved},
		ClientOptions{Timeout: 5 * time.Second, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(resp.Body, []byte("abc-super-secret-long-value")) {
		t.Errorf("token leaked in response body: %s", resp.Body)
	}
	if !bytes.Contains(resp.Body, []byte("<redacted>")) {
		t.Errorf("expected <redacted> marker: %s", resp.Body)
	}
}

func TestDo_FormSessionExpiredRefuses(t *testing.T) {
	setup(t)
	// Pre-write an expired session for credential "c".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("expired session should short-circuit before the HTTP request")
	}))
	defer srv.Close()

	sess := &credential.Session{
		Name:     "c",
		Cookies:  []credential.PersistedCookie{{Name: "session", Value: "x", Sensitive: true}},
		Expires:  time.Now().Add(-1 * time.Hour),
		Acquired: time.Now().Add(-2 * time.Hour),
	}
	if err := credential.WriteSession(sess); err != nil {
		t.Fatal(err)
	}

	resolved := testResolved(t, credential.AuthForm, srv.URL, credential.Secrets{})
	_, err := Do(context.Background(), Request{URL: srv.URL, Auth: resolved},
		ClientOptions{Timeout: 2 * time.Second, MaxBytes: 1024})

	var ae *agenterrors.APIError
	if !agenterrors.As(err, &ae) || ae.FixableBy != agenterrors.FixableByHuman {
		t.Fatalf("expected human-fixable expired-session error, got %v", err)
	}
	if !strings.Contains(ae.Error(), "expired") {
		t.Errorf("error should mention expired: %v", ae)
	}
}

func TestDo_CookieHarvestingPreservesSensitivity(t *testing.T) {
	setup(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "s-abc", HttpOnly: true, Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "theme", Value: "dark", Path: "/"})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// Pre-seed a form-auth session so harvesting writes into it.
	sess := &credential.Session{
		Name:     "c",
		Expires:  time.Now().Add(1 * time.Hour),
		Acquired: time.Now(),
	}
	if err := credential.WriteSession(sess); err != nil {
		t.Fatal(err)
	}

	resolved := testResolved(t, credential.AuthForm, srv.URL, credential.Secrets{})
	resp, err := Do(context.Background(), Request{URL: srv.URL, Auth: resolved},
		ClientOptions{Timeout: 5 * time.Second, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.NewCookies) != 2 {
		t.Fatalf("want 2 new cookies, got %d", len(resp.NewCookies))
	}
	byName := map[string]credential.CookieView{}
	for _, c := range resp.NewCookies {
		byName[c.Name] = c
	}
	if !byName["session"].Sensitive || byName["session"].Value != "<redacted>" {
		t.Errorf("session cookie should be sensitive: %+v", byName["session"])
	}
	if byName["theme"].Sensitive || byName["theme"].Value != "dark" {
		t.Errorf("theme cookie should be visible with value 'dark': %+v", byName["theme"])
	}

	// Session file on disk should now reflect the harvested cookies.
	reread, err := credential.ReadSession("c")
	if err != nil {
		t.Fatal(err)
	}
	if len(reread.Cookies) != 2 {
		t.Errorf("session file didn't persist cookies: %+v", reread.Cookies)
	}
}

func TestDo_TruncatedResponseAuditsAgentFixable(t *testing.T) {
	setup(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		buf := bytes.Repeat([]byte{'x'}, 10_000)
		_, _ = w.Write(buf)
	}))
	defer srv.Close()

	resp, err := Do(context.Background(), Request{URL: srv.URL},
		ClientOptions{Timeout: 5 * time.Second, MaxBytes: 1024})
	if resp == nil || !resp.Truncated || len(resp.Body) != 1024 {
		t.Fatalf("expected truncated resp with len 1024, got %+v (len=%d)", resp, len(resp.Body))
	}
	var ae *agenterrors.APIError
	if !agenterrors.As(err, &ae) || ae.FixableBy != agenterrors.FixableByAgent {
		t.Fatalf("expected agent-fixable truncation error, got %v", err)
	}
}

func TestDo_JSONBodyRoundtrip(t *testing.T) {
	// Sanity: POSTing a JSON body with auth attaches headers in the right order.
	setup(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method: %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("ct: %s", r.Header.Get("Content-Type"))
		}
		var got map[string]any
		_ = json.NewDecoder(r.Body).Decode(&got)
		if got["name"] != "blue" {
			t.Errorf("body: %+v", got)
		}
		w.WriteHeader(201)
	}))
	defer srv.Close()

	resolved := testResolved(t, credential.AuthBearer, srv.URL, credential.Secrets{Token: "t-long-xyz"})
	resp, err := Do(context.Background(), Request{
		Method:  "POST",
		URL:     srv.URL,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    strings.NewReader(`{"name":"blue"}`),
		Auth:    resolved,
	}, ClientOptions{Timeout: 5 * time.Second, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 201 {
		t.Errorf("status: %d", resp.Status)
	}
}

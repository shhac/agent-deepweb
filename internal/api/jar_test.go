package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shhac/agent-deepweb/internal/credential"
)

// TestDo_BYOJarPersistsAcrossRequests verifies the load-bearing contract
// of `--profile none --cookiejar <path>`: a POST that sets cookies
// writes them to the caller-chosen file (plaintext), and a subsequent
// GET against the same jar resends them.
func TestDo_BYOJarPersistsAcrossRequests(t *testing.T) {
	setup(t)

	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Cookie"))
		switch r.URL.Path {
		case "/login":
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "byo-abc", Path: "/", HttpOnly: true})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"hit":true}`))
		}
	}))
	defer srv.Close()

	jarPath := filepath.Join(t.TempDir(), "flow.json")

	// Request 1: anonymous POST that receives Set-Cookie.
	_, err := Do(context.Background(), Request{
		Method:  "POST",
		URL:     srv.URL + "/login",
		Auth:    nil, // --profile none
		JarPath: jarPath,
	}, ClientOptions{Timeout: 5 * time.Second, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}

	// BYO jar file exists, plaintext, contains the cookie.
	raw, err := os.ReadFile(jarPath)
	if err != nil {
		t.Fatalf("BYO jar not written: %v", err)
	}
	if len(raw) > 4 && string(raw[:4]) == "AGD1" {
		t.Error("BYO jar was encrypted (AGD1 magic) — must be plaintext")
	}

	// Request 2: anonymous GET with the same jar; server should now see
	// the cookie from request 1.
	_, err = Do(context.Background(), Request{
		Method:  "GET",
		URL:     srv.URL + "/me",
		Auth:    nil,
		JarPath: jarPath,
	}, ClientOptions{Timeout: 5 * time.Second, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}

	if len(seen) != 2 {
		t.Fatalf("expected 2 requests, seen=%v", seen)
	}
	if seen[0] != "" {
		t.Errorf("first request had cookie: %q", seen[0])
	}
	wantCookie := "session=byo-abc"
	if !contains(seen[1], wantCookie) {
		t.Errorf("second request missing %q in Cookie header: %q", wantCookie, seen[1])
	}
}

// TestDiffNewCookies_Pure covers the diff logic extracted from
// harvestJarCookies without any HTTP or file dependency.
func TestDiffNewCookies_Pure(t *testing.T) {
	before := snapshotCookieKeys([]credential.PersistedCookie{
		{Name: "a", Domain: "x", Path: "/"},
		{Name: "b", Domain: "x", Path: "/"},
	})
	after := []credential.PersistedCookie{
		{Name: "a", Domain: "x", Path: "/"},              // existed
		{Name: "b", Domain: "x", Path: "/"},              // existed
		{Name: "c", Domain: "x", Path: "/", Sensitive: true, Value: "secret"}, // new, sensitive
		{Name: "d", Domain: "x", Path: "/", Value: "vis"},                     // new, visible
	}

	added := diffNewCookies(before, after)
	if len(added) != 2 {
		t.Fatalf("want 2 new, got %d: %+v", len(added), added)
	}

	byName := map[string]credential.CookieView{}
	for _, v := range added {
		byName[v.Name] = v
	}
	if byName["c"].Value != "<redacted>" {
		t.Errorf("new sensitive cookie should be redacted in view: %+v", byName["c"])
	}
	if byName["d"].Value != "vis" {
		t.Errorf("new visible cookie should show value: %+v", byName["d"])
	}
}

// TestPrimeCookieJar_ExpiryGatesFormOnly asserts the expiry branch fires
// only for form-auth profiles and only when the jar has expired.
func TestPrimeCookieJar_ExpiryGatesFormOnly(t *testing.T) {
	setup(t)

	writeExpiredJar := func(name string) {
		t.Helper()
		if _, err := credential.Store(
			credential.Credential{Name: name, Type: credential.AuthForm, Domains: []string{"x.example.com"}, AllowHTTP: true},
			credential.Secrets{},
		); err != nil {
			t.Fatal(err)
		}
		if err := credential.WriteJar(&credential.Jar{
			Name:    name,
			Expires: time.Now().Add(-1 * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}

	writeExpiredJar("form-expired")
	r, err := credential.Resolve("form-expired")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse("http://x.example.com/")

	_, err = primeCookieJar(r, "", u)
	if err == nil {
		t.Fatal("expected expired-session error for form profile")
	}

	// Same conditions, bearer type: no error; the jar's Expires is
	// ignored for non-form types.
	if _, err := credential.Store(
		credential.Credential{Name: "bearer-expired", Type: credential.AuthBearer, Domains: []string{"x.example.com"}, AllowHTTP: true},
		credential.Secrets{Token: "tok-long-enough"},
	); err != nil {
		t.Fatal(err)
	}
	if err := credential.WriteJar(&credential.Jar{
		Name:    "bearer-expired",
		Expires: time.Now().Add(-1 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	r2, _ := credential.Resolve("bearer-expired")
	if _, err := primeCookieJar(r2, "", u); err != nil {
		t.Errorf("bearer profile with 'expired' jar should not error, got %v", err)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// TestDo_AnonymousAuditedAsNone verifies the audit log tags requests
// from `--profile none` (req.Auth == nil) with profile="none". This is
// the tripwire the v2 design relies on for spotting anonymous traffic.
// Also confirms harvestJarCookies returns nil when there's no profile
// AND no BYO jar — anonymous-without-jar must NOT silently leak cookies
// into some default location.
func TestDo_AnonymousAuditedAsNone(t *testing.T) {
	dir := setup(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "abandoned", Value: "x", Path: "/"})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	resp, err := Do(context.Background(), Request{
		URL:     srv.URL,
		Auth:    nil,
		JarPath: "",
	}, ClientOptions{Timeout: 5 * time.Second, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.NewCookies) != 0 {
		t.Errorf("anonymous-without-jar must not surface harvested cookies, got %+v", resp.NewCookies)
	}

	// Audit entry should mark this anonymous.
	logBytes, _ := os.ReadFile(dir + "/audit.log")
	if !contains(string(logBytes), `"profile":"none"`) {
		t.Errorf("audit entry should tag profile=none for nil Auth; got %s", logBytes)
	}
}

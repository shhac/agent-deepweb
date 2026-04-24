package login

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shhac/agent-deepweb/internal/config"
	"github.com/shhac/agent-deepweb/internal/credential"
)

// storeFormProfile is the shared fixture: registers a form-auth
// profile scoped to the given test server's host, with optional
// overrides on the login fields.
func storeFormProfile(t *testing.T, name, serverURL, loginPath string, overrides func(*credential.Secrets)) {
	t.Helper()
	host := strings.TrimPrefix(strings.TrimPrefix(serverURL, "http://"), "https://")
	secrets := credential.Secrets{
		Username: "alice",
		Password: "wonder",
		LoginURL: serverURL + loginPath,
	}
	if overrides != nil {
		overrides(&secrets)
	}
	if _, err := credential.Store(credential.Credential{
		Name:      name,
		Type:      credential.AuthForm,
		Domains:   []string{host},
		AllowHTTP: true, // httptest is http://
	}, secrets); err != nil {
		t.Fatal(err)
	}
}

// setupLoginTest isolates the config dir so jar state doesn't leak
// between subtests.
func setupLoginTest(t *testing.T) {
	t.Helper()
	config.SetConfigDir(t.TempDir())
	restoreBackend := credential.SetBackend(credential.NoopBackend())
	t.Cleanup(func() {
		config.SetConfigDir("")
		config.ClearCache()
		restoreBackend()
	})
}

// TestValidateLoginURL_RefusesOffAllowlist — the login-url's host
// must be inside the credential's domain allowlist. This is the
// guard that stops a malicious redirect target from being used as
// a login endpoint.
func TestValidateLoginURL_RefusesOffAllowlist(t *testing.T) {
	setupLoginTest(t)

	// Profile allows foo.example.com, login-url points at evil.
	_, err := credential.Store(credential.Credential{
		Name:    "p",
		Type:    credential.AuthForm,
		Domains: []string{"foo.example.com"},
	}, credential.Secrets{
		Username: "u",
		Password: "p",
		LoginURL: "https://evil.example.com/login",
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := credential.Resolve("p")
	if err != nil {
		t.Fatal(err)
	}
	_, err = validateLoginURL(resolved)
	if err == nil || !strings.Contains(err.Error(), "allowlist") {
		t.Errorf("want allowlist refusal, got %v", err)
	}
}

// TestValidateLoginURL_MalformedURL — a non-URL login-url must be
// refused before we attempt a request.
func TestValidateLoginURL_MalformedURL(t *testing.T) {
	setupLoginTest(t)

	_, _ = credential.Store(credential.Credential{
		Name: "p", Type: credential.AuthForm,
		Domains: []string{"x"},
	}, credential.Secrets{
		Username: "u", Password: "p",
		LoginURL: "not-a-url",
	})
	resolved, _ := credential.Resolve("p")
	_, err := validateLoginURL(resolved)
	if err == nil {
		t.Error("malformed URL should be refused")
	}
}

// TestDoLogin_NonFormProfileRejected — login is a form-only verb.
// Calling it against a bearer profile should error fast rather than
// try to POST to a nil login-url.
func TestDoLogin_NonFormProfileRejected(t *testing.T) {
	setupLoginTest(t)
	if _, err := credential.Store(credential.Credential{
		Name:    "bearer-profile",
		Type:    credential.AuthBearer,
		Domains: []string{"api.example.com"},
	}, credential.Secrets{Token: "tok"}); err != nil {
		t.Fatal(err)
	}
	err := doLogin("bearer-profile")
	if err == nil || !strings.Contains(err.Error(), "not 'form'") {
		t.Errorf("want form-only error, got %v", err)
	}
}

// TestDoLogin_NonMatchingStatusFails — server returns 401 (or any
// non-2xx non-match); we must surface fixable_by:human without
// persisting a jar.
func TestDoLogin_NonMatchingStatusFails(t *testing.T) {
	setupLoginTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	storeFormProfile(t, "p", srv.URL, "/login", nil)
	err := doLogin("p")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("want 401 error, got %v", err)
	}
	// No jar written.
	if _, err := credential.ReadJar("p"); err == nil {
		t.Error("failed login must not persist a jar")
	}
}

// TestDoLogin_CustomSuccessStatus — some APIs respond with 201 or
// 204 on successful login. success_status lets the profile opt in
// to treat that as success.
func TestDoLogin_CustomSuccessStatus(t *testing.T) {
	setupLoginTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc"})
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	storeFormProfile(t, "p", srv.URL, "/login", func(s *credential.Secrets) {
		s.SuccessStatus = http.StatusNoContent
	})
	if err := doLogin("p"); err != nil {
		t.Fatalf("custom success status should succeed, got %v", err)
	}
	jar, err := credential.ReadJar("p")
	if err != nil {
		t.Fatal(err)
	}
	if len(jar.Cookies) == 0 {
		t.Error("jar should have harvested session cookie")
	}
}

// TestDoLogin_TokenPath_PointsAtMissingKey — --token-path explicitly
// says "the login response carries the bearer here"; if the key
// isn't there, we must surface fixable_by:human not silently store
// an empty token.
func TestDoLogin_TokenPath_PointsAtMissingKey(t *testing.T) {
	setupLoginTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	storeFormProfile(t, "p", srv.URL, "/login", func(s *credential.Secrets) {
		s.TokenPath = "data.token" // not in the response
	})
	err := doLogin("p")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "token") {
		t.Errorf("want token-path failure, got %v", err)
	}
	// No jar written — failed token extraction must not leave a jar
	// with cookies only (since the user declared they wanted a token).
	if _, err := credential.ReadJar("p"); err == nil {
		t.Error("failed token extraction must not persist a jar")
	}
}

// TestDoLogin_SuccessfulFormLogin — the happy path end-to-end:
// POST form body, collect Set-Cookie, persist jar with expiry.
func TestDoLogin_SuccessfulFormLogin(t *testing.T) {
	setupLoginTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.FormValue("username") != "alice" || r.FormValue("password") != "wonder" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "sess-xyz"})
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	storeFormProfile(t, "p", srv.URL, "/login", nil)
	if err := doLogin("p"); err != nil {
		t.Fatalf("doLogin: %v", err)
	}
	jar, err := credential.ReadJar("p")
	if err != nil {
		t.Fatal(err)
	}
	if len(jar.Cookies) == 0 {
		t.Error("jar should have session cookie")
	}
	if jar.Expires.IsZero() {
		t.Error("jar.Expires should be set")
	}
}

package profile

import (
	"strings"
	"testing"

	"github.com/shhac/agent-deepweb/internal/credential"
)

// Per-type Secrets factories. Table-tested for both happy paths and
// missing-required-field errors. These are pure — no HTTP, no FS, no
// keychain.
func TestBuildBearerSecrets(t *testing.T) {
	s, err := buildBearerSecrets(&addOpts{token: "abc-xyz", tokenHeaderSet: "X-Auth", tokenPrefixSet: "T "})
	if err != nil || s.Token != "abc-xyz" || s.Header != "X-Auth" || s.Prefix != "T " {
		t.Errorf("happy path: %+v %v", s, err)
	}
	if _, err := buildBearerSecrets(&addOpts{}); err == nil {
		t.Error("missing token should error")
	}
}

func TestBuildBasicSecrets(t *testing.T) {
	s, err := buildBasicSecrets(&addOpts{username: "u", password: "p"})
	if err != nil || s.Username != "u" || s.Password != "p" {
		t.Errorf("happy path: %+v %v", s, err)
	}
	for _, o := range []*addOpts{{username: "u"}, {password: "p"}, {}} {
		if _, err := buildBasicSecrets(o); err == nil {
			t.Errorf("missing field should error for %+v", o)
		}
	}
}

func TestBuildCookieSecrets(t *testing.T) {
	s, err := buildCookieSecrets(&addOpts{cookie: "session=abc"})
	if err != nil || s.Cookie != "session=abc" {
		t.Errorf("happy path: %+v %v", s, err)
	}
	if _, err := buildCookieSecrets(&addOpts{}); err == nil {
		t.Error("missing cookie should error")
	}
}

func TestBuildCustomSecrets(t *testing.T) {
	s, err := buildCustomSecrets(&addOpts{customHeaders: []string{"X-Api-Key: k", "X-Env: prod"}})
	if err != nil {
		t.Fatal(err)
	}
	if s.Headers["X-Api-Key"] != "k" || s.Headers["X-Env"] != "prod" {
		t.Errorf("headers: %+v", s.Headers)
	}
	if _, err := buildCustomSecrets(&addOpts{}); err == nil {
		t.Error("no custom-headers should error")
	}
	if _, err := buildCustomSecrets(&addOpts{customHeaders: []string{"no-colon"}}); err == nil {
		t.Error("malformed custom-header should error")
	}
}

func TestBuildFormSecrets(t *testing.T) {
	s, err := buildFormSecrets(&addOpts{
		loginURL: "https://api.example.com/login",
		username: "u", password: "p",
		extraFields: []string{"grant_type=password", "scope=read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.LoginURL != "https://api.example.com/login" ||
		s.Username != "u" || s.Password != "p" ||
		s.ExtraFields["grant_type"] != "password" ||
		s.ExtraFields["scope"] != "read" {
		t.Errorf("fields: %+v", s)
	}

	cases := []struct {
		name string
		o    *addOpts
	}{
		{"missing login-url", &addOpts{username: "u", password: "p"}},
		{"missing username", &addOpts{loginURL: "x", password: "p"}},
		{"missing password", &addOpts{loginURL: "x", username: "u"}},
		{"malformed extra-field", &addOpts{loginURL: "x", username: "u", password: "p", extraFields: []string{"no-equals"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := buildFormSecrets(tc.o); err == nil {
				t.Error("expected error")
			}
		})
	}
}

// Sanity that the secretsBuilders table is complete for every declared
// auth type constant.
func TestSecretsBuildersCovered(t *testing.T) {
	for _, tp := range []string{credential.AuthBearer, credential.AuthBasic, credential.AuthCookie, credential.AuthCustom, credential.AuthForm} {
		if _, ok := secretsBuilders[tp]; !ok {
			t.Errorf("missing builder for %q", tp)
		}
	}
}

func TestParseHeaderList(t *testing.T) {
	got, err := parseHeaderList([]string{"Accept: application/json", "X-V: 1"})
	if err != nil {
		t.Fatal(err)
	}
	if got["Accept"] != "application/json" || got["X-V"] != "1" {
		t.Errorf("headers: %+v", got)
	}
	if _, err := parseHeaderList([]string{"no-colon"}); err == nil {
		t.Error("malformed header should error")
	}
	if got, err := parseHeaderList(nil); err != nil || got != nil {
		t.Errorf("nil input should return nil: %v %v", got, err)
	}
	// Error message includes the flag label so humans find the right one.
	_, err = parseHeaderList([]string{"nope"})
	if err == nil || !strings.Contains(err.Error(), "default-header") {
		t.Errorf("err missing flag label: %v", err)
	}
}

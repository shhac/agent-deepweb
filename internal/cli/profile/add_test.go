package profile

import (
	"strings"
	"testing"
)

// buildSecretsForAdd is the unified validator + assembler used by the
// `profile add` RunE. Per-type required-field rules are delegated to
// credential.BuildSecretsCore (covered separately in the credential
// package); these tests focus on the form-only field layering and the
// error wrapping ("for <type> type").
func TestBuildSecretsForAdd_Bearer(t *testing.T) {
	s, err := buildSecretsForAdd(&addOpts{
		authType: "bearer", token: "abc-xyz", tokenHeaderSet: "X-Auth", tokenPrefixSet: "T ",
	})
	if err != nil || s.Token != "abc-xyz" || s.Header != "X-Auth" || s.Prefix != "T " {
		t.Errorf("happy path: %+v %v", s, err)
	}
	_, err = buildSecretsForAdd(&addOpts{authType: "bearer"})
	if err == nil || !strings.Contains(err.Error(), "for bearer type") {
		t.Errorf("missing token: want 'for bearer type', got %v", err)
	}
}

func TestBuildSecretsForAdd_Basic(t *testing.T) {
	s, err := buildSecretsForAdd(&addOpts{authType: "basic", username: "u", password: "p"})
	if err != nil || s.Username != "u" || s.Password != "p" {
		t.Errorf("happy path: %+v %v", s, err)
	}
	for _, o := range []*addOpts{{authType: "basic", username: "u"}, {authType: "basic", password: "p"}, {authType: "basic"}} {
		if _, err := buildSecretsForAdd(o); err == nil {
			t.Errorf("missing field should error for %+v", o)
		}
	}
}

func TestBuildSecretsForAdd_Cookie(t *testing.T) {
	s, err := buildSecretsForAdd(&addOpts{authType: "cookie", cookie: "session=abc"})
	if err != nil || s.Cookie != "session=abc" {
		t.Errorf("happy path: %+v %v", s, err)
	}
	if _, err := buildSecretsForAdd(&addOpts{authType: "cookie"}); err == nil {
		t.Error("missing cookie should error")
	}
}

func TestBuildSecretsForAdd_Custom(t *testing.T) {
	s, err := buildSecretsForAdd(&addOpts{authType: "custom", customHeaders: []string{"X-Api-Key: k", "X-Env: prod"}})
	if err != nil {
		t.Fatal(err)
	}
	if s.Headers["X-Api-Key"] != "k" || s.Headers["X-Env"] != "prod" {
		t.Errorf("headers: %+v", s.Headers)
	}
	if _, err := buildSecretsForAdd(&addOpts{authType: "custom"}); err == nil {
		t.Error("no custom-headers should error")
	}
	if _, err := buildSecretsForAdd(&addOpts{authType: "custom", customHeaders: []string{"no-colon"}}); err == nil {
		t.Error("malformed custom-header should error")
	}
}

func TestBuildSecretsForAdd_Form(t *testing.T) {
	s, err := buildSecretsForAdd(&addOpts{
		authType: "form",
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
		{"missing login-url", &addOpts{authType: "form", username: "u", password: "p"}},
		{"missing username", &addOpts{authType: "form", loginURL: "x", password: "p"}},
		{"missing password", &addOpts{authType: "form", loginURL: "x", username: "u"}},
		{"malformed extra-field", &addOpts{authType: "form", loginURL: "x", username: "u", password: "p", extraFields: []string{"no-equals"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := buildSecretsForAdd(tc.o); err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestBuildSecretsForAdd_UnknownType(t *testing.T) {
	_, err := buildSecretsForAdd(&addOpts{authType: "mystery"})
	if err == nil || !strings.Contains(err.Error(), "unknown auth type") {
		t.Errorf("want unknown-auth error, got %v", err)
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

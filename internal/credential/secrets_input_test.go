package credential

import (
	"strings"
	"testing"
)

// TestBuildSecretsCore_Bearer covers the bearer type's required-field
// rule and its pass-through of the optional header/prefix overrides.
func TestBuildSecretsCore_Bearer(t *testing.T) {
	t.Run("happy: token required, header/prefix pass through", func(t *testing.T) {
		s, err := BuildSecretsCore(AuthBearer, SecretInputs{
			Token:       "tk-1",
			TokenHeader: "X-Auth",
			TokenPrefix: "Bearer ",
		})
		if err != nil {
			t.Fatal(err)
		}
		if s.Token != "tk-1" || s.Header != "X-Auth" || s.Prefix != "Bearer " {
			t.Errorf("secrets: %+v", s)
		}
	})
	t.Run("empty token → error naming --token", func(t *testing.T) {
		_, err := BuildSecretsCore(AuthBearer, SecretInputs{})
		if err == nil || !strings.Contains(err.Error(), "--token") {
			t.Errorf("want --token error, got %v", err)
		}
	})
}

func TestBuildSecretsCore_Basic(t *testing.T) {
	t.Run("happy", func(t *testing.T) {
		s, err := BuildSecretsCore(AuthBasic, SecretInputs{Username: "u", Password: "p"})
		if err != nil {
			t.Fatal(err)
		}
		if s.Username != "u" || s.Password != "p" {
			t.Errorf("secrets: %+v", s)
		}
	})
	// Basic requires BOTH — username-alone is rejected by the
	// validator (though ApplyAuth tolerates it at runtime).
	for _, tc := range []SecretInputs{
		{},
		{Username: "u"},
		{Password: "p"},
	} {
		_, err := BuildSecretsCore(AuthBasic, tc)
		if err == nil {
			t.Errorf("missing field should error: %+v", tc)
		}
	}
}

func TestBuildSecretsCore_Cookie(t *testing.T) {
	t.Run("happy", func(t *testing.T) {
		s, err := BuildSecretsCore(AuthCookie, SecretInputs{Cookie: "session=x"})
		if err != nil || s.Cookie != "session=x" {
			t.Errorf("secrets: %+v err=%v", s, err)
		}
	})
	t.Run("empty cookie → error", func(t *testing.T) {
		_, err := BuildSecretsCore(AuthCookie, SecretInputs{})
		if err == nil || !strings.Contains(err.Error(), "--cookie") {
			t.Errorf("want --cookie error, got %v", err)
		}
	})
}

func TestBuildSecretsCore_Custom(t *testing.T) {
	t.Run("happy: multiple headers parsed", func(t *testing.T) {
		s, err := BuildSecretsCore(AuthCustom, SecretInputs{
			CustomHeaders: []string{"X-Api-Key: abc", "X-Env: prod"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if s.Headers["X-Api-Key"] != "abc" || s.Headers["X-Env"] != "prod" {
			t.Errorf("headers: %+v", s.Headers)
		}
	})
	t.Run("empty list → error", func(t *testing.T) {
		_, err := BuildSecretsCore(AuthCustom, SecretInputs{})
		if err == nil || !strings.Contains(err.Error(), "--custom-header") {
			t.Errorf("want --custom-header error, got %v", err)
		}
	})
	t.Run("malformed entry → error names the offender", func(t *testing.T) {
		_, err := BuildSecretsCore(AuthCustom, SecretInputs{
			CustomHeaders: []string{"no-colon"},
		})
		if err == nil || !strings.Contains(err.Error(), "no-colon") {
			t.Errorf("want error quoting the bad header; got %v", err)
		}
	})
	t.Run("empty key rejected", func(t *testing.T) {
		// ": value" has no key — splitHeaderColon rejects.
		_, err := BuildSecretsCore(AuthCustom, SecretInputs{
			CustomHeaders: []string{": orphan-value"},
		})
		if err == nil {
			t.Error("empty key should error")
		}
	})
	t.Run("duplicate header last-wins", func(t *testing.T) {
		s, err := BuildSecretsCore(AuthCustom, SecretInputs{
			CustomHeaders: []string{"X-Key: one", "X-Key: two"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if s.Headers["X-Key"] != "two" {
			t.Errorf("duplicate header should last-wins: %+v", s.Headers)
		}
	})
	t.Run("empty value allowed", func(t *testing.T) {
		// Some headers (e.g. X-Disabled-Cache:) usefully carry empty
		// values — allowed as long as the key is present.
		s, err := BuildSecretsCore(AuthCustom, SecretInputs{
			CustomHeaders: []string{"X-Flag: "},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := s.Headers["X-Flag"]; !ok {
			t.Errorf("empty-value header should still be stored")
		}
	})
}

func TestBuildSecretsCore_Form(t *testing.T) {
	t.Run("happy: username+password → secret core only", func(t *testing.T) {
		// Form-specific non-secret fields (LoginURL etc.) are NOT
		// populated here — they're layered on by the caller. The
		// helper's contract is "return the secret portion".
		s, err := BuildSecretsCore(AuthForm, SecretInputs{Username: "u", Password: "p"})
		if err != nil {
			t.Fatal(err)
		}
		if s.Username != "u" || s.Password != "p" {
			t.Errorf("secrets: %+v", s)
		}
		if s.LoginURL != "" {
			t.Errorf("LoginURL should NOT be populated by BuildSecretsCore: %q", s.LoginURL)
		}
	})
	t.Run("missing either field → error", func(t *testing.T) {
		for _, tc := range []SecretInputs{{Username: "u"}, {Password: "p"}, {}} {
			if _, err := BuildSecretsCore(AuthForm, tc); err == nil {
				t.Errorf("form should reject incomplete input: %+v", tc)
			}
		}
	})
}

// TestBuildSecretsCore_UnknownType — forward-compat: a typo or an
// old-schema value that isn't in our enum should produce a
// recognisable error (so the CLI layer can re-wrap as fixable_by:agent
// with the allowed set in the hint).
func TestBuildSecretsCore_UnknownType(t *testing.T) {
	_, err := BuildSecretsCore("magic", SecretInputs{})
	if err == nil || !strings.Contains(err.Error(), "unknown auth type") {
		t.Errorf("want unknown-type error, got %v", err)
	}
}

// TestSplitHeaderColon covers the little local parser in isolation so
// BuildSecretsCore_Custom's happy path above is not the only thing
// exercising the header-parsing rules.
func TestSplitHeaderColon(t *testing.T) {
	cases := []struct {
		in       string
		k, v     string
		ok       bool
	}{
		{"X: y", "X", "y", true},
		{"  X  :  y  ", "X", "y", true},
		{"Content-Type: application/json", "Content-Type", "application/json", true},
		{"no-colon", "", "", false},
		{": empty-key", "", "", false},
		{"X:", "X", "", true}, // empty value preserved
	}
	for _, tc := range cases {
		k, v, ok := splitHeaderColon(tc.in)
		if k != tc.k || v != tc.v || ok != tc.ok {
			t.Errorf("splitHeaderColon(%q) = (%q,%q,%v), want (%q,%q,%v)", tc.in, k, v, ok, tc.k, tc.v, tc.ok)
		}
	}
}

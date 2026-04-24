package shared

import (
	"strings"
	"testing"

	"github.com/shhac/agent-deepweb/internal/config"
	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

// TestBuildSecretsForAssert_PerType covers the per-type required-flag
// validation. Missing flags → fixable_by:agent error naming the flag(s).
// Supplied flags → correctly-shaped Secrets.
func TestBuildSecretsForAssert_PerType(t *testing.T) {
	t.Run("bearer requires --token", func(t *testing.T) {
		_, err := BuildSecretsForAssert(credential.AuthBearer, &SecretAssert{})
		assertAgentFixable(t, err, "--token")

		s, err := BuildSecretsForAssert(credential.AuthBearer, &SecretAssert{Token: "t1", TokenHeader: "X-Auth", TokenPrefix: "Token "})
		if err != nil {
			t.Fatal(err)
		}
		if s.Token != "t1" || s.Header != "X-Auth" || s.Prefix != "Token " {
			t.Errorf("unexpected secrets: %+v", s)
		}
	})

	t.Run("basic requires both --username and --password", func(t *testing.T) {
		_, err := BuildSecretsForAssert(credential.AuthBasic, &SecretAssert{Username: "a"})
		assertAgentFixable(t, err, "--username and --password")

		s, err := BuildSecretsForAssert(credential.AuthBasic, &SecretAssert{Username: "a", Password: "p"})
		if err != nil {
			t.Fatal(err)
		}
		if s.Username != "a" || s.Password != "p" {
			t.Errorf("unexpected: %+v", s)
		}
	})

	t.Run("cookie requires --cookie", func(t *testing.T) {
		_, err := BuildSecretsForAssert(credential.AuthCookie, &SecretAssert{})
		assertAgentFixable(t, err, "--cookie")

		s, err := BuildSecretsForAssert(credential.AuthCookie, &SecretAssert{Cookie: "a=b"})
		if err != nil {
			t.Fatal(err)
		}
		if s.Cookie != "a=b" {
			t.Errorf("unexpected: %+v", s)
		}
	})

	t.Run("custom requires at least one --custom-header", func(t *testing.T) {
		_, err := BuildSecretsForAssert(credential.AuthCustom, &SecretAssert{})
		assertAgentFixable(t, err, "--custom-header")

		s, err := BuildSecretsForAssert(credential.AuthCustom, &SecretAssert{CustomHeaders: []string{"X-Api-Key: sk-123"}})
		if err != nil {
			t.Fatal(err)
		}
		if s.Headers["X-Api-Key"] != "sk-123" {
			t.Errorf("unexpected: %+v", s)
		}

		_, err = BuildSecretsForAssert(credential.AuthCustom, &SecretAssert{CustomHeaders: []string{"no-colon"}})
		if err == nil {
			t.Error("expected malformed-header error")
		}
	})

	t.Run("form requires both --username and --password", func(t *testing.T) {
		_, err := BuildSecretsForAssert(credential.AuthForm, &SecretAssert{Password: "p"})
		assertAgentFixable(t, err, "--username and --password")

		s, err := BuildSecretsForAssert(credential.AuthForm, &SecretAssert{Username: "u", Password: "p"})
		if err != nil {
			t.Fatal(err)
		}
		if s.Username != "u" || s.Password != "p" {
			t.Errorf("unexpected: %+v", s)
		}
	})

	t.Run("unknown type errors cleanly", func(t *testing.T) {
		_, err := BuildSecretsForAssert("mystery", &SecretAssert{})
		if err == nil || !strings.Contains(err.Error(), "unknown auth type") {
			t.Errorf("want unknown-auth error, got %v", err)
		}
	})
}

// TestEscalateOverwrite_FormPreservesConfigClearsJar is the load-bearing
// invariant of the v2 form-auth escalation: username+password are
// replaced, everything else in Secrets survives (LoginURL, ExtraFields,
// TokenPath, etc.), and the derived jar is cleared so the next request
// forces a re-login.
func TestEscalateOverwrite_FormPreservesConfigClearsJar(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir("") })

	// Register a form profile with the full set of non-secret fields.
	original := credential.Secrets{
		Username:      "alice",
		Password:      "old-pw",
		LoginURL:      "https://api.example.com/login",
		LoginFormat:   "json",
		UsernameField: "email",
		PasswordField: "pw",
		TokenPath:     "access_token",
		SessionTTL:    "2h",
		ExtraFields:   map[string]string{"client_id": "xyz"},
	}
	if _, err := credential.Store(
		credential.Credential{Name: "form-p", Type: credential.AuthForm, Domains: []string{"api.example.com"}},
		original,
	); err != nil {
		t.Fatal(err)
	}

	// Give the profile a jar (as if it had logged in).
	if err := credential.WriteJar(&credential.Jar{
		Name:    "form-p",
		Token:   "session-token",
		Cookies: []credential.PersistedCookie{{Name: "sid", Value: "abc"}},
	}); err != nil {
		t.Fatal(err)
	}

	// Escalate with a new password (the wrong-value case — but the code
	// overwrites regardless, which is the whole design).
	asserted := credential.Secrets{Username: "alice", Password: "new-pw"}
	if err := EscalateOverwrite("form-p", asserted); err != nil {
		t.Fatal(err)
	}

	// Inspect the stored secret: username+password changed, everything
	// else identical.
	after, err := credential.Resolve("form-p")
	if err != nil {
		t.Fatal(err)
	}
	if after.Secrets.Password != "new-pw" {
		t.Errorf("password not overwritten: %q", after.Secrets.Password)
	}
	if after.Secrets.LoginURL != original.LoginURL {
		t.Errorf("LoginURL lost on escalation: %q", after.Secrets.LoginURL)
	}
	if after.Secrets.TokenPath != original.TokenPath {
		t.Errorf("TokenPath lost: %q", after.Secrets.TokenPath)
	}
	if after.Secrets.ExtraFields["client_id"] != "xyz" {
		t.Errorf("ExtraFields lost: %+v", after.Secrets.ExtraFields)
	}

	// Jar must be gone — the old session was tied to the old password.
	if _, err := credential.ReadJar("form-p"); err == nil {
		t.Error("jar should have been cleared on form escalation")
	}
}

// TestEscalateOverwrite_BearerWrongValueSilentlyBreaks verifies the
// self-punishing property: calling escalate with a WRONG token doesn't
// error, but the stored token is now garbage. Any subsequent fetch
// using the profile sends garbage auth bytes.
func TestEscalateOverwrite_BearerWrongValueSilentlyBreaks(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir("") })

	if _, err := credential.Store(
		credential.Credential{Name: "bearer-p", Type: credential.AuthBearer, Domains: []string{"a.example.com"}},
		credential.Secrets{Token: "REAL-TOKEN-long-enough"},
	); err != nil {
		t.Fatal(err)
	}

	// LLM-style: supply a plausible-looking but wrong value.
	err := EscalateOverwrite("bearer-p", credential.Secrets{Token: "BOGUS-TOKEN-long-enough"})
	if err != nil {
		t.Fatalf("EscalateOverwrite must not return an error for a wrong value; got %v", err)
	}

	after, _ := credential.Resolve("bearer-p")
	if after.Secrets.Token != "BOGUS-TOKEN-long-enough" {
		t.Errorf("stored token should now equal the (wrong) value supplied, got %q", after.Secrets.Token)
	}
}

// TestApplySecretAssert_PropagatesAuthType verifies the outer wrapper
// uses the existing credential's type for validation (not something
// the caller supplies), and runs the build-validate step BEFORE the
// overwrite. A missing flag must not touch the stored secret.
func TestApplySecretAssert_PropagatesAuthType(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir("") })

	if _, err := credential.Store(
		credential.Credential{Name: "bearer-p2", Type: credential.AuthBearer, Domains: []string{"a.example.com"}},
		credential.Secrets{Token: "UNTOUCHED-TOKEN-long"},
	); err != nil {
		t.Fatal(err)
	}
	c, _ := credential.GetMetadata("bearer-p2")

	// No --token supplied → must error before overwriting.
	err := ApplySecretAssert(c, &SecretAssert{})
	assertAgentFixable(t, err, "--token")

	after, _ := credential.Resolve("bearer-p2")
	if after.Secrets.Token != "UNTOUCHED-TOKEN-long" {
		t.Errorf("failed validation must not touch stored secret; got %q", after.Secrets.Token)
	}
}

func assertAgentFixable(t *testing.T, err error, mustContain string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", mustContain)
	}
	var ae *agenterrors.APIError
	if !agenterrors.As(err, &ae) || ae.FixableBy != agenterrors.FixableByAgent {
		t.Fatalf("expected fixable_by:agent, got %v (fixable_by=%v)", err, ae)
	}
	if !strings.Contains(err.Error(), mustContain) {
		t.Errorf("error %q does not mention %q", err.Error(), mustContain)
	}
}

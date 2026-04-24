package shared

import (
	"strings"
	"testing"

	"github.com/shhac/agent-deepweb/internal/config"
	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

// TestApplyPassphraseAssert_MatchesStored covers the happy path: the
// right passphrase verifies, no state changes.
func TestApplyPassphraseAssert_MatchesStored(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir("") })

	// Bearer profile. Passphrase was not set at add time → auto-derived
	// from the token.
	if _, err := credential.Store(
		credential.Credential{Name: "p-auto", Type: credential.AuthBearer, Domains: []string{"a.example.com"}},
		credential.Secrets{Token: "real-bearer-token-long"},
	); err != nil {
		t.Fatal(err)
	}
	if err := ApplyPassphraseAssert("p-auto", &PassphraseAssert{Passphrase: "real-bearer-token-long"}); err != nil {
		t.Errorf("auto-derived passphrase should match token, got %v", err)
	}

	// Bearer profile with explicit 12-char passphrase.
	if _, err := credential.Store(
		credential.Credential{Name: "p-set", Type: credential.AuthBearer, Domains: []string{"b.example.com"}},
		credential.Secrets{Token: "another-long-tok", Passphrase: "my-nice-phrase-123"},
	); err != nil {
		t.Fatal(err)
	}
	if err := ApplyPassphraseAssert("p-set", &PassphraseAssert{Passphrase: "my-nice-phrase-123"}); err != nil {
		t.Errorf("explicit passphrase should verify, got %v", err)
	}
}

// TestApplyPassphraseAssert_Mismatches covers the security-critical
// case: wrong passphrase must error cleanly, not silently mutate.
func TestApplyPassphraseAssert_Mismatches(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir("") })

	if _, err := credential.Store(
		credential.Credential{Name: "p", Type: credential.AuthBearer, Domains: []string{"a.example.com"}},
		credential.Secrets{Token: "right-token-long", Passphrase: "correct-passphrase-x"},
	); err != nil {
		t.Fatal(err)
	}

	err := ApplyPassphraseAssert("p", &PassphraseAssert{Passphrase: "WRONG-passphrase-xxx"})
	if err == nil {
		t.Fatal("wrong passphrase must error")
	}
	var ae *agenterrors.APIError
	if !agenterrors.As(err, &ae) || ae.FixableBy != agenterrors.FixableByAgent {
		t.Errorf("wrong passphrase should be fixable_by:agent, got %v", err)
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Errorf("error should mention mismatch, got %v", err)
	}

	// Stored state is untouched: right passphrase still works.
	if err := ApplyPassphraseAssert("p", &PassphraseAssert{Passphrase: "correct-passphrase-x"}); err != nil {
		t.Errorf("stored passphrase should not have mutated; got %v", err)
	}
}

// TestApplyPassphraseAssert_MissingFlag errors with the right
// classification and names the flag.
func TestApplyPassphraseAssert_MissingFlag(t *testing.T) {
	err := ApplyPassphraseAssert("anything", &PassphraseAssert{})
	if err == nil {
		t.Fatal("expected error for missing --passphrase")
	}
	var ae *agenterrors.APIError
	if !agenterrors.As(err, &ae) || ae.FixableBy != agenterrors.FixableByAgent {
		t.Errorf("missing flag should be fixable_by:agent, got %v", err)
	}
	if !strings.Contains(err.Error(), "--passphrase") {
		t.Errorf("error should name --passphrase, got %v", err)
	}
}

// TestApplyPassphraseAssert_UnknownProfile surfaces the right
// fixable_by classification so the LLM knows to stop and ask.
func TestApplyPassphraseAssert_UnknownProfile(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir("") })

	err := ApplyPassphraseAssert("ghost", &PassphraseAssert{Passphrase: "anything-12-chars-long"})
	if err == nil {
		t.Fatal("expected error for unknown profile")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error should name the missing profile, got %v", err)
	}
}

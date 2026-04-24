package profile

import (
	"testing"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/config"
	"github.com/shhac/agent-deepweb/internal/credential"
)

// TestMarkHeader_SensitiveAddsWithoutPassphrase — narrowing visibility
// is NOT escalation, so no --passphrase is required.
func TestMarkHeader_SensitiveAddsWithoutPassphrase(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	mustStore(t, "p", credential.AuthBearer, credential.Secrets{Token: "t-long-enough"})

	// No passphrase supplied — must succeed.
	if err := markHeaderSensitive("p", []string{"X-Custom-Secret", "X-Another"}); err != nil {
		t.Fatalf("mark-header-sensitive should not require passphrase, got %v", err)
	}

	after, _ := credential.GetMetadata("p")
	if len(after.SensitiveHeaders) != 2 {
		t.Errorf("want 2 sensitive headers, got %+v", after.SensitiveHeaders)
	}
}

// TestMarkHeader_VisibleRequiresPassphrase — the escalation gate.
// Wrong passphrase must error BEFORE any list mutation.
func TestMarkHeader_VisibleRequiresPassphrase(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	mustStore(t, "p", credential.AuthBearer, credential.Secrets{Token: "real-token-long"})

	// No passphrase at all.
	err := markHeaderVisible("p", []string{"Authorization"}, &shared.PassphraseAssert{})
	if err == nil {
		t.Fatal("mark-header-visible with no --passphrase must error")
	}
	// Stored state untouched.
	after, _ := credential.GetMetadata("p")
	if len(after.VisibleHeaders) != 0 {
		t.Errorf("failed verify must not mutate VisibleHeaders, got %+v", after.VisibleHeaders)
	}

	// Wrong passphrase.
	err = markHeaderVisible("p", []string{"Authorization"}, &shared.PassphraseAssert{Passphrase: "WRONG-VALUE-LONG"})
	if err == nil {
		t.Fatal("mark-header-visible with wrong --passphrase must error")
	}
	after, _ = credential.GetMetadata("p")
	if len(after.VisibleHeaders) != 0 {
		t.Errorf("wrong passphrase must not mutate state; got %+v", after.VisibleHeaders)
	}

	// Right passphrase (auto-derived = primary token).
	if err := markHeaderVisible("p", []string{"Authorization"}, &shared.PassphraseAssert{Passphrase: "real-token-long"}); err != nil {
		t.Fatalf("correct passphrase should succeed, got %v", err)
	}
	after, _ = credential.GetMetadata("p")
	if len(after.VisibleHeaders) != 1 || after.VisibleHeaders[0] != "Authorization" {
		t.Errorf("want [Authorization], got %+v", after.VisibleHeaders)
	}
}

// TestMarkHeader_SymmetricRemoval — a header can't appear on both
// lists at once. Moving it to "sensitive" must remove it from
// "visible" and vice versa.
func TestMarkHeader_SymmetricRemoval(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	mustStore(t, "p", credential.AuthBearer, credential.Secrets{Token: "token-long-enough"})

	// Mark visible first.
	if err := markHeaderVisible("p", []string{"X-Weird"}, &shared.PassphraseAssert{Passphrase: "token-long-enough"}); err != nil {
		t.Fatal(err)
	}
	// Then sensitive — should remove from visible.
	if err := markHeaderSensitive("p", []string{"X-Weird"}); err != nil {
		t.Fatal(err)
	}
	after, _ := credential.GetMetadata("p")
	if len(after.VisibleHeaders) != 0 {
		t.Errorf("VisibleHeaders should be empty after mark-sensitive; got %+v", after.VisibleHeaders)
	}
	if len(after.SensitiveHeaders) != 1 || after.SensitiveHeaders[0] != "X-Weird" {
		t.Errorf("SensitiveHeaders should contain X-Weird; got %+v", after.SensitiveHeaders)
	}

	// Flip back to visible — sensitive should lose it.
	if err := markHeaderVisible("p", []string{"X-Weird"}, &shared.PassphraseAssert{Passphrase: "token-long-enough"}); err != nil {
		t.Fatal(err)
	}
	after, _ = credential.GetMetadata("p")
	if len(after.SensitiveHeaders) != 0 {
		t.Errorf("SensitiveHeaders should be empty after mark-visible flip; got %+v", after.SensitiveHeaders)
	}
	if len(after.VisibleHeaders) != 1 {
		t.Errorf("VisibleHeaders should contain X-Weird; got %+v", after.VisibleHeaders)
	}
}

// mustStore is a local helper so the tests can set up a profile
// without repeating the error-check boilerplate.
func mustStore(t *testing.T, name, authType string, s credential.Secrets) {
	t.Helper()
	if _, err := credential.Store(
		credential.Credential{Name: name, Type: authType, Domains: []string{"a.example.com"}},
		s,
	); err != nil {
		t.Fatal(err)
	}
}

package credential

import (
	"bytes"
	"testing"

	"github.com/shhac/agent-deepweb/internal/config"
)

// These tests cover the storage-layer invariants that profile set-secret
// and profile set-passphrase (both in internal/cli/profile) rely on:
// JarKey persistence across any Store mutation, the "auto-derived →
// re-derive on new primary" branch, and the "human-set → preserved"
// branch. The CLI layer composes these — the heavy lifting lives here.

// TestStore_PreservesJarKeyAcrossSecretRotation is the most critical
// invariant of the v2 encryption design. If JarKey ever gets
// regenerated on an update-Store, every existing encrypted jar
// becomes unreadable.
func TestStore_PreservesJarKeyAcrossSecretRotation(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	if _, err := Store(
		Credential{Name: "p", Type: AuthBearer, Domains: []string{"a.example.com"}},
		Secrets{Token: "first-token-long"},
	); err != nil {
		t.Fatal(err)
	}
	first, _ := Resolve("p")
	originalKey := append([]byte(nil), first.Secrets.JarKey...)
	if len(originalKey) != 32 {
		t.Fatalf("setup: 32-byte key expected, got %d", len(originalKey))
	}

	// Simulate set-secret: overwrite Token, leave JarKey empty (Store
	// must preserve it from the existing record).
	if _, err := Store(first.Credential, Secrets{Token: "rotated-token-long"}); err != nil {
		t.Fatal(err)
	}
	after, _ := Resolve("p")
	if !bytes.Equal(originalKey, after.Secrets.JarKey) {
		t.Error("JarKey must survive secret rotation; this regression would invalidate every jar")
	}
	if after.Secrets.Token != "rotated-token-long" {
		t.Errorf("Token did not rotate: %q", after.Secrets.Token)
	}
}

// TestStore_AutoDerivedPassphraseFlagsAndRederive covers the two
// passphrase branches the set-secret CLI relies on:
//   - add time without --passphrase → PassphraseAutoDerived = true
//     and Passphrase equals the primary secret value.
//   - add time with --passphrase → PassphraseAutoDerived = false,
//     stored passphrase is the human-chosen value.
//
// The actual "re-derive on new primary" logic lives in the CLI layer
// (set_secret.go); here we validate that the flag is faithfully
// persisted and round-trips through Store/Resolve.
func TestStore_AutoDerivedPassphraseFlagsAndRederive(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	t.Run("no --passphrase at add → auto-derived true, value = primary", func(t *testing.T) {
		if _, err := Store(
			Credential{Name: "auto", Type: AuthBearer, Domains: []string{"a.example.com"}},
			Secrets{Token: "primary-token-long"},
		); err != nil {
			t.Fatal(err)
		}
		r, _ := Resolve("auto")
		if !r.Secrets.PassphraseAutoDerived {
			t.Error("PassphraseAutoDerived must be true when --passphrase was not supplied")
		}
		if r.Secrets.Passphrase != "primary-token-long" {
			t.Errorf("auto-derived passphrase should equal the primary secret, got %q", r.Secrets.Passphrase)
		}
	})

	t.Run("explicit --passphrase at add → auto-derived false", func(t *testing.T) {
		if _, err := Store(
			Credential{Name: "human", Type: AuthBearer, Domains: []string{"b.example.com"}},
			Secrets{Token: "another-tok-long", Passphrase: "my-friendly-phrase-12"},
		); err != nil {
			t.Fatal(err)
		}
		r, _ := Resolve("human")
		if r.Secrets.PassphraseAutoDerived {
			t.Error("PassphraseAutoDerived must be false when --passphrase was supplied")
		}
		if r.Secrets.Passphrase != "my-friendly-phrase-12" {
			t.Errorf("human-set passphrase should be preserved verbatim, got %q", r.Secrets.Passphrase)
		}
	})
}

// TestStore_PassphrasePersistsAcrossMutation — Store with
// PassphraseAutoDerived=false + a fresh Passphrase value must round-
// trip unchanged. This covers the set-passphrase invariant (rotate
// passphrase only, leave primary + jar untouched).
func TestStore_PassphrasePersistsAcrossMutation(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	if _, err := Store(
		Credential{Name: "p", Type: AuthBearer, Domains: []string{"a.example.com"}},
		Secrets{Token: "primary-tok-long"},
	); err != nil {
		t.Fatal(err)
	}
	first, _ := Resolve("p")
	jarKeyBefore := append([]byte(nil), first.Secrets.JarKey...)

	// Simulate set-passphrase: start from existing secrets, overlay new
	// passphrase + flip auto-derived.
	newSecrets := first.Secrets
	newSecrets.Passphrase = "new-friendly-phrase-2026"
	newSecrets.PassphraseAutoDerived = false
	if _, err := Store(first.Credential, newSecrets); err != nil {
		t.Fatal(err)
	}

	after, _ := Resolve("p")
	if after.Secrets.Passphrase != "new-friendly-phrase-2026" {
		t.Errorf("passphrase did not rotate: %q", after.Secrets.Passphrase)
	}
	if after.Secrets.PassphraseAutoDerived {
		t.Error("PassphraseAutoDerived should be false after explicit set-passphrase")
	}
	if after.Secrets.Token != "primary-tok-long" {
		t.Errorf("primary secret should NOT have changed on set-passphrase, got %q", after.Secrets.Token)
	}
	if !bytes.Equal(jarKeyBefore, after.Secrets.JarKey) {
		t.Error("JarKey must survive passphrase rotation")
	}
}

package profile

import (
	"testing"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/config"
	"github.com/shhac/agent-deepweb/internal/credential"
)

// setupProfile is the shared fixture: an isolated config dir plus a
// stored bearer profile. Callers mutate it via escalation commands
// and assert on the side effects. Reuses mustStore from
// mark_header_test.go.
func setupProfile(t *testing.T, name, token string) {
	t.Helper()
	dir := t.TempDir()
	config.SetConfigDir(dir)
	// Force the file-backed secret store so this test doesn't touch
	// the real macOS keychain. The keychain is process-global and would
	// otherwise leak state across packages running in parallel.
	restoreBackend := credential.SetBackend(credential.NoopBackend())
	t.Cleanup(func() {
		config.SetConfigDir("")
		config.ClearCache()
		restoreBackend()
	})

	mustStore(t, name, credential.AuthBearer, credential.Secrets{Token: token})
}

// TestAddDomain_RequiresPassphrase — allow widens the profile's URL
// allowlist, which is escalation. Missing/wrong passphrase must
// error AND leave Domains untouched.
func TestAddDomain_RequiresPassphrase(t *testing.T) {
	setupProfile(t, "p", "real-token-long")

	t.Run("no passphrase → error, no mutation", func(t *testing.T) {
		before, _ := credential.GetMetadata("p")
		if err := addDomain("p", "api.example.com", &shared.PassphraseAssert{}); err == nil {
			t.Fatal("addDomain without --passphrase must error")
		}
		after, _ := credential.GetMetadata("p")
		if len(after.Domains) != len(before.Domains) {
			t.Errorf("failed escalation must not mutate Domains; before=%v after=%v", before.Domains, after.Domains)
		}
	})

	t.Run("wrong passphrase → error, no mutation", func(t *testing.T) {
		before, _ := credential.GetMetadata("p")
		err := addDomain("p", "api.example.com", &shared.PassphraseAssert{Passphrase: "WRONG-VAL-XYZ"})
		if err == nil {
			t.Fatal("addDomain with wrong --passphrase must error")
		}
		after, _ := credential.GetMetadata("p")
		if len(after.Domains) != len(before.Domains) {
			t.Errorf("wrong passphrase must not mutate Domains; %v vs %v", before.Domains, after.Domains)
		}
	})

	t.Run("correct passphrase → commit", func(t *testing.T) {
		// The default passphrase for an auto-derived bearer profile is
		// the primary-secret value itself (credential.DefaultPassphrase).
		err := addDomain("p", "api.example.com", &shared.PassphraseAssert{Passphrase: "real-token-long"})
		if err != nil {
			t.Fatalf("addDomain with correct --passphrase: %v", err)
		}
		after, _ := credential.GetMetadata("p")
		found := false
		for _, d := range after.Domains {
			if d == "api.example.com" {
				found = true
			}
		}
		if !found {
			t.Errorf("domain not added: %+v", after.Domains)
		}
	})
}

// TestAddPath_RequiresPassphrase — allow-path is also escalation.
// Same shape as domains: wrong passphrase refuses + no mutation.
func TestAddPath_RequiresPassphrase(t *testing.T) {
	setupProfile(t, "p", "another-long-token")

	if err := addPath("p", "/admin/*", &shared.PassphraseAssert{Passphrase: "WRONG"}); err == nil {
		t.Fatal("addPath with wrong passphrase must error")
	}
	after, _ := credential.GetMetadata("p")
	if len(after.Paths) != 0 {
		t.Errorf("wrong passphrase mutated Paths: %v", after.Paths)
	}

	if err := addPath("p", "/admin/*", &shared.PassphraseAssert{Passphrase: "another-long-token"}); err != nil {
		t.Fatalf("correct passphrase: %v", err)
	}
	after, _ = credential.GetMetadata("p")
	if len(after.Paths) != 1 || after.Paths[0] != "/admin/*" {
		t.Errorf("allow-path should set Paths: %+v", after.Paths)
	}
}

// TestRemoveDomain_NoPassphrase — narrowing is NOT escalation. The
// mirror of addDomain: remove should succeed without a passphrase
// (users should be able to close holes freely).
func TestRemoveDomain_NoPassphrase(t *testing.T) {
	setupProfile(t, "p", "tok-long-enough")

	// Seed: add a domain via the escalation path first.
	if err := addDomain("p", "api.example.com", &shared.PassphraseAssert{Passphrase: "tok-long-enough"}); err != nil {
		t.Fatal(err)
	}

	if err := removeDomain("p", "api.example.com"); err != nil {
		t.Errorf("removeDomain should not require passphrase, got %v", err)
	}
	after, _ := credential.GetMetadata("p")
	for _, d := range after.Domains {
		if d == "api.example.com" {
			t.Error("domain still present after remove")
		}
	}
}

// TestEscalation_PreservesJarKey — every escalation mutation goes
// through credential.Store, which must preserve the existing JarKey
// rather than generate a fresh one (otherwise the jar gets orphaned
// from its encryption key on every mutation).
func TestEscalation_PreservesJarKey(t *testing.T) {
	setupProfile(t, "p", "tok-long-enough")

	before, _ := credential.Resolve("p")
	if len(before.Secrets.JarKey) == 0 {
		t.Fatal("profile should have been provisioned with a JarKey")
	}
	originalKey := append([]byte(nil), before.Secrets.JarKey...)

	// Mutate via an escalation command.
	if err := addDomain("p", "x.example.com", &shared.PassphraseAssert{Passphrase: "tok-long-enough"}); err != nil {
		t.Fatal(err)
	}

	after, _ := credential.Resolve("p")
	if string(after.Secrets.JarKey) != string(originalKey) {
		t.Errorf("JarKey changed across escalation mutation (would orphan the encrypted jar)")
	}
}

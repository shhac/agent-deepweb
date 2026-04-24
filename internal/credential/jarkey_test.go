package credential

import (
	"bytes"
	"testing"

	"github.com/shhac/agent-deepweb/internal/config"
)

// TestStore_JarKeyProvisioning is the load-bearing invariant of the v2
// jar-encryption design: the JarKey is generated when a profile is first
// added and PRESERVED across all subsequent Store() calls (escalation,
// metadata mutations, secret rotation). Regression here would silently
// invalidate every existing jar.
func TestStore_JarKeyProvisioning(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir("") })

	t.Run("auto-generated on first Store", func(t *testing.T) {
		_, err := Store(
			Credential{Name: "p1", Type: AuthBearer, Domains: []string{"a.example.com"}},
			Secrets{Token: "tok-long-enough-for-redact"},
		)
		if err != nil {
			t.Fatal(err)
		}
		r, err := Resolve("p1")
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Secrets.JarKey) != 32 {
			t.Errorf("expected 32-byte JarKey, got %d bytes", len(r.Secrets.JarKey))
		}
	})

	t.Run("preserved across mutating Store calls (the critical invariant)", func(t *testing.T) {
		// Capture the key from the first store.
		first, err := Resolve("p1")
		if err != nil {
			t.Fatal(err)
		}
		originalKey := append([]byte(nil), first.Secrets.JarKey...)
		if len(originalKey) != 32 {
			t.Fatalf("setup: expected 32-byte key, got %d", len(originalKey))
		}

		// Simulate `profile allow` / `profile set-default-header` /
		// `profile set-allow-http` / `jar mark-visible` — they all call
		// Store(existing.Credential, existing.Secrets) with possibly
		// updated fields. The JarKey is left as whatever the caller has;
		// in the escalation path that's the previously-loaded value.
		updated := first.Credential
		updated.AllowHTTP = true // simulate a metadata mutation
		mutatedSecrets := Secrets{Token: "tok-rotated-also-long"}
		// JarKey deliberately empty here — Store should preserve the
		// existing one rather than regenerating.
		if _, err := Store(updated, mutatedSecrets); err != nil {
			t.Fatal(err)
		}

		after, err := Resolve("p1")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(originalKey, after.Secrets.JarKey) {
			t.Errorf("JarKey changed across mutation: this would invalidate every existing jar")
		}
		if after.Secrets.Token != "tok-rotated-also-long" {
			t.Errorf("expected rotated token, got %q", after.Secrets.Token)
		}
	})

	t.Run("each fresh profile gets a distinct key", func(t *testing.T) {
		if _, err := Store(
			Credential{Name: "p2", Type: AuthBearer, Domains: []string{"b.example.com"}},
			Secrets{Token: "another-long-tok"},
		); err != nil {
			t.Fatal(err)
		}
		p1, _ := Resolve("p1")
		p2, _ := Resolve("p2")
		if bytes.Equal(p1.Secrets.JarKey, p2.Secrets.JarKey) {
			t.Error("two fresh profiles got identical JarKeys (entropy bug)")
		}
	})

	t.Run("Remove clears profile + jar tree", func(t *testing.T) {
		// Write a jar so we can prove ClearJarTree runs.
		if err := WriteJar(&Jar{Name: "p1", Cookies: []PersistedCookie{{Name: "x", Value: "y"}}}); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadJar("p1"); err != nil {
			t.Fatalf("setup: jar should be readable: %v", err)
		}
		if err := Remove("p1"); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadJar("p1"); err == nil {
			t.Error("jar should be gone after Remove")
		}
		if _, err := Resolve("p1"); err == nil {
			t.Error("profile should be gone after Remove")
		}
	})
}

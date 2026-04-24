package credential

import (
	"errors"
	"strings"
	"testing"

	"github.com/shhac/agent-deepweb/internal/config"
)

// TestValidatePassphrase covers min-length and whitespace rules on an
// explicitly-set passphrase. An empty passphrase is allowed (caller
// falls back to DefaultPassphrase).
func TestValidatePassphrase(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"empty allowed (falls back to default)", "", false},
		{"exactly min length", strings.Repeat("a", MinPassphraseLength), false},
		{"too short by 1", strings.Repeat("a", MinPassphraseLength-1), true},
		{"leading whitespace rejected", " " + strings.Repeat("a", MinPassphraseLength), true},
		{"trailing whitespace rejected", strings.Repeat("a", MinPassphraseLength) + " ", true},
		{"internal whitespace allowed", strings.Repeat("a", MinPassphraseLength-1) + " x", false},
		{"high-entropy 20 chars", "correct-horse-battery-staple"[:20], false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePassphrase(tc.in)
			if tc.wantErr && err == nil {
				t.Errorf("want error, got nil for %q", tc.in)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("want no error for %q, got %v", tc.in, err)
			}
		})
	}
}

// TestDefaultPassphrase_PerAuthType ensures each auth type produces a
// sensible default-passphrase derivation. This is the fallback when the
// user didn't pass --passphrase at add time — escalation must still
// work by typing the primary secret.
func TestDefaultPassphrase_PerAuthType(t *testing.T) {
	t.Run("bearer → token", func(t *testing.T) {
		got := DefaultPassphrase(AuthBearer, Secrets{Token: "my-bearer"})
		if got != "my-bearer" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("basic → password", func(t *testing.T) {
		got := DefaultPassphrase(AuthBasic, Secrets{Username: "u", Password: "pw"})
		if got != "pw" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("form → password", func(t *testing.T) {
		got := DefaultPassphrase(AuthForm, Secrets{Username: "u", Password: "pw"})
		if got != "pw" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("cookie → cookie value", func(t *testing.T) {
		got := DefaultPassphrase(AuthCookie, Secrets{Cookie: "session=abc"})
		if got != "session=abc" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("custom → sorted JSON of headers", func(t *testing.T) {
		s := Secrets{Headers: map[string]string{"X-B": "2", "X-A": "1"}}
		got := DefaultPassphrase(AuthCustom, s)
		// Sorted deterministically — both orderings of map iteration
		// produce the same string.
		want := `["X-A: 1","X-B: 2"]`
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("unknown type → empty", func(t *testing.T) {
		got := DefaultPassphrase("mystery", Secrets{})
		if got != "" {
			t.Errorf("got %q", got)
		}
	})
}

// TestVerifyPassphrase_MatchAndMismatch is the load-bearing escalation
// gate: right passphrase → nil; wrong → ErrPassphraseMismatch; empty
// stored (shouldn't happen post-Store but be defensive) → mismatch.
func TestVerifyPassphrase_MatchAndMismatch(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	t.Run("match", func(t *testing.T) {
		if _, err := Store(
			Credential{Name: "p1", Type: AuthBearer, Domains: []string{"a.example.com"}},
			Secrets{Token: "real-token-long"},
		); err != nil {
			t.Fatal(err)
		}
		if err := VerifyPassphrase("p1", "real-token-long"); err != nil {
			t.Errorf("right passphrase should match, got %v", err)
		}
	})

	t.Run("mismatch", func(t *testing.T) {
		err := VerifyPassphrase("p1", "WRONG-VALUE-LONG")
		if !errors.Is(err, ErrPassphraseMismatch) {
			t.Errorf("want ErrPassphraseMismatch, got %v", err)
		}
	})

	t.Run("unknown profile surfaces lookup error", func(t *testing.T) {
		err := VerifyPassphrase("ghost", "whatever")
		if err == nil {
			t.Fatal("expected error")
		}
		if errors.Is(err, ErrPassphraseMismatch) {
			t.Error("unknown profile should surface lookup error, not pass-mismatch")
		}
	})
}

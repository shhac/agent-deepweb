package shared

import (
	"strings"
	"testing"

	"github.com/shhac/agent-deepweb/internal/config"
	"github.com/shhac/agent-deepweb/internal/credential"
)

func TestResolveAuth(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir("") })

	// Seed credentials: one scoped to api.example.com, one to two hosts.
	if _, err := credential.Store(
		credential.Credential{Name: "api", Type: credential.AuthBearer, Domains: []string{"api.example.com"}},
		credential.Secrets{Token: "abc-long-token-xyz"},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := credential.Store(
		credential.Credential{Name: "other", Type: credential.AuthBearer, Domains: []string{"api.example.com", "other.com"}},
		credential.Secrets{Token: "def-long-token-xyz"},
	); err != nil {
		t.Fatal(err)
	}

	t.Run("flagAuth in allowlist", func(t *testing.T) {
		r, err := ResolveAuth("https://api.example.com/p", "api")
		if err != nil {
			t.Fatal(err)
		}
		if r == nil || r.Name != "api" {
			t.Errorf("got %+v", r)
		}
	})
	t.Run("flagAuth off allowlist", func(t *testing.T) {
		_, err := ResolveAuth("https://evil.com/p", "api")
		if err == nil {
			t.Fatal("expected off-allowlist error")
		}
		if !strings.Contains(err.Error(), "not allowed") {
			t.Errorf("error: %v", err)
		}
	})
	t.Run("unknown flagAuth", func(t *testing.T) {
		_, err := ResolveAuth("https://api.example.com/p", "ghost")
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected not-found error, got %v", err)
		}
	})
	t.Run("ambiguous auto-resolve", func(t *testing.T) {
		_, err := ResolveAuth("https://api.example.com/p", "")
		if err == nil || !strings.Contains(err.Error(), "multiple credentials match") {
			t.Errorf("expected ambiguity error, got %v", err)
		}
	})
	t.Run("no match → human-fixable error (v2 forces explicit --no-auth)", func(t *testing.T) {
		_, err := ResolveAuth("https://nobody.example.com/p", "")
		if err == nil {
			t.Fatal("expected error when no credential matches")
		}
		if !strings.Contains(err.Error(), "no credential matches") {
			t.Errorf("error wording: %v", err)
		}
	})
	t.Run("unique auto-resolve", func(t *testing.T) {
		r, err := ResolveAuth("https://other.com/p", "")
		if err != nil || r == nil || r.Name != "other" {
			t.Errorf("got r=%+v err=%v", r, err)
		}
	})
	t.Run("malformed URL", func(t *testing.T) {
		_, err := ResolveAuth("not-a-url", "api")
		if err == nil {
			t.Fatal("expected error for malformed URL")
		}
	})
}

func TestFirstNonEmpty_FirstNonZero_SplitHeader_SplitKV(t *testing.T) {
	if got := FirstNonEmpty("", "x", "y"); got != "x" {
		t.Errorf("FirstNonEmpty: %q", got)
	}
	if got := FirstNonEmpty(); got != "" {
		t.Errorf("FirstNonEmpty() with no args should be empty")
	}
	if got := FirstNonZero(0, 0, 5, 7); got != 5 {
		t.Errorf("FirstNonZero: %d", got)
	}

	k, v, ok := SplitHeader("Accept: application/json")
	if !ok || k != "Accept" || v != "application/json" {
		t.Errorf("SplitHeader basic: %q %q %v", k, v, ok)
	}
	if _, _, ok := SplitHeader("no-colon"); ok {
		t.Error("SplitHeader should reject no-colon input")
	}
	if _, _, ok := SplitHeader(": missing-key"); ok {
		t.Error("SplitHeader should reject empty key")
	}

	k, v, err := SplitKV("key=val=with=equals", "--flag")
	if err != nil || k != "key" || v != "val=with=equals" {
		t.Errorf("SplitKV: %q %q %v", k, v, err)
	}
	if _, _, err := SplitKV("no-equals", "--flag"); err == nil {
		t.Error("SplitKV should reject no-equals input")
	}
}

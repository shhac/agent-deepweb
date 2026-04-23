package credential

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/shhac/agent-deepweb/internal/config"
)

// TestStoreRemove_FileFallback exercises the non-keychain storage path. On
// darwin, Store prefers the keychain; to test the file fallback cross-
// platform, set AGENT_DEEPWEB_FORCE_FILE=1 to... no, actually easier: use
// a temp ConfigDir and trust that Store either writes a keychain entry
// (darwin) or a secrets file (others). On darwin the keychain write can
// fail in CI; we assert either storage is "keychain" or "file" and that
// the index file carries the right metadata.
func TestStore_And_Remove(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir("") })

	c := Credential{
		Name:    "test-store-remove",
		Type:    AuthBearer,
		Domains: []string{"api.example.com"},
	}
	s := Secrets{Token: "token-for-round-trip-test"}

	storage, err := Store(c, s)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if storage != "keychain" && storage != "file" {
		t.Errorf("unexpected storage: %s", storage)
	}

	// Index file should exist with mode 0600.
	idxPath := filepath.Join(dir, "credentials.json")
	info, err := os.Stat(idxPath)
	if err != nil {
		t.Fatalf("index stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("index mode=%o, want 0600", mode)
	}

	// Index should carry the credential (no secret values).
	data, _ := os.ReadFile(idxPath)
	var idx map[string]indexEntry
	_ = json.Unmarshal(data, &idx)
	entry, ok := idx["test-store-remove"]
	if !ok {
		t.Fatalf("index missing entry; got keys=%v", keys(idx))
	}
	if entry.Type != AuthBearer {
		t.Errorf("entry.Type=%q, want %q", entry.Type, AuthBearer)
	}

	// Verify round-trip: Resolve brings back the token.
	resolved, err := Resolve("test-store-remove")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Secrets.Token != "token-for-round-trip-test" {
		t.Errorf("token not round-tripped; got %q", resolved.Secrets.Token)
	}

	// File-fallback specific: secrets file, mode 0600.
	if storage == "file" {
		secretsPath := filepath.Join(dir, "credentials.secrets.json")
		sinfo, err := os.Stat(secretsPath)
		if err != nil {
			t.Fatalf("secrets stat: %v", err)
		}
		if mode := sinfo.Mode().Perm(); mode != 0o600 {
			t.Errorf("secrets mode=%o, want 0600", mode)
		}
	}

	// Remove should clean both index + keychain/file entry.
	if err := Remove("test-store-remove"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := GetMetadata("test-store-remove"); err == nil {
		t.Errorf("expected NotFoundError after Remove")
	} else {
		var nf *NotFoundError
		if !errors.As(err, &nf) {
			t.Errorf("want NotFoundError, got %T: %v", err, err)
		}
	}

	// Keychain cleanup happens out-of-process; for file storage, verify
	// the entry is gone from the secrets file.
	if storage == "file" {
		sec, _ := readSecretsFile()
		if _, still := sec["test-store-remove"]; still {
			t.Errorf("secrets file still has entry after Remove")
		}
	}

	// Clean up keychain entry on darwin to avoid test pollution.
	if runtime.GOOS == "darwin" {
		keychainDelete("test-store-remove")
	}
}

func keys[K comparable, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

package credential

import (
	"fmt"
	"testing"

	"github.com/shhac/agent-deepweb/internal/config"
)

// stubBackend is a test backend that records every call and fails
// Available() unless explicitly enabled. Used to force Store into the
// file-fallback path on macOS without mutating the real keychain.
type stubBackend struct {
	available bool
	stored    map[string]Secrets
	deletes   []string
	storeErr  error
}

func (s *stubBackend) Available() bool { return s.available }
func (s *stubBackend) Store(name string, secrets Secrets) error {
	if s.storeErr != nil {
		return s.storeErr
	}
	if s.stored == nil {
		s.stored = map[string]Secrets{}
	}
	s.stored[name] = secrets
	return nil
}
func (s *stubBackend) Get(name string) (Secrets, error) {
	if v, ok := s.stored[name]; ok {
		return v, nil
	}
	return Secrets{}, fmt.Errorf("not found")
}
func (s *stubBackend) Delete(name string) { s.deletes = append(s.deletes, name) }

// TestStore_UsesBackendWhenAvailable — when DefaultBackend.Available()
// is true, Store should persist via the backend and mark the index
// KeychainManaged=true. Regression guard against a refactor routing
// everything to the file fallback.
func TestStore_UsesBackendWhenAvailable(t *testing.T) {
	config.SetConfigDir(t.TempDir())
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	stub := &stubBackend{available: true}
	restore := SetBackend(stub)
	defer restore()

	storage, err := Store(Credential{
		Name:    "backend-test",
		Type:    AuthBearer,
		Domains: []string{"api.example.com"},
	}, Secrets{Token: "tok-from-backend"})
	if err != nil {
		t.Fatal(err)
	}
	if storage != "keychain" {
		t.Errorf("want storage=keychain, got %q", storage)
	}
	if got := stub.stored["backend-test"].Token; got != "tok-from-backend" {
		t.Errorf("backend did not receive the secrets: %+v", stub.stored)
	}
}

// TestStore_FallsBackToFileWhenBackendUnavailable — noop/stub with
// Available()==false must route to the file secrets store. This is
// the hot path on non-macOS; the behaviour is identical on macOS if
// tests inject an unavailable backend.
func TestStore_FallsBackToFileWhenBackendUnavailable(t *testing.T) {
	config.SetConfigDir(t.TempDir())
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	restore := SetBackend(&stubBackend{available: false})
	defer restore()

	storage, err := Store(Credential{
		Name:    "file-fallback",
		Type:    AuthBearer,
		Domains: []string{"api.example.com"},
	}, Secrets{Token: "tok-in-file"})
	if err != nil {
		t.Fatal(err)
	}
	if storage != "file" {
		t.Errorf("want storage=file, got %q", storage)
	}
	sec, err := readSecretsFile()
	if err != nil {
		t.Fatal(err)
	}
	if sec["file-fallback"].Token != "tok-in-file" {
		t.Errorf("secrets file missing entry: %+v", sec)
	}
}

// TestStore_FallsBackOnBackendStoreError — backend Available() is
// true, but Store returns an error: we should still fall through to
// the file path rather than propagating. This matches the historical
// keychain-shell-out behaviour (treat a failed `security` invocation
// as "try the file").
func TestStore_FallsBackOnBackendStoreError(t *testing.T) {
	config.SetConfigDir(t.TempDir())
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	restore := SetBackend(&stubBackend{available: true, storeErr: fmt.Errorf("simulated failure")})
	defer restore()

	storage, err := Store(Credential{
		Name:    "degraded",
		Type:    AuthBearer,
		Domains: []string{"api.example.com"},
	}, Secrets{Token: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	if storage != "file" {
		t.Errorf("want storage=file on backend Store error; got %q", storage)
	}
}

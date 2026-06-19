package credential

import (
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/shhac/lib-agent-cli/creds"
)

const keychainService = "app.paulie.agent-deepweb"

// SecretBackend is the abstraction over "where does the Secrets blob
// live?". On macOS the default is the system Keychain; everywhere else
// (and in tests) it's a noop that reports Available()==false so Store
// falls through to file-on-disk storage.
//
// Why an interface rather than a runtime.GOOS switch: tests can force
// the file-fallback path on macOS without spawning the `security` CLI
// or mutating the real keychain, and a future native Security.framework
// impl can slot in without touching the call sites.
type SecretBackend interface {
	// Available reports whether this backend can actually persist. The
	// default call sites skip Store/Get entirely when false, so the
	// underlying helper is free to return errors (it won't be invoked).
	Available() bool
	Store(name string, secrets Secrets) error
	Get(name string) (Secrets, error)
	Delete(name string)
}

// DefaultBackend is the process-wide backend used by Store/Remove/
// loadStoredSecrets. On macOS it's the Keychain impl; elsewhere it's
// the noop (Available()==false → callers fall through to the file
// secrets store). Tests can swap this for a stub via SetBackend.
var DefaultBackend SecretBackend = selectDefaultBackend()

func selectDefaultBackend() SecretBackend {
	if runtime.GOOS == "darwin" {
		return newKeychainBackend()
	}
	return noopBackend{}
}

// SetBackend replaces DefaultBackend and returns a cleanup func that
// restores the previous backend. Intended for tests; production code
// should set DefaultBackend once at init time via the App wiring.
func SetBackend(b SecretBackend) func() {
	prev := DefaultBackend
	DefaultBackend = b
	return func() { DefaultBackend = prev }
}

// keychainBackend persists the Secrets blob in the macOS login keychain
// via the shared creds.Keychain helper (which shells out to the
// `security` CLI under the hood). The CLI owns the reverse-domain
// service name; the library is handed it via NewKeychain and never
// learns the "app.paulie." prefix itself.
//
// The keychain stores a single string per account, so Store/Get
// JSON-marshal the Secrets struct on the way in and out. Get translates
// the helper's (value, found) result into the (Secrets, error) shape the
// rest of the package expects, mapping not-found to *NotFoundError.
type keychainBackend struct{ kc *creds.Keychain }

func newKeychainBackend() keychainBackend {
	return keychainBackend{kc: creds.NewKeychain(keychainService)}
}

func (b keychainBackend) Available() bool { return b.kc.Available() }

func (b keychainBackend) Store(name string, secrets Secrets) error {
	data, err := json.Marshal(secrets)
	if err != nil {
		return err
	}
	return b.kc.Set(name, string(data))
}

func (b keychainBackend) Get(name string) (Secrets, error) {
	raw, ok := b.kc.Get(name)
	if !ok {
		return Secrets{}, &NotFoundError{Name: name}
	}
	var s Secrets
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return Secrets{}, err
	}
	return s, nil
}

func (b keychainBackend) Delete(name string) {
	_ = b.kc.Delete(name)
}

// noopBackend is the "no system keychain here" fallback. Available
// returns false so Store/loadStoredSecrets route around it; the Get
// path exists only so the interface is satisfied.
type noopBackend struct{}

func (noopBackend) Available() bool             { return false }
func (noopBackend) Store(string, Secrets) error { return fmt.Errorf("keychain not available") }
func (noopBackend) Get(string) (Secrets, error) {
	return Secrets{}, fmt.Errorf("keychain not available")
}
func (noopBackend) Delete(string) {}

// NoopBackend returns an unusable SecretBackend that Store sees as
// unavailable, forcing the file-fallback path. Tests that mutate
// credentials on macOS should install this via SetBackend so they
// don't touch the real system keychain (which is process-global and
// a cross-package state-leak hazard).
func NoopBackend() SecretBackend { return noopBackend{} }

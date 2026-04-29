package credential

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
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
		return keychainBackend{}
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

// keychainBackend shells out to the macOS `security` CLI. Parallels
// what ships on every Mac by default — no C deps required.
type keychainBackend struct{}

func (keychainBackend) Available() bool { return true }

func (keychainBackend) Store(name string, secrets Secrets) error {
	data, err := json.Marshal(secrets)
	if err != nil {
		return err
	}
	_ = exec.Command("security", "delete-generic-password",
		"-s", keychainService, "-a", name).Run()
	return exec.Command("security", "add-generic-password",
		"-s", keychainService, "-a", name, "-w", string(data), "-U",
	).Run()
}

func (keychainBackend) Get(name string) (Secrets, error) {
	out, err := exec.Command("security", "find-generic-password",
		"-s", keychainService, "-a", name, "-w",
	).Output()
	if err != nil {
		return Secrets{}, err
	}
	var s Secrets
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &s); err != nil {
		return Secrets{}, err
	}
	return s, nil
}

func (keychainBackend) Delete(name string) {
	_ = exec.Command("security", "delete-generic-password",
		"-s", keychainService, "-a", name).Run()
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

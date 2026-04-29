package cli

import (
	"fmt"
	"testing"

	"github.com/shhac/agent-deepweb/internal/config"
	"github.com/shhac/agent-deepweb/internal/credential"
)

// TestDefaultApp_PopulatesAllDeps — DefaultApp wires every slot. A
// nil dependency would surface as a cryptic panic deep in cobra
// routing; catching it here is cheap.
func TestDefaultApp_PopulatesAllDeps(t *testing.T) {
	a := DefaultApp()
	if a.Config == nil {
		t.Error("Config store not set")
	}
	if a.Audit == nil {
		t.Error("Audit writer not set")
	}
	if a.Track == nil {
		t.Error("Track recorder not set")
	}
	if a.SecretBackend == nil {
		t.Error("SecretBackend not set")
	}
}

// TestApp_InstallOverridesSecretBackend — building an App with a
// custom SecretBackend and calling install() should swap the
// package-level default. Locks in the single documented seam for
// tests to replace credential storage without runtime.GOOS checks.
func TestApp_InstallOverridesSecretBackend(t *testing.T) {
	prev := credential.DefaultBackend
	t.Cleanup(func() { credential.DefaultBackend = prev })

	stub := stubSecretBackend{}
	a := &App{
		Config:        config.NewStore(t.TempDir()),
		SecretBackend: stub,
	}
	a.install()

	if _, ok := credential.DefaultBackend.(stubSecretBackend); !ok {
		t.Errorf("SecretBackend not installed; got %T", credential.DefaultBackend)
	}
}

type stubSecretBackend struct{}

func (stubSecretBackend) Available() bool                        { return false }
func (stubSecretBackend) Store(string, credential.Secrets) error { return fmt.Errorf("stub") }
func (stubSecretBackend) Get(string) (credential.Secrets, error) {
	return credential.Secrets{}, fmt.Errorf("stub")
}
func (stubSecretBackend) Delete(string) {}

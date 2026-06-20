package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/shhac/agent-deepweb/internal/config"
	"github.com/shhac/agent-deepweb/internal/credential"
	"github.com/shhac/agent-deepweb/internal/output"
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

// TestExecute_CobraErrorsRenderStructured — errors that originate inside
// cobra (unknown command/flag, arg-count violations) never pass through a
// RunE handler. libcli.NewRoot classifies them as fixable_by:agent and
// libcli.Run is the single sink that renders them as a structured
// {error,fixable_by} envelope exactly once. This test exercises the tree the
// way Run does — root.Execute() then output.WriteError on the bubbled error —
// without exiting the process.
func TestExecute_CobraErrorsRenderStructured(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"unknown command", []string{"boguscmd"}},
		{"unknown flag", []string{"fetch", "--bogusflag"}},
		{"missing required arg", []string{"fetch"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := newRootCmd("test")
			root.SetArgs(tc.args)

			var buf bytes.Buffer
			execErr := root.Execute()
			if execErr == nil {
				t.Fatalf("expected an error for args %v", tc.args)
			}
			// Mirror libcli.Run's single render of the bubbled error.
			output.WriteError(&buf, execErr)

			payload := buf.Bytes()
			if len(payload) == 0 {
				t.Fatalf("cobra error for %v produced no output (silent)", tc.args)
			}
			var env map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(payload), &env); err != nil {
				t.Fatalf("output was not JSON: %q", payload)
			}
			if env["error"] == nil || env["error"] == "" {
				t.Errorf("missing error field: %v", env)
			}
			if env["fixable_by"] != "agent" {
				t.Errorf("cobra mistakes should be fixable_by:agent, got %v", env["fixable_by"])
			}
		})
	}
}

type stubSecretBackend struct{}

func (stubSecretBackend) Available() bool                        { return false }
func (stubSecretBackend) Store(string, credential.Secrets) error { return fmt.Errorf("stub") }
func (stubSecretBackend) Get(string) (credential.Secrets, error) {
	return credential.Secrets{}, fmt.Errorf("stub")
}
func (stubSecretBackend) Delete(string) {}

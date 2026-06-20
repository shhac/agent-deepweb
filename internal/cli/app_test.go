package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
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

// TestExecute_CobraErrorsRenderStructured — errors that originate inside
// cobra (unknown command/flag, arg-count violations) never pass through a
// RunE handler, so without explicit rendering they exit 1 silently. Each
// must instead reach the user as a structured {error,fixable_by} envelope.
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
			origArgs := os.Args
			os.Args = append([]string{"agent-deepweb"}, tc.args...)
			defer func() { os.Args = origArgs }()

			payload, execErr := captureStderr(t, func() error {
				return DefaultApp().Execute("test")
			})
			if execErr == nil {
				t.Fatalf("expected an error for args %v", tc.args)
			}
			if len(payload) == 0 {
				t.Fatalf("cobra error for %v produced no stderr output (silent exit)", tc.args)
			}
			var env map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(payload), &env); err != nil {
				t.Fatalf("stderr was not JSON: %q", payload)
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

// captureStderr runs fn with os.Stderr redirected to a pipe and returns
// what was written, plus fn's returned error.
func captureStderr(t *testing.T, fn func() error) ([]byte, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	buf := &bytes.Buffer{}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(buf, r)
		close(done)
	}()

	fnErr := fn()

	_ = w.Close()
	<-done
	return buf.Bytes(), fnErr
}

type stubSecretBackend struct{}

func (stubSecretBackend) Available() bool                        { return false }
func (stubSecretBackend) Store(string, credential.Secrets) error { return fmt.Errorf("stub") }
func (stubSecretBackend) Get(string) (credential.Secrets, error) {
	return credential.Secrets{}, fmt.Errorf("stub")
}
func (stubSecretBackend) Delete(string) {}

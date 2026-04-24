package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	cfg "github.com/shhac/agent-deepweb/internal/config"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

// setupConfigDir points the internal/config package at a temp dir for
// the test and restores afterwards. Tests that drive the CLI share
// this plus captureStdout.
func setupConfigDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	cfg.SetConfigDir(dir)
	t.Cleanup(func() { cfg.SetConfigDir(""); cfg.ClearCache() })
}

// exec runs one subcommand through a fresh root and returns
// the stdout JSON plus the execute error. A fresh root per call
// means there's no accumulation of residual SetArgs/SetOut state —
// the JSON capture is synchronous (pipe reader runs in a goroutine
// and is drained via Close before we parse).
func exec(t *testing.T, args ...string) (map[string]any, error) {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	buf := &bytes.Buffer{}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(buf, r)
		close(done)
	}()

	root := &cobra.Command{Use: "root"}
	Register(root, func() *shared.GlobalFlags { return &shared.GlobalFlags{} })
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(args)
	execErr := root.Execute()

	_ = w.Close()
	<-done

	if buf.Len() == 0 {
		return nil, execErr
	}
	payload := bytes.TrimSpace(buf.Bytes())
	var out map[string]any
	if jerr := json.Unmarshal(payload, &out); jerr != nil {
		t.Fatalf("stdout was not JSON: %q (exec err=%v)", payload, execErr)
	}
	return out, execErr
}

// TestSet_ReReadsAfterWrite — `config set` must call cfg.Read() a
// second time after Write so the reported value reflects
// applyDefaults (the pre-Write in-memory *Config was never re-
// inflated). Regression guard for the source="config" vs "default"
// tag going stale right after a set.
func TestSet_ReReadsAfterWrite(t *testing.T) {
	setupConfigDir(t)

	out, err := exec(t, "config", "set", "default.timeout-ms", "12345")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if out["status"] != "ok" {
		t.Errorf("missing status=ok: %+v", out)
	}
	if out["value"] != "12345" || out["source"] != "config" {
		t.Errorf("set payload wrong: %+v", out)
	}
}

// TestUnset_ReportsDefaultSource — after unsetting a key, `get` must
// report source="default". Paired with TestSet above so the round-trip
// is locked in.
func TestUnset_ReportsDefaultSource(t *testing.T) {
	setupConfigDir(t)

	if _, err := exec(t, "config", "set", "default.timeout-ms", "9999"); err != nil {
		t.Fatal(err)
	}
	if _, err := exec(t, "config", "unset", "default.timeout-ms"); err != nil {
		t.Fatal(err)
	}
	out, err := exec(t, "config", "get", "default.timeout-ms")
	if err != nil {
		t.Fatal(err)
	}
	if out["source"] != "default" {
		t.Errorf("after unset source should be 'default', got %+v", out)
	}
}

// TestUnknownKey_HintListsAllKeys — malformed key names must surface
// a hint with every registered key. The hint is generated from
// cfg.Keys at call time, so this test also guards against drift if
// a new key is added and unknownKeyError is forgotten.
func TestUnknownKey_HintListsAllKeys(t *testing.T) {
	setupConfigDir(t)

	_, err := exec(t, "config", "get", "no.such.key")
	if err == nil {
		t.Fatal("expected unknown-key error")
	}
	var apiErr *agenterrors.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want APIError, got %T: %v", err, err)
	}
	hint := apiErr.Hint
	for _, k := range cfg.Keys {
		if !strings.Contains(hint, k.Name) {
			t.Errorf("unknown-key hint missing %q; hint=%q", k.Name, hint)
		}
	}
}

// TestSet_InvalidValueReturnsAgentFixable — a malformed value (e.g.
// non-integer for an int key) should surface fixable_by:agent so the
// LLM can re-type without asking the user.
func TestSet_InvalidValueReturnsAgentFixable(t *testing.T) {
	setupConfigDir(t)

	_, err := exec(t, "config", "set", "default.timeout-ms", "not-a-number")
	if err == nil {
		t.Fatal("expected parse error")
	}
	var apiErr *agenterrors.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want APIError, got %T: %v", err, err)
	}
	if apiErr.FixableBy != agenterrors.FixableByAgent {
		t.Errorf("want fixable_by=agent, got %q", apiErr.FixableBy)
	}
	if !strings.Contains(err.Error(), "integer") {
		t.Errorf("error should mention the expected type, got %q", err.Error())
	}
}

// TestListKeys_ReturnsAllRegisteredKeys — `config list-keys` must
// surface every key in cfg.Keys (LLMs rely on this to self-discover
// what's settable).
func TestListKeys_ReturnsAllRegisteredKeys(t *testing.T) {
	setupConfigDir(t)

	out, err := exec(t, "config", "list-keys")
	if err != nil {
		t.Fatal(err)
	}
	keys, _ := out["keys"].([]any)
	if len(keys) != len(cfg.Keys) {
		t.Errorf("list-keys returned %d rows, expected %d", len(keys), len(cfg.Keys))
	}
	seen := map[string]bool{}
	for _, row := range keys {
		m := row.(map[string]any)
		seen[m["name"].(string)] = true
	}
	for _, k := range cfg.Keys {
		if !seen[k.Name] {
			t.Errorf("list-keys missed key %q", k.Name)
		}
	}
}

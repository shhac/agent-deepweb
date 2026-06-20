package shared

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/config"
	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/output"
)

// Fail returns err for a RunE handler to bubble up. It is a no-op
// classifier seam: the actual structured-JSON render happens exactly once
// in libcli.Run (the single error sink in main), so Fail must NOT write to
// stderr itself — doing so would double-print every error. It is kept as a
// named helper (rather than a bare `return err`) because FailHuman/FailAgent
// classify through it and 100+ call sites read as `return shared.Fail(...)`.
func Fail(err error) error {
	return err
}

// FailHuman wraps a plain error as fixable_by:human and emits via Fail.
// Collapses the very common `Fail(errors.Wrap(err, FixableByHuman))`
// boilerplate that appeared 20+ times across CLI handlers.
func FailHuman(err error) error { return Fail(agenterrors.Wrap(err, agenterrors.FixableByHuman)) }

// FailAgent wraps a plain error as fixable_by:agent and emits via Fail.
func FailAgent(err error) error { return Fail(agenterrors.Wrap(err, agenterrors.FixableByAgent)) }

// RequirePrefix is the shared "--prefix is required" guard used by
// every import-* CLI command. --prefix namespaces the imported
// templates so two imports from different sources can't collide;
// empty-prefix is always a human mistake. Returns a classified
// fixable_by:human error suitable for shared.Fail.
func RequirePrefix(prefix string) error {
	if prefix != "" {
		return nil
	}
	return agenterrors.New(
		"--prefix is required (chooses the name-space for imported templates, e.g. 'myapi')",
		agenterrors.FixableByHuman)
}

// PrintOK emits the canonical {"status":"ok", ...} success envelope to
// stdout. Merges extras into the map; extras override "status" if the
// caller really wants to (rare). Replaces the 15 inline `output.PrintJSON(
// map[string]any{"status":"ok", "name": n, ...})` sites across the CLI.
func PrintOK(extras map[string]any) {
	m := map[string]any{"status": "ok"}
	for k, v := range extras {
		m[k] = v
	}
	output.PrintJSON(m)
}

// FirstNonEmpty returns the first non-empty string in vals, or "".
func FirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// FirstNonZero returns the first non-zero int in vals, or 0.
func FirstNonZero(vals ...int) int {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}

// SplitHeader parses "Name: value", trimming whitespace. Returns ok=false
// if the colon is missing or the key is empty.
func SplitHeader(h string) (key, value string, ok bool) {
	i := strings.IndexByte(h, ':')
	if i < 0 {
		return "", "", false
	}
	k := strings.TrimSpace(h[:i])
	v := strings.TrimSpace(h[i+1:])
	if k == "" {
		return "", "", false
	}
	return k, v, true
}

// SplitKV parses "key=value" (value may contain '='). Returns a classified
// APIError on bad input citing the originating flag label.
func SplitKV(s, flagLabel string) (k, v string, err error) {
	key, val, found := strings.Cut(s, "=")
	if !found {
		return "", "", agenterrors.Newf(agenterrors.FixableByAgent,
			"malformed %s %q (expected key=value)", flagLabel, s)
	}
	return key, val, nil
}

// LoadInlineSpec interprets a flag value as one of: "@-" (stdin), "@path"
// (file), else literal bytes. Errors are fixable_by:agent with a path hint.
// Used by --data, --json, --query, --variables — anywhere a small payload
// can come from a string, a file, or stdin.
func LoadInlineSpec(spec string) ([]byte, error) {
	switch {
	case spec == "@-":
		return io.ReadAll(os.Stdin)
	case strings.HasPrefix(spec, "@"):
		data, err := os.ReadFile(spec[1:])
		if err != nil {
			return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent).
				WithHint("Check the path and ensure the file is readable")
		}
		return data, nil
	default:
		return []byte(spec), nil
	}
}

// LoadProfileMetadata looks up the named profile's metadata (no secrets).
// Returns a classified fixable_by error on lookup failure so the caller
// can `return shared.Fail(err)` directly. Collapses the common idiom
//
//	c, err := credential.GetMetadata(name)
//	if err != nil { return shared.Fail(credential.ClassifyLookupErr(err, name)) }
func LoadProfileMetadata(name string) (*credential.Credential, error) {
	c, err := credential.GetMetadata(name)
	if err != nil {
		return nil, credential.ClassifyLookupErr(err, name)
	}
	return c, nil
}

// LoadProfileResolved is LoadProfileMetadata plus secret resolution.
// Used by commands that need to send a request with the profile's
// credentials attached (e.g. `profile test`, form login).
func LoadProfileResolved(name string) (*credential.Resolved, error) {
	r, err := credential.Resolve(name)
	if err != nil {
		return nil, credential.ClassifyLookupErr(err, name)
	}
	return r, nil
}

// ResolveLimits applies the precedence chain for request timeout and max
// body size:
//
//	explicit flag > global --timeout/--max-size > config default.
//
// Returns an http.Client-ready time.Duration and a byte cap.
func ResolveLimits(flagTimeoutMS int, flagMaxBytes int64, g *GlobalFlags) (time.Duration, int64) {
	cfg := config.Read()
	var gt int
	if g != nil {
		gt = g.TimeoutMS
	}
	timeoutMS := FirstNonZero(flagTimeoutMS, gt, cfg.Defaults.TimeoutMS)
	maxBytes := flagMaxBytes
	if maxBytes == 0 {
		maxBytes = cfg.Defaults.MaxBytes
	}
	return time.Duration(timeoutMS) * time.Millisecond, maxBytes
}

// RegisterUsage wires the canonical `<verb> usage` subcommand. Every CLI
// verb in the tree had the same 4-line block; centralising it removes
// the per-package fmt import and makes the per-verb usage docs findable
// from one place.
func RegisterUsage(parent *cobra.Command, verb, text string) {
	parent.AddCommand(&cobra.Command{
		Use:   "usage",
		Short: "Show detailed reference for " + verb,
		Run:   func(*cobra.Command, []string) { fmt.Print(text) },
	})
}

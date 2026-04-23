package shared

import (
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/config"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/output"
)

// Fail writes err to stderr as structured JSON and returns it so cobra's
// SilenceErrors/SilenceUsage can propagate the non-zero exit without a
// second print. Replaces the near-identical fail() helpers that lived in
// each CLI subpackage.
func Fail(err error) error {
	if err == nil {
		return nil
	}
	output.WriteError(os.Stderr, err)
	return err
}

// RunEFunc is cobra's RunE signature, repeated here so wrapper names read
// nicely.
type RunEFunc = func(cmd *cobra.Command, args []string) error

// HumanOnlyRunE wraps a RunE so that it refuses in agent mode before the
// inner function ever runs. The refusal is emitted as a fixable_by:human
// error naming the verb. Use for any mutating/secret-revealing command.
func HumanOnlyRunE(verb string, fn RunEFunc) RunEFunc {
	return func(cmd *cobra.Command, args []string) error {
		if err := RefuseInAgentMode(verb); err != nil {
			return Fail(err)
		}
		return fn(cmd, args)
	}
}

// RefuseFlag returns an error suitable for Fail() when a human-only flag
// was passed while in agent mode. Caller usage:
//
//	if o.noRedact { if err := RefuseFlag(true, "--no-redact"); err != nil { return Fail(err) } }
//
// Kept explicit (rather than magic-wrapping flags) because the check is
// conditional on the flag being set, not on the command itself.
func RefuseFlag(set bool, flagName string) error {
	if !set {
		return nil
	}
	return RefuseInAgentMode(flagName)
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
// APIError on bad input citing the originating flag label. Returns ok=false
// with a nil error only when an empty input is explicitly allowed — in
// practice callers should treat err != nil as an immediate Fail.
func SplitKV(s, flagLabel string) (k, v string, err error) {
	key, val, found := strings.Cut(s, "=")
	if !found {
		return "", "", agenterrors.Newf(agenterrors.FixableByAgent,
			"malformed %s %q (expected key=value)", flagLabel, s)
	}
	return key, val, nil
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
		gt = g.Timeout
	}
	timeoutMS := FirstNonZero(flagTimeoutMS, gt, cfg.Defaults.TimeoutMS)
	maxBytes := flagMaxBytes
	if maxBytes == 0 {
		maxBytes = cfg.Defaults.MaxBytes
	}
	return time.Duration(timeoutMS) * time.Millisecond, maxBytes
}

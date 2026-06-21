// Package output provides JSON/YAML formatters for stdout and structured error
// writers for stderr. All LLM-facing output flows through here.
//
// The wire mechanism (pretty-JSON encoding with HTML escaping off, the YAML
// encoder, and the {error,fixable_by,hint} stderr contract) is delegated to
// lib-agent-output (+ lib-agent-cli's yaml registration) so it stays consistent
// across the agent-* family. What stays LOCAL is agent-deepweb's two
// response-body display modes: raw response bytes ("raw") and a text preamble
// ("text") for the request verbs, neither of which the shared Format enum
// models. Those are opted in command-aware via libcli.AllowFormats(cmd,
// "raw", "text"); the universal json|yaml|jsonl set is rendered via out.Print.
package output

import (
	"fmt"
	"io"
	"os"

	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	out "github.com/shhac/lib-agent-output"

	// Register the YAML encoder so out.Print honours --format yaml. Blank
	// import: the side-effecting init() installs the encoder. Without this,
	// FormatYAML would error ("no encoder registered").
	_ "github.com/shhac/lib-agent-cli/yaml"
)

// Format is agent-deepweb's local output selector used by the `config get`
// command, which accepts only json|jsonl. The request verbs no longer use this
// type: they take the global --format (validated command-aware by NewRoot via
// AllowFormats) and render through RenderResponse, which understands the full
// json|yaml|jsonl|raw|text set.
type Format string

const (
	FormatJSON   Format = "json"
	FormatNDJSON Format = "jsonl"
)

// ParseFormat validates the `config get` format flag (json|jsonl only). Request
// verbs do NOT use this — their --format is validated centrally in NewRoot.
func ParseFormat(s string) (Format, error) {
	switch s {
	case "", "json":
		return FormatJSON, nil
	case "jsonl", "ndjson":
		return FormatNDJSON, nil
	default:
		return "", agenterrors.Newf(agenterrors.FixableByAgent,
			"unknown format %q, expected: json, jsonl", s)
	}
}

// PrintJSON pretty-prints data to stdout with 2-space indent and HTML escaping
// off, via the shared encoder (no pruning — deepweb envelopes are pre-shaped).
func PrintJSON(data any) {
	_ = out.PrintJSON(os.Stdout, data, nil)
}

// PrintEnvelope renders a pre-shaped envelope in the global --format value
// (json|yaml|jsonl, empty → json). It is the structured-output path for verbs
// that don't carry a single response body to passthrough (graphql/jsonrpc),
// which handle raw/text separately. The format already passed NewRoot's
// validator, so a resolve error is a programming slip — fall back to JSON.
func PrintEnvelope(env any, format string) {
	f, err := out.ResolveFormat(format, out.FormatJSON)
	if err != nil {
		f = out.FormatJSON
	}
	_ = out.Print(os.Stdout, env, f, nil)
}

// PrintBody writes a response body for the raw/text passthrough formats shared
// by all request verbs. raw → body bytes; text → "HTTP <status> <text>\n\n" +
// body. The audit ID (when --track) goes to stderr so it survives these modes
// where the envelope isn't printed. Returns false when format is neither raw
// nor text, so the caller falls through to its structured renderer.
func PrintBody(format string, status int, statusText string, body []byte, auditID string) bool {
	switch format {
	case "raw":
		_, _ = os.Stdout.Write(body)
	case "text":
		_, _ = fmt.Fprintf(os.Stdout, "HTTP %d %s\n\n", status, statusText)
		_, _ = os.Stdout.Write(body)
	default:
		return false
	}
	if auditID != "" {
		fmt.Fprintln(os.Stderr, "audit_id:", auditID)
	}
	return true
}

// WriteError writes a structured JSON error to the given writer. If the error
// is not already an *APIError it is treated as agent-fixable. Delegates to the
// shared writer so the {error,fixable_by,hint} field order and HTML-escaping
// behaviour match the rest of the family.
func WriteError(w io.Writer, err error) {
	out.WriteError(w, err)
}

// Package output provides JSON formatters for stdout and structured error
// writers for stderr. All LLM-facing output flows through here.
//
// The wire mechanism (pretty-JSON encoding with HTML escaping off, and the
// {error,fixable_by,hint} stderr contract) is delegated to lib-agent-output so
// it stays consistent across the agent-* family. What stays LOCAL is
// agent-deepweb's format domain: alongside json/jsonl this CLI also emits raw
// response bytes (FormatRaw) and a text preamble (FormatText) for fetch/graphql,
// neither of which the shared Format enum models.
package output

import (
	"io"
	"os"

	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	out "github.com/shhac/lib-agent-output"
)

// Format is agent-deepweb's local output selector. It is intentionally NOT the
// shared out.Format: this CLI's raw/text body modes have no counterpart there,
// and ParseFormat's accept-set (json/jsonl/raw/text — no yaml) is part of the
// documented CLI contract.
type Format string

const (
	FormatJSON   Format = "json"
	FormatNDJSON Format = "jsonl"
	FormatRaw    Format = "raw"  // raw response body, used by fetch/graphql
	FormatText   Format = "text" // body as text with a tiny header
)

func ParseFormat(s string) (Format, error) {
	switch s {
	case "", "json":
		return FormatJSON, nil
	case "jsonl", "ndjson":
		return FormatNDJSON, nil
	case "raw":
		return FormatRaw, nil
	case "text":
		return FormatText, nil
	default:
		return "", agenterrors.Newf(agenterrors.FixableByAgent,
			"unknown format %q, expected: json, jsonl, raw, text", s)
	}
}

// PrintJSON pretty-prints data to stdout with 2-space indent and HTML escaping
// off, via the shared encoder (no pruning — deepweb envelopes are pre-shaped).
func PrintJSON(data any) {
	_ = out.PrintJSON(os.Stdout, data, nil)
}

// WriteError writes a structured JSON error to the given writer. If the error
// is not already an *APIError it is treated as agent-fixable. Delegates to the
// shared writer so the {error,fixable_by,hint} field order and HTML-escaping
// behaviour match the rest of the family.
func WriteError(w io.Writer, err error) {
	out.WriteError(w, err)
}

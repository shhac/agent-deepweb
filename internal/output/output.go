// Package output provides JSON formatters for stdout and structured error
// writers for stderr. All LLM-facing output flows through here.
package output

import (
	"encoding/json"
	"io"
	"os"

	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

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

// PrintJSON pretty-prints data to stdout with 2-space indent.
func PrintJSON(data any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(data)
}

// WriteError writes a structured JSON error to the given writer.
// If the error is not already an *APIError it is wrapped as agent-fixable.
func WriteError(w io.Writer, err error) {
	var aerr *agenterrors.APIError
	if !agenterrors.As(err, &aerr) {
		aerr = agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}
	payload := map[string]any{
		"error":      aerr.Message,
		"fixable_by": string(aerr.FixableBy),
	}
	if aerr.Hint != "" {
		payload["hint"] = aerr.Hint
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}

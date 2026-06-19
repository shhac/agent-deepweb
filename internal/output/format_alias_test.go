package output

import (
	"testing"

	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

// TestParseFormat_Contract pins agent-deepweb's local format domain. Unlike the
// shared lenient parser, this CLI also accepts raw/text (response-body modes)
// and does NOT accept yaml. "ndjson" is accepted as an alias for "jsonl".
func TestParseFormat_Contract(t *testing.T) {
	cases := []struct {
		in   string
		want Format
	}{
		{"", FormatJSON},
		{"json", FormatJSON},
		{"jsonl", FormatNDJSON},
		{"ndjson", FormatNDJSON},
		{"raw", FormatRaw},
		{"text", FormatText},
	}
	for _, c := range cases {
		got, err := ParseFormat(c.in)
		if err != nil {
			t.Errorf("ParseFormat(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseFormat(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestParseFormat_RejectsUnknown — an invalid format is the calling agent's to
// fix, so the error must classify as fixable_by: agent. yaml is intentionally
// rejected: this CLI has no YAML output path.
func TestParseFormat_RejectsUnknown(t *testing.T) {
	for _, in := range []string{"yaml", "yml", "xml", "bogus"} {
		_, err := ParseFormat(in)
		if err == nil {
			t.Errorf("ParseFormat(%q) = nil error, want rejection", in)
			continue
		}
		var aerr *agenterrors.APIError
		if !agenterrors.As(err, &aerr) {
			t.Errorf("ParseFormat(%q) error is not an *APIError: %v", in, err)
			continue
		}
		if aerr.FixableBy != agenterrors.FixableByAgent {
			t.Errorf("ParseFormat(%q) fixable_by = %q, want agent", in, aerr.FixableBy)
		}
	}
}

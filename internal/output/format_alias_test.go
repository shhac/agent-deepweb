package output

import (
	"testing"

	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

// TestParseFormat_Contract pins the local format domain used by `config get`,
// which renders only json|jsonl. The request verbs no longer use ParseFormat:
// their --format (incl. yaml/raw/text) is validated centrally in NewRoot via
// AllowFormats. "ndjson" is accepted as an alias for "jsonl".
func TestParseFormat_Contract(t *testing.T) {
	cases := []struct {
		in   string
		want Format
	}{
		{"", FormatJSON},
		{"json", FormatJSON},
		{"jsonl", FormatNDJSON},
		{"ndjson", FormatNDJSON},
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
// fix, so the error must classify as fixable_by: agent. yaml/raw/text are
// rejected here because `config get` has no such output path; on the request
// verbs they are accepted via AllowFormats / out.Print instead.
func TestParseFormat_RejectsUnknown(t *testing.T) {
	for _, in := range []string{"yaml", "yml", "raw", "text", "xml", "bogus"} {
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

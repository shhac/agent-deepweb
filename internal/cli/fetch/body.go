package fetch

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"strings"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

// buildBody assembles the request body from --data / --json / --form
// (mutually exclusive). Returns the body reader and a Content-Type string
// (empty when the caller should not set it). Pure given opts — no I/O
// other than reading the stdin/@file specs.
func buildBody(o *opts) (io.Reader, string, error) {
	hasData := o.data != ""
	hasJSON := o.jsonBody != ""
	hasForm := len(o.form) > 0

	n := 0
	for _, b := range []bool{hasData, hasJSON, hasForm} {
		if b {
			n++
		}
	}
	if n == 0 {
		return nil, "", nil
	}
	if n > 1 {
		return nil, "", agenterrors.New("--data / --json / --form are mutually exclusive", agenterrors.FixableByAgent)
	}

	switch {
	case hasData:
		b, err := shared.LoadInlineSpec(o.data)
		if err != nil {
			return nil, "", err
		}
		return bytes.NewReader(b), "", nil
	case hasJSON:
		b, err := shared.LoadInlineSpec(o.jsonBody)
		if err != nil {
			return nil, "", err
		}
		var anyv any
		if err := json.Unmarshal(b, &anyv); err != nil {
			return nil, "", agenterrors.Newf(agenterrors.FixableByAgent,
				"--json is not valid JSON: %s", err.Error()).
				WithHint("Pass a valid JSON string, @file path, or @- for stdin")
		}
		return bytes.NewReader(b), "application/json", nil
	case hasForm:
		values := url.Values{}
		for _, f := range o.form {
			k, v, err := shared.SplitKV(f, "--form")
			if err != nil {
				return nil, "", err
			}
			values.Add(k, v)
		}
		return strings.NewReader(values.Encode()), "application/x-www-form-urlencoded", nil
	}
	return nil, "", nil
}


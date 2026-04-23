package tpl

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"strings"

	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/template"
)

// buildTemplateBody renders the template's body_template according to
// body_format. Returns the reader and suggested Content-Type (empty when
// caller shouldn't override). Pure given the Template + typed params.
//
// No body_format (or empty body_template) → no body. Authors must opt in
// explicitly; callers depend on body=nil to pick the right default method.
func buildTemplateBody(tpl *template.Template, typed map[string]any) (io.Reader, string, error) {
	format := strings.ToLower(tpl.BodyFormat)
	if format == "" || len(tpl.BodyTemplate) == 0 {
		return nil, "", nil
	}

	switch format {
	case "json":
		b, err := template.SubstituteBody(tpl.BodyTemplate, typed)
		if err != nil {
			return nil, "", agenterrors.Wrap(err, agenterrors.FixableByAgent)
		}
		if len(b) == 0 {
			return nil, "", nil
		}
		return bytes.NewReader(b), "application/json", nil
	case "form":
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(tpl.BodyTemplate, &raw); err != nil {
			return nil, "", agenterrors.Wrap(err, agenterrors.FixableByAgent)
		}
		values := url.Values{}
		for k, v := range raw {
			var str string
			if err := json.Unmarshal(v, &str); err == nil {
				s, err := template.SubstituteString(str, typed, false)
				if err != nil {
					return nil, "", agenterrors.Wrap(err, agenterrors.FixableByAgent)
				}
				values.Add(k, s)
			}
		}
		return strings.NewReader(values.Encode()), "application/x-www-form-urlencoded", nil
	case "raw":
		var raw string
		if err := json.Unmarshal(tpl.BodyTemplate, &raw); err != nil {
			return nil, "", agenterrors.Wrap(err, agenterrors.FixableByAgent).
				WithHint("body_format=raw expects body_template to be a JSON string")
		}
		s, err := template.SubstituteString(raw, typed, false)
		if err != nil {
			return nil, "", agenterrors.Wrap(err, agenterrors.FixableByAgent)
		}
		return strings.NewReader(s), "", nil
	default:
		return nil, "", agenterrors.Newf(agenterrors.FixableByHuman,
			"template %q: unknown body_format %q", tpl.Name, tpl.BodyFormat)
	}
}

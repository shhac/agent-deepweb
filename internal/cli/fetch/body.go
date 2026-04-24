package fetch

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

// buildBody assembles the request body from --data / --json / --form /
// --file (mutually exclusive). --file may be combined with --form to
// produce a multipart/form-data body containing both file parts and
// plain text parts (the most common shape for "upload + caption"
// uploads). All other combinations are rejected.
//
// Returns the body reader and a Content-Type string (empty when the
// caller should not set it).
func buildBody(o *opts) (io.Reader, string, error) {
	hasData := o.data != ""
	hasJSON := o.jsonBody != ""
	hasForm := len(o.form) > 0
	hasFile := len(o.file) > 0

	switch {
	case hasFile:
		// --file always means multipart/form-data. May coexist with --form
		// (text fields). All other body flags are rejected.
		if hasData || hasJSON {
			return nil, "", agenterrors.New("--file cannot be combined with --data or --json (use --form for text parts in a multipart body)", agenterrors.FixableByAgent)
		}
		return buildMultipart(o.file, o.form)
	case hasData && (hasJSON || hasForm), hasJSON && hasForm:
		return nil, "", agenterrors.New("--data / --json / --form are mutually exclusive (combine --form with --file for multipart)", agenterrors.FixableByAgent)
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

// buildMultipart assembles a multipart/form-data body from --file specs
// and (optional) --form text parts. Each --file flag is parsed as
//
//	field=@path[;type=MIME][;filename=NAME]
//
// where type defaults to application/octet-stream and filename defaults
// to the path's basename. Multiple --file flags become multiple file
// parts in the same body (e.g. `--file photo=@cat.jpg --file
// doc=@notes.pdf`).
//
// The Content-Type header includes the boundary token mime/multipart
// generates, so the caller doesn't need to set Content-Type explicitly.
func buildMultipart(files, formFields []string) (io.Reader, string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	for _, spec := range files {
		if err := writeFilePart(mw, spec); err != nil {
			return nil, "", err
		}
	}
	for _, f := range formFields {
		k, v, err := shared.SplitKV(f, "--form")
		if err != nil {
			return nil, "", err
		}
		if err := mw.WriteField(k, v); err != nil {
			return nil, "", agenterrors.Wrap(err, agenterrors.FixableByAgent)
		}
	}
	if err := mw.Close(); err != nil {
		return nil, "", agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}
	return &buf, mw.FormDataContentType(), nil
}

// writeFilePart writes one --file spec to the multipart writer. Owns
// the file's full lifecycle: parse spec, open, write MIME header, copy
// bytes, close (via defer — guarantees no FD leak even when CreatePart
// or io.Copy errors). Pure orchestration; no business logic about
// multiple files or text fields.
func writeFilePart(mw *multipart.Writer, spec string) error {
	field, path, mimeType, filename, err := parseFileSpec(spec)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return agenterrors.Wrap(err, agenterrors.FixableByAgent).
			WithHint("Check the --file path exists and is readable")
	}
	defer f.Close() //nolint:errcheck

	// Write a part with explicit Content-Type so multipart-aware servers
	// see "image/jpeg" rather than the default "application/octet-stream".
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="`+escapeQuotes(field)+`"; filename="`+escapeQuotes(filename)+`"`)
	hdr.Set("Content-Type", mimeType)
	part, err := mw.CreatePart(hdr)
	if err != nil {
		return agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}
	if _, err := io.Copy(part, f); err != nil {
		return agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}
	return nil
}

// parseFileSpec parses `field=@path[;type=MIME][;filename=NAME]` into its
// components. Returns a fixable_by:agent error on malformed input.
func parseFileSpec(spec string) (field, path, mimeType, filename string, err error) {
	eq := strings.IndexByte(spec, '=')
	if eq < 1 {
		return "", "", "", "", agenterrors.Newf(agenterrors.FixableByAgent,
			"malformed --file %q (expected field=@path[;type=MIME][;filename=NAME])", spec)
	}
	field = spec[:eq]
	rest := spec[eq+1:]
	if !strings.HasPrefix(rest, "@") {
		return "", "", "", "", agenterrors.Newf(agenterrors.FixableByAgent,
			"--file value must start with @ (the file path); got %q", spec)
	}
	rest = rest[1:]

	// Defaults; overridden by ;type= / ;filename= options below.
	mimeType = "application/octet-stream"
	parts := strings.Split(rest, ";")
	path = parts[0]
	filename = filepath.Base(path)
	for _, opt := range parts[1:] {
		kv := strings.SplitN(opt, "=", 2)
		if len(kv) != 2 {
			return "", "", "", "", agenterrors.Newf(agenterrors.FixableByAgent,
				"malformed --file option %q (expected key=value)", opt)
		}
		switch strings.ToLower(strings.TrimSpace(kv[0])) {
		case "type":
			mimeType = strings.TrimSpace(kv[1])
		case "filename":
			filename = strings.TrimSpace(kv[1])
		default:
			return "", "", "", "", agenterrors.Newf(agenterrors.FixableByAgent,
				"unknown --file option %q (allowed: type, filename)", kv[0])
		}
	}
	if path == "" {
		return "", "", "", "", agenterrors.Newf(agenterrors.FixableByAgent,
			"--file path is empty in %q", spec)
	}
	return field, path, mimeType, filename, nil
}

// escapeQuotes is the minimal Content-Disposition quoting we need:
// backslash-escape any embedded double quote so the header parses
// reliably. The MIME spec is fussier than this in theory, but in
// practice 99% of multipart parsers handle `\"` and nothing else.
func escapeQuotes(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}

package fetch

import (
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseFileSpec covers the `--file field=@path[;type=;filename=]`
// parser. Malformed inputs must surface fixable_by:agent errors so the
// LLM can correct + retry without user involvement.
func TestParseFileSpec(t *testing.T) {
	cases := []struct {
		name         string
		spec         string
		wantField    string
		wantPath     string
		wantMIME     string
		wantFilename string
		wantErr      bool
	}{
		{
			name:         "basic happy path",
			spec:         "photo=@/tmp/cat.jpg",
			wantField:    "photo",
			wantPath:     "/tmp/cat.jpg",
			wantMIME:     "application/octet-stream",
			wantFilename: "cat.jpg",
		},
		{
			name:         "explicit type + filename",
			spec:         "doc=@/tmp/notes.bin;type=application/pdf;filename=report.pdf",
			wantField:    "doc",
			wantPath:     "/tmp/notes.bin",
			wantMIME:     "application/pdf",
			wantFilename: "report.pdf",
		},
		{
			name:         "basename default when path has no dir",
			spec:         "f=@file.txt",
			wantField:    "f",
			wantPath:     "file.txt",
			wantMIME:     "application/octet-stream",
			wantFilename: "file.txt",
		},
		{name: "missing = → error", spec: "nokey@/tmp/x", wantErr: true},
		{name: "missing @ → error", spec: "f=/tmp/x", wantErr: true},
		{name: "empty path → error", spec: "f=@", wantErr: true},
		{name: "empty field → error", spec: "=@/tmp/x", wantErr: true},
		{name: "unknown option → error", spec: "f=@/tmp/x;weird=value", wantErr: true},
		{name: "malformed option (no =) → error", spec: "f=@/tmp/x;justakey", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			field, path, mime, filename, err := parseFileSpec(tc.spec)
			if tc.wantErr {
				if err == nil {
					t.Errorf("want error for %q, got nil", tc.spec)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if field != tc.wantField || path != tc.wantPath || mime != tc.wantMIME || filename != tc.wantFilename {
				t.Errorf("got (%q,%q,%q,%q), want (%q,%q,%q,%q)",
					field, path, mime, filename,
					tc.wantField, tc.wantPath, tc.wantMIME, tc.wantFilename)
			}
		})
	}
}

// TestBuildBody_FileRejectsDataAndJSON — the mutual exclusion of body
// flags. Without this, an LLM could smuggle --data bytes alongside
// --file and get an ambiguous body shape.
func TestBuildBody_FileRejectsDataAndJSON(t *testing.T) {
	tmp := t.TempDir()
	photo := filepath.Join(tmp, "p.txt")
	_ = os.WriteFile(photo, []byte("x"), 0o600)

	// --file + --data → error
	o := &opts{file: []string{"p=@" + photo}, data: "stuff"}
	if _, _, err := buildBody(o); err == nil {
		t.Error("--file with --data should error")
	}
	// --file + --json → error
	o = &opts{file: []string{"p=@" + photo}, jsonBody: `{"x":1}`}
	if _, _, err := buildBody(o); err == nil {
		t.Error("--file with --json should error")
	}
	// --file + --form → ALLOWED (multipart with text part)
	o = &opts{file: []string{"p=@" + photo}, form: []string{"caption=hi"}}
	reader, ct, err := buildBody(o)
	if err != nil {
		t.Fatalf("--file + --form should be allowed, got %v", err)
	}
	if !strings.HasPrefix(ct, "multipart/form-data; boundary=") {
		t.Errorf("content-type: %q", ct)
	}
	if reader == nil {
		t.Error("reader should not be nil")
	}
}

// TestBuildMultipart_MultipleFiles_ProducesParts assembles two file
// parts + one text part and verifies the MIME parser can read them
// back out with the expected field names and headers.
func TestBuildMultipart_MultipleFiles_ProducesParts(t *testing.T) {
	tmp := t.TempDir()
	photo := filepath.Join(tmp, "photo.txt")
	doc := filepath.Join(tmp, "notes.bin")
	_ = os.WriteFile(photo, []byte("photo-bytes"), 0o600)
	_ = os.WriteFile(doc, []byte("doc-bytes"), 0o600)

	reader, ct, err := buildMultipart(
		[]string{
			"photo=@" + photo,
			"doc=@" + doc + ";type=application/pdf;filename=report.pdf",
		},
		[]string{"caption=hello"},
	)
	if err != nil {
		t.Fatal(err)
	}
	// Parse Content-Type to get the boundary.
	boundary := strings.TrimPrefix(ct, "multipart/form-data; boundary=")
	if boundary == ct {
		t.Fatalf("couldn't extract boundary from %q", ct)
	}

	mr := multipart.NewReader(reader, boundary)
	parts := map[string]struct {
		ct       string
		filename string
		body     string
	}{}
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(p)
		parts[p.FormName()] = struct {
			ct       string
			filename string
			body     string
		}{
			ct:       p.Header.Get("Content-Type"),
			filename: p.FileName(),
			body:     string(b),
		}
	}
	if got := parts["photo"]; got.ct != "application/octet-stream" || got.filename != "photo.txt" || got.body != "photo-bytes" {
		t.Errorf("photo part: %+v", got)
	}
	if got := parts["doc"]; got.ct != "application/pdf" || got.filename != "report.pdf" || got.body != "doc-bytes" {
		t.Errorf("doc part: %+v", got)
	}
	if got := parts["caption"]; got.body != "hello" {
		t.Errorf("caption part: %+v", got)
	}
}

// TestEscapeQuotes — Content-Disposition fields with embedded quotes
// must be backslash-escaped so the header parses reliably.
func TestEscapeQuotes(t *testing.T) {
	cases := map[string]string{
		"simple":           "simple",
		`has "quote"`:      `has \"quote\"`,
		`back\slash`:       `back\slash`, // we don't escape the backslash
		`mixed "and"\stuff`: `mixed \"and\"\stuff`,
	}
	for in, want := range cases {
		if got := escapeQuotes(in); got != want {
			t.Errorf("escapeQuotes(%q) = %q, want %q", in, got, want)
		}
	}
}

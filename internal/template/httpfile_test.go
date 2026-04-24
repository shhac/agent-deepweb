package template

import (
	"strings"
	"testing"

	"github.com/shhac/agent-deepweb/internal/config"
)

// TestImportHTTPText_CoreShapes covers the common cases: named block,
// @var declaration, request line, headers, JSON body.
func TestImportHTTPText_CoreShapes(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	text := `@baseUrl = https://api.example.com

### list users
GET {{baseUrl}}/users?page=1
Accept: application/json

### create user
POST {{baseUrl}}/users
Content-Type: application/json

{"name":"x"}

### named-op
# This is a comment
GET {{baseUrl}}/healthz
`

	imported, err := ImportHTTPText(text, ImportHTTPFileOptions{Prefix: "h"})
	if err != nil {
		t.Fatalf("ImportHTTPText: %v", err)
	}
	if len(imported) != 3 {
		t.Fatalf("want 3 imports, got %d: %v", len(imported), imported)
	}

	list, err := Get("h.list_users")
	if err != nil {
		t.Fatal(err)
	}
	if list.Method != "GET" {
		t.Errorf("method: %q", list.Method)
	}
	if list.URL != "{{baseUrl}}/users" {
		t.Errorf("URL shouldn't carry query: %q", list.URL)
	}
	if list.Query["page"] != "1" {
		t.Errorf("query lift: %+v", list.Query)
	}
	// @baseUrl default flows into the baseUrl param.
	if list.Parameters["baseUrl"].Default != "https://api.example.com" {
		t.Errorf("baseUrl default not inherited: %+v", list.Parameters["baseUrl"])
	}

	// Named block: the ### trailing name becomes the leaf name.
	named, err := Get("h.named-op")
	if err != nil {
		t.Fatalf("h.named-op: %v (imported=%v)", err, imported)
	}
	if named.Method != "GET" || named.URL != "{{baseUrl}}/healthz" {
		t.Errorf("named-op: %+v", named)
	}
}

// TestImportHTTPText_JSONBody — body_format is inferred from
// Content-Type AND from a JSON-shaped body even if the header is
// missing.
func TestImportHTTPText_JSONBody(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	text := `### create
POST https://x/items
Content-Type: application/json

{"name":"bob"}
`
	if _, err := ImportHTTPText(text, ImportHTTPFileOptions{Prefix: "h"}); err != nil {
		t.Fatal(err)
	}
	got, _ := Get("h.create")
	if got.BodyFormat != "json" {
		t.Errorf("body_format: %q", got.BodyFormat)
	}
}

// TestImportHTTPText_FormBody — x-www-form-urlencoded body parses
// into a keyed form body_template.
func TestImportHTTPText_FormBody(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	text := `### login
POST https://x/login
Content-Type: application/x-www-form-urlencoded

u=alice&p=secret
`
	if _, err := ImportHTTPText(text, ImportHTTPFileOptions{Prefix: "h"}); err != nil {
		t.Fatal(err)
	}
	got, _ := Get("h.login")
	if got.BodyFormat != "form" {
		t.Errorf("body_format: %q", got.BodyFormat)
	}
	body := string(got.BodyTemplate)
	if !strings.Contains(body, `"u"`) || !strings.Contains(body, `"alice"`) {
		t.Errorf("form body: %s", body)
	}
}

// TestImportHTTPText_HandlesCRLFAndComments — Windows-exported files
// (CRLF line endings) + `#`/`//` comments don't break parsing.
func TestImportHTTPText_HandlesCRLFAndComments(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	text := "@base = https://x\r\n" +
		"// a comment\r\n" +
		"### only\r\n" +
		"# line comment\r\n" +
		"GET {{base}}/healthz HTTP/1.1\r\n"
	imported, err := ImportHTTPText(text, ImportHTTPFileOptions{Prefix: "h"})
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) != 1 {
		t.Errorf("want 1 import, got %v", imported)
	}
}

// TestImportHTTPText_AutoNameWhenNoLabel — a block without a trailing
// label on `###` gets an auto-name from method + path.
func TestImportHTTPText_AutoNameWhenNoLabel(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	text := "###\nGET https://x/items\n"
	imported, err := ImportHTTPText(text, ImportHTTPFileOptions{Prefix: "h"})
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) != 1 {
		t.Fatalf("want 1 import, got %v", imported)
	}
	if !strings.Contains(imported[0], "get_") || !strings.Contains(imported[0], "items") {
		t.Errorf("auto-name should include method + path: %q", imported[0])
	}
}

// TestImportHTTPText_RefusesEmpty — fully empty file or a file with
// only comments / declarations is a user error.
func TestImportHTTPText_RefusesEmpty(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	_, err := ImportHTTPText("", ImportHTTPFileOptions{Prefix: "h"})
	if err == nil || !strings.Contains(err.Error(), "no request blocks") {
		t.Errorf("want empty-file error, got %v", err)
	}
}

// TestImportHTTPText_MissingRequestLine — a block with headers but
// no request line is a parse error the user should see immediately.
func TestImportHTTPText_MissingRequestLine(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	text := "### broken\nAccept: application/json\n"
	_, err := ImportHTTPText(text, ImportHTTPFileOptions{Prefix: "h"})
	if err == nil || !strings.Contains(err.Error(), "request line") {
		t.Errorf("want no-request-line error, got %v", err)
	}
}

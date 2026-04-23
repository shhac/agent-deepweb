package template

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shhac/agent-deepweb/internal/config"
)

// ImportFile disambiguates "one template" from "map of templates" by
// looking for a top-level "method" key. This test locks the heuristic
// in place and guards the two edge cases.
func TestImportFile(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir("") })

	t.Run("single-template file", func(t *testing.T) {
		p := filepath.Join(dir, "single.json")
		_ = os.WriteFile(p, []byte(`{
			"name": "one.thing",
			"method": "GET",
			"url": "https://example.com/"
		}`), 0o644)
		got, err := ImportFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0] != "one.thing" {
			t.Errorf("got %v", got)
		}
	})

	t.Run("single-template needs name", func(t *testing.T) {
		p := filepath.Join(dir, "noname.json")
		_ = os.WriteFile(p, []byte(`{"method":"GET","url":"https://example.com/"}`), 0o644)
		_, err := ImportFile(p)
		if err == nil || !strings.Contains(err.Error(), "name") {
			t.Errorf("want name-required error, got %v", err)
		}
	})

	t.Run("map-of-templates", func(t *testing.T) {
		p := filepath.Join(dir, "map.json")
		_ = os.WriteFile(p, []byte(`{
			"a.one": {"method":"GET","url":"https://example.com/1"},
			"b.two": {"method":"POST","url":"https://example.com/2"}
		}`), 0o644)
		got, err := ImportFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0] != "a.one" || got[1] != "b.two" {
			t.Errorf("got %v (expected sorted [a.one, b.two])", got)
		}
		// Verify templates actually stored.
		if _, err := Get("a.one"); err != nil {
			t.Errorf("Get(a.one): %v", err)
		}
	})

	t.Run("malformed JSON", func(t *testing.T) {
		p := filepath.Join(dir, "bad.json")
		_ = os.WriteFile(p, []byte(`{not json`), 0o644)
		_, err := ImportFile(p)
		if err == nil {
			t.Error("expected parse error")
		}
	})
}

package credential

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestBYOJar_PlainRoundTrip exercises the bring-your-own jar path:
// missing file → empty Jar (not error), write → plaintext JSON file
// without the AGD1 magic prefix, mode 0600. This is what the
// `--cookiejar <path>` flag relies on.
func TestBYOJar_PlainRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "byo.json")

	t.Run("missing file returns empty jar", func(t *testing.T) {
		j, err := ReadJarPlain(path)
		if err != nil {
			t.Fatalf("expected nil error for missing file, got %v", err)
		}
		if j == nil || len(j.Cookies) != 0 {
			t.Errorf("expected empty jar, got %+v", j)
		}
	})

	t.Run("write then read round-trip", func(t *testing.T) {
		in := &Jar{
			Name: "anon",
			Cookies: []PersistedCookie{
				{Name: "session", Value: "secret-123", Domain: "x.com", Path: "/", Sensitive: true},
				{Name: "theme", Value: "dark", Domain: "x.com", Path: "/"},
			},
		}
		if err := WriteJarPlain(path, in); err != nil {
			t.Fatal(err)
		}
		got, err := ReadJarPlain(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Cookies) != 2 || got.Cookies[0].Value != "secret-123" {
			t.Errorf("round-trip lost data: %+v", got)
		}
	})

	t.Run("file is plaintext (NO AGD1 magic)", func(t *testing.T) {
		// This is the load-bearing rule of the BYO design — encrypting
		// would corrupt the user's chosen path with bytes only the
		// (possibly-nonexistent) profile key could decrypt.
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.HasPrefix(raw, []byte("AGD1")) {
			t.Errorf("BYO jar must NOT be encrypted; got AGD1 prefix at start of %s", path)
		}
		if !bytes.Contains(raw, []byte(`"name": "anon"`)) {
			t.Errorf("BYO jar should be readable JSON, got: %s", raw[:min(80, len(raw))])
		}
	})

	t.Run("file mode is 0600", func(t *testing.T) {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("expected mode 0600, got %o", perm)
		}
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

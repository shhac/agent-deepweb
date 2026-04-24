package credential

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/shhac/agent-deepweb/internal/config"
)

// jarPath returns the per-profile jar location: profiles/<name>/jar.json.
// One subdirectory per profile keeps the layout open for future per-profile
// auxiliary state without polluting the top-level config dir.
func jarPath(name string) string {
	return filepath.Join(config.ConfigDir(), "profiles", name, "jar.json")
}

// ReadJar loads and decrypts the on-disk jar for the named profile.
// Returns os.ErrNotExist if no jar file is present (a normal pre-login
// state). Callers must not print cookie values directly.
func ReadJar(name string) (*Jar, error) { return readJar(name) }

func readJar(name string) (*Jar, error) {
	data, err := os.ReadFile(jarPath(name))
	if err != nil {
		return nil, err
	}
	plaintext, err := decryptJarBytes(name, data)
	if err != nil {
		return nil, err
	}
	var j Jar
	if err := json.Unmarshal(plaintext, &j); err != nil {
		return nil, err
	}
	return &j, nil
}

// WriteJar persists the jar to disk encrypted with the profile's JarKey.
// The containing profiles/<name>/ directory is created with mode 0700.
func WriteJar(j *Jar) error {
	dir := filepath.Dir(jarPath(j.Name))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	plaintext, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	ciphertext, err := encryptJarBytes(j.Name, plaintext)
	if err != nil {
		return err
	}
	return os.WriteFile(jarPath(j.Name), ciphertext, 0o600)
}

func ClearJar(name string) error {
	err := os.Remove(jarPath(name))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ClearJarTree removes the entire profiles/<name>/ directory (jar + any
// future per-profile state). Called by Remove() so a deleted profile
// leaves no cookies behind.
func ClearJarTree(name string) error {
	err := os.RemoveAll(filepath.Dir(jarPath(name)))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ReadJarPlain loads a bring-your-own jar from an arbitrary file path.
// Plaintext JSON, no encryption — the caller chose the location and the
// trade-off. Used by the `--cookiejar <path>` flag, including the
// `--profile none --cookiejar <path>` LLM-authored-flow case. Returns
// (zero-value Jar, nil) if the file is missing — that's the "fresh jar"
// case, not an error.
func ReadJarPlain(path string) (*Jar, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Jar{}, nil
		}
		return nil, err
	}
	var j Jar
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, err
	}
	return &j, nil
}

// WriteJarPlain persists a bring-your-own jar to an arbitrary file path
// as plaintext JSON, mode 0600. The directory must already exist (we do
// not auto-create — caller-chosen paths shouldn't surprise users with
// new directories).
func WriteJarPlain(path string, j *Jar) error {
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

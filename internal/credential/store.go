package credential

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"

	"github.com/shhac/agent-deepweb/internal/config"
)

func indexPath() string {
	return filepath.Join(config.ConfigDir(), "credentials.json")
}

func secretsFilePath() string {
	return filepath.Join(config.ConfigDir(), "credentials.secrets.json")
}

func readIndex() (map[string]indexEntry, error) {
	data, err := os.ReadFile(indexPath())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]indexEntry{}, nil
		}
		return nil, err
	}
	var m map[string]indexEntry
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]indexEntry{}
	}
	return m, nil
}

func writeIndex(m map[string]indexEntry) error {
	dir := config.ConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(indexPath(), append(data, '\n'), 0o600)
}

func readSecretsFile() (map[string]Secrets, error) {
	data, err := os.ReadFile(secretsFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Secrets{}, nil
		}
		return nil, err
	}
	var m map[string]Secrets
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]Secrets{}
	}
	return m, nil
}

func writeSecretsFile(m map[string]Secrets) error {
	dir := config.ConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(secretsFilePath(), append(data, '\n'), 0o600)
}

// Store persists a new or updated credential. Secrets are written to the
// Keychain on macOS; on failure or non-macOS, fall back to the 0600 secrets
// file. Returns "keychain" or "file" so the caller can surface the choice.
//
// JarKey provisioning: if the supplied Secrets has no JarKey but the
// profile already has one stored, the existing key is preserved (so
// profile mutations don't invalidate the jar). If neither has a key,
// a fresh one is generated.
func Store(c Credential, s Secrets) (storage string, err error) {
	idx, err := readIndex()
	if err != nil {
		return "", err
	}
	if len(s.JarKey) == 0 {
		if existing, err := loadStoredSecrets(c.Name, idx); err == nil && len(existing.JarKey) > 0 {
			s.JarKey = existing.JarKey
		} else {
			k, err := generateJarKey()
			if err != nil {
				return "", err
			}
			s.JarKey = k
		}
	}
	// Auto-populate the passphrase when the caller didn't set one. On
	// initial add, this defaults to the primary-secret representative
	// value (so an existing user who never set --passphrase can still
	// escalate by typing the primary secret). On subsequent Store calls
	// (set-secret, etc.) the caller is responsible for passing the
	// right Passphrase — we only fill in the blank.
	if s.Passphrase == "" {
		s.Passphrase = DefaultPassphrase(c.Type, s)
		s.PassphraseAutoDerived = true
	}
	entry := entryFromCredential(c)

	if runtime.GOOS == "darwin" {
		if err := keychainStore(c.Name, s); err == nil {
			entry.KeychainManaged = true
			idx[c.Name] = entry
			if err := writeIndex(idx); err != nil {
				return "", err
			}
			// If a prior file-backed secret existed, clean it up.
			if sec, err := readSecretsFile(); err == nil {
				if _, ok := sec[c.Name]; ok {
					delete(sec, c.Name)
					_ = writeSecretsFile(sec)
				}
			}
			return "keychain", nil
		}
	}

	// File fallback.
	sec, err := readSecretsFile()
	if err != nil {
		return "", err
	}
	sec[c.Name] = s
	if err := writeSecretsFile(sec); err != nil {
		return "", err
	}
	idx[c.Name] = entry
	if err := writeIndex(idx); err != nil {
		return "", err
	}
	return "file", nil
}

// Remove deletes the credential and its secret material AND clears the
// profile's jar directory (cookies, encrypted state). A profile gone
// from the index leaves nothing behind.
func Remove(name string) error {
	idx, err := readIndex()
	if err != nil {
		return err
	}
	e, ok := idx[name]
	if !ok {
		return &NotFoundError{Name: name}
	}
	if e.KeychainManaged {
		keychainDelete(name)
	} else {
		if sec, err := readSecretsFile(); err == nil {
			delete(sec, name)
			_ = writeSecretsFile(sec)
		}
	}
	_ = ClearJarTree(name)
	delete(idx, name)
	return writeIndex(idx)
}

// loadStoredSecrets fetches the existing Secrets for name (Keychain or
// file). Used by Store to preserve fields like JarKey across mutations.
// Returns an error if the profile isn't in the index (caller treats that
// as "no existing key — generate a new one").
func loadStoredSecrets(name string, idx map[string]indexEntry) (Secrets, error) {
	e, ok := idx[name]
	if !ok {
		return Secrets{}, &NotFoundError{Name: name}
	}
	if e.KeychainManaged {
		return keychainGet(name)
	}
	sec, err := readSecretsFile()
	if err != nil {
		return Secrets{}, err
	}
	s, ok := sec[name]
	if !ok {
		return Secrets{}, &NotFoundError{Name: name}
	}
	return s, nil
}

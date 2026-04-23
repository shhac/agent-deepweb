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
func Store(c Credential, s Secrets) (storage string, err error) {
	idx, err := readIndex()
	if err != nil {
		return "", err
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

// Remove deletes the credential and its secret material.
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
	delete(idx, name)
	return writeIndex(idx)
}

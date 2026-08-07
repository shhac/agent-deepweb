package credential

import (
	"errors"
	"path/filepath"

	"github.com/shhac/agent-deepweb/internal/config"
	"github.com/shhac/lib-agent-cli/creds"
)

func indexPath() string {
	return filepath.Join(config.ConfigDir(), "credentials.json")
}

func secretsFilePath() string {
	return filepath.Join(config.ConfigDir(), "credentials.secrets.json")
}

// indexStore and secretsStore are the two backing files: 0600 writes into
// a 0700 parent, replacement by rename so a reader never sees a
// half-written document, and Update for a serialized read-modify-write.
//
// Both used to be hand-rolled with os.ReadFile/os.WriteFile, which carried
// a lost-update race: two concurrent invocations each built their write
// from a snapshot taken before the other landed. For the index that is
// worse than an ordinary lost write — the secret it pointed at is still in
// the keychain, but nothing references it any more, so `profile list`
// can't show it and `profile remove` can't look it up to revoke it.
//
// They are separate locks. Every path that needs both takes the index lock
// first and the secrets lock inside it; keep that order or the two can
// deadlock against each other.
func indexStore() creds.Store   { return creds.Store{Path: indexPath()} }
func secretsStore() creds.Store { return creds.Store{Path: secretsFilePath()} }

// errSkipWrite lets a mutate callback decline to persist anything without
// the update helpers treating it as a real failure — for the cases that
// deliberately wrote nothing before this change.
var errSkipWrite = errors.New("credential: skip write")

func readIndex() (map[string]indexEntry, error) {
	m := map[string]indexEntry{}
	if err := indexStore().Load(&m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]indexEntry{}
	}
	return m, nil
}

// updateIndex applies mutate to the credential index under one exclusive
// lock spanning load, mutate and save. Returning an error from mutate
// aborts without writing, which is what keeps a not-found lookup from
// rewriting the file.
func updateIndex(mutate func(m map[string]indexEntry) error) error {
	m := map[string]indexEntry{}
	err := indexStore().Update(&m, func() error {
		if m == nil {
			m = map[string]indexEntry{}
		}
		return mutate(m)
	})
	if errors.Is(err, errSkipWrite) {
		return nil
	}
	return err
}

func readSecretsFile() (map[string]Secrets, error) {
	m := map[string]Secrets{}
	if err := secretsStore().Load(&m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]Secrets{}
	}
	return m, nil
}

// updateSecretsFile is updateIndex's counterpart for the file-fallback
// secrets store. Only ever called from inside the index lock — see the
// ordering note on indexStore.
func updateSecretsFile(mutate func(m map[string]Secrets) error) error {
	m := map[string]Secrets{}
	err := secretsStore().Update(&m, func() error {
		if m == nil {
			m = map[string]Secrets{}
		}
		return mutate(m)
	})
	if errors.Is(err, errSkipWrite) {
		return nil
	}
	return err
}

// Store persists a new or updated credential. Secrets are written to the
// Keychain on macOS; on failure or non-macOS, fall back to the 0600 secrets
// file. Returns "keychain" or "file" so the caller can surface the choice.
//
// JarKey provisioning: if the supplied Secrets has no JarKey but the
// profile already has one stored, the existing key is preserved (so
// profile mutations don't invalidate the jar). If neither has a key,
// a fresh one is generated.
//
// The whole body runs inside the index lock, so the keychain write and the
// index entry that points at it land together: a concurrent writer can no
// longer erase the entry between the two and strand the secret.
func Store(c Credential, s Secrets) (storage string, err error) {
	if err := updateIndex(func(idx map[string]indexEntry) error {
		if err := provisionJarKey(c.Name, &s, idx); err != nil {
			return err
		}
		provisionPassphrase(c, &s)
		entry := entryFromCredential(c)

		if DefaultBackend.Available() {
			if err := DefaultBackend.Store(c.Name, s); err == nil {
				entry.KeychainManaged = true
				idx[c.Name] = entry
				// If a prior file-backed secret existed, clean it up.
				_ = updateSecretsFile(func(sec map[string]Secrets) error {
					if _, ok := sec[c.Name]; !ok {
						return errSkipWrite
					}
					delete(sec, c.Name)
					return nil
				})
				storage = "keychain"
				return nil
			}
		}

		// File fallback.
		if err := updateSecretsFile(func(sec map[string]Secrets) error {
			sec[c.Name] = s
			return nil
		}); err != nil {
			return err
		}
		idx[c.Name] = entry
		storage = "file"
		return nil
	}); err != nil {
		return "", err
	}
	return storage, nil
}

// provisionJarKey ensures s.JarKey is populated before Store persists.
// Precedence: caller-supplied key > existing key for this profile
// (preserves jar encryption across mutations) > freshly-generated key.
// Returns an error only if generation fails — everything else is
// idempotent and non-destructive.
func provisionJarKey(name string, s *Secrets, idx map[string]indexEntry) error {
	if len(s.JarKey) > 0 {
		return nil
	}
	if existing, err := loadStoredSecrets(name, idx); err == nil && len(existing.JarKey) > 0 {
		s.JarKey = existing.JarKey
		return nil
	}
	k, err := generateJarKey()
	if err != nil {
		return err
	}
	s.JarKey = k
	return nil
}

// provisionPassphrase auto-populates s.Passphrase when the caller
// didn't supply one. Default is the primary-secret representative
// value for this auth type — so an existing user who never ran
// `profile add --passphrase` can still escalate by retyping the
// token/password. On subsequent Store calls (set-secret, etc.) the
// caller is responsible for passing the right Passphrase; we only
// fill in the blank.
func provisionPassphrase(c Credential, s *Secrets) {
	if s.Passphrase != "" {
		return
	}
	s.Passphrase = DefaultPassphrase(c.Type, *s)
	s.PassphraseAutoDerived = true
}

// Remove deletes the credential and its secret material AND clears the
// profile's jar directory (cookies, encrypted state). A profile gone
// from the index leaves nothing behind.
func Remove(name string) error {
	return updateIndex(func(idx map[string]indexEntry) error {
		e, ok := idx[name]
		if !ok {
			return &NotFoundError{Name: name}
		}
		if e.KeychainManaged {
			DefaultBackend.Delete(name)
		} else {
			_ = updateSecretsFile(func(sec map[string]Secrets) error {
				delete(sec, name)
				return nil
			})
		}
		_ = ClearJarTree(name)
		delete(idx, name)
		return nil
	})
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
		return DefaultBackend.Get(name)
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

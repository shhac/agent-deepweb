package credential

import "fmt"

// List returns all credential metadata in the index (no secrets).
func List() ([]Credential, error) {
	idx, err := readIndex()
	if err != nil {
		return nil, err
	}
	out := make([]Credential, 0, len(idx))
	for _, e := range idx {
		out = append(out, entryToCredential(e))
	}
	return out, nil
}

// GetMetadata returns the credential metadata without secret material.
// Safe to print — contains no secret values.
func GetMetadata(name string) (*Credential, error) {
	idx, err := readIndex()
	if err != nil {
		return nil, err
	}
	e, ok := idx[name]
	if !ok {
		return nil, &NotFoundError{Name: name}
	}
	c := entryToCredential(e)
	return &c, nil
}

// Resolve returns the credential plus its secret material, ready to attach
// to an outgoing request. The caller must not print or log any field of
// the returned Secrets struct.
func Resolve(name string) (*Resolved, error) {
	c, err := GetMetadata(name)
	if err != nil {
		return nil, err
	}
	idx, err := readIndex()
	if err != nil {
		return nil, err
	}
	e := idx[name]

	var secrets Secrets
	if e.KeychainManaged {
		s, err := keychainGet(name)
		if err != nil {
			return nil, fmt.Errorf("read keychain for %q: %w", name, err)
		}
		secrets = s
	} else {
		sec, err := readSecretsFile()
		if err != nil {
			return nil, err
		}
		s, ok := sec[name]
		if !ok {
			return nil, fmt.Errorf("secret payload for %q missing", name)
		}
		secrets = s
	}
	return &Resolved{Credential: *c, Secrets: secrets}, nil
}

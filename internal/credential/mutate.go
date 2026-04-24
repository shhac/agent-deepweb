package credential

// The setter functions are used by the `creds set-*` / `creds allow-*`
// CLI commands. Each updates one field of the metadata index entry
// without touching secrets.

// SetDomains replaces the credential's domain allowlist.
func SetDomains(name string, domains []string) error {
	return mutateEntry(name, func(e *indexEntry) { e.Domains = domains })
}

// SetPaths replaces the credential's path allowlist.
func SetPaths(name string, paths []string) error {
	return mutateEntry(name, func(e *indexEntry) { e.Paths = paths })
}

// SetHealth updates the stored health-check URL (used by `creds test`).
func SetHealth(name, url string) error {
	return mutateEntry(name, func(e *indexEntry) { e.Health = url })
}

// SetUserAgent updates the credential's User-Agent override. "" to clear.
func SetUserAgent(name, ua string) error {
	return mutateEntry(name, func(e *indexEntry) { e.UserAgent = ua })
}

// SetDefaultHeaders replaces the credential's default-headers map.
func SetDefaultHeaders(name string, headers map[string]string) error {
	return mutateEntry(name, func(e *indexEntry) { e.DefaultHeaders = headers })
}

// SetAllowHTTP toggles http:// permission on a credential. Off-by-default.
func SetAllowHTTP(name string, allow bool) error {
	return mutateEntry(name, func(e *indexEntry) { e.AllowHTTP = allow })
}

// SetSensitiveHeaders replaces the per-profile "force redact" header list.
// Header names are stored as supplied; the redactor compares case-
// insensitively on the way out.
func SetSensitiveHeaders(name string, headers []string) error {
	return mutateEntry(name, func(e *indexEntry) { e.SensitiveHeaders = headers })
}

// SetVisibleHeaders replaces the per-profile "force visible" header list.
// Header names are stored as supplied; the redactor compares case-
// insensitively on the way out. Widening visibility is escalation — the
// CLI layer gates this behind --passphrase.
func SetVisibleHeaders(name string, headers []string) error {
	return mutateEntry(name, func(e *indexEntry) { e.VisibleHeaders = headers })
}

// mutateEntry loads the index, applies fn to the named entry, and writes
// it back. Returns NotFoundError if the credential doesn't exist.
func mutateEntry(name string, fn func(*indexEntry)) error {
	idx, err := readIndex()
	if err != nil {
		return err
	}
	e, ok := idx[name]
	if !ok {
		return &NotFoundError{Name: name}
	}
	fn(&e)
	idx[name] = e
	return writeIndex(idx)
}

// Package credential stores named auth credentials and their secret values.
// The index file (credentials.json) holds non-secret metadata (name, auth
// type, domain allowlist); secret values go to the macOS Keychain when
// available, or to credentials.secrets.json (mode 0600) on other platforms.
// No command in this package exposes secret values back to the caller as a
// plain string — consumers receive opaque Secrets that only the HTTP
// client knows how to apply.
//
// File layout inside this package:
//
//	credential.go   Type definitions (Credential, Secrets, Resolved,
//	                NotFoundError, indexEntry) + entryToCredential.
//	store.go        Index/secrets file I/O + Store/Remove (provisions JarKey).
//	query.go        List / GetMetadata / Resolve.
//	mutate.go       Per-field setters (SetDomains, SetPaths, …).
//	match.go        Host/port/path matching for URL allowlist.
//	cookie.go       PersistedCookie + classification.
//	jar.go          Per-profile encrypted Jar (cookies, optional token,
//	                expiry) + AES-256-GCM read/write at profiles/<name>/jar.json.
//	notfound.go     WrapNotFound helper for CLI callers.
//	keychain.go     macOS Keychain adapter.
package credential

import "fmt"

const (
	AuthBearer = "bearer"
	AuthBasic  = "basic"
	AuthCookie = "cookie"
	AuthForm   = "form"
	AuthCustom = "custom"
)

// Credential is the LLM-safe view of a stored credential: it has no
// secret values on it, only metadata. Use Resolve to get a credential
// bound with its secrets for actual HTTP use.
type Credential struct {
	Name           string            `json:"name"`
	Type           string            `json:"type"`    // one of Auth* constants
	Domains        []string          `json:"domains"` // host[:port] allowlist (exact or *.wildcard)
	Paths          []string          `json:"paths,omitempty"`
	DefaultHeaders map[string]string `json:"default_headers,omitempty"` // non-secret headers applied to every request
	UserAgent      string            `json:"user_agent,omitempty"`      // overrides default User-Agent when set
	Health         string            `json:"health,omitempty"`
	Notes          string            `json:"notes,omitempty"`
	AllowHTTP      bool              `json:"allow_http,omitempty"` // permit http:// (not just https://); human-set
	Storage        string            `json:"storage,omitempty"`    // "keychain" or "file"
}

// Secrets holds the actual secret material for a credential. This struct
// never leaves the binary — it's applied to outgoing HTTP requests and
// discarded.
type Secrets struct {
	// Bearer / custom token
	Token  string `json:"token,omitempty"`
	Header string `json:"header,omitempty"` // header to set (default "Authorization")
	Prefix string `json:"prefix,omitempty"` // token prefix (default "Bearer ")

	// Basic
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`

	// Cookie (raw Cookie header value; applied verbatim)
	Cookie string `json:"cookie,omitempty"`

	// Custom: map of header name → value, applied verbatim.
	Headers map[string]string `json:"headers,omitempty"`

	// Form login: used by the `login` command to produce a session.
	// Never sent with regular fetches — fetches use the derived session.
	LoginURL      string            `json:"login_url,omitempty"`
	LoginMethod   string            `json:"login_method,omitempty"`
	LoginFormat   string            `json:"login_format,omitempty"`
	UsernameField string            `json:"username_field,omitempty"`
	PasswordField string            `json:"password_field,omitempty"`
	ExtraFields   map[string]string `json:"extra_fields,omitempty"`
	SuccessStatus int               `json:"success_status,omitempty"`
	TokenPath     string            `json:"token_path,omitempty"`
	SessionTTL    string            `json:"session_ttl,omitempty"`

	// LoginBodyTemplate, if set, overrides the default form/JSON login
	// body construction. The template is a JSON string with `{{username}}`,
	// `{{password}}`, and `{{<extra-field-name>}}` placeholders; substituted
	// values are JSON-string-escaped so embedded quotes/backslashes don't
	// produce malformed output. Content-Type is forced to application/json
	// when this is set. Use for odd login shapes — GraphQL-mutation logins,
	// OAuth password-grant bodies that don't fit flat form/JSON, etc.
	LoginBodyTemplate string `json:"login_body_template,omitempty"`

	// JarKey is a 32-byte AES-256 key used to encrypt the profile's jar
	// file (profiles/<name>/jar.json). Provisioned at profile-add time
	// and preserved across mutations; cleared on profile remove. Stored
	// alongside the primary secret (Keychain on macOS, secrets file
	// elsewhere) so its protection matches the primary secret's.
	JarKey []byte `json:"jar_key,omitempty"`

	// Passphrase is the human-level authorization key used to authenticate
	// modifications to the profile (allow, allow-path, set-default-header,
	// set-allow-http, set-secret, jar mark-visible). At add time the
	// human can optionally set a short friendly phrase via --passphrase;
	// otherwise it defaults to a representative value of the primary
	// secret (bearer token, password, etc.) — see DefaultPassphrase.
	//
	// Unlike the primary secret (used by the HTTP client), the passphrase
	// is verified (constant-time compare) at escalation time. A wrong
	// value errors cleanly — no silent profile breakage. The LLM without
	// the passphrase can't perform any modification; the oracle surface
	// is mitigated by a 12-char minimum when explicitly set and by the
	// harness-level allowlist that denies escalation commands in the
	// first place.
	Passphrase string `json:"passphrase,omitempty"`

	// PassphraseAutoDerived is true when the stored Passphrase was
	// defaulted from the primary secret (the user didn't pass
	// --passphrase at add time). set-secret uses this: when true,
	// the Passphrase is re-derived from the NEW primary on rotation;
	// when false (human-chosen passphrase), it persists.
	PassphraseAutoDerived bool `json:"passphrase_auto_derived,omitempty"`
}

// Resolved is the internal view used by the HTTP layer: metadata + live
// secrets (and optional session for form auth).
type Resolved struct {
	Credential
	Secrets Secrets
}

type NotFoundError struct{ Name string }

func (e *NotFoundError) Error() string { return fmt.Sprintf("profile %q not found", e.Name) }

// PrimarySecretFlagHint returns the flag(s) a human must pass to escalate
// a profile of the given auth type. Used in human-fixable hint text so
// the exact command to run is obvious, not just "re-supply the primary
// secret." Returns "--token <T>" for bearer/custom, "--username <U>
// --password <P>" for basic/form, etc.
func PrimarySecretFlagHint(authType string) string {
	switch authType {
	case AuthBearer:
		return "--token <T>"
	case AuthBasic:
		return "--username <U> --password <P>"
	case AuthCookie:
		return "--cookie <C>"
	case AuthCustom:
		return "--custom-header 'K: V'"
	case AuthForm:
		return "--username <U> --password <P>"
	default:
		return "--token <T>"
	}
}

// indexEntry is the on-disk JSON shape for a single credential's metadata.
// When KeychainManaged is false (non-macOS), the matching Secrets live
// in a sibling file keyed by Name. Secret values never appear in this
// struct.
type indexEntry struct {
	Name            string            `json:"name"`
	Type            string            `json:"type"`
	Domains         []string          `json:"domains"`
	Paths           []string          `json:"paths,omitempty"`
	DefaultHeaders  map[string]string `json:"default_headers,omitempty"`
	UserAgent       string            `json:"user_agent,omitempty"`
	Health          string            `json:"health,omitempty"`
	Notes           string            `json:"notes,omitempty"`
	AllowHTTP       bool              `json:"allow_http,omitempty"`
	KeychainManaged bool              `json:"keychain_managed,omitempty"`
}

func entryToCredential(e indexEntry) Credential {
	storage := "file"
	if e.KeychainManaged {
		storage = "keychain"
	}
	return Credential{
		Name:           e.Name,
		Type:           e.Type,
		Domains:        e.Domains,
		Paths:          e.Paths,
		DefaultHeaders: e.DefaultHeaders,
		UserAgent:      e.UserAgent,
		Health:         e.Health,
		Notes:          e.Notes,
		AllowHTTP:      e.AllowHTTP,
		Storage:        storage,
	}
}

// entryFromCredential is the dual: flatten a Credential into an index entry
// (minus the Storage/KeychainManaged fields, which callers set based on
// where the secret ended up).
func entryFromCredential(c Credential) indexEntry {
	return indexEntry{
		Name:           c.Name,
		Type:           c.Type,
		Domains:        c.Domains,
		Paths:          c.Paths,
		DefaultHeaders: c.DefaultHeaders,
		UserAgent:      c.UserAgent,
		Health:         c.Health,
		Notes:          c.Notes,
		AllowHTTP:      c.AllowHTTP,
	}
}

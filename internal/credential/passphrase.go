package credential

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// MinPassphraseLength is the floor we enforce when a human explicitly
// sets --passphrase at profile add or change-passphrase time. 12 chars
// across a typical 64-char set is ~7×10^21 combinations — far beyond
// feasible brute force through the escalation-command oracle.
//
// The auto-derived passphrase (when the user didn't explicitly set one)
// bypasses this floor: it IS the primary secret, which is whatever
// length the upstream service mandates.
const MinPassphraseLength = 12

// ErrPassphraseMismatch is the sentinel for a failed verification.
// Callers translate to a fixable_by:agent APIError at the CLI edge.
var ErrPassphraseMismatch = errors.New("passphrase does not match")

// DefaultPassphrase returns the representative primary-secret value for
// the given auth type — what the stored Passphrase will be if the user
// doesn't supply --passphrase at add time.
//
// For most types the primary secret is a single string (token, password,
// cookie value). For custom auth — which is a header map with no single
// "primary" — we marshal the sorted headers so the default is
// deterministic. Humans using custom auth are expected to set an
// explicit --passphrase rather than type the marshaled map.
func DefaultPassphrase(authType string, s Secrets) string {
	switch authType {
	case AuthBearer:
		return s.Token
	case AuthBasic, AuthForm:
		return s.Password
	case AuthCookie:
		return s.Cookie
	case AuthCustom:
		keys := make([]string, 0, len(s.Headers))
		for k := range s.Headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		pairs := make([]string, 0, len(keys))
		for _, k := range keys {
			pairs = append(pairs, k+": "+s.Headers[k])
		}
		b, _ := json.Marshal(pairs)
		return string(b)
	default:
		return ""
	}
}

// ValidatePassphrase enforces MinPassphraseLength on an explicitly-set
// passphrase. Empty is allowed (caller uses DefaultPassphrase fallback).
func ValidatePassphrase(p string) error {
	if p == "" {
		return nil
	}
	if len(p) < MinPassphraseLength {
		return fmt.Errorf("passphrase must be at least %d characters (got %d)", MinPassphraseLength, len(p))
	}
	if strings.TrimSpace(p) != p {
		return errors.New("passphrase must not have leading or trailing whitespace")
	}
	return nil
}

// VerifyPassphrase loads the named profile and constant-time-compares
// the supplied passphrase against the stored one. Returns nil on match,
// ErrPassphraseMismatch on mismatch, or a profile-lookup error.
//
// The compare is constant-time so a timing-attack oracle can't shortcut
// the 12-char search space. The harness-level allowlist (SKILL.md)
// should already deny the LLM from running escalation commands, making
// this a defense-in-depth layer rather than the primary boundary.
func VerifyPassphrase(name, supplied string) error {
	r, err := Resolve(name)
	if err != nil {
		return err
	}
	if r.Secrets.Passphrase == "" {
		// Should never happen post-Store (Store auto-populates), but
		// be defensive: treat as mismatch rather than permit an empty
		// passphrase to authenticate.
		return ErrPassphraseMismatch
	}
	if subtle.ConstantTimeCompare([]byte(supplied), []byte(r.Secrets.Passphrase)) != 1 {
		return ErrPassphraseMismatch
	}
	return nil
}

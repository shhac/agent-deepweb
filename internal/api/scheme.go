package api

import (
	"net/url"
	"strings"

	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

// enforceScheme refuses http:// for auth-attaching credentials unless
// (a) the host is loopback (dev servers) or (b) the credential has
// AllowHTTP=true (human-set via `creds set-allow-http`). The per-request
// allow-http flag was removed in v2 — http opt-in is a per-credential
// property only.
func enforceScheme(u *url.URL, auth *credential.Resolved) error {
	if u == nil || auth == nil {
		return nil
	}
	if strings.EqualFold(u.Scheme, "https") {
		return nil
	}
	if !strings.EqualFold(u.Scheme, "http") {
		return nil // other schemes (ws etc) — out of scope
	}
	if isLoopback(u.Hostname()) {
		return nil
	}
	if auth.AllowHTTP {
		return nil
	}
	return agenterrors.Newf(agenterrors.FixableByHuman,
		"refusing to send credential %q over http://", auth.Name).
		WithHint("Credentials travel in cleartext on http://. Use https:// or run 'agent-deepweb creds set-allow-http " + auth.Name + " true' to opt in for this credential.")
}

// isLoopback returns true for hosts that are safe to send credentials to
// over plain http (local dev). Includes the .localhost TLD (RFC 6761).
func isLoopback(host string) bool {
	host = strings.ToLower(host)
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || strings.HasSuffix(host, ".localhost")
}

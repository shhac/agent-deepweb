package api

import (
	"net/url"
	"strings"

	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

// enforceScheme refuses http:// for auth-attaching credentials unless
// (a) the host is loopback (dev servers), (b) the credential has
// AllowHTTP=true, or (c) the per-request perRequestAllow is set. Errors
// are fixable_by:human since the fix is a human re-registering/consent.
func enforceScheme(u *url.URL, auth *credential.Resolved, perRequestAllow bool) error {
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
	if auth.AllowHTTP || perRequestAllow {
		return nil
	}
	return agenterrors.Newf(agenterrors.FixableByHuman,
		"refusing to send credential %q over http://", auth.Name).
		WithHint("Credentials travel in cleartext on http://. Ask the user to use https:// or (human-only) run 'agent-deepweb creds set-allow-http " + auth.Name + " true'.")
}

// isLoopback returns true for hosts that are safe to send credentials to
// over plain http (local dev). Includes the .localhost TLD (RFC 6761).
func isLoopback(host string) bool {
	host = strings.ToLower(host)
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || strings.HasSuffix(host, ".localhost")
}

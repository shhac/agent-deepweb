// Package shared holds helpers used by multiple cobra command packages:
// credential resolution by flag or URL, global flag struct, and small
// CLI primitives. Keeps the command packages from having a cyclic dep on
// internal/cli.
package shared

import (
	"net/url"
	"strings"

	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

// GlobalFlags is the set of flags registered on the root command.
type GlobalFlags struct {
	Auth    string
	Format  string
	Timeout int
}

// Globals is injected from cli.root — keeps command packages from depending
// on the root package directly.
type Globals func() *GlobalFlags

// ResolveAuth resolves the credential to use for a given URL. Resolution rules:
//   - If flagAuth is set, use that credential; fail if the URL isn't in its
//     host/path allowlist.
//   - Else, look up credentials whose allowlist matches the URL.
//     Exactly one → use it.
//     Many → error naming the candidates so the caller can pick.
//     Zero → error directing the caller to register a credential or
//     pass --no-auth explicitly. (We never silently fall through to
//     anonymous — that turned agent-deepweb into a generic outbound HTTP
//     client and was a v1 hole.)
func ResolveAuth(rawURL, flagAuth string) (*credential.Resolved, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil, agenterrors.Newf(agenterrors.FixableByAgent,
			"URL %q is not absolute", rawURL).
			WithHint("Use scheme://host/path form")
	}

	if flagAuth != "" {
		c, err := credential.Resolve(flagAuth)
		if err != nil {
			return nil, credential.ClassifyLookupErr(err, flagAuth)
		}
		if !c.MatchesURL(u) {
			return nil, agenterrors.Newf(agenterrors.FixableByHuman,
				"credential %q is not allowed on %s (host/path not in allowlist)", flagAuth, rawURL).
				WithHint("Ask the user to run 'agent-deepweb creds allow " + flagAuth + " " + u.Host + "' or widen --path")
		}
		return c, nil
	}

	matches, err := credential.FindByURL(u)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByHuman)
	}
	switch len(matches) {
	case 0:
		return nil, agenterrors.Newf(agenterrors.FixableByHuman,
			"no credential matches %s", rawURL).
			WithHint("Ask the user to register one with 'agent-deepweb creds add', or pass --no-auth to make an anonymous request explicitly.")
	case 1:
		return credential.Resolve(matches[0].Name)
	default:
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, m.Name)
		}
		return nil, agenterrors.Newf(agenterrors.FixableByAgent,
			"multiple credentials match %s: %s", rawURL, strings.Join(names, ", ")).
			WithHint("Pass --auth <name> to pick one")
	}
}

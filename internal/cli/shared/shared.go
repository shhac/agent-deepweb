// Package shared holds helpers used by multiple cobra command packages:
// profile resolution by flag or URL, global flag struct, and small CLI
// primitives. Keeps the command packages from having a cyclic dep on
// internal/cli.
package shared

import (
	"net/url"
	"strings"

	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

// ProfileNone is the sentinel value for `--profile none`: explicit
// anonymous mode, no auth attached, no allowlist check, no jar (unless
// --cookiejar is also passed).
const ProfileNone = "none"

// GlobalFlags is the set of flags registered on the root command.
type GlobalFlags struct {
	Profile string
	Format  string
	Timeout int
}

// Globals is injected from cli.root — keeps command packages from depending
// on the root package directly.
type Globals func() *GlobalFlags

// ResolveProfile resolves the profile to use for a given URL. Resolution
// rules:
//   - If profileFlag == "none", return (nil, nil) — explicit anonymous.
//   - If profileFlag is set, use that profile; fail if the URL isn't in
//     its host/path allowlist.
//   - Else, look up profiles whose allowlist matches the URL. Exactly
//     one → use it. Many → error naming the candidates so the caller can
//     pick. Zero → error directing the caller to register a profile or
//     pass --profile none. (We never silently fall through to anonymous —
//     that turned agent-deepweb into a generic outbound HTTP client and
//     was a v1 hole.)
func ResolveProfile(rawURL, profileFlag string) (*credential.Resolved, error) {
	if profileFlag == ProfileNone {
		return nil, nil
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil, agenterrors.Newf(agenterrors.FixableByAgent,
			"URL %q is not absolute", rawURL).
			WithHint("Use scheme://host/path form")
	}

	if profileFlag != "" {
		c, err := credential.Resolve(profileFlag)
		if err != nil {
			return nil, credential.ClassifyLookupErr(err, profileFlag)
		}
		if !c.MatchesURL(u) {
			return nil, agenterrors.Newf(agenterrors.FixableByHuman,
				"profile %q is not allowed on %s (host/path not in allowlist)", profileFlag, rawURL).
				WithHint("Ask the user to run 'agent-deepweb profile allow " + profileFlag + " " + u.Host + "' or widen --path")
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
			"no profile matches %s", rawURL).
			WithHint("Ask the user to register one with 'agent-deepweb profile add', or pass --profile none to make an anonymous request explicitly.")
	case 1:
		return credential.Resolve(matches[0].Name)
	default:
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, m.Name)
		}
		return nil, agenterrors.Newf(agenterrors.FixableByAgent,
			"multiple profiles match %s: %s", rawURL, strings.Join(names, ", ")).
			WithHint("Pass --profile <name> to pick one")
	}
}

// Package shared holds helpers used by multiple cobra command packages:
// agent-mode detection, credential resolution by flag or URL, global flag
// struct. Keeps the command packages from having a cyclic dep on internal/cli.
package shared

import (
	"net/url"
	"os"
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

// IsAgentMode reports whether the binary is running under an LLM-agent
// harness. Set AGENT_DEEPWEB_MODE=agent in your skill/hook config. In
// agent mode, human-only commands refuse to run.
func IsAgentMode() bool {
	return strings.EqualFold(os.Getenv("AGENT_DEEPWEB_MODE"), "agent")
}

// RefuseInAgentMode returns an APIError if we're in agent mode, else nil.
func RefuseInAgentMode(verb string) error {
	if !IsAgentMode() {
		return nil
	}
	return agenterrors.Newf(agenterrors.FixableByHuman,
		"%s is a human-only operation", verb).
		WithHint("This command writes or reveals secrets and is refused in agent mode. Ask the user to run it.")
}

// ResolveAuth resolves the credential to use for a given URL. The URL is
// parsed here so host+port+path matching can happen against the credential's
// allowlist. Resolution rules:
//   - If flagAuth is set, use that credential; fail if the URL isn't in its
//     host/path allowlist.
//   - Else, look up credentials whose allowlist matches the URL. Exactly
//     one → use it. Zero → nil (caller decides if anonymous is allowed).
//     Many → error asking for --auth <name>.
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
			if _, ok := err.(*credential.NotFoundError); ok {
				return nil, agenterrors.Newf(agenterrors.FixableByAgent,
					"credential %q not found", flagAuth).
					WithHint("Run 'agent-deepweb creds list' to see available credentials")
			}
			return nil, agenterrors.Wrap(err, agenterrors.FixableByHuman)
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
		return nil, nil
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

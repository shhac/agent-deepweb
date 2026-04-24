package profile

import (
	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/credential"
)

// `profile allow` and `profile allow-path` are escalation paths — they
// widen what hosts/paths an existing credential will be sent to. Both
// require --passphrase verification (see internal/cli/shared/secret_assert.go).
//
// `disallow` and `disallow-path` shrink the allowlist. Shrinking is
// not escalation, so they don't require the passphrase.

func registerAllow(parent *cobra.Command) {
	a := &shared.PassphraseAssert{}
	cmd := &cobra.Command{
		Use:   "allow <name> <domain>",
		Short: "Add host[:port] to allowlist (requires --passphrase)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return addDomain(args[0], args[1], a)
		},
	}
	shared.BindPassphraseAssertFlags(cmd, a)
	parent.AddCommand(cmd)
}

func registerDisallow(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "disallow <name> <domain>",
		Short: "Remove host[:port] from allowlist",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return removeDomain(args[0], args[1])
		},
	})
}

func registerAllowPath(parent *cobra.Command) {
	a := &shared.PassphraseAssert{}
	cmd := &cobra.Command{
		Use:   "allow-path <name> <pattern>",
		Short: "Add URL path pattern to allowlist (requires --passphrase)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return addPath(args[0], args[1], a)
		},
	}
	shared.BindPassphraseAssertFlags(cmd, a)
	parent.AddCommand(cmd)
}

func registerDisallowPath(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "disallow-path <name> <pattern>",
		Short: "Remove URL path pattern",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return removePath(args[0], args[1])
		},
	})
}

// addDomain widens the host allowlist (escalation — passphrase verified).
func addDomain(name, domain string, assert *shared.PassphraseAssert) error {
	c, err := shared.LoadAndAssert(name, assert)
	if err != nil {
		return shared.Fail(err)
	}
	updated, noop := mutateSlice(c.Domains, domain, true)
	return commitDomains(name, updated, noop)
}

// removeDomain narrows the host allowlist. No passphrase needed.
func removeDomain(name, domain string) error {
	c, err := shared.LoadProfileMetadata(name)
	if err != nil {
		return shared.Fail(err)
	}
	updated, noop := mutateSlice(c.Domains, domain, false)
	return commitDomains(name, updated, noop)
}

// addPath widens the path allowlist (escalation).
func addPath(name, pattern string, assert *shared.PassphraseAssert) error {
	c, err := shared.LoadAndAssert(name, assert)
	if err != nil {
		return shared.Fail(err)
	}
	updated, noop := mutateSlice(c.Paths, pattern, true)
	return commitPaths(name, updated, noop)
}

// removePath narrows the path allowlist. No passphrase needed.
func removePath(name, pattern string) error {
	c, err := shared.LoadProfileMetadata(name)
	if err != nil {
		return shared.Fail(err)
	}
	updated, noop := mutateSlice(c.Paths, pattern, false)
	return commitPaths(name, updated, noop)
}

// commitDomains writes the updated host list and prints the canonical
// success envelope (or a noop variant when nothing changed).
func commitDomains(name string, updated []string, noop bool) error {
	if noop {
		shared.PrintOK(map[string]any{"name": name, "domains": updated, "noop": true})
		return nil
	}
	if err := credential.SetDomains(name, updated); err != nil {
		return shared.FailHuman(err)
	}
	shared.PrintOK(map[string]any{"name": name, "domains": updated})
	return nil
}

// commitPaths is the path-allowlist analogue of commitDomains.
func commitPaths(name string, updated []string, noop bool) error {
	if noop {
		shared.PrintOK(map[string]any{"name": name, "paths": updated, "noop": true})
		return nil
	}
	if err := credential.SetPaths(name, updated); err != nil {
		return shared.FailHuman(err)
	}
	shared.PrintOK(map[string]any{"name": name, "paths": updated})
	return nil
}

// mutateSlice is the idempotent append/remove logic shared by domains
// and paths. Returns the updated list and a noop flag (true when `add`
// was requested but the item was already present).
func mutateSlice(existing []string, item string, add bool) (updated []string, noop bool) {
	if add {
		for _, d := range existing {
			if d == item {
				return existing, true
			}
		}
		return append(existing, item), false
	}
	out := existing[:0:0]
	for _, d := range existing {
		if d != item {
			out = append(out, d)
		}
	}
	return out, false
}

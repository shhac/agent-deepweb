package profile

import (
	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/credential"
)

// `creds allow` and `creds allow-path` are escalation paths — they widen
// what hosts/paths an existing credential will be sent to. We protect
// these by requiring the credential's primary secret be re-asserted (see
// secret_assert.go). Wrong value → silent overwrite, broken cred.
//
// `disallow` and `disallow-path` shrink the allowlist. Shrinking is not
// escalation, so they don't require the primary secret.

func registerAllow(parent *cobra.Command) {
	a := &shared.SecretAssert{}
	cmd := &cobra.Command{
		Use:   "allow <name> <domain>",
		Short: "Add host[:port] to allowlist (re-supply credential's primary secret)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mutateDomains(args[0], args[1], true, a)
		},
	}
	shared.BindSecretAssertFlags(cmd, a)
	parent.AddCommand(cmd)
}

func registerDisallow(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "disallow <name> <domain>",
		Short: "Remove host[:port] from allowlist",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mutateDomains(args[0], args[1], false, nil)
		},
	})
}

func registerAllowPath(parent *cobra.Command) {
	a := &shared.SecretAssert{}
	cmd := &cobra.Command{
		Use:   "allow-path <name> <pattern>",
		Short: "Add URL path pattern to allowlist (re-supply credential's primary secret)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mutatePaths(args[0], args[1], true, a)
		},
	}
	shared.BindSecretAssertFlags(cmd, a)
	parent.AddCommand(cmd)
}

func registerDisallowPath(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "disallow-path <name> <pattern>",
		Short: "Remove URL path pattern",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mutatePaths(args[0], args[1], false, nil)
		},
	})
}

// mutateSlice is the idempotent append/remove logic shared by domains and
// paths. Returns the updated list and a noop flag (true when `add` was
// requested but the item was already present).
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

// mutateDomains adds or removes a host from the credential's allowlist.
// When add=true, this is escalation — `assert` must contain the primary
// secret, which is re-applied via escalateOverwrite. When add=false,
// `assert` is ignored.
func mutateDomains(name, domain string, add bool, assert *shared.SecretAssert) error {
	c, err := credential.GetMetadata(name)
	if err != nil {
		return shared.Fail(credential.ClassifyLookupErr(err, name))
	}
	if add {
		if err := shared.ApplySecretAssert(c, assert); err != nil {
			return shared.Fail(err)
		}
	}
	updated, noop := mutateSlice(c.Domains, domain, add)
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

func mutatePaths(name, pattern string, add bool, assert *shared.SecretAssert) error {
	c, err := credential.GetMetadata(name)
	if err != nil {
		return shared.Fail(credential.ClassifyLookupErr(err, name))
	}
	if add {
		if err := shared.ApplySecretAssert(c, assert); err != nil {
			return shared.Fail(err)
		}
	}
	updated, noop := mutateSlice(c.Paths, pattern, add)
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


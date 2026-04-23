package creds

import (
	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/credential"
)

func registerAllow(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "allow <name> <domain>",
		Short: "Add host[:port] to allowlist (human-only)",
		Args:  cobra.ExactArgs(2),
		RunE: shared.HumanOnlyRunE("creds allow", func(cmd *cobra.Command, args []string) error {
			return mutateDomains(args[0], args[1], true)
		}),
	})
}

func registerDisallow(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "disallow <name> <domain>",
		Short: "Remove host[:port] from allowlist (human-only)",
		Args:  cobra.ExactArgs(2),
		RunE: shared.HumanOnlyRunE("creds disallow", func(cmd *cobra.Command, args []string) error {
			return mutateDomains(args[0], args[1], false)
		}),
	})
}

func registerAllowPath(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "allow-path <name> <pattern>",
		Short: "Add URL path pattern to allowlist (human-only)",
		Args:  cobra.ExactArgs(2),
		RunE: shared.HumanOnlyRunE("creds allow-path", func(cmd *cobra.Command, args []string) error {
			return mutatePaths(args[0], args[1], true)
		}),
	})
}

func registerDisallowPath(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "disallow-path <name> <pattern>",
		Short: "Remove URL path pattern (human-only)",
		Args:  cobra.ExactArgs(2),
		RunE: shared.HumanOnlyRunE("creds disallow-path", func(cmd *cobra.Command, args []string) error {
			return mutatePaths(args[0], args[1], false)
		}),
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

func mutateDomains(name, domain string, add bool) error {
	c, err := credential.GetMetadata(name)
	if err != nil {
		return shared.Fail(credential.ClassifyLookupErr(err, name))
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

func mutatePaths(name, pattern string, add bool) error {
	c, err := credential.GetMetadata(name)
	if err != nil {
		return shared.Fail(credential.ClassifyLookupErr(err, name))
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

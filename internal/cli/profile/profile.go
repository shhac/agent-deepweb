// Package profile implements the `profile` command tree for managing
// stored profiles. A profile bundles a credential's secret material, host/
// path allowlist, default headers, User-Agent override, and (for form
// auth) a derived session jar — all the state that constitutes one
// "identity" against an upstream service.
//
// The package is split into per-concern files:
//
//	profile.go  Register + list + show
//	test.go     test (health-check) subcommand
//	add.go            add subcommand, addOpts, per-auth-type Secrets builders
//	remove.go         remove subcommand
//	domains.go        allow/disallow and allow-path/disallow-path
//	config.go         set-health, (un)set-default-header, set-allow-http, set-user-agent
//	set_secret.go     rotate the primary secret without touching anything else
//	set_passphrase.go rotate the passphrase without touching the primary
//
// Subcommand wiring lives here; each register* lives in the file that
// implements its subcommand. Mutating subcommands that widen scope or
// reveal secrets require the profile's primary secret to be re-asserted
// — see internal/cli/shared/secret_assert.go.
package profile

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/credential"
	"github.com/shhac/agent-deepweb/internal/output"
)

func Register(root *cobra.Command, _ shared.Globals) {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage stored profiles (auth identities)",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "llm-help",
		Short: "Show detailed reference for profile",
		Run:   func(cmd *cobra.Command, args []string) { fmt.Print(usageText) },
	})

	registerList(cmd)
	registerShow(cmd)
	registerTest(cmd)
	registerAdd(cmd)
	registerRemove(cmd)
	registerAllow(cmd)
	registerDisallow(cmd)
	registerAllowPath(cmd)
	registerDisallowPath(cmd)
	registerSetHealth(cmd)
	registerSetDefaultHeader(cmd)
	registerUnsetDefaultHeader(cmd)
	registerSetAllowHTTP(cmd)
	registerSetUserAgent(cmd)
	registerSetSecret(cmd)
	registerSetPassphrase(cmd)
	registerMarkHeader(cmd)

	root.AddCommand(cmd)
}

func registerList(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List profiles (no secret values)",
		RunE: func(cmd *cobra.Command, args []string) error {
			profiles, err := credential.List()
			if err != nil {
				return shared.FailHuman(err)
			}
			output.PrintJSON(map[string]any{"profiles": profiles})
			return nil
		},
	})
}

func registerShow(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "show <name>",
		Short: "Show profile metadata (no secret values)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := shared.LoadProfileMetadata(args[0])
			if err != nil {
				return shared.Fail(err)
			}
			output.PrintJSON(c)
			return nil
		},
	})
}

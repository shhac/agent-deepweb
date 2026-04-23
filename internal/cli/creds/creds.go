// Package creds implements the `creds` command tree for managing stored
// credentials. The package is split into per-concern files:
//
//	creds.go    Register + list + show
//	test.go     test (health-check) subcommand
//	add.go      add subcommand, addOpts, per-auth-type Secrets builders
//	remove.go   remove subcommand
//	domains.go  allow/disallow and allow-path/disallow-path + mutateSlice
//	config.go   set-health, (un)set-default-header, set-allow-http, set-user-agent
//
// Subcommand wiring lives here; each register* lives in the file that
// implements its subcommand.
package creds

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/credential"
	"github.com/shhac/agent-deepweb/internal/output"
)

func Register(root *cobra.Command, _ shared.Globals) {
	cmd := &cobra.Command{
		Use:   "creds",
		Short: "Manage stored credentials",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "llm-help",
		Short: "Show detailed reference for creds",
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

	root.AddCommand(cmd)
}

func registerList(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List credentials (no secret values)",
		RunE: func(cmd *cobra.Command, args []string) error {
			creds, err := credential.List()
			if err != nil {
				return shared.FailHuman(err)
			}
			output.PrintJSON(map[string]any{"credentials": creds})
			return nil
		},
	})
}

func registerShow(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "show <name>",
		Short: "Show credential metadata (no secret values)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := credential.GetMetadata(args[0])
			if err != nil {
				return shared.Fail(credential.ClassifyLookupErr(err, args[0]))
			}
			output.PrintJSON(c)
			return nil
		},
	})
}

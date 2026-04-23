package creds

import (
	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/output"
)

func registerRemove(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a credential (human-only)",
		Args:  cobra.ExactArgs(1),
		RunE: shared.HumanOnlyRunE("creds remove", func(cmd *cobra.Command, args []string) error {
			if err := credential.Remove(args[0]); err != nil {
				if ae := credential.WrapNotFound(err, args[0]); ae != nil {
					return shared.Fail(ae)
				}
				return shared.Fail(agenterrors.Wrap(err, agenterrors.FixableByHuman))
			}
			output.PrintJSON(map[string]any{"status": "ok", "name": args[0]})
			return nil
		}),
	})
}

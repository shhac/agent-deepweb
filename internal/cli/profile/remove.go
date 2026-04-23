package profile

import (
	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/credential"
)

func registerRemove(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a credential (human-only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := credential.Remove(args[0]); err != nil {
				return shared.Fail(credential.ClassifyLookupErr(err, args[0]))
			}
			shared.PrintOK(map[string]any{"name": args[0]})
			return nil
		},
	})
}

package profile

import (
	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/credential"
	"github.com/shhac/agent-deepweb/internal/track"
)

func registerRemove(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a profile — clears secret + jar + any tracked records",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := credential.Remove(name); err != nil {
				return shared.Fail(credential.ClassifyLookupErr(err, name))
			}
			// Orphan cleanup: also purge any track records that belonged
			// to this profile. Best-effort; a prune failure doesn't fail
			// the remove (the profile itself is already gone).
			tracked, _ := track.PruneByProfile(name)
			shared.PrintOK(map[string]any{"name": name, "tracked_records_removed": tracked})
			return nil
		},
	})
}

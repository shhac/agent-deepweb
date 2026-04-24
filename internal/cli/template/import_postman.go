package templatecli

import (
	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/template/importers"
)

func registerImportPostman(parent *cobra.Command) {
	var (
		prefix     string
		profile    string
		folderPath string
	)
	cmd := &cobra.Command{
		Use:   "import-postman <collection.json>",
		Short: "Import templates from a Postman Collection v2.1 JSON file (human-only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if prefix == "" {
				return shared.Fail(agenterrors.New(
					"--prefix is required (chooses the name-space for imported templates, e.g. 'myapi')",
					agenterrors.FixableByHuman))
			}
			opts := template.ImportPostmanOptions{
				Prefix:     prefix,
				Profile:    profile,
				FolderPath: folderPath,
			}
			imported, err := template.ImportPostmanFile(args[0], opts)
			if err != nil {
				return shared.Fail(agenterrors.Wrap(err, agenterrors.FixableByHuman).
					WithHint("Export your Postman collection as 'Collection v2.1' — v2.0 also works; older versions do not."))
			}
			shared.PrintOK(map[string]any{
				"imported":    imported,
				"count":       len(imported),
				"prefix":      prefix,
				"profile":     profile,
				"folder":      folderPath,
			})
			return nil
		},
	}
	cmd.Flags().StringVar(&prefix, "prefix", "", "Name-space for imported templates (required, e.g. 'github' → 'github.get_user')")
	cmd.Flags().StringVar(&profile, "profile", "", "Bind every imported template to this profile")
	cmd.Flags().StringVar(&folderPath, "folder", "", "Only import requests under a folder whose name contains this string (case-insensitive)")
	parent.AddCommand(cmd)
}

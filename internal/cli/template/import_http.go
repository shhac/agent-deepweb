package templatecli

import (
	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/template"
)

func registerImportHTTP(parent *cobra.Command) {
	var (
		prefix  string
		profile string
	)
	cmd := &cobra.Command{
		Use:   "import-http <file.http>",
		Short: "Import templates from a .http file (VS Code REST Client / JetBrains HTTP Client) (human-only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if prefix == "" {
				return shared.Fail(agenterrors.New(
					"--prefix is required",
					agenterrors.FixableByHuman))
			}
			imported, err := template.ImportHTTPFile(args[0], template.ImportHTTPFileOptions{
				Prefix:  prefix,
				Profile: profile,
			})
			if err != nil {
				return shared.Fail(agenterrors.Wrap(err, agenterrors.FixableByHuman).
					WithHint("Expected VS Code REST Client / JetBrains HTTP Client format: `### name` separators, optional `@var = value` declarations, `METHOD URL`, headers, blank line, body."))
			}
			shared.PrintOK(map[string]any{
				"imported": imported,
				"count":    len(imported),
				"prefix":   prefix,
				"profile":  profile,
			})
			return nil
		},
	}
	cmd.Flags().StringVar(&prefix, "prefix", "", "Name-space for imported templates (required)")
	cmd.Flags().StringVar(&profile, "profile", "", "Bind every imported template to this profile")
	parent.AddCommand(cmd)
}

package templatecli

import (
	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/template"
)

// registerImportOpenAPI wires `template import-openapi <spec>`. Distinct
// from the general `import` verb because the translation from an
// OpenAPI document is lossy (one operation → one simplified template)
// and carries its own set of flags (--prefix, --profile, --tag,
// --server). Keeping it a sibling verb lets the help text document the
// mapping rules in one place.
func registerImportOpenAPI(parent *cobra.Command) {
	var (
		prefix         string
		profile        string
		tags           []string
		serverOverride string
	)
	cmd := &cobra.Command{
		Use:   "import-openapi <spec-file>",
		Short: "Import one template per operation from an OpenAPI v3 JSON spec (human-only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if prefix == "" {
				return shared.Fail(agenterrors.New(
					"--prefix is required (chooses the name-space for imported templates, e.g. 'github')",
					agenterrors.FixableByHuman))
			}
			opts := template.ImportOpenAPIOptions{
				Prefix:         prefix,
				Profile:        profile,
				TagFilter:      tags,
				ServerOverride: serverOverride,
			}
			imported, err := template.ImportOpenAPIFile(args[0], opts)
			if err != nil {
				return shared.Fail(agenterrors.Wrap(err, agenterrors.FixableByHuman).
					WithHint("YAML specs must be converted to JSON first (e.g. 'yq -o=json . spec.yaml > spec.json')"))
			}
			shared.PrintOK(map[string]any{
				"imported":    imported,
				"count":       len(imported),
				"prefix":      prefix,
				"profile":     profile,
				"tag_filter":  tags,
				"server_used": serverOverride,
			})
			return nil
		},
	}
	cmd.Flags().StringVar(&prefix, "prefix", "", "Name-space for imported templates (required, e.g. 'github' → 'github.get_user')")
	cmd.Flags().StringVar(&profile, "profile", "", "Bind every imported template to this profile")
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "Only import operations carrying any of these OpenAPI tags (repeatable; comma-separated also allowed)")
	cmd.Flags().StringVar(&serverOverride, "server", "", "Override the spec's servers[0].url (useful for staging/dev targets)")
	parent.AddCommand(cmd)
}

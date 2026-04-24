package templatecli

import (
	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/template"
)

func registerImportHAR(parent *cobra.Command) {
	var (
		prefix      string
		profile     string
		urlContains string
		dedupe      bool
	)
	cmd := &cobra.Command{
		Use:   "import-har <capture.har>",
		Short: "Import templates from an HTTP Archive (HAR) file (human-only)",
		Long: `Import templates from a browser's HAR export.

HAR entries carry the REAL session cookies and auth headers captured
by your browser. Those are stripped at import time (authorization,
cookie, x-csrf-*, x-api-key, x-xsrf-*) — the templates should be
re-run with a real --profile attached, not the stale capture.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if prefix == "" {
				return shared.Fail(agenterrors.New(
					"--prefix is required",
					agenterrors.FixableByHuman))
			}
			imported, err := template.ImportHARFile(args[0], template.ImportHAROptions{
				Prefix:      prefix,
				Profile:     profile,
				URLContains: urlContains,
				Dedupe:      dedupe,
			})
			if err != nil {
				return shared.Fail(agenterrors.Wrap(err, agenterrors.FixableByHuman).
					WithHint("Export from Chrome/Firefox dev tools → Network → right-click → Save all as HAR"))
			}
			shared.PrintOK(map[string]any{
				"imported":     imported,
				"count":        len(imported),
				"prefix":       prefix,
				"profile":      profile,
				"url_contains": urlContains,
				"dedupe":       dedupe,
			})
			return nil
		},
	}
	cmd.Flags().StringVar(&prefix, "prefix", "", "Name-space for imported templates (required)")
	cmd.Flags().StringVar(&profile, "profile", "", "Bind every imported template to this profile")
	cmd.Flags().StringVar(&urlContains, "url-contains", "", "Only import entries whose URL contains this substring (case-sensitive)")
	cmd.Flags().BoolVar(&dedupe, "dedupe", false, "Collapse duplicate (method, url, body-shape) entries to one template")
	parent.AddCommand(cmd)
}

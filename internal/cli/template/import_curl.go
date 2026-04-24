package templatecli

import (
	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/template"
)

func registerImportCurl(parent *cobra.Command) {
	var (
		name    string
		profile string
	)
	cmd := &cobra.Command{
		Use:   "import-curl <curl-command>",
		Short: "Import one template from a shell-pasted curl command (human-only)",
		Long: `Import a single template from a pasted curl invocation. Useful when
engineers share API examples as a "curl this" snippet. Wrap the whole
command in single quotes so your shell doesn't swallow the curl flags:

  agent-deepweb template import-curl 'curl -X POST https://api/items \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"x\"}"' --name myapi.create_item --profile myapi

Flags ignored (we always follow redirects; auth comes from --profile):
  -L / --location, -v / --verbose, -s / --silent, --compressed,
  --http2, --http1.1, --insecure, -u / --user, -b / --cookie, -F / --form`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return shared.Fail(agenterrors.New(
					"--name is required (there's no filename to derive one from)",
					agenterrors.FixableByHuman))
			}
			stored, err := template.ImportCurl(args[0], template.ImportCurlOptions{
				Name:    name,
				Profile: profile,
			})
			if err != nil {
				return shared.Fail(agenterrors.Wrap(err, agenterrors.FixableByHuman).
					WithHint("Paste the entire curl command as a single single-quoted string so your shell preserves the -H / -d values"))
			}
			shared.PrintOK(map[string]any{
				"imported": stored,
				"profile":  profile,
			})
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Template name (required, e.g. 'myapi.create_item')")
	cmd.Flags().StringVar(&profile, "profile", "", "Bind the imported template to this profile")
	parent.AddCommand(cmd)
}

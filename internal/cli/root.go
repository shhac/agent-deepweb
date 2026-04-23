package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/api"
	"github.com/shhac/agent-deepweb/internal/cli/audit"
	"github.com/shhac/agent-deepweb/internal/cli/creds"
	"github.com/shhac/agent-deepweb/internal/cli/fetch"
	"github.com/shhac/agent-deepweb/internal/cli/graphql"
	"github.com/shhac/agent-deepweb/internal/cli/login"
	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/cli/tpl"
)

var (
	flagAuth    string
	flagFormat  string
	flagTimeout int
)

func allGlobals() *shared.GlobalFlags {
	return &shared.GlobalFlags{
		Auth:    flagAuth,
		Format:  flagFormat,
		Timeout: flagTimeout,
	}
}

func newRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "agent-deepweb",
		Short:         "curl-with-auth for AI agents",
		Long:          "Authenticated HTTP fetcher where credentials are stored by the user and referenced by name; the LLM never sees secret values.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&flagAuth, "auth", "", "Credential alias (or AGENT_DEEPWEB_AUTH)")
	root.PersistentFlags().StringVar(&flagFormat, "format", "", "Output format: json, jsonl, raw, text")
	root.PersistentFlags().IntVar(&flagTimeout, "timeout", 0, "Request timeout in milliseconds")

	if envAuth := os.Getenv("AGENT_DEEPWEB_AUTH"); envAuth != "" && flagAuth == "" {
		flagAuth = envAuth
	}

	registerUsageCommand(root)
	fetch.Register(root, allGlobals)
	graphql.Register(root, allGlobals)
	creds.Register(root, allGlobals)
	login.Register(root, allGlobals)
	audit.Register(root, allGlobals)
	tpl.Register(root, allGlobals)

	return root
}

func Execute(version string) error {
	// Propagate version to the api package so the default User-Agent
	// is "agent-deepweb/<version>" (curl-style).
	api.Version = version
	// Errors are already written to stderr as structured JSON by the
	// individual RunE handlers via output.WriteError. Cobra's own
	// error printing is silenced (SilenceErrors) so the user/LLM sees
	// exactly one JSON error, and the non-zero exit comes from main.
	return newRootCmd(version).Execute()
}

package cli

import (
	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/api"
	"github.com/shhac/agent-deepweb/internal/cli/audit"
	configcli "github.com/shhac/agent-deepweb/internal/cli/config"
	"github.com/shhac/agent-deepweb/internal/cli/fetch"
	"github.com/shhac/agent-deepweb/internal/cli/graphql"
	"github.com/shhac/agent-deepweb/internal/cli/jsonrpc"
	"github.com/shhac/agent-deepweb/internal/cli/login"
	"github.com/shhac/agent-deepweb/internal/cli/profile"
	"github.com/shhac/agent-deepweb/internal/cli/shared"
	templatecli "github.com/shhac/agent-deepweb/internal/cli/template"
	"github.com/shhac/agent-deepweb/internal/config"
)

var (
	flagProfile string
	flagFormat  string
	flagTimeout int
)

func allGlobals() *shared.GlobalFlags {
	return &shared.GlobalFlags{
		Profile: flagProfile,
		Format:  flagFormat,
		Timeout: flagTimeout,
	}
}

func newRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "agent-deepweb",
		Short:         "curl-with-auth for AI agents",
		Long:          "Authenticated HTTP fetcher where profiles (auth identities) are registered by the user and referenced by name; the LLM never sees secret values.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&flagProfile, "profile", "", "Profile name, or 'none' for explicit anonymous (falls back to config 'default.profile')")
	root.PersistentFlags().StringVar(&flagFormat, "format", "", "Output format: json, jsonl, raw, text")
	root.PersistentFlags().IntVar(&flagTimeout, "timeout", 0, "Request timeout in milliseconds (falls back to config 'default.timeout-ms')")

	// Precedence for --profile: flag > config.default.profile > empty.
	// No env var — config replaces AGENT_DEEPWEB_PROFILE in v0.4.
	if flagProfile == "" {
		flagProfile = config.Read().Defaults.Profile
	}

	registerUsageCommand(root)
	fetch.Register(root, allGlobals)
	graphql.Register(root, allGlobals)
	jsonrpc.Register(root, allGlobals)
	profile.Register(root, allGlobals)
	login.Register(root, allGlobals)
	audit.Register(root, allGlobals)
	templatecli.Register(root, allGlobals)
	configcli.Register(root, allGlobals)

	return root
}

// Execute is the convenience entrypoint used by main.go: it builds
// the default App, installs it, and runs the cobra tree. Tests or
// embedders that need custom dependencies should construct an App
// and call (*App).Execute directly.
func Execute(version string) error {
	return DefaultApp().Execute(version)
}

// Execute runs the cobra tree with this App's dependencies installed
// as the process-wide defaults. Version is propagated to the api
// package so the default User-Agent is "agent-deepweb/<version>"
// (curl-style).
func (a *App) Execute(version string) error {
	a.install()
	api.Version = version
	// Errors are already written to stderr as structured JSON by the
	// individual RunE handlers via output.WriteError. Cobra's own
	// error printing is silenced (SilenceErrors) so the user/LLM sees
	// exactly one JSON error, and the non-zero exit comes from main.
	return newRootCmd(version).Execute()
}

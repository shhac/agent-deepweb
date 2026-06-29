package cli

import (
	"github.com/spf13/cobra"

	libcli "github.com/shhac/lib-agent-cli/cli"
	agentmcp "github.com/shhac/lib-agent-mcp"
	out "github.com/shhac/lib-agent-output"

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
	"github.com/shhac/agent-deepweb/internal/credential"
)

func newRootCmd(version string) *cobra.Command {
	g := &shared.GlobalFlags{}
	allGlobals := func() *shared.GlobalFlags { return g }

	root := libcli.NewRoot(libcli.Options{
		Use:     "agent-deepweb",
		Short:   "curl-with-auth for AI agents",
		Version: version,
		Globals: &g.Globals,
		// deepweb is a request tool (fetch/graphql/jsonrpc), not a list-default
		// CLI, so the default is JSON, not NDJSON. The request verbs render the
		// envelope via out.Print, which honours this default when --format is
		// empty.
		DefaultFormat: out.FormatJSON,
		// Precedence for --profile: flag > config.default.profile > empty.
		// No env var — config replaces AGENT_DEEPWEB_PROFILE in v0.4. Runs in
		// PersistentPreRunE so it sees the parsed --profile flag value.
		ConfigDefaults: func() {
			if g.Profile == "" {
				g.Profile = config.Read().Defaults.Profile
			}
		},
		UnknownHint: "run 'agent-deepweb usage' to see the available commands",
	})
	root.Long = "Authenticated HTTP fetcher where profiles (auth identities) are registered by the user and referenced by name; the LLM never sees secret values."

	// The global --format usage from libcli.NewRoot ("json, yaml, jsonl") is now
	// accurate: deepweb renders all three. The request verbs additionally accept
	// raw/text (opted in via libcli.AllowFormats); that's documented per-verb.

	root.PersistentFlags().StringVarP(&g.Profile, "profile", "p", "", "Profile name, or 'none' for explicit anonymous (falls back to config 'default.profile')")

	registerUsageCommand(root)
	fetch.Register(root, allGlobals)
	graphql.Register(root, allGlobals)
	jsonrpc.Register(root, allGlobals)
	profile.Register(root, allGlobals)
	login.Register(root, allGlobals)
	audit.Register(root, allGlobals)
	templatecli.Register(root, allGlobals)
	configcli.Register(root, allGlobals)

	// Expose the whole command tree as an MCP server (added last, so it reflects
	// the complete tree). --color/--expose are output-shaping, irrelevant to a
	// tool call, so hide them from the generated schemas.
	// Opt the agent-facing groups into the MCP tool surface: each becomes one
	// coarse tool that dispatches its subcommands (with a "help" verb), so the
	// surface is ~one-tool-per-group instead of one-per-leaf. Credential/config/
	// usage commands are deliberately left out — they aren't agent tasks.
	exposeGroups(root,
		"audit", "fetch", "graphql", "jar", "jsonrpc", "login", "template")

	root.AddCommand(agentmcp.Command(root, agentmcp.WithHiddenFlags("color", "expose"),
		agentmcp.WithOAuthKeyringService(credential.MCPKeychainService())))

	return root
}

// Run is the convenience entrypoint used by main.go: it builds the default
// App, installs it, and runs the cobra tree via libcli.Run — the single
// sink that renders any bubbled error as the family's structured JSON on
// stderr (exactly once) and exits 0/1. Tests or embedders that need custom
// dependencies should construct an App and call (*App).Run directly.
func Run(version string) {
	DefaultApp().Run(version)
}

// Run installs this App's dependencies as the process-wide defaults, then
// runs the cobra tree via libcli.Run. Version is propagated to the api
// package so the default User-Agent is "agent-deepweb/<version>"
// (curl-style). libcli.Run renders any error (RunE body, PersistentPreRunE
// check, flag-parse, or unknown-command) as one structured JSON envelope on
// stderr and exits 1; success exits 0.
//
// Error model: RunE handlers classify-and-bubble their errors via shared.Fail
// (which now only classifies — it does NOT render). The single render happens
// here in libcli.Run, so every error — RunE *APIError or cobra-originated
// unknown-command/flag (libcli.NewRoot classifies those as fixable_by:agent) —
// prints exactly once.
func (a *App) Run(version string) {
	a.install()
	api.Version = version
	libcli.Run(newRootCmd(version))
}

// exposeGroups opts the named top-level commands into the MCP tool surface.
// A name with no matching command is skipped silently — the list is a curation
// of agent-facing groups, not a registration check.
func exposeGroups(root *cobra.Command, names ...string) {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	for _, c := range root.Commands() {
		if want[c.Name()] {
			agentmcp.Expose(c)
		}
	}
}

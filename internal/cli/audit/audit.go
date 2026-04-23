package audit

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	auditpkg "github.com/shhac/agent-deepweb/internal/audit"
	"github.com/shhac/agent-deepweb/internal/cli/shared"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/output"
)

func Register(root *cobra.Command, _ shared.Globals) {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Inspect the request audit log",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "llm-help",
		Short: "Show detailed reference for audit",
		Run:   func(cmd *cobra.Command, args []string) { fmt.Print(usageText) },
	})

	var nLines int
	tailCmd := &cobra.Command{
		Use:   "tail",
		Short: "Show the last N audit entries (default 50)",
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := auditpkg.Tail(nLines)
			if err != nil {
				output.WriteError(os.Stderr, agenterrors.Wrap(err, agenterrors.FixableByHuman))
				return err
			}
			output.PrintJSON(map[string]any{
				"enabled": auditpkg.Enabled(),
				"entries": entries,
			})
			return nil
		},
	}
	tailCmd.Flags().IntVarP(&nLines, "lines", "n", 50, "Number of entries to tail")
	cmd.AddCommand(tailCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "summary",
		Short: "Grouped summary of recent audit entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Tail of 1000 is a reasonable default to summarise over
			entries, err := auditpkg.Tail(1000)
			if err != nil {
				output.WriteError(os.Stderr, agenterrors.Wrap(err, agenterrors.FixableByHuman))
				return err
			}
			output.PrintJSON(auditpkg.Summarize(entries))
			return nil
		},
	})

	root.AddCommand(cmd)
}

const usageText = `audit — inspect the request audit log

USAGE
  agent-deepweb audit tail [-n N]
  agent-deepweb audit summary

SUMMARY
  Every fetch / graphql / tpl run request is logged to
  ~/.config/agent-deepweb/audit.log (JSONL) with method, host, path,
  credential name, status, bytes, duration, and (for errors)
  fixable_by classification. The log does NOT include bodies, headers,
  secret values, or query strings.

DISABLING
  Set AGENT_DEEPWEB_AUDIT=off to disable writing (the file is not
  touched until the next request). Default is on.

ENTRY SHAPE
  { "ts":"2026-04-23T16:00:00Z",
    "method":"GET",
    "scheme":"https",
    "host":"api.github.com",
    "path":"/user",
    "credential":"github",
    "template":"",
    "agent_mode":true,
    "status":200,
    "bytes":1234,
    "duration_ms":142,
    "outcome":"ok" }

  On error: outcome="error", adds "error" and "fixable_by".
`

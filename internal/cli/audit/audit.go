package audit

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	auditpkg "github.com/shhac/agent-deepweb/internal/audit"
	"github.com/shhac/agent-deepweb/internal/cli/shared"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/output"
	"github.com/shhac/agent-deepweb/internal/track"
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

	cmd.AddCommand(&cobra.Command{
		Use:   "show <audit-id>",
		Short: "Print the full tracked record for an audit ID (only exists when --track was used)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rec, err := track.Read(args[0])
			if err != nil {
				if os.IsNotExist(err) {
					return shared.Fail(agenterrors.Newf(agenterrors.FixableByHuman,
						"no tracked record for %q (pruned after TTL, or --track was not set on the original request)", args[0]))
				}
				return shared.FailHuman(err)
			}
			output.PrintJSON(rec)
			return nil
		},
	})

	var pruneOlderThan string
	pruneCmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove tracked records older than a duration (default TTL from AGENT_DEEPWEB_TRACK_TTL or 7 days)",
		RunE: func(cmd *cobra.Command, args []string) error {
			var d time.Duration
			if pruneOlderThan != "" {
				parsed, err := time.ParseDuration(pruneOlderThan)
				if err != nil {
					return shared.Fail(agenterrors.Newf(agenterrors.FixableByAgent,
						"--older-than %q is not a valid duration (e.g. 24h, 7d-equivalent 168h)", pruneOlderThan))
				}
				d = parsed
			} else {
				d = track.DefaultTTL
			}
			removed, err := track.PruneOlderThan(d)
			if err != nil {
				return shared.FailHuman(err)
			}
			shared.PrintOK(map[string]any{"removed": removed})
			return nil
		},
	}
	pruneCmd.Flags().StringVar(&pruneOlderThan, "older-than", "", "Duration threshold (default: AGENT_DEEPWEB_TRACK_TTL or 7 days)")
	cmd.AddCommand(pruneCmd)

	root.AddCommand(cmd)
}

const usageText = `audit — inspect the request audit log + tracked records

USAGE
  agent-deepweb audit tail [-n N]
  agent-deepweb audit summary
  agent-deepweb audit show <audit-id>
  agent-deepweb audit prune [--older-than <duration>]

SUMMARY
  Every fetch / graphql / tpl run request is logged to
  ~/.config/agent-deepweb/audit.log (JSONL) with method, host, path,
  profile, status, bytes, duration, and (for errors) fixable_by. The
  audit log does NOT include bodies, headers, secret values, or query
  strings — it's metadata only.

  When a request is run with --track, a full-fidelity record (redacted
  request+response headers and bodies) is ALSO persisted to
  ~/.config/agent-deepweb/track/<id>.json. Retrieve later with
  'audit show <id>'.

DISABLING
  Set AGENT_DEEPWEB_AUDIT=off to disable writing the audit log (the
  file is not touched until the next request). Default is on.

  Tracked records are only written when a caller passes --track; they
  respect the TTL set by AGENT_DEEPWEB_TRACK_TTL (default 7 days).

ENTRY SHAPE
  { "ts":"2026-04-23T16:00:00Z",
    "method":"GET",
    "scheme":"https",
    "host":"api.github.com",
    "path":"/user",
    "profile":"github",
    "jar":"",
    "template":"",
    "status":200,
    "bytes":1234,
    "duration_ms":142,
    "outcome":"ok" }

  On error: outcome="error", adds "error" and "fixable_by".
`

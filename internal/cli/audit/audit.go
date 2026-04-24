package audit

import (
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

	shared.LLMHelp(cmd, "audit", usageText)

	var nLines int
	tailCmd := &cobra.Command{
		Use:   "tail",
		Short: "Show the last N audit entries (default 50)",
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := auditpkg.Tail(nLines)
			if err != nil {
				return shared.FailHuman(err)
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
				return shared.FailHuman(err)
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

	var (
		pruneOlderThan  string
		pruneOnlyTracks bool
	)
	pruneCmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove expired tracked records (default: use each record's own ExpiresAt)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Only target today is tracked records. The flag exists as a
			// forward-compat hook for --only-log (audit.log pruning) and
			// as documentation of the default. Refusing --only-tracks=false
			// now means we can't silently change behaviour when the log
			// target lands later.
			if !pruneOnlyTracks {
				return shared.Fail(agenterrors.New(
					"--only-tracks=false is not supported yet (the audit log itself has no TTL today)",
					agenterrors.FixableByAgent))
			}
			removed, err := runTrackPrune(pruneOlderThan)
			if err != nil {
				return shared.Fail(err)
			}
			shared.PrintOK(map[string]any{"removed": removed})
			return nil
		},
	}
	pruneCmd.Flags().StringVar(&pruneOlderThan, "older-than", "", "Ignore each record's ExpiresAt; prune anything older than this duration (e.g. 24h, 168h)")
	pruneCmd.Flags().BoolVar(&pruneOnlyTracks, "only-tracks", true, "Target only tracked records (default; the audit log itself is not currently prunable)")
	cmd.AddCommand(pruneCmd)

	root.AddCommand(cmd)
}

// runTrackPrune dispatches to the right track-package pruner based on
// --older-than. Parses the duration up front so the error (fixable_by:
// agent, with the bad value quoted) is surfaced with minimal nesting.
func runTrackPrune(olderThan string) (int, error) {
	if olderThan != "" {
		d, err := time.ParseDuration(olderThan)
		if err != nil {
			return 0, agenterrors.Newf(agenterrors.FixableByAgent,
				"--older-than %q is not a valid duration (e.g. 24h, 168h)", olderThan)
		}
		return track.PruneOlderThan(d)
	}
	// Default: respect each record's own ExpiresAt so old records don't
	// outlive their original contract just because the config TTL was
	// bumped later.
	return track.PruneExpired()
}

const usageText = `audit — inspect the request audit log + tracked records

USAGE
  agent-deepweb audit tail [-n N]
  agent-deepweb audit summary
  agent-deepweb audit show <audit-id>
  agent-deepweb audit prune [--older-than <duration>]

SUMMARY
  Every fetch / graphql / template run request is logged to
  ~/.config/agent-deepweb/audit.log (JSONL) with method, host, path,
  profile, status, bytes, duration, and (for errors) fixable_by. The
  audit log does NOT include bodies, headers, secret values, or query
  strings — it's metadata only.

  When a request is run with --track, a full-fidelity record (redacted
  request+response headers and bodies) is ALSO persisted to
  ~/.config/agent-deepweb/track/<id>.json. Retrieve later with
  'audit show <id>'.

DISABLING
  Set 'agent-deepweb config set audit.enabled false' to disable writing
  the audit log (the file is not touched until the next request).
  Default is on.

  Tracked records are only written when a caller passes --track; they
  respect the TTL set by 'agent-deepweb config set track.ttl <duration>'
  (default 168h / 7 days). Each record stamps its own expires_at at
  write time, so bumping the TTL later affects only new records.

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

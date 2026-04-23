package login

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/output"
)

func registerSession(root *cobra.Command) {
	session := &cobra.Command{
		Use:   "session",
		Short: "Session lifecycle commands",
	}
	session.AddCommand(&cobra.Command{
		Use:   "llm-help",
		Short: "Show detailed reference for login/session",
		Run:   func(cmd *cobra.Command, args []string) { fmt.Print(usageText) },
	})
	session.AddCommand(&cobra.Command{
		Use:   "status <name>",
		Short: "Session metadata summary (cookie count, expiry — no values)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := credential.GetSessionStatus(args[0])
			if err != nil {
				return shared.FailHuman(err)
			}
			output.PrintJSON(status)
			return nil
		},
	})
	session.AddCommand(&cobra.Command{
		Use:   "show <name>",
		Short: "Show cookies (sensitive values redacted; visible values shown)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			show, err := credential.GetSessionShow(args[0])
			if err != nil {
				return shared.FailHuman(err)
			}
			output.PrintJSON(show)
			return nil
		},
	})
	session.AddCommand(&cobra.Command{
		Use:   "clear <name>",
		Short: "Wipe stored session (human-only)",
		Args:  cobra.ExactArgs(1),
		RunE: shared.HumanOnlyRunE("session clear", func(cmd *cobra.Command, args []string) error {
			if err := credential.ClearSession(args[0]); err != nil {
				return shared.FailHuman(err)
			}
			shared.PrintOK(map[string]any{"name": args[0]})
			return nil
		}),
	})
	session.AddCommand(&cobra.Command{
		Use:   "set-expires <name> <duration|RFC3339>",
		Short: "Override session expiry (e.g. '2h' or '2026-05-01T00:00:00Z') (human-only)",
		Args:  cobra.ExactArgs(2),
		RunE: shared.HumanOnlyRunE("session set-expires", func(cmd *cobra.Command, args []string) error {
			sess, err := readSessionOrFail(args[0])
			if err != nil {
				return err
			}
			newExp, err := parseExpiresSpec(args[1])
			if err != nil {
				return shared.Fail(err)
			}
			sess.Expires = newExp
			if err := credential.WriteSession(sess); err != nil {
				return shared.FailHuman(err)
			}
			status, _ := credential.GetSessionStatus(args[0])
			output.PrintJSON(status)
			return nil
		}),
	})
	session.AddCommand(&cobra.Command{
		Use:   "mark-sensitive <name> <cookie-name>",
		Short: "Force cookie value to be redacted in session show (human-only)",
		Args:  cobra.ExactArgs(2),
		RunE:  markSensitivity(true),
	})
	session.AddCommand(&cobra.Command{
		Use:   "mark-visible <name> <cookie-name>",
		Short: "Force cookie value to be shown in session show (human-only)",
		Args:  cobra.ExactArgs(2),
		RunE:  markSensitivity(false),
	})
	root.AddCommand(session)
}

// markSensitivity returns a RunE that toggles the Sensitive flag on one
// cookie. Both the sensitive=true and sensitive=false variants share this
// implementation.
func markSensitivity(sensitive bool) shared.RunEFunc {
	verb := "session mark-visible"
	if sensitive {
		verb = "session mark-sensitive"
	}
	return shared.HumanOnlyRunE(verb, func(cmd *cobra.Command, args []string) error {
		sess, err := readSessionOrFail(args[0])
		if err != nil {
			return err
		}
		if !sess.MarkCookieSensitivity(args[1], sensitive) {
			return shared.Fail(agenterrors.Newf(agenterrors.FixableByAgent,
				"cookie %q not found in session %q", args[1], args[0]))
		}
		if err := credential.WriteSession(sess); err != nil {
			return shared.FailHuman(err)
		}
		show, _ := credential.GetSessionShow(args[0])
		output.PrintJSON(show)
		return nil
	})
}

// readSessionOrFail loads a session file, returning a classified error
// (and writing it via shared.Fail) if the file is missing.
func readSessionOrFail(name string) (*credential.Session, error) {
	sess, err := credential.ReadSession(name)
	if err != nil {
		return nil, shared.Fail(agenterrors.Wrap(err, agenterrors.FixableByAgent).
			WithHint("No session file. Run 'agent-deepweb login <name>' first."))
	}
	return sess, nil
}

// parseExpiresSpec accepts either a Go duration ("2h") or an RFC3339 timestamp.
func parseExpiresSpec(spec string) (time.Time, error) {
	if d, err := time.ParseDuration(spec); err == nil {
		return time.Now().UTC().Add(d), nil
	}
	if t, err := time.Parse(time.RFC3339, spec); err == nil {
		return t, nil
	}
	return time.Time{}, agenterrors.Newf(agenterrors.FixableByAgent,
		"expires %q is neither a duration nor RFC3339 time", spec)
}

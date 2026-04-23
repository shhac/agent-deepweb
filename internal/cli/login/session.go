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
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := credential.ClearSession(args[0]); err != nil {
				return shared.FailHuman(err)
			}
			shared.PrintOK(map[string]any{"name": args[0]})
			return nil
		},
	})
	session.AddCommand(&cobra.Command{
		Use:   "set-expires <name> <duration|RFC3339>",
		Short: "Override session expiry (e.g. '2h' or '2026-05-01T00:00:00Z') (human-only)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
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
		},
	})
	// mark-sensitive only narrows what the LLM can see, so it doesn't
	// require the primary secret. mark-visible WIDENS what the LLM sees
	// (un-masks a stored cookie value) and IS escalation: the credential's
	// primary secret must be re-asserted, with the same overwrite-or-
	// silently-break semantics as creds allow / set-default-header.
	session.AddCommand(&cobra.Command{
		Use:   "mark-sensitive <name> <cookie> [cookie...]",
		Short: "Force one or more cookie values to be redacted in session show",
		Args:  cobra.MinimumNArgs(2),
		RunE:  markSensitivity(true, nil),
	})
	visibleAssert := &shared.SecretAssert{}
	visibleCmd := &cobra.Command{
		Use:   "mark-visible <name> <cookie> [cookie...]",
		Short: "Un-mask one or more cookie values (re-supply credential's primary secret)",
		Args:  cobra.MinimumNArgs(2),
		RunE:  markSensitivity(false, visibleAssert),
	}
	shared.BindSecretAssertFlags(visibleCmd, visibleAssert)
	session.AddCommand(visibleCmd)
	root.AddCommand(session)
}

// markSensitivity returns a RunE that toggles the Sensitive flag on one
// or more cookies. The variadic form (multiple cookie names after the
// session name) lets a human flip a batch in one call instead of N.
//
// When `assert` is non-nil (the un-mask path), the credential's primary
// secret must be re-asserted before the cookie sensitivity is changed.
// Wrong values overwrite the stored secret with garbage — the LLM gains
// nothing from un-masking a session whose underlying credential is now
// broken.
func markSensitivity(sensitive bool, assert *shared.SecretAssert) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		name := args[0]
		cookies := args[1:]
		if assert != nil {
			c, err := credential.GetMetadata(name)
			if err != nil {
				return shared.Fail(credential.ClassifyLookupErr(err, name))
			}
			if err := shared.ApplySecretAssert(c, assert); err != nil {
				return shared.Fail(err)
			}
		}
		sess, err := readSessionOrFail(name)
		if err != nil {
			return err
		}
		var missing []string
		for _, c := range cookies {
			if !sess.MarkCookieSensitivity(c, sensitive) {
				missing = append(missing, c)
			}
		}
		if len(missing) > 0 {
			return shared.Fail(agenterrors.Newf(agenterrors.FixableByAgent,
				"cookie(s) %v not found in session %q", missing, name))
		}
		if err := credential.WriteSession(sess); err != nil {
			return shared.FailHuman(err)
		}
		show, _ := credential.GetSessionShow(name)
		output.PrintJSON(show)
		return nil
	}
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

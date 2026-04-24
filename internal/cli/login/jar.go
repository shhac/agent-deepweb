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

// registerJar wires the `jar` verb tree onto root. In v2 the per-profile
// persistent state (cookies + form-acquired token + expiry) is called a
// "jar" — the term is profile-type-agnostic, matches the storage filename
// (profiles/<name>/jar.json), and matches the audit log's `jar` field.
func registerJar(root *cobra.Command) {
	jar := &cobra.Command{
		Use:   "jar",
		Short: "Per-profile cookie jar inspection and management",
	}
	jar.AddCommand(&cobra.Command{
		Use:   "llm-help",
		Short: "Show detailed reference for login/jar",
		Run:   func(cmd *cobra.Command, args []string) { fmt.Print(usageText) },
	})
	jar.AddCommand(&cobra.Command{
		Use:   "status <name>",
		Short: "Jar metadata summary (cookie count, expiry — no values)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := credential.GetJarStatus(args[0])
			if err != nil {
				return shared.FailHuman(err)
			}
			output.PrintJSON(status)
			return nil
		},
	})
	jar.AddCommand(&cobra.Command{
		Use:   "show <name>",
		Short: "Show cookies (sensitive values redacted; visible values shown)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			show, err := credential.GetJarShow(args[0])
			if err != nil {
				return shared.FailHuman(err)
			}
			output.PrintJSON(show)
			return nil
		},
	})
	jar.AddCommand(&cobra.Command{
		Use:   "clear <name>",
		Short: "Wipe stored jar (cookies + token) for the profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := credential.ClearJar(args[0]); err != nil {
				return shared.FailHuman(err)
			}
			shared.PrintOK(map[string]any{"name": args[0]})
			return nil
		},
	})
	jar.AddCommand(&cobra.Command{
		Use:   "set-expires <name> <duration|RFC3339>",
		Short: "Override jar expiry (e.g. '2h' or '2026-05-01T00:00:00Z')",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			j, err := readJarOrFail(args[0])
			if err != nil {
				return err
			}
			newExp, err := parseExpiresSpec(args[1])
			if err != nil {
				return shared.Fail(err)
			}
			j.Expires = newExp
			if err := credential.WriteJar(j); err != nil {
				return shared.FailHuman(err)
			}
			status, _ := credential.GetJarStatus(args[0])
			output.PrintJSON(status)
			return nil
		},
	})
	// mark-sensitive only narrows what the LLM can see, so it doesn't
	// require the primary secret. mark-visible WIDENS what the LLM sees
	// (un-masks a stored cookie value) and IS escalation: the profile's
	// primary secret must be re-asserted, with the same overwrite-or-
	// silently-break semantics as `profile allow` / `profile set-default-header`.
	jar.AddCommand(&cobra.Command{
		Use:   "mark-sensitive <name> <cookie> [cookie...]",
		Short: "Force one or more cookie values to be redacted in jar show",
		Args:  cobra.MinimumNArgs(2),
		RunE:  markSensitivity(true, nil),
	})
	visibleAssert := &shared.PassphraseAssert{}
	visibleCmd := &cobra.Command{
		Use:   "mark-visible <name> <cookie> [cookie...]",
		Short: "Un-mask one or more cookie values (re-supply profile's primary secret)",
		Args:  cobra.MinimumNArgs(2),
		RunE:  markSensitivity(false, visibleAssert),
	}
	shared.BindPassphraseAssertFlags(visibleCmd, visibleAssert)
	jar.AddCommand(visibleCmd)
	root.AddCommand(jar)
}

// markSensitivity returns a RunE that toggles the Sensitive flag on one
// or more cookies. The variadic form (multiple cookie names after the
// profile name) lets a human flip a batch in one call instead of N.
//
// When `assert` is non-nil (the un-mask path), the profile's primary
// secret must be re-asserted before the cookie sensitivity is changed.
// Wrong values overwrite the stored secret with garbage — the LLM gains
// nothing from un-masking a jar whose underlying profile is now broken.
func markSensitivity(sensitive bool, assert *shared.PassphraseAssert) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		name := args[0]
		cookies := args[1:]
		if assert != nil {
			if _, err := shared.LoadAndAssert(name, assert); err != nil {
				return shared.Fail(err)
			}
		}
		j, err := readJarOrFail(name)
		if err != nil {
			return err
		}
		var missing []string
		for _, c := range cookies {
			if !j.MarkCookieSensitivity(c, sensitive) {
				missing = append(missing, c)
			}
		}
		if len(missing) > 0 {
			return shared.Fail(agenterrors.Newf(agenterrors.FixableByAgent,
				"cookie(s) %v not found in jar %q", missing, name))
		}
		if err := credential.WriteJar(j); err != nil {
			return shared.FailHuman(err)
		}
		show, _ := credential.GetJarShow(name)
		output.PrintJSON(show)
		return nil
	}
}

// readJarOrFail loads a jar file, returning a classified error (and
// writing it via shared.Fail) if the file is missing.
func readJarOrFail(name string) (*credential.Jar, error) {
	j, err := credential.ReadJar(name)
	if err != nil {
		return nil, shared.Fail(agenterrors.Wrap(err, agenterrors.FixableByAgent).
			WithHint("No jar file. For form profiles, run 'agent-deepweb login <name>' first."))
	}
	return j, nil
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

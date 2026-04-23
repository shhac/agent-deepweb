// Package login wires up the `login` and `session` command trees.
//
// File layout:
//
//	login.go    Register (login + session wiring) + llm-help
//	session.go  session status/show/clear/set-expires/mark-sensitive/mark-visible
//	form.go     doLogin + extractJSONToken + computeExpiry (the form-login engine)
package login

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
)

func Register(root *cobra.Command, _ shared.Globals) {
	login := &cobra.Command{
		Use:   "login <name>",
		Short: "Run a credential's form-login flow to produce a session (human-only)",
		Args:  cobra.ExactArgs(1),
		RunE: shared.HumanOnlyRunE("login", func(cmd *cobra.Command, args []string) error {
			return doLogin(args[0])
		}),
	}
	login.AddCommand(&cobra.Command{
		Use:   "llm-help",
		Short: "Show detailed reference for login/session",
		Run:   func(cmd *cobra.Command, args []string) { fmt.Print(usageText) },
	})
	root.AddCommand(login)

	registerSession(root)
}

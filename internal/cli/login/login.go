// Package login wires up the `login` and `jar` command trees.
//
// File layout:
//
//	login.go    Register (login + jar wiring) + usage
//	jar.go      jar status/show/clear/set-expires/mark-sensitive/mark-visible
//	form.go     doLogin + extractJSONToken + computeExpiry (the form-login engine)
package login

import (
	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
)

func Register(root *cobra.Command, _ shared.Globals) {
	login := &cobra.Command{
		Use:   "login <name>",
		Short: "Run a profile's form-login flow to populate its jar",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return doLogin(args[0])
		},
	}
	shared.RegisterUsage(login, "login", usageText)
	root.AddCommand(login)

	registerJar(root)
}

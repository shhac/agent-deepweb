package profile

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

// Set-* command security analysis:
//   - set-default-header → ESCALATION (can inject any header, e.g. forged
//     X-Forwarded-For). Gated by primary-secret re-assertion.
//   - set-allow-http → ESCALATION (enables cleartext credential transmission).
//     Gated.
//   - set-health → not escalation. The health URL is consulted by
//     `creds test`, which itself runs the allowlist check, so an LLM
//     pointing health at attacker.com gets stopped one step later.
//   - set-user-agent → not escalation. Pure fingerprint convenience.
//   - unset-default-header → destruction (removing a header), not escalation.

func registerSetHealth(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "set-health <name> <url>",
		Short: "Set credential's health-check URL",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := credential.SetHealth(args[0], args[1]); err != nil {
				return shared.FailHuman(err)
			}
			shared.PrintOK(map[string]any{"name": args[0], "health": args[1]})
			return nil
		},
	})
}

func registerSetDefaultHeader(parent *cobra.Command) {
	a := &shared.PassphraseAssert{}
	cmd := &cobra.Command{
		Use:   "set-default-header <name> <header>",
		Short: "Add/replace a default header 'K: V' (re-supply credential's primary secret)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			k, v, ok := shared.SplitHeader(args[1])
			if !ok {
				return shared.Fail(agenterrors.Newf(agenterrors.FixableByAgent, "malformed header %q", args[1]))
			}
			c, err := shared.LoadProfileMetadata(args[0])
			if err != nil {
				return shared.Fail(err)
			}
			if err := shared.ApplyPassphraseAssert(c.Name, a); err != nil {
				return shared.Fail(err)
			}
			if c.DefaultHeaders == nil {
				c.DefaultHeaders = map[string]string{}
			}
			c.DefaultHeaders[k] = v
			if err := credential.SetDefaultHeaders(args[0], c.DefaultHeaders); err != nil {
				return shared.FailHuman(err)
			}
			shared.PrintOK(map[string]any{"name": args[0], "default_headers": c.DefaultHeaders})
			return nil
		},
	}
	shared.BindPassphraseAssertFlags(cmd, a)
	parent.AddCommand(cmd)
}

func registerUnsetDefaultHeader(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "unset-default-header <name> <header-key>",
		Short: "Remove a default header by key",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := shared.LoadProfileMetadata(args[0])
			if err != nil {
				return shared.Fail(err)
			}
			if c.DefaultHeaders != nil {
				delete(c.DefaultHeaders, args[1])
			}
			if err := credential.SetDefaultHeaders(args[0], c.DefaultHeaders); err != nil {
				return shared.FailHuman(err)
			}
			shared.PrintOK(map[string]any{"name": args[0], "default_headers": c.DefaultHeaders})
			return nil
		},
	})
}

func registerSetAllowHTTP(parent *cobra.Command) {
	a := &shared.PassphraseAssert{}
	cmd := &cobra.Command{
		Use:   "set-allow-http <name> <true|false>",
		Short: "Permit http:// for this credential (re-supply credential's primary secret)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := shared.LoadProfileMetadata(args[0])
			if err != nil {
				return shared.Fail(err)
			}
			if err := shared.ApplyPassphraseAssert(c.Name, a); err != nil {
				return shared.Fail(err)
			}
			v := strings.ToLower(args[1])
			allow := v == "true" || v == "1" || v == "yes"
			if err := credential.SetAllowHTTP(args[0], allow); err != nil {
				return shared.FailHuman(err)
			}
			shared.PrintOK(map[string]any{"name": args[0], "allow_http": allow})
			return nil
		},
	}
	shared.BindPassphraseAssertFlags(cmd, a)
	parent.AddCommand(cmd)
}

func registerSetUserAgent(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "set-user-agent <name> <value>",
		Short: "Override User-Agent for this credential; use '' to clear",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := credential.SetUserAgent(args[0], args[1]); err != nil {
				return shared.FailHuman(err)
			}
			shared.PrintOK(map[string]any{"name": args[0], "user_agent": args[1]})
			return nil
		},
	})
}

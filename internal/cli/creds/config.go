package creds

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

func registerSetHealth(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "set-health <name> <url>",
		Short: "Set credential's health-check URL (human-only)",
		Args:  cobra.ExactArgs(2),
		RunE: shared.HumanOnlyRunE("creds set-health", func(cmd *cobra.Command, args []string) error {
			if err := credential.SetHealth(args[0], args[1]); err != nil {
				return shared.FailHuman(err)
			}
			shared.PrintOK(map[string]any{"name": args[0], "health": args[1]})
			return nil
		}),
	})
}

func registerSetDefaultHeader(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "set-default-header <name> <header>",
		Short: "Add/replace a non-secret default header 'K: V' (human-only)",
		Args:  cobra.ExactArgs(2),
		RunE: shared.HumanOnlyRunE("creds set-default-header", func(cmd *cobra.Command, args []string) error {
			k, v, ok := shared.SplitHeader(args[1])
			if !ok {
				return shared.Fail(agenterrors.Newf(agenterrors.FixableByAgent, "malformed header %q", args[1]))
			}
			c, err := credential.GetMetadata(args[0])
			if err != nil {
				return shared.Fail(credential.ClassifyLookupErr(err, args[0]))
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
		}),
	})
}

func registerUnsetDefaultHeader(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "unset-default-header <name> <header-key>",
		Short: "Remove a default header by key (human-only)",
		Args:  cobra.ExactArgs(2),
		RunE: shared.HumanOnlyRunE("creds unset-default-header", func(cmd *cobra.Command, args []string) error {
			c, err := credential.GetMetadata(args[0])
			if err != nil {
				return shared.Fail(credential.ClassifyLookupErr(err, args[0]))
			}
			if c.DefaultHeaders != nil {
				delete(c.DefaultHeaders, args[1])
			}
			if err := credential.SetDefaultHeaders(args[0], c.DefaultHeaders); err != nil {
				return shared.FailHuman(err)
			}
			shared.PrintOK(map[string]any{"name": args[0], "default_headers": c.DefaultHeaders})
			return nil
		}),
	})
}

func registerSetAllowHTTP(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "set-allow-http <name> <true|false>",
		Short: "Permit http:// for this credential (human-only)",
		Args:  cobra.ExactArgs(2),
		RunE: shared.HumanOnlyRunE("creds set-allow-http", func(cmd *cobra.Command, args []string) error {
			v := strings.ToLower(args[1])
			allow := v == "true" || v == "1" || v == "yes"
			if err := credential.SetAllowHTTP(args[0], allow); err != nil {
				return shared.FailHuman(err)
			}
			shared.PrintOK(map[string]any{"name": args[0], "allow_http": allow})
			return nil
		}),
	})
}

func registerSetUserAgent(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "set-user-agent <name> <value>",
		Short: "Override User-Agent for this credential; use '' to clear (human-only)",
		Args:  cobra.ExactArgs(2),
		RunE: shared.HumanOnlyRunE("creds set-user-agent", func(cmd *cobra.Command, args []string) error {
			if err := credential.SetUserAgent(args[0], args[1]); err != nil {
				return shared.FailHuman(err)
			}
			shared.PrintOK(map[string]any{"name": args[0], "user_agent": args[1]})
			return nil
		}),
	})
}

package profile

import (
	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

// registerSetPassphrase wires `profile set-passphrase <name>` — rotate
// ONLY the passphrase; touch nothing else about the profile.
//
// Use when a human wants to change their escalation phrase without
// rotating the primary secret. Authenticate with the current
// --passphrase (constant-time verified as usual); the new value is
// validated for min-length and stored.
//
// After set-passphrase: PassphraseAutoDerived is false (the human has
// explicitly picked a phrase), so subsequent set-secret calls preserve
// the new passphrase instead of re-deriving from the primary.
func registerSetPassphrase(parent *cobra.Command) {
	auth := &shared.PassphraseAssert{}
	var newPassphrase string
	cmd := &cobra.Command{
		Use:   "set-passphrase <name>",
		Short: "Rotate the profile's passphrase (requires current --passphrase; --new-passphrase is the replacement)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			c, err := shared.LoadAndAssert(name, auth)
			if err != nil {
				return shared.Fail(err)
			}
			if newPassphrase == "" {
				return shared.Fail(agenterrors.New("--new-passphrase is required", agenterrors.FixableByAgent))
			}
			if err := credential.ValidatePassphrase(newPassphrase); err != nil {
				return shared.Fail(agenterrors.Newf(agenterrors.FixableByAgent, "%s", err.Error()))
			}
			existing, err := credential.Resolve(name)
			if err != nil {
				return shared.Fail(credential.ClassifyLookupErr(err, name))
			}
			newSecrets := existing.Secrets
			newSecrets.Passphrase = newPassphrase
			newSecrets.PassphraseAutoDerived = false
			if _, err := credential.Store(*c, newSecrets); err != nil {
				return shared.FailHuman(err)
			}
			shared.PrintOK(map[string]any{"name": name})
			return nil
		},
	}
	shared.BindPassphraseAssertFlags(cmd, auth)
	cmd.Flags().StringVar(&newPassphrase, "new-passphrase", "", "The new passphrase (min 12 chars, no leading/trailing whitespace)")
	parent.AddCommand(cmd)
}

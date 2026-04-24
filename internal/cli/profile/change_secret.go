package profile

import (
	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

// registerChangeSecret wires `profile change-secret <name>` — the
// "rotate the primary secret, touch nothing else" verb.
//
// Today the only way to change a stored password/token was to either
// (a) `profile remove` + `profile add` (loses the jar + JarKey; forces
// re-login on form profiles), or (b) run any escalation command with
// the new creds and let the v0.2-era overwrite side-effect carry them
// through. Both are hacky. `change-secret` is the explicit verb for
// the common "my password rotated, update it" case.
//
// Mechanics:
//   - Authenticate via --passphrase (verified, constant-time).
//   - Build new Secrets from the per-type flags (same shape as
//     `profile add` takes).
//   - Overwrite the stored primary secret; preserve allowlist, default
//     headers, JarKey, and any human-set Passphrase.
//   - If the stored Passphrase was auto-derived from the primary secret
//     (user never set --passphrase at add time), re-derive it from the
//     new primary so authentication keeps working symmetrically.
//   - For form auth, clear the jar — the old session was tied to the
//     old password.
func registerChangeSecret(parent *cobra.Command) {
	auth := &shared.PassphraseAssert{}
	var (
		newToken       string
		newTokenHeader string
		newTokenPrefix string
		newUsername    string
		newPassword    string
		newCookie      string
		newCustom      []string
		newPassphrase  string
	)
	cmd := &cobra.Command{
		Use:   "change-secret <name>",
		Short: "Rotate the profile's primary secret (preserves allowlist, headers, jar-key)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			c, err := shared.LoadProfileMetadata(name)
			if err != nil {
				return shared.Fail(err)
			}
			if err := shared.ApplyPassphraseAssert(name, auth); err != nil {
				return shared.Fail(err)
			}

			newSecrets, err := credential.BuildSecretsCore(c.Type, credential.SecretInputs{
				Token:         newToken,
				TokenHeader:   newTokenHeader,
				TokenPrefix:   newTokenPrefix,
				Username:      newUsername,
				Password:      newPassword,
				Cookie:        newCookie,
				CustomHeaders: newCustom,
			})
			if err != nil {
				return shared.Fail(agenterrors.Newf(agenterrors.FixableByAgent,
					"%s for the new primary secret", err.Error()))
			}

			existing, err := credential.Resolve(name)
			if err != nil {
				return shared.Fail(credential.ClassifyLookupErr(err, name))
			}
			if c.Type == credential.AuthForm {
				// Preserve form config (LoginURL, ExtraFields, TokenPath, etc.);
				// replace only the secret-bearing fields we just built.
				merged := existing.Secrets
				merged.Username = newSecrets.Username
				merged.Password = newSecrets.Password
				newSecrets = merged
			}
			newSecrets.JarKey = existing.Secrets.JarKey

			// Passphrase handling:
			//   - If --new-passphrase supplied: validate + store (auto-derived = false).
			//   - Else if existing was auto-derived: re-derive from new primary.
			//   - Else (human-set passphrase): preserve.
			switch {
			case newPassphrase != "":
				if err := credential.ValidatePassphrase(newPassphrase); err != nil {
					return shared.Fail(agenterrors.Newf(agenterrors.FixableByAgent, "%s", err.Error()))
				}
				newSecrets.Passphrase = newPassphrase
				newSecrets.PassphraseAutoDerived = false
			case existing.Secrets.PassphraseAutoDerived:
				newSecrets.Passphrase = credential.DefaultPassphrase(c.Type, newSecrets)
				newSecrets.PassphraseAutoDerived = true
			default:
				newSecrets.Passphrase = existing.Secrets.Passphrase
				newSecrets.PassphraseAutoDerived = false
			}

			if _, err := credential.Store(*c, newSecrets); err != nil {
				return shared.FailHuman(err)
			}
			if c.Type == credential.AuthForm {
				_ = credential.ClearJar(name)
			}
			shared.PrintOK(map[string]any{"name": name})
			return nil
		},
	}
	shared.BindPassphraseAssertFlags(cmd, auth)

	f := cmd.Flags()
	f.StringVar(&newToken, "token", "", "New bearer token (for --type bearer)")
	f.StringVar(&newTokenHeader, "token-header", "", "Header name for bearer token")
	f.StringVar(&newTokenPrefix, "token-prefix", "", "Prefix for bearer token")
	f.StringVar(&newUsername, "username", "", "New username (for --type basic or form)")
	f.StringVar(&newPassword, "password", "", "New password (for --type basic or form)")
	f.StringVar(&newCookie, "cookie", "", "New raw cookie value (for --type cookie)")
	f.StringArrayVar(&newCustom, "custom-header", nil, "New custom header 'K: V' (for --type custom; repeatable, replaces the whole set)")
	f.StringVar(&newPassphrase, "new-passphrase", "", "Also rotate the passphrase to this value (min 12 chars). Default: preserve existing / re-derive if auto-derived.")

	parent.AddCommand(cmd)
}

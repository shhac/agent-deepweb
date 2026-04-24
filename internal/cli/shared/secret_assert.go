package shared

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

// PassphraseAssert collects the --passphrase flag shared by every
// escalation command. Compared (constant-time) against the stored
// Secrets.Passphrase at the CLI edge; a mismatch yields a fixable_by:
// agent error without mutating any state.
//
// Replaces the v0.2-era SecretAssert, which required re-pasting the
// primary secret per-type (--token / --username+--password / --cookie /
// --custom-header). That had nasty UX for long tokens and had
// "overwrite, don't verify" semantics that silently broke profiles on
// typos. Passphrase verification is cleaner: wrong value errors, right
// value proceeds, no stored state changes.
type PassphraseAssert struct {
	Passphrase string
}

// BindPassphraseAssertFlags adds --passphrase to a cobra command.
func BindPassphraseAssertFlags(cmd *cobra.Command, a *PassphraseAssert) {
	cmd.Flags().StringVar(&a.Passphrase, "passphrase", "",
		"Profile's passphrase (set at 'profile add'; defaults to the primary secret if --passphrase wasn't used at add time)")
}

// LoadAndAssert is the canonical preamble for every escalation command:
// load the profile's metadata then verify the supplied passphrase.
// Collapses ~6 lines of load+error-check+assert+error-check boilerplate
// at every call site.
//
// On lookup or mismatch, the error is already classified — callers can
// `return shared.Fail(err)` directly without wrapping.
func LoadAndAssert(name string, a *PassphraseAssert) (*credential.Credential, error) {
	c, err := LoadProfileMetadata(name)
	if err != nil {
		return nil, err
	}
	if err := ApplyPassphraseAssert(c.Name, a); err != nil {
		return nil, err
	}
	return c, nil
}

// ApplyPassphraseAssert verifies the supplied passphrase against the
// stored one for the named profile. Returns a classified error on
// missing flag or mismatch; the caller should propagate via shared.Fail
// without performing the mutation.
//
// Does not touch stored state on either branch. The mutation (scope
// widening, header change, primary-secret rotation, etc.) is the
// caller's responsibility and runs only on success.
func ApplyPassphraseAssert(name string, a *PassphraseAssert) error {
	if a.Passphrase == "" {
		return agenterrors.New("--passphrase is required", agenterrors.FixableByAgent).
			WithHint("Use the passphrase you set at 'profile add' (or, if you didn't, the profile's primary secret — token/password/cookie/custom-header value)")
	}
	err := credential.VerifyPassphrase(name, a.Passphrase)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, credential.ErrPassphraseMismatch):
		return agenterrors.Newf(agenterrors.FixableByAgent,
			"--passphrase does not match the one stored for %q", name).
			WithHint("Check the passphrase with the user. If they don't remember, they can run 'agent-deepweb profile set-secret " + name + "' to rotate.")
	default:
		return credential.ClassifyLookupErr(err, name)
	}
}

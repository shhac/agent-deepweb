package shared

import (
	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

// SecretAssert collects the per-type "primary secret" flags that escalation
// commands require. The user always re-supplies the credential's primary
// secret to perform an escalation (widen allowlist, change defaults,
// un-mask cookies). Two failure modes:
//
//   - Missing required flag → command errors with fixable_by:agent and a
//     hint naming what's missing. Helps a forgetful human.
//   - Wrong value supplied → command silently overwrites the stored secret
//     with what was supplied. The credential is now broken; subsequent
//     fetches send garbage auth bytes — no exfil. For form-auth, also
//     invalidates the derived session.
//
// This puts the asymmetry between humans (who know the secret, no harm)
// and LLMs (who don't, end up with a broken credential they can't exploit)
// at the right place: the write itself.
type SecretAssert struct {
	// bearer
	Token       string
	TokenHeader string
	TokenPrefix string
	// basic + form
	Username string
	Password string
	// cookie
	Cookie string
	// custom
	CustomHeaders []string
}

// BindSecretAssertFlags adds the primary-secret flags to a cobra command.
// The same flag set covers all auth types — BuildSecretsForAssert decides
// later which fields are required for the existing credential's type.
func BindSecretAssertFlags(cmd *cobra.Command, a *SecretAssert) {
	f := cmd.Flags()
	f.StringVar(&a.Token, "token", "", "Bearer token (required when escalating a bearer credential)")
	f.StringVar(&a.TokenHeader, "token-header", "", "Override token header name")
	f.StringVar(&a.TokenPrefix, "token-prefix", "", "Override token prefix")
	f.StringVar(&a.Username, "username", "", "Username (required when escalating a basic or form credential)")
	f.StringVar(&a.Password, "password", "", "Password (required when escalating a basic or form credential)")
	f.StringVar(&a.Cookie, "cookie", "", "Cookie value (required when escalating a cookie credential)")
	f.StringArrayVar(&a.CustomHeaders, "custom-header", nil, "Header 'K: V' (required when escalating a custom credential; repeatable)")
}

// BuildSecretsForAssert takes the existing credential's type and validates
// that the per-type required flags were supplied (returns fixable_by:agent
// error if not). On success, returns the Secrets struct that will be
// stored — which contains the user-supplied values. If the user supplied
// the right values, this struct equals what's already stored (overwrite is
// a no-op). If they supplied wrong values, the credential is overwritten
// with garbage.
func BuildSecretsForAssert(authType string, a *SecretAssert) (credential.Secrets, error) {
	switch authType {
	case credential.AuthBearer:
		if a.Token == "" {
			return credential.Secrets{}, agenterrors.New(
				"--token is required to escalate a bearer credential", agenterrors.FixableByAgent)
		}
		return credential.Secrets{
			Token:  a.Token,
			Header: a.TokenHeader,
			Prefix: a.TokenPrefix,
		}, nil
	case credential.AuthBasic:
		if a.Username == "" || a.Password == "" {
			return credential.Secrets{}, agenterrors.New(
				"--username and --password are required to escalate a basic credential", agenterrors.FixableByAgent)
		}
		return credential.Secrets{Username: a.Username, Password: a.Password}, nil
	case credential.AuthCookie:
		if a.Cookie == "" {
			return credential.Secrets{}, agenterrors.New(
				"--cookie is required to escalate a cookie credential", agenterrors.FixableByAgent)
		}
		return credential.Secrets{Cookie: a.Cookie}, nil
	case credential.AuthCustom:
		if len(a.CustomHeaders) == 0 {
			return credential.Secrets{}, agenterrors.New(
				"--custom-header is required to escalate a custom credential", agenterrors.FixableByAgent)
		}
		headers := map[string]string{}
		for _, h := range a.CustomHeaders {
			k, v, ok := SplitHeader(h)
			if !ok {
				return credential.Secrets{}, agenterrors.Newf(agenterrors.FixableByAgent,
					"malformed --custom-header %q", h)
			}
			headers[k] = v
		}
		return credential.Secrets{Headers: headers}, nil
	case credential.AuthForm:
		// Form needs username+password (the inputs to the login flow);
		// the rest of the form-credential fields (login URL, token path,
		// etc.) are kept as-is — escalation only re-asserts the secret.
		if a.Username == "" || a.Password == "" {
			return credential.Secrets{}, agenterrors.New(
				"--username and --password are required to escalate a form credential", agenterrors.FixableByAgent)
		}
		return credential.Secrets{Username: a.Username, Password: a.Password}, nil
	default:
		return credential.Secrets{}, agenterrors.Newf(agenterrors.FixableByAgent,
			"unknown auth type %q; cannot escalate", authType)
	}
}

// EscalateOverwrite re-asserts the primary secret of an existing credential
// by overwriting the stored Secrets. For non-form types, the new Secrets
// fully replace the stored ones. For form, the new {Username, Password}
// fields are merged into the existing form config (LoginURL, ExtraFields,
// TokenPath, etc. preserved); the derived session file is then deleted to
// force a re-login.
//
// Returns no error from a "wrong" secret — by design, an LLM with garbage
// values just breaks the credential, which is what we want.
func EscalateOverwrite(name string, asserted credential.Secrets) error {
	existing, err := credential.Resolve(name)
	if err != nil {
		return credential.ClassifyLookupErr(err, name)
	}
	newSecrets := asserted
	if existing.Type == credential.AuthForm {
		// Preserve form config; replace only username+password.
		merged := existing.Secrets
		merged.Username = asserted.Username
		merged.Password = asserted.Password
		newSecrets = merged
	}
	if _, err := credential.Store(existing.Credential, newSecrets); err != nil {
		return agenterrors.Wrap(err, agenterrors.FixableByHuman)
	}
	// For form auth, invalidate the derived session — the new password
	// might not produce the same session, so any cookies/token from the
	// old session are now suspect. The next request will fail-with-hint
	// telling the user to re-login.
	if existing.Type == credential.AuthForm {
		_ = credential.ClearJar(name)
	}
	return nil
}

// ApplySecretAssert is the convenience caller: validates the per-type
// required flags then overwrites the credential's stored secret. Wrong
// value → silent overwrite (the credential is now broken).
func ApplySecretAssert(c *credential.Credential, a *SecretAssert) error {
	secrets, err := BuildSecretsForAssert(c.Type, a)
	if err != nil {
		return err
	}
	return EscalateOverwrite(c.Name, secrets)
}

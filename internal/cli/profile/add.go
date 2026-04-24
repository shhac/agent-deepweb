package profile

import (
	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/output"
)

// buildSecretsForAdd produces the stored Secrets for a `profile add` call,
// combining the secret-bearing core (validated via the shared
// credential.BuildSecretsCore — same validator used by the escalation
// path) with form-only non-secret fields (LoginURL, TokenPath, etc.).
//
// Single source of truth: changing the per-type required-flag rules in
// BuildSecretsCore automatically affects both add-time and escalate-time
// validation, no drift.
func buildSecretsForAdd(o *addOpts) (credential.Secrets, error) {
	s, err := credential.BuildSecretsCore(o.authType, credential.SecretInputs{
		Token:         o.token,
		TokenHeader:   o.tokenHeaderSet,
		TokenPrefix:   o.tokenPrefixSet,
		Username:      o.username,
		Password:      o.password,
		Cookie:        o.cookie,
		CustomHeaders: o.customHeaders,
	})
	if err != nil {
		return credential.Secrets{}, agenterrors.Newf(agenterrors.FixableByAgent,
			"%s for %s type", err.Error(), o.authType)
	}
	if o.authType != credential.AuthForm {
		return s, nil
	}
	// Form-only: layer on the non-secret config fields.
	if o.loginURL == "" {
		return credential.Secrets{}, agenterrors.New("--login-url is required for form type", agenterrors.FixableByAgent)
	}
	s.LoginURL = o.loginURL
	s.LoginMethod = o.loginMethod
	s.LoginFormat = o.loginFormat
	s.LoginBodyTemplate = o.loginBodyTemplate
	s.UsernameField = o.usernameField
	s.PasswordField = o.passwordField
	s.SuccessStatus = o.successStatus
	s.TokenPath = o.tokenPath
	s.SessionTTL = o.sessionTTL
	s.Header = o.formTokenHeader
	s.Prefix = o.formTokenPrefix
	if len(o.extraFields) > 0 {
		s.ExtraFields = map[string]string{}
		for _, f := range o.extraFields {
			k, v, err := shared.SplitKV(f, "--extra-field")
			if err != nil {
				return credential.Secrets{}, err
			}
			s.ExtraFields[k] = v
		}
	}
	return s, nil
}

func registerAdd(parent *cobra.Command) {
	o := &addOpts{}
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Register a new credential (human-only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if o.authType == "" {
				return shared.Fail(agenterrors.New("--type is required", agenterrors.FixableByAgent).
					WithHint("Choose one of: bearer, basic, cookie, form, custom"))
			}
			if len(o.domains) == 0 {
				return shared.Fail(agenterrors.New("at least one --domain is required", agenterrors.FixableByAgent).
					WithHint("A credential is only used on hosts in its allowlist (host or host:port)"))
			}
			defaultHeaders, err := parseHeaderList(o.headers)
			if err != nil {
				return shared.Fail(err)
			}
			secrets, err := buildSecretsForAdd(o)
			if err != nil {
				return shared.Fail(err)
			}
			if err := credential.ValidatePassphrase(o.passphrase); err != nil {
				return shared.Fail(agenterrors.Newf(agenterrors.FixableByAgent, "%s", err.Error()))
			}
			if o.passphrase != "" {
				secrets.Passphrase = o.passphrase
				secrets.PassphraseAutoDerived = false
			}

			c := credential.Credential{
				Name:           args[0],
				Type:           o.authType,
				Domains:        o.domains,
				Paths:          o.paths,
				DefaultHeaders: defaultHeaders,
				UserAgent:      o.userAgent,
				Health:         o.health,
				Notes:          o.notes,
				AllowHTTP:      o.allowHTTP,
			}
			storage, err := credential.Store(c, secrets)
			if err != nil {
				return shared.FailHuman(err)
			}
			output.PrintJSON(map[string]any{
				"status":  "ok",
				"name":    c.Name,
				"type":    c.Type,
				"domains": c.Domains,
				"paths":   c.Paths,
				"storage": storage,
			})
			return nil
		},
	}
	bindAddFlags(cmd, o)
	parent.AddCommand(cmd)
}

// parseHeaderList parses a list of "K: V" strings into a map, failing on
// malformed entries.
func parseHeaderList(hs []string) (map[string]string, error) {
	if len(hs) == 0 {
		return nil, nil
	}
	out := map[string]string{}
	for _, h := range hs {
		k, v, ok := shared.SplitHeader(h)
		if !ok {
			return nil, agenterrors.Newf(agenterrors.FixableByAgent, "malformed --default-header %q", h)
		}
		out[k] = v
	}
	return out, nil
}

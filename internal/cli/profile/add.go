package profile

import (
	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/output"
)

// addOpts collects every flag registerAdd binds. Kept as one struct (rather
// than one-struct-per-auth-type) so `creds add --type X` stays a single
// invocation. The per-type validation and Secrets construction lives in
// the secretsBuilders map below.
type addOpts struct {
	authType  string
	domains   []string
	paths     []string
	headers   []string // --default-header, non-secret
	userAgent string
	health    string
	notes     string
	allowHTTP bool

	// bearer
	token          string
	tokenHeaderSet string
	tokenPrefixSet string

	// basic + form
	username string
	password string

	// cookie
	cookie string

	// custom headers (secret)
	customHeaders []string

	// form login
	loginURL          string
	loginMethod       string
	loginFormat       string
	loginBodyTemplate string
	usernameField     string
	passwordField     string
	extraFields       []string
	successStatus     int
	tokenPath         string
	formTokenHeader   string
	formTokenPrefix   string
	sessionTTL        string
}

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

func bindAddFlags(cmd *cobra.Command, o *addOpts) {
	f := cmd.Flags()
	f.StringVar(&o.authType, "type", "", "Auth type: bearer, basic, cookie, form, custom")
	f.StringArrayVar(&o.domains, "domain", nil, "Allowed host[:port] (repeatable; exact or *.wildcard; port optional)")
	f.StringArrayVar(&o.paths, "path", nil, "Allowed URL path pattern (repeatable; exact, /prefix/*, or path.Match glob)")
	f.StringArrayVar(&o.headers, "default-header", nil, "Non-secret header 'K: V' applied to every request (repeatable)")
	f.StringVar(&o.userAgent, "user-agent", "", "Override User-Agent for this credential (default: agent-deepweb/<version>)")
	f.StringVar(&o.health, "health", "", "URL for 'creds test' health check")
	f.StringVar(&o.notes, "notes", "", "Freeform note")
	f.BoolVar(&o.allowHTTP, "allow-http", false, "Permit http:// (not just https://); default is https-only except loopback")

	f.StringVar(&o.token, "token", "", "Bearer token (for --type bearer)")
	f.StringVar(&o.tokenHeaderSet, "token-header", "", "Header name for bearer token (default Authorization)")
	f.StringVar(&o.tokenPrefixSet, "token-prefix", "", "Bearer-token prefix (default 'Bearer ')")

	f.StringVar(&o.username, "username", "", "Username (for basic or form type)")
	f.StringVar(&o.password, "password", "", "Password (for basic or form type)")

	f.StringVar(&o.cookie, "cookie", "", "Raw Cookie header value (for --type cookie)")

	f.StringArrayVar(&o.customHeaders, "custom-header", nil, "Header 'K: V' for --type custom (repeatable; treated as secret)")

	f.StringVar(&o.loginURL, "login-url", "", "Form-login URL (for --type form)")
	f.StringVar(&o.loginMethod, "login-method", "", "HTTP method for login (default POST)")
	f.StringVar(&o.loginFormat, "login-format", "", "'form' (default) or 'json' body for login")
	f.StringVar(&o.loginBodyTemplate, "login-body-template", "", "Override body with a JSON template; {{username}}/{{password}}/{{<extra>}} are JSON-escaped-substituted. Content-Type is forced application/json.")
	f.StringVar(&o.usernameField, "username-field", "", "Form field for username (default 'username')")
	f.StringVar(&o.passwordField, "password-field", "", "Form field for password (default 'password')")
	f.StringArrayVar(&o.extraFields, "extra-field", nil, "Extra non-secret form field 'k=v' (repeatable)")
	f.IntVar(&o.successStatus, "success-status", 0, "Expected HTTP status for successful login (default 200)")
	f.StringVar(&o.tokenPath, "token-path", "", "Dot-path into response JSON to extract token (e.g. 'access_token' or 'data.token')")
	f.StringVar(&o.formTokenHeader, "form-token-header", "", "Header for extracted token (default Authorization)")
	f.StringVar(&o.formTokenPrefix, "form-token-prefix", "", "Prefix for extracted token (default 'Bearer ')")
	f.StringVar(&o.sessionTTL, "session-ttl", "", "Cap session expiry (e.g. '1h', '24h')")
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

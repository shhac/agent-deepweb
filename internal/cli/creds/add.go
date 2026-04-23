package creds

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
	loginURL        string
	loginMethod     string
	loginFormat     string
	usernameField   string
	passwordField   string
	extraFields     []string
	successStatus   int
	tokenPath       string
	formTokenHeader string
	formTokenPrefix string
	sessionTTL      string
}

// secretsBuilder is a per-auth-type factory: given the opts, produce the
// stored Secrets or a classified error. Splitting it out of the RunE makes
// each branch independently unit-testable.
type secretsBuilder func(o *addOpts) (credential.Secrets, error)

var secretsBuilders = map[string]secretsBuilder{
	credential.AuthBearer: buildBearerSecrets,
	credential.AuthBasic:  buildBasicSecrets,
	credential.AuthCookie: buildCookieSecrets,
	credential.AuthCustom: buildCustomSecrets,
	credential.AuthForm:   buildFormSecrets,
}

func buildBearerSecrets(o *addOpts) (credential.Secrets, error) {
	if o.token == "" {
		return credential.Secrets{}, agenterrors.New("--token is required for bearer type", agenterrors.FixableByAgent)
	}
	return credential.Secrets{
		Token:  o.token,
		Header: o.tokenHeaderSet,
		Prefix: o.tokenPrefixSet,
	}, nil
}

func buildBasicSecrets(o *addOpts) (credential.Secrets, error) {
	if o.username == "" || o.password == "" {
		return credential.Secrets{}, agenterrors.New("--username and --password are required for basic type", agenterrors.FixableByAgent)
	}
	return credential.Secrets{Username: o.username, Password: o.password}, nil
}

func buildCookieSecrets(o *addOpts) (credential.Secrets, error) {
	if o.cookie == "" {
		return credential.Secrets{}, agenterrors.New("--cookie is required for cookie type", agenterrors.FixableByAgent)
	}
	return credential.Secrets{Cookie: o.cookie}, nil
}

func buildCustomSecrets(o *addOpts) (credential.Secrets, error) {
	if len(o.customHeaders) == 0 {
		return credential.Secrets{}, agenterrors.New("--custom-header is required for custom type", agenterrors.FixableByAgent)
	}
	headers := map[string]string{}
	for _, h := range o.customHeaders {
		k, v, ok := shared.SplitHeader(h)
		if !ok {
			return credential.Secrets{}, agenterrors.Newf(agenterrors.FixableByAgent,
				"malformed --custom-header %q", h)
		}
		headers[k] = v
	}
	return credential.Secrets{Headers: headers}, nil
}

func buildFormSecrets(o *addOpts) (credential.Secrets, error) {
	if o.loginURL == "" {
		return credential.Secrets{}, agenterrors.New("--login-url is required for form type", agenterrors.FixableByAgent)
	}
	if o.username == "" || o.password == "" {
		return credential.Secrets{}, agenterrors.New("--username and --password are required for form type", agenterrors.FixableByAgent)
	}
	s := credential.Secrets{
		LoginURL:      o.loginURL,
		LoginMethod:   o.loginMethod,
		LoginFormat:   o.loginFormat,
		Username:      o.username,
		Password:      o.password,
		UsernameField: o.usernameField,
		PasswordField: o.passwordField,
		SuccessStatus: o.successStatus,
		TokenPath:     o.tokenPath,
		SessionTTL:    o.sessionTTL,
		// Token header/prefix for the Bearer produced by login.
		Header: o.formTokenHeader,
		Prefix: o.formTokenPrefix,
	}
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
			builder, ok := secretsBuilders[o.authType]
			if !ok {
				return shared.Fail(agenterrors.Newf(agenterrors.FixableByAgent, "unknown --type %q", o.authType).
					WithHint("One of: bearer, basic, cookie, form, custom"))
			}

			defaultHeaders, err := parseHeaderList(o.headers)
			if err != nil {
				return shared.Fail(err)
			}
			secrets, err := builder(o)
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

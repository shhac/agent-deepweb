package profile

import (
	"github.com/spf13/cobra"
)

// addOpts collects every flag registerAdd binds. Kept as one struct (rather
// than one-struct-per-auth-type) so `profile add --type X` stays a single
// invocation. The per-type validation and Secrets construction lives in
// buildSecretsForAdd (add.go).
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

	// passphrase — optional human-level authorization phrase for
	// escalation commands. Defaults to the primary-secret representative
	// value when not set at add time (see credential.DefaultPassphrase).
	passphrase string

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
	f.StringVar(&o.passphrase, "passphrase", "", "Human-level passphrase for escalation commands (min 12 chars). Defaults to the primary secret if not set — so you can always type the token/password to escalate.")

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

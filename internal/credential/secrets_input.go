package credential

import (
	"errors"
	"fmt"
	"strings"
)

// SecretInputs collects raw user input for the secret-bearing fields of
// a credential, in a shape that's symmetric across the two callers that
// need it: profile add (where the secret is being supplied for the first
// time) and the escalation flow (where the secret is being re-asserted).
//
// Both callers run the same validation and produce the same Secrets
// struct for a given auth type; SecretInputs is the unified input
// shape, BuildSecretsCore is the unified validator.
type SecretInputs struct {
	// bearer
	Token       string
	TokenHeader string
	TokenPrefix string

	// basic + form
	Username string
	Password string

	// cookie
	Cookie string

	// custom — raw "K: V" strings parsed inside BuildSecretsCore.
	CustomHeaders []string
}

// BuildSecretsCore validates per-type required fields and returns the
// secret portion of Secrets. The error is plain-text and lacks ambient
// context (e.g. "for bearer type" vs "to escalate a bearer
// credential") — the caller wraps with whatever phrasing fits.
//
// For form auth, only {Username, Password} are populated here. Form-
// specific non-secret fields (LoginURL, TokenPath, ExtraFields, etc.)
// are layered on by the caller — those are profile config, not the
// re-asserted secret, so they don't belong in this helper.
func BuildSecretsCore(authType string, in SecretInputs) (Secrets, error) {
	switch authType {
	case AuthBearer:
		if in.Token == "" {
			return Secrets{}, errors.New("--token is required")
		}
		return Secrets{Token: in.Token, Header: in.TokenHeader, Prefix: in.TokenPrefix}, nil
	case AuthBasic:
		if in.Username == "" || in.Password == "" {
			return Secrets{}, errors.New("--username and --password are required")
		}
		return Secrets{Username: in.Username, Password: in.Password}, nil
	case AuthCookie:
		if in.Cookie == "" {
			return Secrets{}, errors.New("--cookie is required")
		}
		return Secrets{Cookie: in.Cookie}, nil
	case AuthCustom:
		if len(in.CustomHeaders) == 0 {
			return Secrets{}, errors.New("--custom-header is required")
		}
		headers := map[string]string{}
		for _, h := range in.CustomHeaders {
			k, v, ok := splitHeaderColon(h)
			if !ok {
				return Secrets{}, fmt.Errorf("malformed --custom-header %q", h)
			}
			headers[k] = v
		}
		return Secrets{Headers: headers}, nil
	case AuthForm:
		if in.Username == "" || in.Password == "" {
			return Secrets{}, errors.New("--username and --password are required")
		}
		return Secrets{Username: in.Username, Password: in.Password}, nil
	default:
		return Secrets{}, fmt.Errorf("unknown auth type %q", authType)
	}
}

// splitHeaderColon parses "Name: value", trimming whitespace. Local copy
// of shared.SplitHeader to avoid an import cycle (shared imports
// credential for LoadProfileMetadata).
func splitHeaderColon(h string) (key, value string, ok bool) {
	i := strings.IndexByte(h, ':')
	if i < 0 {
		return "", "", false
	}
	k := strings.TrimSpace(h[:i])
	v := strings.TrimSpace(h[i+1:])
	if k == "" {
		return "", "", false
	}
	return k, v, true
}

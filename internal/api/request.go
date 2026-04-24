package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/shhac/agent-deepweb/internal/config"
	"github.com/shhac/agent-deepweb/internal/credential"
)

// buildHTTPRequest composes the *http.Request from a high-level Request
// struct. Header application order matters:
//  1. credential's DefaultHeaders (non-secret, metadata)
//  2. user-supplied Headers (can override defaults)
//  3. ApplyAuth (last — so user can't clobber Authorization)
//  4. User-Agent (always set last)
func buildHTTPRequest(ctx context.Context, req Request) (*http.Request, error) {
	method := strings.ToUpper(req.Method)
	if method == "" {
		method = http.MethodGet
	}

	u := req.URL
	if len(req.Query) > 0 {
		var parts []string
		for k, vs := range req.Query {
			for _, v := range vs {
				parts = append(parts, fmt.Sprintf("%s=%s", k, v))
			}
		}
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		u = u + sep + strings.Join(parts, "&")
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, u, req.Body)
	if err != nil {
		return nil, err
	}

	if req.Auth != nil {
		for k, v := range req.Auth.DefaultHeaders {
			httpReq.Header.Set(k, v)
		}
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	ApplyAuth(httpReq, req.Auth)
	httpReq.Header.Set("User-Agent", resolveUserAgent(req))
	return httpReq, nil
}

// resolveUserAgent picks the User-Agent by precedence:
//  1. per-request UserAgent field
//  2. credential's UserAgent
//  3. user-set Header "User-Agent" (via --header 'User-Agent: ...')
//  4. config key default.user-agent
//  5. default "agent-deepweb/<Version>"
func resolveUserAgent(req Request) string {
	if req.UserAgent != "" {
		return req.UserAgent
	}
	if req.Auth != nil && req.Auth.UserAgent != "" {
		return req.Auth.UserAgent
	}
	if hv, ok := req.Headers["User-Agent"]; ok && hv != "" {
		return hv
	}
	if cfgUA := strings.TrimSpace(config.Read().Defaults.UserAgent); cfgUA != "" {
		return cfgUA
	}
	return "agent-deepweb/" + Version
}

// viewPersisted projects a PersistedCookie into its LLM-facing CookieView
// with sensitive values masked. Lives in api (not credential) because it's
// only used when surfacing new cookies on a response envelope.
func viewPersisted(p credential.PersistedCookie) credential.CookieView {
	val := p.Value
	if p.Sensitive {
		val = "<redacted>"
	}
	return credential.CookieView{
		Name:      p.Name,
		Value:     val,
		Domain:    p.Domain,
		Path:      p.Path,
		Expires:   p.Expires,
		HttpOnly:  p.HttpOnly,
		Secure:    p.Secure,
		Sensitive: p.Sensitive,
	}
}

package api

import (
	"encoding/base64"
	"net/http"

	"github.com/shhac/agent-deepweb/internal/credential"
)

// ApplyAuth mutates req by attaching headers for the given credential.
// No secret value is ever returned to the caller.
//
// Note: cookie-jar session cookies are attached by the http.Client.Jar (set
// up in client.Do for form-auth credentials), not here. This function only
// sets Authorization / Cookie / custom headers that aren't jar-managed.
func ApplyAuth(req *http.Request, resolved *credential.Resolved) {
	if resolved == nil {
		return
	}
	switch resolved.Type {
	case credential.AuthBearer, credential.AuthCustom:
		applyBearerOrCustom(req, resolved)
	case credential.AuthBasic:
		applyBasic(req, resolved)
	case credential.AuthCookie:
		applyCookie(req, resolved)
	case credential.AuthForm:
		applyFormToken(req, resolved)
	}
}

func applyBearerOrCustom(req *http.Request, r *credential.Resolved) {
	if r.Type == credential.AuthCustom {
		for k, v := range r.Secrets.Headers {
			req.Header.Set(k, v)
		}
	}
	if r.Secrets.Token == "" {
		return
	}
	header := r.Secrets.Header
	if header == "" {
		header = "Authorization"
	}
	prefix := r.Secrets.Prefix
	if prefix == "" && header == "Authorization" {
		prefix = "Bearer "
	}
	req.Header.Set(header, prefix+r.Secrets.Token)
}

func applyBasic(req *http.Request, r *credential.Resolved) {
	if r.Secrets.Username == "" && r.Secrets.Password == "" {
		return
	}
	token := r.Secrets.Username + ":" + r.Secrets.Password
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(token)))
}

func applyCookie(req *http.Request, r *credential.Resolved) {
	if r.Secrets.Cookie != "" {
		req.Header.Set("Cookie", r.Secrets.Cookie)
	}
}

// applyFormToken attaches a token extracted at login time (from the login
// response body) as an Authorization header. Cookies from that same login
// flow are attached via the http.Client's cookiejar, set up in client.Do.
func applyFormToken(req *http.Request, r *credential.Resolved) {
	sess, err := credential.ReadSession(r.Name)
	if err != nil || sess.Token == "" {
		return
	}
	header := sess.TokenHeader
	if header == "" {
		header = "Authorization"
	}
	prefix := sess.TokenPrefix
	if prefix == "" && header == "Authorization" {
		prefix = "Bearer "
	}
	req.Header.Set(header, prefix+sess.Token)
}

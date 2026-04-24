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
	case credential.AuthBearer:
		applyBearer(req, resolved)
	case credential.AuthCustom:
		applyCustom(req, resolved)
	case credential.AuthBasic:
		applyBasic(req, resolved)
	case credential.AuthCookie:
		applyCookie(req, resolved)
	case credential.AuthForm:
		applyFormToken(req, resolved)
	}
}

func applyBearer(req *http.Request, r *credential.Resolved) {
	if r.Secrets.Token == "" {
		return
	}
	setBearerLikeHeader(req, r.Secrets.Header, r.Secrets.Prefix, r.Secrets.Token)
}

// applyCustom sets each header in r.Secrets.Headers verbatim. The token
// fields (Header/Prefix/Token) are NOT applied — that's the bearer arm —
// so a custom credential is purely "attach these headers, nothing else."
func applyCustom(req *http.Request, r *credential.Resolved) {
	for k, v := range r.Secrets.Headers {
		req.Header.Set(k, v)
	}
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
	sess, err := credential.ReadJar(r.Name)
	if err != nil || sess.Token == "" {
		return
	}
	setBearerLikeHeader(req, sess.TokenHeader, sess.TokenPrefix, sess.Token)
}

// setBearerLikeHeader writes "<header>: <prefix><token>" to req with the
// Authorization/Bearer defaults filled in. Both bearer-token and form-
// auth share this shape; centralising it avoids drift between the two.
func setBearerLikeHeader(req *http.Request, header, prefix, token string) {
	if header == "" {
		header = "Authorization"
	}
	if prefix == "" && header == "Authorization" {
		prefix = "Bearer "
	}
	req.Header.Set(header, prefix+token)
}

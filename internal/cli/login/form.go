package login

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"

	"golang.org/x/net/publicsuffix"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/output"
)

// doLogin is the top-level form-login orchestrator.
//
//  1. Resolve the credential; confirm type=form.
//  2. Validate the login-url is inside the credential's allowlist.
//  3. Build the POST body (form or JSON) from username/password/extra_fields.
//  4. Issue the login request with a fresh cookiejar + 30s timeout.
//  5. Check the HTTP status matches success-status (default 200).
//  6. Assemble a Jar from the response (cookies + token + expiry).
//  7. Write the jar file.
//
// Nothing from the response body is returned to the caller — we print
// only the jar summary (cookie count, sensitive count, expiry).
func doLogin(name string) error {
	resolved, err := shared.LoadProfileResolved(name)
	if err != nil {
		return shared.Fail(err)
	}
	if resolved.Type != credential.AuthForm {
		return shared.Fail(agenterrors.Newf(agenterrors.FixableByAgent,
			"credential %q is type %q, not 'form'", name, resolved.Type).
			WithHint("login is only applicable to --type form credentials"))
	}

	loginURL, err := validateLoginURL(resolved)
	if err != nil {
		return shared.Fail(err)
	}

	body, contentType, err := buildLoginBody(resolved.Secrets)
	if err != nil {
		return shared.Fail(err)
	}

	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	resp, err := performLoginRequest(resolved, loginURL, body, contentType, jar)
	if err != nil {
		return shared.Fail(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	expected := resolved.Secrets.SuccessStatus
	if expected == 0 {
		expected = http.StatusOK
	}
	if resp.StatusCode != expected {
		return shared.Fail(agenterrors.Newf(agenterrors.FixableByHuman,
			"login returned HTTP %d (expected %d)", resp.StatusCode, expected).
			WithHint("Check the credential's --username / --password / --login-url with the user"))
	}

	sess, err := assembleJar(name, resolved, resp, jar, loginURL)
	if err != nil {
		return shared.Fail(err)
	}

	if err := credential.WriteJar(sess); err != nil {
		return shared.FailHuman(err)
	}

	status, _ := credential.GetJarStatus(name)
	output.PrintJSON(map[string]any{
		"status":  "ok",
		"session": status,
	})
	return nil
}

// assembleJar turns a successful login response into a fully-populated
// Jar: cookies harvested from Set-Cookie headers + cookiejar (in case of
// redirects), optional bearer token extracted via dot-path, and the
// computed expiry. Pure given the inputs — no FS, no print. Tests can
// drive it with an httptest.ResponseRecorder.
func assembleJar(name string, resolved *credential.Resolved, resp *http.Response, jar http.CookieJar, loginURL *url.URL) (*credential.Jar, error) {
	sess := &credential.Jar{Name: name, Acquired: time.Now().UTC()}
	sess.HarvestResponse(resp, loginURL)
	mergeJarCookies(sess, jar, loginURL)

	if resolved.Secrets.TokenPath != "" {
		bodyBytes, _ := readCapped(resp.Body, 2*1024*1024)
		token, err := extractJSONToken(bodyBytes, resolved.Secrets.TokenPath)
		if err != nil {
			return nil, agenterrors.Wrap(err, agenterrors.FixableByHuman).
				WithHint("Check --token-path matches the login response shape")
		}
		if token == "" {
			return nil, agenterrors.Newf(agenterrors.FixableByHuman,
				"login response had no value at --token-path %q", resolved.Secrets.TokenPath)
		}
		sess.Token = token
		sess.TokenHeader = resolved.Secrets.Header
		sess.TokenPrefix = resolved.Secrets.Prefix
	}

	sess.Expires = computeExpiry(sess, resolved.Secrets.SessionTTL)
	return sess, nil
}

// validateLoginURL parses the credential's login-url and confirms it
// falls inside the credential's host/path allowlist. Rejects malformed
// URLs and off-allowlist hosts with fixable_by:human.
func validateLoginURL(resolved *credential.Resolved) (*url.URL, error) {
	s := resolved.Secrets
	if s.LoginURL == "" {
		return nil, agenterrors.New("credential has no login-url", agenterrors.FixableByHuman)
	}
	u, err := url.Parse(s.LoginURL)
	if err != nil || u.Host == "" {
		return nil, agenterrors.Newf(agenterrors.FixableByHuman,
			"login-url %q is malformed", s.LoginURL)
	}
	if !resolved.MatchesURL(u) {
		return nil, agenterrors.Newf(agenterrors.FixableByHuman,
			"login-url host %q not in allowlist for %q", u.Host, resolved.Name).
			WithHint("Widen the credential's --domain to include " + u.Host)
	}
	return u, nil
}

// buildLoginBody serialises the login payload. Returns the bytes and the
// Content-Type. Pure given the Secrets struct — testable in isolation.
func buildLoginBody(s credential.Secrets) (body []byte, contentType string, err error) {
	format := s.LoginFormat
	if format == "" {
		format = "form"
	}
	userField := s.UsernameField
	if userField == "" {
		userField = "username"
	}
	passField := s.PasswordField
	if passField == "" {
		passField = "password"
	}

	switch format {
	case "json":
		payload := map[string]any{userField: s.Username, passField: s.Password}
		for k, v := range s.ExtraFields {
			payload[k] = v
		}
		b, _ := json.Marshal(payload)
		return b, "application/json", nil
	case "form":
		values := url.Values{}
		values.Set(userField, s.Username)
		values.Set(passField, s.Password)
		for k, v := range s.ExtraFields {
			values.Set(k, v)
		}
		return []byte(values.Encode()), "application/x-www-form-urlencoded", nil
	default:
		return nil, "", agenterrors.Newf(agenterrors.FixableByHuman,
			"unknown login-format %q", format).
			WithHint("Must be 'form' or 'json'")
	}
}

// performLoginRequest issues the login POST. The caller owns closing the
// returned response body.
func performLoginRequest(resolved *credential.Resolved, loginURL *url.URL, body []byte, contentType string, jar *cookiejar.Jar) (*http.Response, error) {
	s := resolved.Secrets
	method := s.LoginMethod
	if method == "" {
		method = "POST"
	}
	// No context.WithTimeout here — if we cancelled the context when this
	// function returned, the caller could not read the body. http.Client
	// below has a 30s Timeout that covers the full round-trip including body.
	req, err := http.NewRequestWithContext(context.Background(), method, s.LoginURL, bytes.NewReader(body))
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "agent-deepweb/login")
	for k, v := range resolved.DefaultHeaders {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 30 * time.Second, Jar: jar}
	resp, err := client.Do(req)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByRetry).
			WithHint("Login request failed at transport level; check connectivity")
	}
	return resp, nil
}

// mergeJarCookies folds cookiejar-captured cookies into sess for cookies
// that weren't already harvested from Set-Cookie response headers. This
// catches cookies set by redirects or related subdomains.
func mergeJarCookies(sess *credential.Jar, jar http.CookieJar, loginURL *url.URL) {
	for _, c := range jar.Cookies(loginURL) {
		dup := false
		for _, existing := range sess.Cookies {
			if existing.Name == c.Name && existing.Domain == loginURL.Hostname() {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		sess.Cookies = append(sess.Cookies, credential.PersistedCookie{
			Name:      c.Name,
			Value:     c.Value,
			Domain:    loginURL.Hostname(),
			Path:      "/",
			Sensitive: credential.ClassifyCookie(c),
		})
	}
}

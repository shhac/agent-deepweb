// Package api is the HTTP transport layer. It builds requests from a
// high-level Request struct, attaches auth (via ApplyAuth in auth.go),
// enforces scheme policy (scheme.go), classifies responses and transport
// errors into structured APIErrors (classify.go), and logs each request
// to the audit package on the way out.
//
// File layout:
//
//	client.go   Request, Response, ClientOptions, Do (orchestrator)
//	request.go  buildHTTPRequest, resolveUserAgent, viewPersisted
//	scheme.go   enforceScheme, isLoopback
//	classify.go classifyTransport, classifyHTTP
//	auth.go     ApplyAuth
//	redact.go   Header/body/literal-value redactors
package api

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"

	"github.com/shhac/agent-deepweb/internal/audit"
	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"golang.org/x/net/publicsuffix"
)

// Version is the agent-deepweb build version used in the default User-Agent
// header ("agent-deepweb/<version>"). Set by cmd/agent-deepweb from the
// -ldflags variable.
var Version = "dev"

// Request is a high-level HTTP request description, translated into an
// *http.Request inside Do. The Resolved credential (if any) is applied
// after user headers so user-supplied headers cannot overwrite the auth.
type Request struct {
	Method  string
	URL     string
	Headers map[string]string
	Query   map[string][]string
	Body    io.Reader
	Auth    *credential.Resolved
	// AllowHTTP permits http:// on bearer/basic/cookie/form credentials.
	// Default false — must be opted into per-request (human-only flag).
	AllowHTTP bool
	// UserAgent, if non-empty, overrides everything (credential's UA, env,
	// and the default). Empty = fall through the precedence chain.
	UserAgent string
	// TemplateName is set by `tpl run` so the audit log can record which
	// template produced the request. Empty for ad-hoc fetches.
	TemplateName string
}

// Response is the redacted, size-capped response surfaced to the caller.
type Response struct {
	Status      int
	StatusText  string
	Headers     http.Header
	ContentType string
	Body        []byte
	Truncated   bool
	// NewCookies: cookies captured from Set-Cookie on this response that
	// were *not* already in the session (post-harvest diff). Visible ones
	// have values; sensitive ones are redacted. Empty unless auth is form.
	NewCookies []credential.CookieView `json:"new_cookies,omitempty"`
}

// ClientOptions carry request-level defaults that would otherwise pile up
// as parameters to Do.
type ClientOptions struct {
	Timeout         time.Duration
	MaxBytes        int64
	Redact          bool
	FollowRedirects bool
}

func (c *ClientOptions) applyDefaults() {
	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}
	if c.MaxBytes == 0 {
		c.MaxBytes = 10 * 1024 * 1024
	}
}

// defaultStr returns d when s is empty. Small helper kept here because the
// audit defer in Do is the only caller.
func defaultStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// Do executes the request and returns a redacted, size-capped response.
// Errors are pre-classified as APIError with a fixable_by hint. Every
// completed or failed request is audited via internal/audit.
//
// The top-level shape:
//  1. parse URL, enforce scheme policy
//  2. prime a cookiejar (fresh, or seeded from session for form auth)
//  3. build http.Request (headers, auth, UA)
//  4. execute → read capped body → harvest cookies → redact → classify
//  5. (always) write one audit entry with the outcome
func Do(ctx context.Context, req Request, opts ClientOptions) (resp *Response, outErr error) {
	opts.applyDefaults()
	started := time.Now()

	defer auditOnExit(req, started, &resp, &outErr)()

	parsedURL, err := url.Parse(req.URL)
	if err != nil || parsedURL.Host == "" {
		return nil, agenterrors.Newf(agenterrors.FixableByAgent,
			"URL %q is not absolute", req.URL).
			WithHint("Use scheme://host/path form")
	}

	if err := enforceScheme(parsedURL, req.Auth, req.AllowHTTP); err != nil {
		return nil, err
	}

	jar, err := primeCookieJar(req.Auth, parsedURL)
	if err != nil {
		return nil, err
	}

	httpReq, err := buildHTTPRequest(ctx, req)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}

	client := &http.Client{Timeout: opts.Timeout, Jar: jar}
	if !opts.FollowRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, classifyTransport(err)
	}
	defer httpResp.Body.Close() //nolint:errcheck

	body, truncated, err := readCappedBody(httpResp.Body, opts.MaxBytes)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByRetry)
	}

	newCookieViews := harvestSessionCookies(req.Auth, httpResp, parsedURL)

	contentType := httpResp.Header.Get("Content-Type")
	headers := httpResp.Header
	if opts.Redact {
		headers = RedactHeaders(httpResp.Header)
		body = RedactJSONBody(body, contentType)
		body = RedactSecretEcho(body, req.Auth)
	}

	resp = &Response{
		Status:      httpResp.StatusCode,
		StatusText:  httpResp.Status,
		Headers:     headers,
		ContentType: contentType,
		Body:        body,
		Truncated:   truncated,
		NewCookies:  newCookieViews,
	}

	if httpResp.StatusCode >= 400 {
		outErr = classifyHTTP(httpResp.StatusCode, httpResp.Header, req.Auth)
		return resp, outErr
	}
	if truncated {
		outErr = agenterrors.Newf(agenterrors.FixableByAgent,
			"response body exceeded --max-size (%d bytes)", opts.MaxBytes).
			WithHint("Retry with --max-size <bytes> or narrow the request (query params, pagination)")
		return resp, outErr
	}
	return resp, nil
}

// primeCookieJar returns a cookiejar seeded from the credential's session
// (for form auth) or a fresh one. An expired session short-circuits with
// fixable_by:human before any network call.
func primeCookieJar(auth *credential.Resolved, parsedURL *url.URL) (http.CookieJar, error) {
	if auth != nil && auth.Type == credential.AuthForm {
		sess, err := credential.ReadSession(auth.Name)
		if err == nil {
			if sess.IsExpired() {
				return nil, agenterrors.Newf(agenterrors.FixableByHuman,
					"session for %q is expired", auth.Name).
					WithHint("Ask the user to run 'agent-deepweb login " + auth.Name + "'")
			}
			jar, _ := sess.NewJar(parsedURL)
			if jar != nil {
				return jar, nil
			}
		}
	}
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	return jar, nil
}

// readCappedBody reads at most maxBytes from r. Returns truncated=true when
// the underlying stream had more data than we kept.
func readCappedBody(r io.Reader, maxBytes int64) ([]byte, bool, error) {
	limited := io.LimitReader(r, maxBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, err
	}
	if int64(len(body)) > maxBytes {
		return body[:maxBytes], true, nil
	}
	return body, false, nil
}

// harvestSessionCookies captures Set-Cookie headers from a form-auth
// response into the stored session, preserving per-cookie sensitivity,
// and returns a list of newly-captured cookie views for the envelope.
func harvestSessionCookies(auth *credential.Resolved, httpResp *http.Response, parsedURL *url.URL) []credential.CookieView {
	if auth == nil || auth.Type != credential.AuthForm {
		return nil
	}
	if len(httpResp.Cookies()) == 0 {
		return nil
	}
	sess, err := credential.ReadSession(auth.Name)
	if err != nil {
		return nil
	}
	before := make(map[string]struct{}, len(sess.Cookies))
	for _, c := range sess.Cookies {
		before[c.Name+"\x00"+c.Domain+"\x00"+c.Path] = struct{}{}
	}
	if changed := sess.HarvestResponse(httpResp, parsedURL); changed {
		_ = credential.WriteSession(sess)
	}
	var added []credential.CookieView
	for _, c := range sess.Cookies {
		if _, had := before[c.Name+"\x00"+c.Domain+"\x00"+c.Path]; !had {
			added = append(added, viewPersisted(c))
		}
	}
	return added
}

// auditOnExit returns a deferred closure that writes one audit entry with
// the outcome of Do. Pulling this out of Do's body keeps the main flow
// readable.
func auditOnExit(req Request, started time.Time, resp **Response, outErr *error) func() {
	return func() {
		scheme, host, path := audit.FromURL(req.URL)
		entry := audit.Entry{
			Method:     defaultStr(req.Method, "GET"),
			Scheme:     scheme,
			Host:       host,
			Path:       path,
			Template:   req.TemplateName,
			AgentMode:  isAgentMode(),
			DurationMS: time.Since(started).Milliseconds(),
		}
		if req.Auth != nil {
			entry.Credential = req.Auth.Name
		}
		if resp != nil && *resp != nil {
			entry.Status = (*resp).Status
			entry.Bytes = len((*resp).Body)
		}
		if *outErr == nil {
			entry.Outcome = "ok"
		} else {
			entry.Outcome = "error"
			entry.Error = (*outErr).Error()
			var ae *agenterrors.APIError
			if agenterrors.As(*outErr, &ae) {
				entry.FixableBy = string(ae.FixableBy)
			}
		}
		audit.Append(entry)
	}
}

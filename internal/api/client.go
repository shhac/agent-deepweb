// Package api is the HTTP transport layer. It builds requests from a
// high-level Request struct, attaches auth (via ApplyAuth in auth.go),
// enforces scheme policy (scheme.go), classifies responses and transport
// errors into structured APIErrors (classify.go), and logs each request
// to the audit package on the way out.
//
// File layout:
//
//	client.go   Request, Response, ClientOptions, Do (orchestrator)
//	jar.go      primeCookieJar / harvestJarCookies + helpers
//	request.go  buildHTTPRequest, resolveUserAgent, viewPersisted
//	scheme.go   enforceScheme, isLoopback
//	classify.go classifyTransport, classifyHTTP
//	auth.go     ApplyAuth
//	redact.go   Header/body/literal-value redactors
package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/shhac/agent-deepweb/internal/audit"
	"github.com/shhac/agent-deepweb/internal/config"
	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
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
	// JarPath, if set, overrides the profile's encrypted default jar with
	// a plaintext bring-your-own jar at this path. Used by `--cookiejar
	// <path>`, including the `--profile none --cookiejar <path>` LLM-
	// authored-flow case.
	JarPath string
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
	// were *not* already in the profile's jar (post-harvest diff). Visible
	// ones have values; sensitive ones are redacted. Empty when no profile
	// is attached.
	NewCookies []credential.CookieView `json:"new_cookies,omitempty"`
}

// ClientOptions carry request-level defaults that would otherwise pile up
// as parameters to Do. Redaction is always on — there's no "no-redact"
// escape hatch in v2; if a human really wants raw output they can use curl.
type ClientOptions struct {
	Timeout         time.Duration
	MaxBytes        int64
	FollowRedirects bool
}

func (c *ClientOptions) applyDefaults() {
	if c.Timeout == 0 {
		c.Timeout = time.Duration(config.DefaultTimeoutMS) * time.Millisecond
	}
	if c.MaxBytes == 0 {
		c.MaxBytes = config.DefaultMaxBytes
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

	// Named returns let the deferred audit see the final resp/outErr
	// without any manual `outErr = err` bookkeeping at every error site.
	defer func() { audit.Append(buildAuditEntry(req, started, resp, outErr)) }()

	parsedURL, err := url.Parse(req.URL)
	if err != nil || parsedURL.Host == "" {
		return nil, agenterrors.Newf(agenterrors.FixableByAgent,
			"URL %q is not absolute", req.URL).
			WithHint("Use scheme://host/path form")
	}

	if err := enforceScheme(parsedURL, req.Auth); err != nil {
		return nil, err
	}

	jar, err := primeCookieJar(req.Auth, req.JarPath, parsedURL)
	if err != nil {
		return nil, err
	}

	httpReq, err := buildHTTPRequest(ctx, req)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}

	client := &http.Client{
		Timeout:       opts.Timeout,
		Jar:           jar,
		CheckRedirect: buildRedirectPolicy(req.Auth, opts.FollowRedirects),
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

	newCookieViews := harvestJarCookies(req.Auth, req.JarPath, httpResp, parsedURL)

	contentType := httpResp.Header.Get("Content-Type")
	// Redaction is mandatory in v2 — the layered redactors run on every
	// response, no opt-out flag. If a human really needs raw output, they
	// can use curl.
	headers := RedactHeaders(httpResp.Header)
	body = RedactJSONBody(body, contentType)
	body = RedactSecretEcho(body, req.Auth)

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
		return resp, classifyHTTP(httpResp.StatusCode, httpResp.Header, req.Auth)
	}
	if truncated {
		return resp, agenterrors.Newf(agenterrors.FixableByAgent,
			"response body exceeded --max-size (%d bytes)", opts.MaxBytes).
			WithHint("Retry with --max-size <bytes> or narrow the request (query params, pagination)")
	}
	return resp, nil
}

// buildRedirectPolicy returns the CheckRedirect function for the HTTP
// client:
//
//  1. If the caller disabled redirects, hand back the first response.
//  2. If a credential is attached, refuse redirects that leave the
//     credential's URL allowlist. Go's default policy already strips the
//     Authorization header on cross-host redirects, but it still *follows*
//     them — which would let an upstream (or a compromised upstream) bounce
//     us to an arbitrary host, turning agent-deepweb into an outbound hop
//     around the harness's sandbox.
//  3. No credential, redirects allowed: fall back to stdlib default
//     (max 10 redirects).
func buildRedirectPolicy(auth *credential.Resolved, follow bool) func(*http.Request, []*http.Request) error {
	if !follow {
		return func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	if auth == nil {
		return nil // stdlib default — 10-redirect cap
	}
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		if !auth.MatchesURL(req.URL) {
			return agenterrors.Newf(agenterrors.FixableByHuman,
				"refusing redirect to %s — outside allowlist for %q", req.URL.Host, auth.Name).
				WithHint("Ask the user to run 'agent-deepweb profile allow " + auth.Name + " " + req.URL.Host + " " + credential.PrimarySecretFlagHint(auth.Type) + "'.")
		}
		return nil
	}
}

// buildAuditEntry converts a Do invocation's inputs + outcome into an
// audit.Entry. Pure function — all state comes from its args, making the
// deferred call site in Do trivial to read.
func buildAuditEntry(req Request, started time.Time, resp *Response, outErr error) audit.Entry {
	scheme, host, path := audit.FromURL(req.URL)
	e := audit.Entry{
		Method:     defaultStr(req.Method, "GET"),
		Scheme:     scheme,
		Host:       host,
		Path:       path,
		Jar:        req.JarPath,
		Template:   req.TemplateName,
		DurationMS: time.Since(started).Milliseconds(),
	}
	if req.Auth != nil {
		e.Profile = req.Auth.Name
	} else {
		// req.Auth == nil means explicit anonymous (`--profile none`) at
		// this layer — Resolve already errored out for the no-match case.
		e.Profile = "none"
	}
	if resp != nil {
		e.Status = resp.Status
		e.Bytes = len(resp.Body)
	}
	if outErr == nil {
		e.Outcome = "ok"
	} else {
		e.Outcome = "error"
		e.Error = outErr.Error()
		var ae *agenterrors.APIError
		if agenterrors.As(outErr, &ae) {
			e.FixableBy = string(ae.FixableBy)
		}
	}
	return e
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

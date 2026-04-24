// Package api is the HTTP transport layer. It builds requests from a
// high-level Request struct, attaches auth (via ApplyAuth in auth.go),
// enforces scheme policy (scheme.go), classifies responses and transport
// errors into structured APIErrors (classify.go), and logs each request
// to the audit package on the way out.
//
// File layout:
//
//	types.go    Request, Response, SentRequest, ClientOptions
//	client.go   Do (orchestrator) + extracted pipeline helpers
//	record.go   buildAuditEntry, buildTrackRecord, writeTrackRecord (pure)
//	jar.go      primeCookieJar / harvestJarCookies
//	request.go  buildHTTPRequest, resolveUserAgent, viewPersisted
//	scheme.go   enforceScheme, isLoopback
//	classify.go classifyTransport, classifyHTTP
//	auth.go     ApplyAuth
//	redact.go   Header / JSON body / literal-byte echo redactors
package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/shhac/agent-deepweb/internal/audit"
	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

// Do executes the request and returns a redacted, size-capped response.
// Errors are pre-classified as APIError with a fixable_by hint. Every
// completed or failed request is audited via internal/audit.
//
// The top-level shape:
//  1. parse URL, buffer request body, enforce scheme, prime cookiejar
//  2. build http.Request (headers, auth, UA), construct client
//  3. execute → read capped response body → harvest cookies → redact
//  4. assemble Response (including the redacted SentRequest view)
//  5. optionally persist a full-fidelity track record
//  6. classify HTTP-level errors and truncation
//  7. (always, via deferred) write one audit entry with the outcome
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

	sentBody, err := bufferRequestBody(&req, opts.MaxBytes)
	if err != nil {
		return nil, err
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
	headers, body := redactResponse(httpResp.Header, body, req.Auth)
	sent := buildSentRequest(httpReq, sentBody, req.Auth)

	resp = &Response{
		Status:      httpResp.StatusCode,
		StatusText:  httpResp.Status,
		Headers:     headers,
		ContentType: httpResp.Header.Get("Content-Type"),
		Body:        body,
		Truncated:   truncated,
		NewCookies:  newCookieViews,
		Sent:        sent,
	}
	if req.Track {
		resp.AuditID = writeTrackRecord(req, resp, started)
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

// bufferRequestBody reads req.Body into a cap-bounded byte slice and
// replaces req.Body with a bytes.Reader so the buffered copy can be
// both sent to the server AND recorded in the envelope/track. Returns
// a nil slice unchanged when req.Body is nil (GET, etc.).
func bufferRequestBody(req *Request, maxBytes int64) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}
	buf, _, err := readCappedBody(req.Body, maxBytes)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}
	// Over-cap on the outbound side means the body itself wouldn't fit
	// under --max-size, which is a caller bug; we surface truncation via
	// Sent.BodyBytes in the envelope rather than erroring here.
	req.Body = bytes.NewReader(buf)
	return buf, nil
}

// redactResponse runs the three-layer redaction pipeline on the
// response headers + body. Extracted so request-side redaction
// (buildSentRequest) can pair visually with it and so tests can
// exercise the pipeline without httptest.
func redactResponse(rawHeaders http.Header, body []byte, auth *credential.Resolved) (http.Header, []byte) {
	contentType := rawHeaders.Get("Content-Type")
	headers := RedactHeaders(rawHeaders, auth)
	body = RedactJSONBody(body, contentType)
	body = RedactSecretEcho(body, auth)
	return headers, body
}

// buildSentRequest produces the post-redaction view of what went out
// on the wire. Pure given (httpReq, sentBody, auth) — doesn't touch
// network or FS. Mirrors redactResponse so the two sides share the
// same masking rules (headerRedactPattern + body-field patterns +
// literal-byte echo).
func buildSentRequest(httpReq *http.Request, sentBody []byte, auth *credential.Resolved) SentRequest {
	reqCT := httpReq.Header.Get("Content-Type")
	// Copy the body before redacting so the original slice (held by the
	// http.Client's body reader) stays intact.
	redactedBody := RedactJSONBody(append([]byte(nil), sentBody...), reqCT)
	redactedBody = RedactSecretEcho(redactedBody, auth)
	return SentRequest{
		Method:    httpReq.Method,
		URL:       httpReq.URL.String(),
		Headers:   RedactHeaders(httpReq.Header, auth),
		Body:      redactedBody,
		BodyBytes: len(sentBody),
		RequestCT: reqCT,
	}
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

// readCappedBody reads at most maxBytes from r. Returns truncated=true
// when the underlying stream had more data than we kept.
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

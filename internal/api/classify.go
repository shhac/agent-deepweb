package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

// classifyTransport converts a Go transport error into an APIError with
// the right fixable_by. Timeouts → retry with an explicit timeout hint;
// DNS/dial errors → retry with a connectivity hint.
func classifyTransport(err error) *agenterrors.APIError {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "context deadline exceeded"):
		return agenterrors.New("request timed out", agenterrors.FixableByRetry).
			WithHint("Increase --timeout or narrow the request and retry")
	case strings.Contains(msg, "no such host"),
		strings.Contains(msg, "dial tcp"),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "connection reset"):
		return agenterrors.New("network error: "+msg, agenterrors.FixableByRetry).
			WithHint("Check connectivity / DNS and retry")
	default:
		return agenterrors.New("transport error: "+msg, agenterrors.FixableByRetry)
	}
}

// classifyHTTP maps a response status to an APIError with fixable_by:
//
//	401/403 → human (form auth hints at re-login; others hint at verify)
//	404     → agent (typo)
//	429     → retry (respecting Retry-After)
//	5xx     → retry
//	other 4xx → agent
func classifyHTTP(status int, header http.Header, auth *credential.Resolved) *agenterrors.APIError {
	authName := ""
	if auth != nil {
		authName = auth.Name
	}
	switch {
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		if auth != nil && auth.Type == credential.AuthForm {
			return agenterrors.Newf(agenterrors.FixableByHuman, "HTTP %d: auth failed", status).
				WithHint(fmt.Sprintf("Session for %q may be expired. Ask the user to run 'agent-deepweb login %s'", authName, authName))
		}
		return agenterrors.Newf(agenterrors.FixableByHuman, "HTTP %d: auth failed", status).
			WithHint("Credentials rejected by upstream. Ask the user to verify or rotate the stored credential.")
	case status == http.StatusNotFound:
		return agenterrors.New("HTTP 404: not found", agenterrors.FixableByAgent).
			WithHint("Check the URL path and query params")
	case status == http.StatusTooManyRequests:
		hint := "Rate limited — retry after backoff"
		if ra := header.Get("Retry-After"); ra != "" {
			hint = "Rate limited — server says Retry-After: " + ra
		}
		return agenterrors.New("HTTP 429: rate limited", agenterrors.FixableByRetry).WithHint(hint)
	case status >= 500:
		return agenterrors.Newf(agenterrors.FixableByRetry, "HTTP %d: upstream error", status).
			WithHint("Upstream server error — retry in a few seconds")
	case status >= 400:
		return agenterrors.Newf(agenterrors.FixableByAgent, "HTTP %d: client error", status).
			WithHint("Check the request shape (method, headers, body)")
	}
	return nil
}

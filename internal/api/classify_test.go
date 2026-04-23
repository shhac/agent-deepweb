package api

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

func TestClassifyTransport(t *testing.T) {
	cases := []struct {
		name    string
		errText string
		want    agenterrors.FixableBy
		hint    string
	}{
		{"deadline", "context deadline exceeded", agenterrors.FixableByRetry, "Increase --timeout"},
		{"dns", "dial tcp: no such host", agenterrors.FixableByRetry, "connectivity"},
		{"refused", "dial tcp 127.0.0.1:9: connection refused", agenterrors.FixableByRetry, "connectivity"},
		{"reset", "read tcp: connection reset by peer", agenterrors.FixableByRetry, "connectivity"},
		{"other", "unknown TLS glitch", agenterrors.FixableByRetry, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ae := classifyTransport(errors.New(tc.errText))
			if ae.FixableBy != tc.want {
				t.Errorf("FixableBy=%s, want %s", ae.FixableBy, tc.want)
			}
			if tc.hint != "" && !strings.Contains(ae.Hint, tc.hint) {
				t.Errorf("hint %q missing %q", ae.Hint, tc.hint)
			}
		})
	}
}

func TestClassifyHTTP(t *testing.T) {
	bearer := &credential.Resolved{Credential: credential.Credential{Name: "c", Type: credential.AuthBearer}}
	form := &credential.Resolved{Credential: credential.Credential{Name: "f", Type: credential.AuthForm}}

	cases := []struct {
		name   string
		status int
		auth   *credential.Resolved
		header http.Header
		want   agenterrors.FixableBy
		hint   string
	}{
		{"401 bearer", 401, bearer, nil, agenterrors.FixableByHuman, "verify or rotate"},
		{"401 form", 401, form, nil, agenterrors.FixableByHuman, "login f"},
		{"403 bearer", 403, bearer, nil, agenterrors.FixableByHuman, ""},
		{"404", 404, bearer, nil, agenterrors.FixableByAgent, "URL path"},
		{"429 no retry-after", 429, bearer, http.Header{}, agenterrors.FixableByRetry, "backoff"},
		{"429 with retry-after", 429, bearer, http.Header{"Retry-After": []string{"5"}}, agenterrors.FixableByRetry, "Retry-After: 5"},
		{"500", 500, bearer, nil, agenterrors.FixableByRetry, "Upstream"},
		{"502", 502, bearer, nil, agenterrors.FixableByRetry, ""},
		{"418 other 4xx", 418, bearer, nil, agenterrors.FixableByAgent, "request shape"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ae := classifyHTTP(tc.status, tc.header, tc.auth)
			if ae == nil {
				t.Fatalf("want error for status %d, got nil", tc.status)
			}
			if ae.FixableBy != tc.want {
				t.Errorf("FixableBy=%s, want %s", ae.FixableBy, tc.want)
			}
			if tc.hint != "" && !strings.Contains(ae.Hint, tc.hint) {
				t.Errorf("hint %q missing %q", ae.Hint, tc.hint)
			}
		})
	}
	// Sanity: non-error statuses classify to nil.
	if got := classifyHTTP(200, nil, bearer); got != nil {
		t.Errorf("200 should classify nil, got %v", got)
	}
	if got := classifyHTTP(302, nil, bearer); got != nil {
		t.Errorf("302 should classify nil, got %v", got)
	}
}

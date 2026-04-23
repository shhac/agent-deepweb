package api

import (
	"net/url"
	"strings"
	"testing"

	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

func TestEnforceScheme(t *testing.T) {
	bearer := &credential.Resolved{Credential: credential.Credential{Name: "c", Type: credential.AuthBearer}}
	bearerHTTP := &credential.Resolved{Credential: credential.Credential{Name: "c2", Type: credential.AuthBearer, AllowHTTP: true}}

	cases := []struct {
		name       string
		rawURL     string
		auth       *credential.Resolved
		perReqOK   bool
		wantErr    bool
		hintHasStr string
	}{
		{"https passes", "https://api.example.com/", bearer, false, false, ""},
		{"unknown scheme (ws) passes", "ws://api.example.com/", bearer, false, false, ""},
		{"http to public host blocked", "http://api.example.com/", bearer, false, true, "set-allow-http"},
		{"http to public host with per-request allow", "http://api.example.com/", bearer, true, false, ""},
		{"http to public host with credential allow", "http://api.example.com/", bearerHTTP, false, false, ""},
		{"http to localhost passes", "http://localhost:8080/", bearer, false, false, ""},
		{"http to 127.0.0.1 passes", "http://127.0.0.1:8080/", bearer, false, false, ""},
		{"http to ipv6 loopback passes", "http://[::1]:8080/", bearer, false, false, ""},
		{"http to foo.localhost passes", "http://foo.localhost/", bearer, false, false, ""},
		{"no auth → no check", "http://evil.example.com/", nil, false, false, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := url.Parse(tc.rawURL)
			if err != nil {
				t.Fatalf("bad test URL: %v", err)
			}
			gotErr := enforceScheme(u, tc.auth, tc.perReqOK)
			if tc.wantErr && gotErr == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && gotErr != nil {
				t.Fatalf("unexpected error: %v", gotErr)
			}
			if tc.hintHasStr != "" && gotErr != nil {
				var ae *agenterrors.APIError
				if !agenterrors.As(gotErr, &ae) {
					t.Fatalf("error is not *APIError: %T", gotErr)
				}
				joined := ae.Error() + "|" + ae.Hint
				if !strings.Contains(joined, tc.hintHasStr) {
					t.Errorf("%q missing substring %q", joined, tc.hintHasStr)
				}
			}
		})
	}
}

func TestIsLoopback(t *testing.T) {
	yes := []string{"localhost", "LOCALHOST", "127.0.0.1", "::1", "api.localhost", "foo.bar.localhost"}
	no := []string{"example.com", "api.example.com", "192.168.1.1", "10.0.0.1", "localhostx", ""}
	for _, h := range yes {
		if !isLoopback(h) {
			t.Errorf("isLoopback(%q) = false, want true", h)
		}
	}
	for _, h := range no {
		if isLoopback(h) {
			t.Errorf("isLoopback(%q) = true, want false", h)
		}
	}
}

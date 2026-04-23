package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

func TestBuildRedirectPolicy_NoFollow(t *testing.T) {
	policy := buildRedirectPolicy(nil, false)
	if policy == nil {
		t.Fatal("policy should be non-nil for no-follow")
	}
	req, _ := http.NewRequest("GET", "https://example.com/", nil)
	if err := policy(req, nil); err != http.ErrUseLastResponse {
		t.Errorf("want ErrUseLastResponse, got %v", err)
	}
}

func TestBuildRedirectPolicy_NoAuth_DefaultApplies(t *testing.T) {
	// follow=true, auth=nil → use stdlib default. We return nil to mean
	// "stdlib default."
	if policy := buildRedirectPolicy(nil, true); policy != nil {
		t.Errorf("policy should be nil for no-auth follow mode (use stdlib default), got non-nil")
	}
}

func TestBuildRedirectPolicy_AuthRefusesOffAllowlist(t *testing.T) {
	auth := &credential.Resolved{
		Credential: credential.Credential{
			Name:    "c",
			Domains: []string{"api.example.com"},
		},
	}
	policy := buildRedirectPolicy(auth, true)

	// In-allowlist redirect → allowed
	u, _ := url.Parse("https://api.example.com/redirected")
	in := &http.Request{URL: u}
	if err := policy(in, nil); err != nil {
		t.Errorf("in-allowlist redirect refused: %v", err)
	}

	// Off-allowlist redirect → refused with fixable_by:human
	u2, _ := url.Parse("https://evil.example.com/leak")
	out := &http.Request{URL: u2}
	err := policy(out, nil)
	if err == nil {
		t.Fatal("expected refusal for off-allowlist redirect")
	}
	var ae *agenterrors.APIError
	if !agenterrors.As(err, &ae) {
		t.Fatalf("want APIError, got %T", err)
	}
	if ae.FixableBy != agenterrors.FixableByHuman {
		t.Errorf("want human, got %s", ae.FixableBy)
	}
	if !strings.Contains(ae.Error(), "outside allowlist") {
		t.Errorf("error wording: %v", ae)
	}

	// Too-many-redirects guard fires even for in-allowlist
	via := make([]*http.Request, 10)
	err = policy(in, via)
	if err == nil || !strings.Contains(err.Error(), "10 redirects") {
		t.Errorf("expected too-many-redirects error, got %v", err)
	}
}

// TestDo_RefusesOffAllowlistRedirect end-to-end: httptest server redirects
// cross-host, agent-deepweb follows — but the redirect policy refuses.
func TestDo_RefusesOffAllowlistRedirect(t *testing.T) {
	setup(t)
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example.com/collect", http.StatusFound)
	}))
	defer redirector.Close()

	// Build a credential scoped to ONLY the redirector's host, so the
	// redirect target (evil.example.com) is off-allowlist.
	u, _ := url.Parse(redirector.URL)
	resolved := &credential.Resolved{
		Credential: credential.Credential{
			Name:      "c",
			Type:      credential.AuthBearer,
			Domains:   []string{u.Host},
			AllowHTTP: true,
		},
		Secrets: credential.Secrets{Token: "abc-long-token-xyz"},
	}

	_, err := Do(t.Context(), Request{URL: redirector.URL, Auth: resolved},
		ClientOptions{MaxBytes: 1024, FollowRedirects: true})
	if err == nil {
		t.Fatal("expected redirect refusal")
	}
	if !strings.Contains(err.Error(), "evil.example.com") && !strings.Contains(err.Error(), "outside allowlist") {
		t.Errorf("wrong error: %v", err)
	}
}

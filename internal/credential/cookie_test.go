package credential

import (
	"net/http"
	"testing"
)

func TestClassifyCookie(t *testing.T) {
	cases := []struct {
		name      string
		cookie    *http.Cookie
		sensitive bool
	}{
		// HttpOnly is always sensitive.
		{"httponly with banal name", &http.Cookie{Name: "anything", HttpOnly: true}, true},

		// Name-pattern hits.
		{"session", &http.Cookie{Name: "session"}, true},
		{"JSESSIONID", &http.Cookie{Name: "JSESSIONID"}, true},
		{"PHPSESSID", &http.Cookie{Name: "PHPSESSID"}, true},
		{"asp.net_sessionid", &http.Cookie{Name: "ASP.NET_SessionId"}, true},
		{"connect.sid", &http.Cookie{Name: "connect.sid"}, true},
		{"auth_token", &http.Cookie{Name: "auth_token"}, true},
		{"csrf", &http.Cookie{Name: "csrf_token"}, true},
		{"xsrf", &http.Cookie{Name: "xsrf"}, true},
		{"access in name", &http.Cookie{Name: "access_token_v2"}, true},
		{"refresh in name", &http.Cookie{Name: "refresh_cookie"}, true},
		{"remember_me", &http.Cookie{Name: "remember_me"}, true},
		{"bearer in name", &http.Cookie{Name: "bearer_token_v1"}, true},

		// Name-pattern misses → visible.
		{"theme", &http.Cookie{Name: "theme"}, false},
		{"locale", &http.Cookie{Name: "locale"}, false},
		{"last_visited", &http.Cookie{Name: "last_visited"}, false},
		{"analytics _ga", &http.Cookie{Name: "_ga"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyCookie(tc.cookie); got != tc.sensitive {
				t.Fatalf("ClassifyCookie(%q HttpOnly=%v) = %v, want %v",
					tc.cookie.Name, tc.cookie.HttpOnly, got, tc.sensitive)
			}
		})
	}
}

func TestFromHTTP_ClassifiesAndPreservesFields(t *testing.T) {
	c := &http.Cookie{
		Name:     "session",
		Value:    "secret-value",
		Domain:   "example.com",
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
	pc := FromHTTP(c)
	if !pc.Sensitive {
		t.Error("expected sensitive=true for HttpOnly cookie named 'session'")
	}
	if pc.Name != "session" || pc.Value != "secret-value" || pc.Domain != "example.com" {
		t.Errorf("fields not preserved: %+v", pc)
	}
	if !pc.Secure || !pc.HttpOnly || pc.SameSite != http.SameSiteStrictMode {
		t.Errorf("flags not preserved: %+v", pc)
	}
}

func TestSessionMarkCookieSensitivity(t *testing.T) {
	s := &Jar{
		Name: "t",
		Cookies: []PersistedCookie{
			{Name: "session", Sensitive: true},
			{Name: "theme", Sensitive: false},
		},
	}
	// Override session → visible.
	if !s.MarkCookieSensitivity("session", false) {
		t.Fatal("expected hit for 'session'")
	}
	if s.Cookies[0].Sensitive {
		t.Error("session should now be visible")
	}
	// Override theme → sensitive.
	if !s.MarkCookieSensitivity("theme", true) {
		t.Fatal("expected hit for 'theme'")
	}
	if !s.Cookies[1].Sensitive {
		t.Error("theme should now be sensitive")
	}
	// Unknown cookie.
	if s.MarkCookieSensitivity("ghost", true) {
		t.Error("expected miss for 'ghost'")
	}
}

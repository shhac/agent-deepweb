package credential

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestHarvestResponse_PreservesSensitive(t *testing.T) {
	// Pre-seed session with a human-overridden sensitive cookie whose
	// name wouldn't trip the classifier by itself ("theme" is visible
	// by default). A refresh of that cookie from the server must NOT
	// flip it back to visible.
	s := &Jar{
		Name: "t",
		Cookies: []PersistedCookie{
			{Name: "theme", Value: "old", Domain: "example.com", Path: "/", Sensitive: true},
		},
	}
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Add("Set-Cookie", "theme=new; Path=/")

	u, _ := url.Parse("https://example.com/")
	if !s.HarvestResponse(resp, u) {
		t.Fatal("expected HarvestResponse to report changes")
	}
	if len(s.Cookies) != 1 {
		t.Fatalf("want 1 cookie, got %d", len(s.Cookies))
	}
	c := s.Cookies[0]
	if c.Value != "new" {
		t.Errorf("value not updated: %q", c.Value)
	}
	if !c.Sensitive {
		t.Errorf("Sensitive flipped to false — human override lost!")
	}
}

func TestHarvestResponse_NewCookieClassified(t *testing.T) {
	s := &Jar{Name: "t"}
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Add("Set-Cookie", "session=abc; Path=/; HttpOnly")
	resp.Header.Add("Set-Cookie", "theme=dark; Path=/")

	u, _ := url.Parse("https://example.com/")
	if !s.HarvestResponse(resp, u) {
		t.Fatal("expected changes")
	}
	byName := map[string]PersistedCookie{}
	for _, c := range s.Cookies {
		byName[c.Name] = c
	}
	if !byName["session"].Sensitive {
		t.Errorf("session should be sensitive (HttpOnly)")
	}
	if byName["theme"].Sensitive {
		t.Errorf("theme should be visible")
	}
}

func TestNewJar_FiltersExpired(t *testing.T) {
	now := time.Now()
	s := &Jar{
		Cookies: []PersistedCookie{
			{Name: "alive", Value: "v1", Domain: "example.com", Path: "/", Expires: now.Add(1 * time.Hour)},
			{Name: "dead", Value: "v2", Domain: "example.com", Path: "/", Expires: now.Add(-1 * time.Hour)},
		},
	}
	u, _ := url.Parse("https://example.com/")
	jar, err := s.NewCookieJar(u)
	if err != nil {
		t.Fatal(err)
	}
	got := jar.Cookies(u)
	if len(got) != 1 || got[0].Name != "alive" {
		t.Errorf("expected only the live cookie, got %v", got)
	}
}

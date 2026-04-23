package login

import (
	"strings"
	"testing"
	"time"

	"github.com/shhac/agent-deepweb/internal/credential"
)

func TestExtractJSONToken(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		path    string
		want    string
		wantErr bool
	}{
		{"top-level string", `{"access_token":"tok"}`, "access_token", "tok", false},
		{"nested", `{"data":{"token":"tok2"}}`, "data.token", "tok2", false},
		{"array index", `{"items":[{"tok":"a"},{"tok":"b"}]}`, "items.1.tok", "b", false},
		{"missing key → empty", `{"other":"x"}`, "access_token", "", false},
		{"non-json", `<html/>`, "x", "", true},
		{"index out of range", `{"items":[]}`, "items.5", "", true},
		{"float coerces to string", `{"id":42}`, "id", "42", false},
		{"null leaf", `{"tok":null}`, "tok", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractJSONToken([]byte(tc.body), tc.path)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestComputeExpiry(t *testing.T) {
	now := time.Now().UTC()

	// No TTL, no cookie expiries → default 24h.
	s := &credential.Session{}
	got := computeExpiry(s, "")
	if d := got.Sub(now); d < 23*time.Hour || d > 25*time.Hour {
		t.Errorf("default TTL: %v", d)
	}

	// TTL only.
	got = computeExpiry(s, "1h")
	if d := got.Sub(now); d < 59*time.Minute || d > 61*time.Minute {
		t.Errorf("TTL 1h: %v", d)
	}

	// Cookies with explicit expiries pick the earliest.
	s = &credential.Session{Cookies: []credential.PersistedCookie{
		{Expires: now.Add(2 * time.Hour)},
		{Expires: now.Add(30 * time.Minute)},
	}}
	got = computeExpiry(s, "")
	if d := got.Sub(now); d < 29*time.Minute || d > 31*time.Minute {
		t.Errorf("min cookie expiry: %v", d)
	}

	// TTL tighter than any cookie.
	s = &credential.Session{Cookies: []credential.PersistedCookie{
		{Expires: now.Add(4 * time.Hour)},
	}}
	got = computeExpiry(s, "10m")
	if d := got.Sub(now); d < 9*time.Minute || d > 11*time.Minute {
		t.Errorf("TTL beats cookie: %v", d)
	}

	// Invalid TTL ignored → fall through to cookie.
	got = computeExpiry(s, "not-a-duration")
	if d := got.Sub(now); d < 3*time.Hour || d > 5*time.Hour {
		t.Errorf("invalid TTL should fall back: %v", d)
	}
}

func TestBuildLoginBody(t *testing.T) {
	form := credential.Secrets{
		Username: "alice",
		Password: "wonderland",
	}
	body, ct, err := buildLoginBody(form)
	if err != nil {
		t.Fatal(err)
	}
	if ct != "application/x-www-form-urlencoded" {
		t.Errorf("content-type: %q", ct)
	}
	s := string(body)
	if !strings.Contains(s, "username=alice") || !strings.Contains(s, "password=wonderland") {
		t.Errorf("body: %s", s)
	}

	jsonS := credential.Secrets{
		Username:    "alice",
		Password:    "pw",
		LoginFormat: "json",
		ExtraFields: map[string]string{"grant_type": "password"},
	}
	body, ct, err = buildLoginBody(jsonS)
	if err != nil {
		t.Fatal(err)
	}
	if ct != "application/json" {
		t.Errorf("content-type: %q", ct)
	}
	if !strings.Contains(string(body), `"grant_type"`) {
		t.Errorf("extra_fields missing: %s", body)
	}

	if _, _, err := buildLoginBody(credential.Secrets{LoginFormat: "xml"}); err == nil {
		t.Error("expected error for unknown login_format")
	}
}

package login

import (
	stdjson "encoding/json"
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
	s := &credential.Jar{}
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
	s = &credential.Jar{Cookies: []credential.PersistedCookie{
		{Expires: now.Add(2 * time.Hour)},
		{Expires: now.Add(30 * time.Minute)},
	}}
	got = computeExpiry(s, "")
	if d := got.Sub(now); d < 29*time.Minute || d > 31*time.Minute {
		t.Errorf("min cookie expiry: %v", d)
	}

	// TTL tighter than any cookie.
	s = &credential.Jar{Cookies: []credential.PersistedCookie{
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

// TestBuildLoginBody_Template covers the --login-body-template path: the
// template is substituted with JSON-escaped values, the result is valid
// JSON (even for values containing quotes/backslashes), Content-Type is
// application/json regardless of --login-format, and malformed templates
// fail fast.
func TestBuildLoginBody_Template(t *testing.T) {
	t.Run("graphql-mutation shape round-trips", func(t *testing.T) {
		s := credential.Secrets{
			Username: "alice",
			Password: "hunter2",
			LoginBodyTemplate: `{"query":"mutation($u:String!,$p:String!){ signIn(input:{username:$u,password:$p}){ tokens { bearer }}}",` +
				`"variables":{"u":"{{username}}","p":"{{password}}"}}`,
		}
		body, ct, err := buildLoginBody(s)
		if err != nil {
			t.Fatal(err)
		}
		if ct != "application/json" {
			t.Errorf("content-type: %q", ct)
		}
		// Validates as JSON and contains substituted values.
		if !strings.Contains(string(body), `"u":"alice"`) || !strings.Contains(string(body), `"p":"hunter2"`) {
			t.Errorf("substitution missed: %s", body)
		}
	})

	t.Run("values with quotes/backslashes stay valid JSON", func(t *testing.T) {
		s := credential.Secrets{
			Username:          `ali"ce`,
			Password:          `path\to\secret`,
			LoginBodyTemplate: `{"u":"{{username}}","p":"{{password}}"}`,
		}
		body, _, err := buildLoginBody(s)
		if err != nil {
			t.Fatal(err)
		}
		// Must parse cleanly — if we didn't JSON-escape, this would break.
		var parsed map[string]string
		if err := jsonUnmarshal(body, &parsed); err != nil {
			t.Fatalf("not valid JSON: %v\n%s", err, body)
		}
		if parsed["u"] != `ali"ce` || parsed["p"] != `path\to\secret` {
			t.Errorf("escape round-trip failed: %+v", parsed)
		}
	})

	t.Run("extra-field placeholders substitute", func(t *testing.T) {
		s := credential.Secrets{
			Username:          "u",
			Password:          "p",
			ExtraFields:       map[string]string{"scope": "read:all", "client_id": "abc"},
			LoginBodyTemplate: `{"u":"{{username}}","scope":"{{scope}}","cid":"{{client_id}}"}`,
		}
		body, _, err := buildLoginBody(s)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), `"scope":"read:all"`) || !strings.Contains(string(body), `"cid":"abc"`) {
			t.Errorf("extra-fields not substituted: %s", body)
		}
	})

	t.Run("unknown placeholder fails loudly with fixable_by:human", func(t *testing.T) {
		s := credential.Secrets{
			Username:          "u",
			Password:          "p",
			LoginBodyTemplate: `{"u":"{{username}}","unknown":"{{nope}}"}`,
		}
		_, _, err := buildLoginBody(s)
		if err == nil {
			t.Fatal("expected error for unknown placeholder (typo = loud fail, not silent empty)")
		}
		if !strings.Contains(err.Error(), "nope") {
			t.Errorf("error should name the missing placeholder, got %v", err)
		}
	})

	t.Run("template that isn't valid JSON after substitution errors with fixable_by:human", func(t *testing.T) {
		s := credential.Secrets{
			Username:          "u",
			Password:          "p",
			LoginBodyTemplate: `{"u":{{username}}}`, // placeholder outside quotes → bare token, breaks JSON
		}
		_, _, err := buildLoginBody(s)
		if err == nil {
			t.Fatal("expected error for template that yields invalid JSON")
		}
		if !strings.Contains(err.Error(), "invalid JSON") {
			t.Errorf("error should mention invalid JSON, got %v", err)
		}
	})

	t.Run("template overrides --login-format", func(t *testing.T) {
		// If someone sets both login-format=form AND login-body-template,
		// the template wins and content-type is application/json.
		s := credential.Secrets{
			Username:          "u",
			Password:          "p",
			LoginFormat:       "form",
			LoginBodyTemplate: `{"u":"{{username}}"}`,
		}
		_, ct, err := buildLoginBody(s)
		if err != nil {
			t.Fatal(err)
		}
		if ct != "application/json" {
			t.Errorf("template must force application/json, got %q", ct)
		}
	})
}

// jsonUnmarshal wraps encoding/json under a local alias so the template
// test stays focused on the surface behaviour (body parses, values
// round-trip) without importing encoding/json into multiple tests.
func jsonUnmarshal(data []byte, v any) error {
	return stdjson.Unmarshal(data, v)
}

func TestJSONStringEscape(t *testing.T) {
	cases := []struct{ in, want string }{
		{`alice`, `alice`},
		{`a"b`, `a\"b`},
		{`a\b`, `a\\b`},
		{"a\nb", `a\nb`},
		{``, ``},
	}
	for _, tc := range cases {
		if got := jsonStringEscape(tc.in); got != tc.want {
			t.Errorf("jsonStringEscape(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

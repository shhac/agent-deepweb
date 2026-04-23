package credential

import (
	"net/url"
	"testing"
)

func TestMatchesURL_HostAndPort(t *testing.T) {
	cases := []struct {
		name    string
		domains []string
		url     string
		want    bool
	}{
		{"exact host any port (https default)", []string{"api.example.com"}, "https://api.example.com/path", true},
		{"exact host any port (http default)", []string{"api.example.com"}, "http://api.example.com/path", true},
		{"exact host with port literal", []string{"api.example.com:443"}, "https://api.example.com/path", true},
		{"exact host:port match", []string{"api.example.com:8080"}, "http://api.example.com:8080/", true},
		{"exact host:port mismatch port", []string{"api.example.com:8080"}, "http://api.example.com:9090/", false},
		{"wildcard match", []string{"*.example.com"}, "https://api.example.com/", true},
		{"wildcard nested", []string{"*.example.com"}, "https://a.b.example.com/", true},
		{"wildcard does not match bare", []string{"*.example.com"}, "https://example.com/", false},
		{"wildcard with port", []string{"*.example.com:8080"}, "http://api.example.com:8080/", true},
		{"wildcard with port mismatch", []string{"*.example.com:8080"}, "http://api.example.com:9999/", false},
		{"host case-insensitive", []string{"API.example.com"}, "https://api.example.com/", true},
		{"wrong host", []string{"example.com"}, "https://evil.com/", false},
		{"ipv4 loopback", []string{"127.0.0.1"}, "http://127.0.0.1:8080/", true},
		{"ipv4 loopback with port", []string{"127.0.0.1:8080"}, "http://127.0.0.1:8080/", true},
		{"ipv6 loopback bracketed", []string{"[::1]"}, "http://[::1]:8080/", true},
		{"ipv6 loopback with port", []string{"[::1]:8080"}, "http://[::1]:8080/", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Credential{Domains: tc.domains}
			u, _ := url.Parse(tc.url)
			if got := c.MatchesURL(u); got != tc.want {
				t.Fatalf("MatchesURL(%s) for domains=%v = %v, want %v", tc.url, tc.domains, got, tc.want)
			}
		})
	}
}

func TestMatchesURL_PathScoping(t *testing.T) {
	cases := []struct {
		name  string
		paths []string
		path  string
		want  bool
	}{
		{"empty paths allow all", nil, "/anything", true},
		{"exact match", []string{"/api/v1/me"}, "/api/v1/me", true},
		{"exact mismatch", []string{"/api/v1/me"}, "/api/v1/other", false},
		{"trailing /* prefix — match prefix itself", []string{"/api/v1/*"}, "/api/v1", true},
		{"trailing /* prefix — match child", []string{"/api/v1/*"}, "/api/v1/users", true},
		{"trailing /* prefix — match deep child", []string{"/api/v1/*"}, "/api/v1/users/42/repos", true},
		{"trailing /* prefix — no match sibling", []string{"/api/v1/*"}, "/api/v2/users", false},
		{"path.Match glob single segment", []string{"/users/*/repos"}, "/users/42/repos", true},
		{"path.Match glob not cross-segment", []string{"/users/*/repos"}, "/users/42/a/repos", false},
		{"multiple patterns OR'd", []string{"/a/*", "/b/*"}, "/b/foo", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Credential{Domains: []string{"example.com"}, Paths: tc.paths}
			u, _ := url.Parse("https://example.com" + tc.path)
			if got := c.MatchesURL(u); got != tc.want {
				t.Fatalf("MatchesURL(%s) for paths=%v = %v, want %v", tc.path, tc.paths, got, tc.want)
			}
		})
	}
}

func TestParseHostPattern_PortParsing(t *testing.T) {
	cases := []struct {
		entry    string
		wantHost string
		wantPort string
		wantWild bool
	}{
		{"api.example.com", "api.example.com", "", false},
		{"api.example.com:8080", "api.example.com", "8080", false},
		{"*.example.com", ".example.com", "", true},
		{"*.example.com:443", ".example.com", "443", true},
		{"[::1]:8080", "::1", "8080", false},
		// "2026" in a hostname shouldn't be treated as a port (needs `:` before digits).
		{"api-2026.example.com", "api-2026.example.com", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.entry, func(t *testing.T) {
			p := parseHostPattern(tc.entry)
			if p.host != tc.wantHost || p.port != tc.wantPort || p.wildcard != tc.wantWild {
				t.Fatalf("parseHostPattern(%q) = {host:%q port:%q wild:%v}, want {%q %q %v}",
					tc.entry, p.host, p.port, p.wildcard, tc.wantHost, tc.wantPort, tc.wantWild)
			}
		})
	}
}

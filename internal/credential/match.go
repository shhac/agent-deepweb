package credential

import (
	"net/url"
	"path"
	"strings"
)

// Domain allowlist entries support:
//
//	host                       any port on host
//	host:port                  only that port on host
//	*.suffix                   any subdomain of suffix (any port)
//	*.suffix:port              any subdomain of suffix (that port)
//
// Ports are matched explicitly — an entry without a port accepts any port.
// Hostname comparison is case-insensitive; IPv6 literals are supported (the
// URL's [::1] form is canonicalised by url.Hostname()).
type hostPattern struct {
	host     string // lowercased; "example.com" or ".example.com" (leading dot = wildcard)
	wildcard bool
	port     string // empty = any port
}

func parseHostPattern(entry string) hostPattern {
	entry = strings.TrimSpace(strings.ToLower(entry))

	// Split host:port, carefully — IPv6 is [::1]:8080.
	host, port := entry, ""
	if i := strings.LastIndex(entry, ":"); i > 0 {
		// Only treat as port split if what's after is all digits.
		cand := entry[i+1:]
		if isAllDigits(cand) {
			host = entry[:i]
			port = cand
		}
	}
	// Trim IPv6 brackets.
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")

	p := hostPattern{host: host, port: port}
	if strings.HasPrefix(host, "*.") {
		p.wildcard = true
		p.host = host[1:] // ".example.com"
	}
	return p
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// matches reports whether the pattern covers the URL's host+port.
// urlHost is the raw u.Host (may contain port), urlHostname is u.Hostname(),
// urlPort is u.Port() (may be empty).
func (p hostPattern) matches(urlHostname, urlPort string) bool {
	if p.port != "" && p.port != urlPort {
		return false
	}
	if p.wildcard {
		suffix := p.host // ".example.com"
		return strings.HasSuffix(urlHostname, suffix) && len(urlHostname) > len(suffix)
	}
	return urlHostname == p.host
}

// Path patterns:
//
//	exact              /api/v1/me
//	trailing /*        /api/v1/*      recursive prefix (matches /api/v1/ and below)
//	middle glob        /users/*/repos uses path.Match (single-segment '*')
//
// Empty Paths → match all paths on allowed hosts.
func pathMatches(pattern, urlPath string) bool {
	if pattern == "" {
		return true
	}
	if !strings.ContainsAny(pattern, "*?") {
		return pattern == urlPath
	}
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		return urlPath == prefix || strings.HasPrefix(urlPath, prefix+"/")
	}
	ok, _ := path.Match(pattern, urlPath)
	return ok
}

// MatchesURL reports whether a request to the given URL is covered by this
// credential's host allowlist AND path allowlist (if any).
func (c *Credential) MatchesURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	hostname := strings.ToLower(u.Hostname())
	port := u.Port()
	// Default ports so entries like "api.example.com:443" still work when the
	// URL is https without explicit port.
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}

	hostOK := false
	for _, entry := range c.Domains {
		p := parseHostPattern(entry)
		if p.matches(hostname, port) {
			hostOK = true
			break
		}
	}
	if !hostOK {
		return false
	}

	if len(c.Paths) == 0 {
		return true
	}
	for _, pat := range c.Paths {
		if pathMatches(pat, u.Path) {
			return true
		}
	}
	return false
}

// FindByURL scans the index for credentials covering the URL's host+port+path.
func FindByURL(u *url.URL) ([]Credential, error) {
	all, err := List()
	if err != nil {
		return nil, err
	}
	var matches []Credential
	for _, c := range all {
		if c.MatchesURL(u) {
			matches = append(matches, c)
		}
	}
	return matches, nil
}

package credential

import (
	"net/http"
	"regexp"
	"time"
)

// PersistedCookie is the serialisable form of an http.Cookie, with an extra
// Sensitive flag that decides whether the value is shown to the LLM.
type PersistedCookie struct {
	Name     string        `json:"name"`
	Value    string        `json:"value"`
	Domain   string        `json:"domain,omitempty"`
	Path     string        `json:"path,omitempty"`
	Expires  time.Time     `json:"expires,omitempty"`
	MaxAge   int           `json:"max_age,omitempty"`
	Secure   bool          `json:"secure,omitempty"`
	HttpOnly bool          `json:"http_only,omitempty"`
	SameSite http.SameSite `json:"same_site,omitempty"`
	// Sensitive=true → LLM-facing output shows "<redacted>" in place of Value.
	// Classified automatically at capture time; overrideable per cookie.
	Sensitive bool `json:"sensitive"`
}

// sensitiveCookieNamePattern flags names that almost always hold auth
// material. Matched as a case-insensitive substring.
var sensitiveCookieNamePattern = regexp.MustCompile(
	`(?i)(sessionid|session|sess|sid|jsessionid|phpsessid|asp\.net_sessionid|connect\.sid|auth|token|csrf|xsrf|bearer|access|refresh|remember)`,
)

// ClassifyCookie returns true if this cookie should be treated as sensitive.
// The default heuristic: HttpOnly is the strongest signal; failing that, the
// cookie name is matched against known auth-cookie patterns. Human override
// via `session mark-visible` / `session mark-sensitive`.
func ClassifyCookie(c *http.Cookie) bool {
	if c.HttpOnly {
		return true
	}
	return sensitiveCookieNamePattern.MatchString(c.Name)
}

// FromHTTP snapshots an http.Cookie into a PersistedCookie and classifies it.
func FromHTTP(c *http.Cookie) PersistedCookie {
	return PersistedCookie{
		Name:      c.Name,
		Value:     c.Value,
		Domain:    c.Domain,
		Path:      c.Path,
		Expires:   c.Expires,
		MaxAge:    c.MaxAge,
		Secure:    c.Secure,
		HttpOnly:  c.HttpOnly,
		SameSite:  c.SameSite,
		Sensitive: ClassifyCookie(c),
	}
}

// ToHTTP materialises the cookie for sending via a jar or Cookie header.
func (p PersistedCookie) ToHTTP() *http.Cookie {
	return &http.Cookie{
		Name:     p.Name,
		Value:    p.Value,
		Domain:   p.Domain,
		Path:     p.Path,
		Expires:  p.Expires,
		MaxAge:   p.MaxAge,
		Secure:   p.Secure,
		HttpOnly: p.HttpOnly,
		SameSite: p.SameSite,
	}
}

// Expired reports whether the cookie has a known expiry in the past.
// Session cookies (no Expires/MaxAge) are never "expired" — they die with
// the session file's top-level expiry.
func (p PersistedCookie) Expired() bool {
	if p.MaxAge < 0 {
		return true
	}
	if !p.Expires.IsZero() && time.Now().After(p.Expires) {
		return true
	}
	return false
}

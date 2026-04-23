package credential

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shhac/agent-deepweb/internal/config"
	"golang.org/x/net/publicsuffix"
)

// Session is the derived auth state produced by a form-login flow:
// a set of cookies (each flagged sensitive or visible) plus an optional
// response-body token (Bearer) and the session-level acquired/expires
// timestamps. Stored at sessions/<name>.json, mode 0600.
type Session struct {
	Name        string            `json:"name"`
	Cookies     []PersistedCookie `json:"cookies,omitempty"`
	Token       string            `json:"token,omitempty"`        // extracted from login response body
	TokenHeader string            `json:"token_header,omitempty"` // default Authorization
	TokenPrefix string            `json:"token_prefix,omitempty"` // default "Bearer "
	Acquired    time.Time         `json:"acquired"`
	Expires     time.Time         `json:"expires,omitempty"`
}

func sessionPath(name string) string {
	return filepath.Join(config.ConfigDir(), "sessions", name+".json")
}

// CookieView is the LLM-facing shape: sensitive cookies have Value masked.
type CookieView struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain,omitempty"`
	Path     string    `json:"path,omitempty"`
	Expires  time.Time `json:"expires,omitempty"`
	HttpOnly bool      `json:"http_only,omitempty"`
	Secure   bool      `json:"secure,omitempty"`
	// When sensitive, Value is "<redacted>".
	Sensitive bool `json:"sensitive"`
}

func viewCookie(p PersistedCookie) CookieView {
	val := p.Value
	if p.Sensitive {
		val = "<redacted>"
	}
	return CookieView{
		Name:      p.Name,
		Value:     val,
		Domain:    p.Domain,
		Path:      p.Path,
		Expires:   p.Expires,
		HttpOnly:  p.HttpOnly,
		Secure:    p.Secure,
		Sensitive: p.Sensitive,
	}
}

// SessionStatus is what `session status` returns — summary, no cookies.
type SessionStatus struct {
	Exists         bool      `json:"exists"`
	Acquired       time.Time `json:"acquired,omitempty"`
	Expires        time.Time `json:"expires,omitempty"`
	IsExpired      bool      `json:"is_expired,omitempty"`
	CookieCount    int       `json:"cookie_count"`
	SensitiveCount int       `json:"sensitive_count"`
	HasToken       bool      `json:"has_token"`
}

// SessionShow is what `session show` returns — includes per-cookie views
// with values masked for sensitive cookies.
type SessionShow struct {
	SessionStatus
	Cookies []CookieView `json:"cookies,omitempty"`
}

func GetSessionStatus(name string) (*SessionStatus, error) {
	s, err := readSession(name)
	if err != nil {
		if os.IsNotExist(err) {
			return &SessionStatus{Exists: false}, nil
		}
		return nil, err
	}
	return s.summary(), nil
}

func GetSessionShow(name string) (*SessionShow, error) {
	s, err := readSession(name)
	if err != nil {
		if os.IsNotExist(err) {
			return &SessionShow{SessionStatus: SessionStatus{Exists: false}}, nil
		}
		return nil, err
	}
	views := make([]CookieView, 0, len(s.Cookies))
	for _, c := range s.Cookies {
		views = append(views, viewCookie(c))
	}
	return &SessionShow{SessionStatus: *s.summary(), Cookies: views}, nil
}

func (s *Session) summary() *SessionStatus {
	sensitive := 0
	for _, c := range s.Cookies {
		if c.Sensitive {
			sensitive++
		}
	}
	return &SessionStatus{
		Exists:         true,
		Acquired:       s.Acquired,
		Expires:        s.Expires,
		IsExpired:      s.IsExpired(),
		CookieCount:    len(s.Cookies),
		SensitiveCount: sensitive,
		HasToken:       s.Token != "",
	}
}

func (s *Session) IsExpired() bool {
	if !s.Expires.IsZero() && time.Now().After(s.Expires) {
		return true
	}
	return false
}

// ReadSession is used internally by the HTTP client to prime a jar.
// Callers must not print cookie values directly.
func ReadSession(name string) (*Session, error) {
	return readSession(name)
}

func readSession(name string) (*Session, error) {
	data, err := os.ReadFile(sessionPath(name))
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// WriteSession persists the session to disk with mode 0600.
func WriteSession(s *Session) error {
	dir := filepath.Dir(sessionPath(s.Name))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sessionPath(s.Name), append(data, '\n'), 0o600)
}

func ClearSession(name string) error {
	err := os.Remove(sessionPath(name))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// NewJar returns an RFC-6265 cookiejar seeded with the session's cookies
// scoped to baseURL's host. Cookies whose stored Domain doesn't match are
// still seeded for the host so manually-added cookies work.
func (s *Session) NewJar(baseURL *url.URL) (*cookiejar.Jar, error) {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, err
	}
	if baseURL == nil || len(s.Cookies) == 0 {
		return jar, nil
	}
	var live []*http.Cookie
	for _, pc := range s.Cookies {
		if pc.Expired() {
			continue
		}
		live = append(live, pc.ToHTTP())
	}
	// The jar applies cookies to the URL the caller specifies; since our
	// persisted cookies already carry Domain/Path, the jar will only send
	// them back on matching requests.
	jar.SetCookies(baseURL, live)
	return jar, nil
}

// HarvestFromJar walks the jar for the given URL and updates the session's
// Cookies slice: new cookies are classified; existing cookies are updated
// in place preserving the Sensitive flag. Returns true if anything changed.
func (s *Session) HarvestFromJar(jar http.CookieJar, u *url.URL) bool {
	if jar == nil || u == nil {
		return false
	}
	jarCookies := jar.Cookies(u)
	if len(jarCookies) == 0 {
		return false
	}
	existing := make(map[string]int, len(s.Cookies))
	for i, c := range s.Cookies {
		existing[cookieKey(c.Name, c.Domain, c.Path)] = i
	}
	changed := false
	for _, jc := range jarCookies {
		// The stdlib jar drops Domain/Path/Expires on retrieval, so fill
		// from the host if not set. This is a limitation of net/http/cookiejar —
		// we can only retain what jar.Cookies(u) returns, which is name+value.
		domain := u.Hostname()
		path := "/"
		key := cookieKey(jc.Name, domain, path)
		if idx, ok := existing[key]; ok {
			if s.Cookies[idx].Value != jc.Value {
				s.Cookies[idx].Value = jc.Value
				changed = true
			}
			continue
		}
		// New cookie — classify fresh.
		pc := PersistedCookie{
			Name:      jc.Name,
			Value:     jc.Value,
			Domain:    domain,
			Path:      path,
			Sensitive: ClassifyCookie(&http.Cookie{Name: jc.Name}),
		}
		s.Cookies = append(s.Cookies, pc)
		changed = true
	}
	return changed
}

// HarvestResponse captures Set-Cookie headers directly from an HTTP response.
// This keeps attributes (Domain, Path, Expires, HttpOnly, Secure, SameSite)
// that the stdlib jar drops on retrieval.
func (s *Session) HarvestResponse(resp *http.Response, baseURL *url.URL) bool {
	if resp == nil {
		return false
	}
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		return false
	}
	existing := make(map[string]int, len(s.Cookies))
	for i, c := range s.Cookies {
		existing[cookieKey(c.Name, c.Domain, c.Path)] = i
	}
	changed := false
	host := ""
	if baseURL != nil {
		host = baseURL.Hostname()
	}
	for _, jc := range cookies {
		// Defaults when server didn't specify
		domain := jc.Domain
		if domain == "" {
			domain = host
		}
		path := jc.Path
		if path == "" {
			path = "/"
		}
		key := cookieKey(jc.Name, domain, path)
		if idx, ok := existing[key]; ok {
			// Preserve Sensitive (human may have overridden).
			keep := s.Cookies[idx].Sensitive
			s.Cookies[idx] = FromHTTP(jc)
			s.Cookies[idx].Domain = domain
			s.Cookies[idx].Path = path
			s.Cookies[idx].Sensitive = keep || s.Cookies[idx].Sensitive
			changed = true
			continue
		}
		pc := FromHTTP(jc)
		pc.Domain = domain
		pc.Path = path
		s.Cookies = append(s.Cookies, pc)
		changed = true
	}
	return changed
}

func cookieKey(name, domain, path string) string {
	return strings.ToLower(name) + "\x00" + strings.ToLower(domain) + "\x00" + path
}

// MarkCookieSensitive / MarkCookieVisible toggle the Sensitive flag on
// a stored cookie by name. Returns false if the cookie wasn't found.
func (s *Session) MarkCookieSensitivity(name string, sensitive bool) bool {
	hit := false
	for i := range s.Cookies {
		if strings.EqualFold(s.Cookies[i].Name, name) {
			s.Cookies[i].Sensitive = sensitive
			hit = true
		}
	}
	return hit
}

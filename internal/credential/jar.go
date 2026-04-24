package credential

import (
	"os"
	"time"
)

// Jar is a profile's persistent state: cookies (each flagged sensitive
// or visible) plus, for form-auth profiles, an extracted Bearer token
// and the session-level acquired/expires timestamps. Stored at
// profiles/<name>/jar.json, mode 0600, AES-256-GCM encrypted with a
// per-profile key (Secrets.JarKey).
//
// In v1 this was named Session and only existed for form-auth profiles.
// In v2 every profile type can have a jar — bearer/basic/cookie/custom
// upstreams that set cookies on responses (rolling sessions, anti-CSRF
// tokens) accumulate them across requests just like form-auth.
//
// File layout for the jar concern in this package:
//
//	jar.go          Jar struct + view types + summary helpers (this file)
//	jar_io.go       jarPath, ReadJar/WriteJar/ClearJar* (encrypted) + ReadJarPlain/WriteJarPlain (BYO plaintext)
//	jar_crypto.go   AES-256-GCM frame format, key generation, key lookup
//	jar_harvest.go  NewCookieJar, HarvestFromCookieJar, HarvestResponse, MarkCookieSensitivity
type Jar struct {
	Name        string            `json:"name"`
	Cookies     []PersistedCookie `json:"cookies,omitempty"`
	Token       string            `json:"token,omitempty"`        // form-auth only: extracted from login response body
	TokenHeader string            `json:"token_header,omitempty"` // default Authorization
	TokenPrefix string            `json:"token_prefix,omitempty"` // default "Bearer "
	Acquired    time.Time         `json:"acquired"`
	Expires     time.Time         `json:"expires,omitempty"`
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

// JarStatus is what `jar status` returns — summary, no cookies.
type JarStatus struct {
	Exists         bool      `json:"exists"`
	Acquired       time.Time `json:"acquired,omitempty"`
	Expires        time.Time `json:"expires,omitempty"`
	IsExpired      bool      `json:"is_expired,omitempty"`
	CookieCount    int       `json:"cookie_count"`
	SensitiveCount int       `json:"sensitive_count"`
	HasToken       bool      `json:"has_token"`
}

// JarShow is what `jar show` returns — includes per-cookie views with
// values masked for sensitive cookies.
type JarShow struct {
	JarStatus
	Cookies []CookieView `json:"cookies,omitempty"`
}

func GetJarStatus(name string) (*JarStatus, error) {
	j, err := readJar(name)
	if err != nil {
		if os.IsNotExist(err) {
			return &JarStatus{Exists: false}, nil
		}
		return nil, err
	}
	return j.summary(), nil
}

func GetJarShow(name string) (*JarShow, error) {
	j, err := readJar(name)
	if err != nil {
		if os.IsNotExist(err) {
			return &JarShow{JarStatus: JarStatus{Exists: false}}, nil
		}
		return nil, err
	}
	views := make([]CookieView, 0, len(j.Cookies))
	for _, c := range j.Cookies {
		views = append(views, viewCookie(c))
	}
	return &JarShow{JarStatus: *j.summary(), Cookies: views}, nil
}

func (j *Jar) summary() *JarStatus {
	sensitive := 0
	for _, c := range j.Cookies {
		if c.Sensitive {
			sensitive++
		}
	}
	return &JarStatus{
		Exists:         true,
		Acquired:       j.Acquired,
		Expires:        j.Expires,
		IsExpired:      j.IsExpired(),
		CookieCount:    len(j.Cookies),
		SensitiveCount: sensitive,
		HasToken:       j.Token != "",
	}
}

func (j *Jar) IsExpired() bool {
	return !j.Expires.IsZero() && time.Now().After(j.Expires)
}

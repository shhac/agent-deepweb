package credential

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// NewCookieJar returns an RFC-6265 cookiejar seeded with the jar's cookies
// scoped to baseURL's host. Cookies whose stored Domain doesn't match are
// still seeded for the host so manually-added cookies work.
func (j *Jar) NewCookieJar(baseURL *url.URL) (*cookiejar.Jar, error) {
	cj, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, err
	}
	if baseURL == nil || len(j.Cookies) == 0 {
		return cj, nil
	}
	var live []*http.Cookie
	for _, pc := range j.Cookies {
		if pc.Expired() {
			continue
		}
		live = append(live, pc.ToHTTP())
	}
	cj.SetCookies(baseURL, live)
	return cj, nil
}

// HarvestFromCookieJar walks the cookiejar for the given URL and updates
// the jar's Cookies slice: new cookies are classified; existing cookies
// are updated in place preserving the Sensitive flag. Returns true if
// anything changed.
func (j *Jar) HarvestFromCookieJar(cj http.CookieJar, u *url.URL) bool {
	if cj == nil || u == nil {
		return false
	}
	jarCookies := cj.Cookies(u)
	if len(jarCookies) == 0 {
		return false
	}
	existing := make(map[string]int, len(j.Cookies))
	for i, c := range j.Cookies {
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
			if j.Cookies[idx].Value != jc.Value {
				j.Cookies[idx].Value = jc.Value
				changed = true
			}
			continue
		}
		pc := PersistedCookie{
			Name:      jc.Name,
			Value:     jc.Value,
			Domain:    domain,
			Path:      path,
			Sensitive: ClassifyCookie(&http.Cookie{Name: jc.Name}),
		}
		j.Cookies = append(j.Cookies, pc)
		changed = true
	}
	return changed
}

// HarvestResponse captures Set-Cookie headers directly from an HTTP response.
// This keeps attributes (Domain, Path, Expires, HttpOnly, Secure, SameSite)
// that the stdlib jar drops on retrieval.
func (j *Jar) HarvestResponse(resp *http.Response, baseURL *url.URL) bool {
	if resp == nil {
		return false
	}
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		return false
	}
	existing := make(map[string]int, len(j.Cookies))
	for i, c := range j.Cookies {
		existing[cookieKey(c.Name, c.Domain, c.Path)] = i
	}
	changed := false
	host := ""
	if baseURL != nil {
		host = baseURL.Hostname()
	}
	for _, jc := range cookies {
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
			keep := j.Cookies[idx].Sensitive
			j.Cookies[idx] = FromHTTP(jc)
			j.Cookies[idx].Domain = domain
			j.Cookies[idx].Path = path
			j.Cookies[idx].Sensitive = keep || j.Cookies[idx].Sensitive
			changed = true
			continue
		}
		pc := FromHTTP(jc)
		pc.Domain = domain
		pc.Path = path
		j.Cookies = append(j.Cookies, pc)
		changed = true
	}
	return changed
}

func cookieKey(name, domain, path string) string {
	return strings.ToLower(name) + "\x00" + strings.ToLower(domain) + "\x00" + path
}

// MarkCookieSensitivity toggles the Sensitive flag on a stored cookie by
// name. Returns false if the cookie wasn't found.
func (j *Jar) MarkCookieSensitivity(name string, sensitive bool) bool {
	hit := false
	for i := range j.Cookies {
		if strings.EqualFold(j.Cookies[i].Name, name) {
			j.Cookies[i].Sensitive = sensitive
			hit = true
		}
	}
	return hit
}

package api

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"

	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"golang.org/x/net/publicsuffix"
)

// primeCookieJar returns a cookiejar seeded from the profile's stored
// jar (for any profile type). An expired session-bound jar (form auth
// only — that's the only type with a session-level expiry) short-circuits
// with fixable_by:human before any network call.
//
// jarPath, if non-empty, points to a bring-your-own plaintext jar that
// overrides the profile's encrypted default. The expiry check is skipped
// for BYO jars — a caller using `--cookiejar` explicitly owns lifecycle.
func primeCookieJar(auth *credential.Resolved, jarPath string, parsedURL *url.URL) (http.CookieJar, error) {
	switch {
	case jarPath != "":
		return primeFromBYO(jarPath, parsedURL), nil
	case auth != nil:
		return primeFromProfile(auth, parsedURL)
	default:
		return freshJar(), nil
	}
}

// primeFromBYO loads a plaintext jar from the caller-supplied path. A
// read error yields an empty in-memory jar — the caller's lifecycle, the
// caller's problem.
func primeFromBYO(jarPath string, parsedURL *url.URL) http.CookieJar {
	jarState, err := credential.ReadJarPlain(jarPath)
	if err != nil {
		return freshJar()
	}
	if cj, _ := jarState.NewCookieJar(parsedURL); cj != nil {
		return cj
	}
	return freshJar()
}

// primeFromProfile loads the profile's encrypted jar. Form-auth profiles
// short-circuit on expiry (the human is expected to re-login). Other
// types and pre-jar profiles fall through to a fresh jar.
func primeFromProfile(auth *credential.Resolved, parsedURL *url.URL) (http.CookieJar, error) {
	jarState, err := credential.ReadJar(auth.Name)
	if err != nil {
		return freshJar(), nil
	}
	if auth.Type == credential.AuthForm && jarState.IsExpired() {
		return nil, agenterrors.Newf(agenterrors.FixableByHuman,
			"session for %q is expired", auth.Name).
			WithHint("Ask the user to run 'agent-deepweb login " + auth.Name + "'")
	}
	if cj, _ := jarState.NewCookieJar(parsedURL); cj != nil {
		return cj, nil
	}
	return freshJar(), nil
}

func freshJar() http.CookieJar {
	cj, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	return cj
}

// harvestJarCookies captures Set-Cookie headers from a response into
// the appropriate jar (BYO plaintext if jarPath != "", else the
// profile's encrypted default), preserving per-cookie sensitivity, and
// returns a list of newly-captured cookie views for the envelope.
//
// Returns nil when no profile is attached AND no BYO jar was specified
// (the truly-anonymous `--profile none` case — cookies are accepted by
// the in-memory jar for the duration of the request but not persisted).
func harvestJarCookies(auth *credential.Resolved, jarPath string, httpResp *http.Response, parsedURL *url.URL) []credential.CookieView {
	if len(httpResp.Cookies()) == 0 {
		return nil
	}
	if jarPath == "" && auth == nil {
		return nil
	}

	jarState, write := loadOrInitJar(auth, jarPath)
	before := snapshotCookieKeys(jarState.Cookies)
	if changed := jarState.HarvestResponse(httpResp, parsedURL); changed {
		_ = write(jarState)
	}
	return diffNewCookies(before, jarState.Cookies)
}

// loadOrInitJar selects the jar source — BYO plaintext if jarPath is set,
// else the profile's encrypted default — and returns it together with a
// matching write function. A read error (file missing, decryption error,
// JSON garbage) yields a fresh empty jar so the request can still
// harvest into it; the write strategy is the same either way.
func loadOrInitJar(auth *credential.Resolved, jarPath string) (*credential.Jar, func(*credential.Jar) error) {
	if jarPath != "" {
		j, err := credential.ReadJarPlain(jarPath)
		if err != nil {
			j = &credential.Jar{Acquired: time.Now().UTC()}
		}
		return j, func(j *credential.Jar) error { return credential.WriteJarPlain(jarPath, j) }
	}
	j, err := credential.ReadJar(auth.Name)
	if err != nil {
		j = &credential.Jar{Name: auth.Name, Acquired: time.Now().UTC()}
	}
	return j, credential.WriteJar
}

// snapshotCookieKeys produces a set of {name|domain|path} keys for diff
// purposes. Pure helper.
func snapshotCookieKeys(cs []credential.PersistedCookie) map[string]struct{} {
	out := make(map[string]struct{}, len(cs))
	for _, c := range cs {
		out[c.Name+"\x00"+c.Domain+"\x00"+c.Path] = struct{}{}
	}
	return out
}

// diffNewCookies returns the cookies in `after` whose keys weren't in
// `before` (post-harvest minus pre-harvest), as redacted-aware views.
// Pure: testable without HTTP, files, or profiles.
func diffNewCookies(before map[string]struct{}, after []credential.PersistedCookie) []credential.CookieView {
	var added []credential.CookieView
	for _, c := range after {
		if _, had := before[c.Name+"\x00"+c.Domain+"\x00"+c.Path]; !had {
			added = append(added, viewPersisted(c))
		}
	}
	return added
}

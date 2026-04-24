package credential

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
type Jar struct {
	Name        string            `json:"name"`
	Cookies     []PersistedCookie `json:"cookies,omitempty"`
	Token       string            `json:"token,omitempty"`        // form-auth only: extracted from login response body
	TokenHeader string            `json:"token_header,omitempty"` // default Authorization
	TokenPrefix string            `json:"token_prefix,omitempty"` // default "Bearer "
	Acquired    time.Time         `json:"acquired"`
	Expires     time.Time         `json:"expires,omitempty"`
}

// jarPath returns the per-profile jar location: profiles/<name>/jar.json.
// One subdirectory per profile keeps the layout open for future per-profile
// auxiliary state without polluting the top-level config dir.
func jarPath(name string) string {
	return filepath.Join(config.ConfigDir(), "profiles", name, "jar.json")
}

// jarMagic is the 4-byte file prefix identifying an encrypted jar. Format:
//
//	"AGD1" || nonce(12) || ciphertext+tag(N)
//
// "AGD" = agent-deepweb, "1" = format version. A future format change
// (different cipher, larger nonce) bumps the digit.
var jarMagic = []byte("AGD1")

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

// ReadJar loads and decrypts the on-disk jar for the named profile.
// Returns os.ErrNotExist if no jar file is present (a normal pre-login
// state). Callers must not print cookie values directly.
func ReadJar(name string) (*Jar, error) { return readJar(name) }

func readJar(name string) (*Jar, error) {
	data, err := os.ReadFile(jarPath(name))
	if err != nil {
		return nil, err
	}
	plaintext, err := decryptJarBytes(name, data)
	if err != nil {
		return nil, err
	}
	var j Jar
	if err := json.Unmarshal(plaintext, &j); err != nil {
		return nil, err
	}
	return &j, nil
}

// WriteJar persists the jar to disk encrypted with the profile's JarKey.
// The containing profiles/<name>/ directory is created with mode 0700.
func WriteJar(j *Jar) error {
	dir := filepath.Dir(jarPath(j.Name))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	plaintext, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	ciphertext, err := encryptJarBytes(j.Name, plaintext)
	if err != nil {
		return err
	}
	return os.WriteFile(jarPath(j.Name), ciphertext, 0o600)
}

func ClearJar(name string) error {
	err := os.Remove(jarPath(name))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ClearJarTree removes the entire profiles/<name>/ directory (jar + any
// future per-profile state). Called by Remove() so a deleted profile
// leaves no cookies behind.
func ClearJarTree(name string) error {
	err := os.RemoveAll(filepath.Dir(jarPath(name)))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ReadJarPlain loads a bring-your-own jar from an arbitrary file path.
// Plaintext JSON, no encryption — the caller chose the location and the
// trade-off. Used by the `--cookiejar <path>` flag, including the
// `--profile none --cookiejar <path>` LLM-authored-flow case. Returns
// (zero-value Jar, nil) if the file is missing — that's the "fresh jar"
// case, not an error.
func ReadJarPlain(path string) (*Jar, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Jar{}, nil
		}
		return nil, err
	}
	var j Jar
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, err
	}
	return &j, nil
}

// WriteJarPlain persists a bring-your-own jar to an arbitrary file path
// as plaintext JSON, mode 0600. The directory must already exist (we do
// not auto-create — caller-chosen paths shouldn't surprise users with
// new directories).
func WriteJarPlain(path string, j *Jar) error {
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

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

// jarKeyFor loads the JarKey for the named profile. Returns
// errMissingJarKey if the profile has no key yet (legitimate for ad-hoc
// callers, but a bug for the WriteJar/ReadJar path since profile add
// always provisions one).
func jarKeyFor(name string) ([]byte, error) {
	r, err := Resolve(name)
	if err != nil {
		return nil, err
	}
	if len(r.Secrets.JarKey) == 0 {
		return nil, errMissingJarKey
	}
	if len(r.Secrets.JarKey) != 32 {
		return nil, fmt.Errorf("jar key for %q is %d bytes, expected 32", name, len(r.Secrets.JarKey))
	}
	return r.Secrets.JarKey, nil
}

var errMissingJarKey = errors.New("profile has no jar encryption key")

// generateJarKey returns 32 random bytes suitable for AES-256-GCM. Used
// by Store when provisioning a new profile.
func generateJarKey() ([]byte, error) {
	k := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, k); err != nil {
		return nil, err
	}
	return k, nil
}

// encryptJarBytes encrypts plaintext for the named profile. Output:
// magic(4) || nonce(12) || ciphertext+tag.
func encryptJarBytes(name string, plaintext []byte) ([]byte, error) {
	key, err := jarKeyFor(name)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(jarMagic)+len(nonce)+len(plaintext)+gcm.Overhead())
	out = append(out, jarMagic...)
	out = append(out, nonce...)
	out = gcm.Seal(out, nonce, plaintext, nil)
	return out, nil
}

// decryptJarBytes is the inverse of encryptJarBytes. Refuses to operate
// on data without the magic prefix — there is no plaintext fallback. A
// jar written by an older binary that ever existed must be rebuilt by
// re-running the relevant login flow.
func decryptJarBytes(name string, data []byte) ([]byte, error) {
	if len(data) < len(jarMagic) || !bytesEqual(data[:len(jarMagic)], jarMagic) {
		return nil, fmt.Errorf("jar for %q has unrecognised format (wrong magic)", name)
	}
	key, err := jarKeyFor(name)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	body := data[len(jarMagic):]
	if len(body) < gcm.NonceSize()+gcm.Overhead() {
		return nil, fmt.Errorf("jar for %q is truncated", name)
	}
	nonce := body[:gcm.NonceSize()]
	ciphertext := body[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("jar for %q failed to decrypt (key mismatch?): %w", name, err)
	}
	return plaintext, nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

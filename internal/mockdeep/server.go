// Package mockdeep is a deliberately dumb HTTP server used to exercise
// agent-deepweb end-to-end. Every auth style is represented by a distinct
// endpoint that accepts exactly one fixed credential, so tests can assert
// that agent-deepweb attached the right thing.
//
// The hardcoded "valid" credential values are exported as constants so
// tests can reference them symbolically rather than copying strings.
package mockdeep

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Canonical valid values. Tests should reference these; humans who run
// the server interactively can read them from `mockdeep help`.
const (
	ValidBearerToken = "valid-token-bearer"
	ValidAPIKey      = "valid-api-key"
	APIKeyHeader     = "X-API-Key"
	ValidUsername    = "alice"
	ValidPassword    = "wonderland"
	LoginToken       = "login-issued-token-xyz" // returned by /login, accepted by /token-protected
	SessionCookie    = "sess-abc123"            // Set-Cookie by /login, accepted by /session
)

// Server is the mockdeep HTTP handler. The zero value is not usable —
// use New().
type Server struct {
	mux    *http.ServeMux
	hitsMu sync.Mutex
	hits   map[string]int // request counts per path, for debugging / tests
}

func New() *Server {
	s := &Server{mux: http.NewServeMux(), hits: map[string]int{}}
	s.register()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.hitsMu.Lock()
	s.hits[r.URL.Path]++
	s.hitsMu.Unlock()
	s.mux.ServeHTTP(w, r)
}

// Hits returns a snapshot of the request counter (useful in tests).
func (s *Server) Hits() map[string]int {
	s.hitsMu.Lock()
	defer s.hitsMu.Unlock()
	out := make(map[string]int, len(s.hits))
	for k, v := range s.hits {
		out[k] = v
	}
	return out
}

func (s *Server) register() {
	s.mux.HandleFunc("/healthz", s.healthz)
	s.mux.HandleFunc("/headers", s.headers)
	s.mux.HandleFunc("/echo", s.echo)

	s.mux.HandleFunc("/whoami", s.whoami)                  // Bearer valid-token-bearer
	s.mux.HandleFunc("/basic", s.basic)                    // Basic alice:wonderland
	s.mux.HandleFunc("/api-key", s.apiKey)                 // X-API-Key: valid-api-key
	s.mux.HandleFunc("/login", s.login)                    // POST → sets cookie + returns token
	s.mux.HandleFunc("/session", s.session)                // Cookie session=<SessionCookie>
	s.mux.HandleFunc("/token-protected", s.tokenProtected) // Bearer <LoginToken>

	s.mux.HandleFunc("/graphql", s.graphql)

	s.mux.HandleFunc("/status/", s.status)
	s.mux.HandleFunc("/slow", s.slow)
	s.mux.HandleFunc("/large", s.large)
	s.mux.HandleFunc("/redirect", s.redirect)

	s.mux.HandleFunc("/", s.index)
}

// ---------- generic helpers ----------

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

// ---------- endpoints ----------

// Routes returns a human-readable list of routes with their expected
// credentials. Used by the index handler (served at `GET /`) and by
// cmd/mockdeep's `flag.Usage` so both stay in sync.
func Routes() []string {
	return []string{
		"GET  /healthz",
		"GET  /headers",
		"ANY  /echo",
		"GET  /whoami            (Bearer " + ValidBearerToken + ")",
		"GET  /basic             (Basic " + ValidUsername + ":" + ValidPassword + ")",
		"GET  /api-key           (" + APIKeyHeader + ": " + ValidAPIKey + ")",
		"POST /login             (form or JSON: username=" + ValidUsername + ", password=" + ValidPassword + ")",
		"GET  /session           (Cookie session=" + SessionCookie + ")",
		"GET  /token-protected   (Bearer " + LoginToken + ")",
		"POST /graphql           (Bearer " + ValidBearerToken + ")",
		"GET  /status/<code>",
		"GET  /slow?ms=<n>",
		"GET  /large?bytes=<n>",
		"GET  /redirect?to=<path>",
	}
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		fail(w, http.StatusNotFound, "no such route: "+r.URL.Path)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "mockdeep",
		"routes":  Routes(),
	})
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) headers(w http.ResponseWriter, r *http.Request) {
	headers := map[string]string{}
	for k, v := range r.Header {
		headers[k] = strings.Join(v, ", ")
	}
	writeJSON(w, http.StatusOK, map[string]any{"headers": headers})
}

func (s *Server) echo(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	headers := map[string]string{}
	for k, v := range r.Header {
		headers[k] = strings.Join(v, ", ")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"method":  r.Method,
		"path":    r.URL.Path,
		"query":   r.URL.Query(),
		"headers": headers,
		"body":    string(body),
	})
}

func (s *Server) whoami(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	want := "Bearer " + ValidBearerToken
	if auth != want {
		w.Header().Set("WWW-Authenticate", `Bearer realm="mockdeep"`)
		fail(w, http.StatusUnauthorized, "expected "+want)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":        ValidUsername,
		"auth_method": "bearer",
		// Deliberately echo the token back — a hostile endpoint might do this.
		// agent-deepweb's byte-level redactor should mask it on the way out.
		"seen_authorization": auth,
	})
}

func (s *Server) basic(w http.ResponseWriter, r *http.Request) {
	user, pass, ok := r.BasicAuth()
	if !ok || user != ValidUsername || pass != ValidPassword {
		w.Header().Set("WWW-Authenticate", `Basic realm="mockdeep"`)
		fail(w, http.StatusUnauthorized, "basic auth required: "+ValidUsername+":"+ValidPassword)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":        user,
		"auth_method": "basic",
	})
}

func (s *Server) apiKey(w http.ResponseWriter, r *http.Request) {
	got := r.Header.Get(APIKeyHeader)
	if got != ValidAPIKey {
		fail(w, http.StatusUnauthorized, "expected "+APIKeyHeader+": "+ValidAPIKey)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":        ValidUsername,
		"auth_method": "api_key",
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	user, pass := extractLoginCreds(r)
	if user != ValidUsername || pass != ValidPassword {
		fail(w, http.StatusUnauthorized, "bad username/password")
		return
	}
	// The auth cookie: HttpOnly → auto-classified as sensitive.
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    SessionCookie,
		Path:     "/",
		HttpOnly: true,
	})
	// Non-sensitive UI state cookies that the LLM should be able to see.
	http.SetCookie(w, &http.Cookie{
		Name:  "theme",
		Value: "dark",
		Path:  "/",
	})
	http.SetCookie(w, &http.Cookie{
		Name:  "locale",
		Value: "en-GB",
		Path:  "/",
	})
	// Additional sensitive-by-name cookie.
	http.SetCookie(w, &http.Cookie{
		Name:  "csrf_token",
		Value: "csrf-" + SessionCookie,
		Path:  "/",
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"token": LoginToken,
		// Keep as a realistic JWT-ish field the LLM might try to grab. The
		// redactor should mask "access_token" and literal LoginToken everywhere.
		"access_token": LoginToken,
		"expires_in":   3600,
	})
}

func extractLoginCreds(r *http.Request) (user, pass string) {
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		return body.Username, body.Password
	}
	_ = r.ParseForm()
	return r.FormValue("username"), r.FormValue("password")
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("session")
	if err != nil || c.Value != SessionCookie {
		fail(w, http.StatusUnauthorized, "need Cookie session="+SessionCookie)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":        ValidUsername,
		"auth_method": "session_cookie",
	})
}

func (s *Server) tokenProtected(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	want := "Bearer " + LoginToken
	if auth != want {
		fail(w, http.StatusUnauthorized, "expected Bearer <login-issued-token> — obtain via POST /login")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":        ValidUsername,
		"auth_method": "login_issued_bearer",
	})
}

// GraphQL: 200 OK with `{data, errors}` envelope. Unauthenticated errors
// come back as a GraphQL error with extensions.code=UNAUTHENTICATED so
// agent-deepweb can classify them as fixable_by:human.
func (s *Server) graphql(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var body struct {
		Query         string         `json:"query"`
		OperationName string         `json:"operationName"`
		Variables     map[string]any `json:"variables"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	if r.Header.Get("Authorization") != "Bearer "+ValidBearerToken {
		writeJSON(w, http.StatusOK, map[string]any{
			"errors": []map[string]any{{
				"message":    "unauthenticated",
				"extensions": map[string]any{"code": "UNAUTHENTICATED"},
			}},
		})
		return
	}
	// Cheap query routing based on substring.
	switch {
	case strings.Contains(body.Query, "me"):
		writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{
				"me": map[string]any{"id": "1", "name": ValidUsername},
			},
		})
	case strings.Contains(body.Query, "ping"):
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"ping": "pong"}})
	default:
		writeJSON(w, http.StatusOK, map[string]any{
			"errors": []map[string]any{{
				"message": "unknown query — try '{ me { id name } }' or '{ ping }'",
			}},
		})
	}
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	codeStr := strings.TrimPrefix(r.URL.Path, "/status/")
	code, err := strconv.Atoi(codeStr)
	if err != nil || code < 100 || code > 599 {
		fail(w, http.StatusBadRequest, "bad status code: "+codeStr)
		return
	}
	if code == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", "1")
	}
	writeJSON(w, code, map[string]any{"status": code})
}

func (s *Server) slow(w http.ResponseWriter, r *http.Request) {
	ms, _ := strconv.Atoi(r.URL.Query().Get("ms"))
	if ms <= 0 {
		ms = 1000
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
	writeJSON(w, http.StatusOK, map[string]any{"slept_ms": ms})
}

func (s *Server) large(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.URL.Query().Get("bytes"))
	if n <= 0 {
		n = 256
	}
	if n > 50*1024*1024 {
		n = 50 * 1024 * 1024
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprint(n))
	w.WriteHeader(http.StatusOK)
	buf := make([]byte, 4096)
	for i := range buf {
		buf[i] = 'x'
	}
	remaining := n
	for remaining > 0 {
		chunk := len(buf)
		if chunk > remaining {
			chunk = remaining
		}
		_, _ = w.Write(buf[:chunk])
		remaining -= chunk
	}
}

func (s *Server) redirect(w http.ResponseWriter, r *http.Request) {
	to := r.URL.Query().Get("to")
	if to == "" {
		to = "/whoami"
	}
	http.Redirect(w, r, to, http.StatusFound)
}

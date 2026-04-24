// Package mockdeep is a deliberately dumb HTTP server used to exercise
// agent-deepweb end-to-end. Every auth style is represented by a distinct
// endpoint that accepts exactly one fixed credential, so tests can assert
// that agent-deepweb attached the right thing.
//
// The hardcoded "valid" credential values are exported as constants
// (auth_handlers.go) so tests can reference them symbolically rather
// than copying strings.
//
// File layout:
//
//	server.go         Server struct, routing, generic helpers, index/healthz
//	auth_handlers.go  /whoami, /basic, /api-key, /login, /session, /token-protected, /graphql + canonical creds
//	util_handlers.go  /headers, /echo, /status, /slow, /large, /redirect
package mockdeep

import (
	"encoding/json"
	"net/http"
	"sync"
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

// ---------- generic helpers shared by all handler files ----------

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

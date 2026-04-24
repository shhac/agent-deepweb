package mockdeep

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Canonical valid values. Tests should reference these; humans who run
// the server interactively can read them from `mockdeep -creds`.
const (
	ValidBearerToken = "valid-token-bearer"
	ValidAPIKey      = "valid-api-key"
	APIKeyHeader     = "X-API-Key"
	ValidUsername    = "alice"
	ValidPassword    = "wonderland"
	LoginToken       = "login-issued-token-xyz" // returned by /login, accepted by /token-protected
	SessionCookie    = "sess-abc123"            // Set-Cookie by /login, accepted by /session
)

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


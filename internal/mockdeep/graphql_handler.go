package mockdeep

import (
	"encoding/json"
	"net/http"
	"strings"
)

// graphql: 200 OK with `{data, errors}` envelope. Unauthenticated
// errors come back as a GraphQL error with extensions.code =
// UNAUTHENTICATED so agent-deepweb can classify them as
// fixable_by:human.
//
// Cohabits with graphql_introspection.go (the canned __schema
// response builder). Kept separate from auth_handlers.go because
// GraphQL is a protocol, not an auth style — the distinction matters
// once the mock starts supporting mutations or subscriptions that
// also need their own handlers.
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

	// Introspection and `ping` are unauthenticated by convention —
	// clients use them to discover the schema + check liveness before
	// they have a token. `me` still requires the bearer.
	if strings.Contains(body.Query, "__schema") {
		writeJSON(w, http.StatusOK, introspectionResponse())
		return
	}
	if strings.Contains(body.Query, "ping") {
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"ping": "pong"}})
		return
	}
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
	default:
		writeJSON(w, http.StatusOK, map[string]any{
			"errors": []map[string]any{{
				"message": "unknown query — try '{ me { id name } }' or '{ ping }'",
			}},
		})
	}
}

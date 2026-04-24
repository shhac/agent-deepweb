package mockdeep

import (
	"encoding/json"
	"io"
	"net/http"
)

// jsonrpc implements a minimal JSON-RPC 2.0 endpoint mirroring the
// spec's §5.1 error shape, so agent-deepweb's classifyRPC can be
// exercised end-to-end against real wire responses rather than
// hand-crafted ones.
//
// Methods:
//
//	echo    returns the params verbatim as result.
//	add     takes two ints in an array, returns their sum.
//	whoami  auth-guarded: requires Authorization: Bearer <ValidBearerToken>
//	        — lets tests confirm that the jsonrpc verb attaches profile
//	        auth headers correctly over POST.
//
// Anything else returns -32601 (Method not found).
// A malformed JSON body returns -32700 (Parse error).
// Missing "method" or non-"2.0" jsonrpc returns -32600 (Invalid Request).
//
// Notifications (requests with no id) return HTTP 204 with no body,
// which is the spec-sanctioned shape.
func (s *Server) jsonrpc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	raw, _ := io.ReadAll(r.Body)

	// Parse permissively first so we can distinguish "bad JSON" from
	// "JSON but bad RPC envelope".
	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
		ID      json.RawMessage `json:"id"` // nil raw = notification
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		rpcErr(w, nil, -32700, "Parse error: "+err.Error())
		return
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		rpcErr(w, req.ID, -32600, "Invalid Request")
		return
	}

	// Notification: no id field → the client doesn't expect a reply.
	if len(req.ID) == 0 || string(req.ID) == "null" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch req.Method {
	case "echo":
		// Result is whatever the caller sent as params.
		var p any
		_ = json.Unmarshal(req.Params, &p)
		rpcOK(w, req.ID, p)
	case "add":
		var pair []int
		if err := json.Unmarshal(req.Params, &pair); err != nil || len(pair) != 2 {
			rpcErr(w, req.ID, -32602, "Invalid params: expected [int,int]")
			return
		}
		rpcOK(w, req.ID, pair[0]+pair[1])
	case "whoami":
		if r.Header.Get("Authorization") != "Bearer "+ValidBearerToken {
			// -32001 is in the reserved "server error" range per spec.
			rpcErr(w, req.ID, -32001, "Unauthenticated")
			return
		}
		rpcOK(w, req.ID, map[string]any{"user": ValidUsername})
	default:
		rpcErr(w, req.ID, -32601, "Method not found: "+req.Method)
	}
}

// rpcOK writes a JSON-RPC 2.0 success envelope. The id field is
// round-tripped as bytes so we don't mis-coerce a string id to a
// number or vice versa.
func rpcOK(w http.ResponseWriter, id json.RawMessage, result any) {
	resRaw, _ := json.Marshal(result)
	out := map[string]json.RawMessage{
		"jsonrpc": json.RawMessage(`"2.0"`),
		"id":      id,
		"result":  resRaw,
	}
	writeJSON(w, http.StatusOK, out)
}

// rpcErr writes a JSON-RPC 2.0 error envelope.
func rpcErr(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	if id == nil {
		id = json.RawMessage("null")
	}
	errObj := map[string]any{"code": code, "message": message}
	out := map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"error":   errObj,
	}
	writeJSON(w, http.StatusOK, out)
}

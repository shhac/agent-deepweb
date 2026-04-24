package jsonrpc

import (
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

// classifyRPC maps standard JSON-RPC 2.0 error codes (§5.1 of the spec)
// to fixable_by hints. The LLM can correct parse/method/params issues
// itself; internal errors and server-defined codes go to the human
// because there's nothing an agent can change at the wire level.
//
// Standard codes:
//
//	-32700 Parse error       — invalid JSON we sent. Agent-fixable.
//	-32600 Invalid Request   — malformed Request object. Agent.
//	-32601 Method not found  — wrong method name / discovery miss. Agent.
//	-32602 Invalid params    — param shape/type wrong. Agent.
//	-32603 Internal error    — server-side. Human (or retry).
//	-32000 .. -32099         — server-defined (implementation-specific).
//	                           Default to human since the semantics are
//	                           application-level.
func classifyRPC(code int, message string) *agenterrors.APIError {
	switch code {
	case -32700:
		return agenterrors.Newf(agenterrors.FixableByAgent,
			"JSON-RPC parse error (-32700): %s", message).
			WithHint("The server could not parse the JSON we sent. Check --params is valid JSON.")
	case -32600:
		return agenterrors.Newf(agenterrors.FixableByAgent,
			"JSON-RPC invalid request (-32600): %s", message).
			WithHint("The Request object is malformed. Verify --method and --params shape.")
	case -32601:
		return agenterrors.Newf(agenterrors.FixableByAgent,
			"JSON-RPC method not found (-32601): %s", message).
			WithHint("The method name is not recognised by this server. Check the server's RPC docs or use a discovery call if available (e.g. rpc_modules for Ethereum).")
	case -32602:
		return agenterrors.Newf(agenterrors.FixableByAgent,
			"JSON-RPC invalid params (-32602): %s", message).
			WithHint("Server rejected --params. Re-check the expected param shape (order, types, object vs array).")
	case -32603:
		return agenterrors.Newf(agenterrors.FixableByHuman,
			"JSON-RPC internal error (-32603): %s", message).
			WithHint("Server-side failure. Usually transient; may be worth a retry. If it persists, ask the user.")
	}
	// Reserved server-defined range: -32000..-32099
	if code <= -32000 && code >= -32099 {
		return agenterrors.Newf(agenterrors.FixableByHuman,
			"JSON-RPC server error (%d): %s", code, message).
			WithHint("Application-specific error code. Consult the server's documentation for this code's meaning.")
	}
	// Anything else is application-defined (spec allows codes outside
	// the reserved range). No pattern to key on, punt to human.
	return agenterrors.Newf(agenterrors.FixableByHuman,
		"JSON-RPC error (%d): %s", code, message).
		WithHint("Non-standard error code. Consult the server's documentation.")
}

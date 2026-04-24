package jsonrpc

import (
	"strings"
	"testing"

	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

func TestClassifyRPC(t *testing.T) {
	cases := []struct {
		name    string
		code    int
		message string
		want    agenterrors.FixableBy
		hintHas string
	}{
		{"parse error → agent", -32700, "bad json", agenterrors.FixableByAgent, "valid JSON"},
		{"invalid request → agent", -32600, "no method", agenterrors.FixableByAgent, "Request object"},
		{"method not found → agent", -32601, "nope", agenterrors.FixableByAgent, "not recognised"},
		{"invalid params → agent", -32602, "wrong shape", agenterrors.FixableByAgent, "Re-check"},
		{"internal error → human (transient-ish)", -32603, "oops", agenterrors.FixableByHuman, "Server-side"},
		{"server-defined range low → human", -32000, "app err", agenterrors.FixableByHuman, "Application-specific"},
		{"server-defined range high → human", -32099, "app err", agenterrors.FixableByHuman, "Application-specific"},
		{"non-standard code → human", 42, "app err", agenterrors.FixableByHuman, "Non-standard"},
		{"non-standard negative outside reserved → human", -32100, "app err", agenterrors.FixableByHuman, "Non-standard"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ae := classifyRPC(tc.code, tc.message)
			if ae.FixableBy != tc.want {
				t.Errorf("code=%d FixableBy=%s, want %s", tc.code, ae.FixableBy, tc.want)
			}
			if tc.hintHas != "" && !strings.Contains(ae.Hint, tc.hintHas) {
				t.Errorf("code=%d hint missing %q: %q", tc.code, tc.hintHas, ae.Hint)
			}
			if !strings.Contains(ae.Error(), tc.message) {
				t.Errorf("error message should include server message %q: %q", tc.message, ae.Error())
			}
		})
	}
}

// TestBuildPayload_CoreShapes — the wire shape the server sees must
// match spec §4: jsonrpc="2.0", method string, id integer by default,
// params only when provided, id absent when --notify.
func TestBuildPayload_CoreShapes(t *testing.T) {
	t.Run("minimal call: method only", func(t *testing.T) {
		b, err := buildPayload(&opts{method: "eth_blockNumber", id: "1"})
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		for _, want := range []string{`"jsonrpc":"2.0"`, `"method":"eth_blockNumber"`, `"id":1`} {
			if !strings.Contains(s, want) {
				t.Errorf("body missing %q: %s", want, s)
			}
		}
		if strings.Contains(s, `"params"`) {
			t.Errorf("no --params → body must not carry a params key: %s", s)
		}
	})

	t.Run("params as array", func(t *testing.T) {
		b, err := buildPayload(&opts{method: "eth_getBalance", params: `["0xabc","latest"]`, id: "7"})
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		if !strings.Contains(s, `"params":["0xabc","latest"]`) {
			t.Errorf("params not preserved as array: %s", s)
		}
		if !strings.Contains(s, `"id":7`) {
			t.Errorf("numeric id should coerce: %s", s)
		}
	})

	t.Run("params as object", func(t *testing.T) {
		b, err := buildPayload(&opts{method: "x", params: `{"k":"v"}`, id: "1"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `"params":{"k":"v"}`) {
			t.Errorf("object params not preserved: %s", b)
		}
	})

	t.Run("notify: no id", func(t *testing.T) {
		b, err := buildPayload(&opts{method: "ping", id: "1", notify: true})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), `"id"`) {
			t.Errorf("--notify must not emit an id field: %s", b)
		}
	})

	t.Run("string id preserved when non-numeric", func(t *testing.T) {
		b, err := buildPayload(&opts{method: "x", id: "req-abc"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `"id":"req-abc"`) {
			t.Errorf("non-numeric id should be a JSON string: %s", b)
		}
	})

	t.Run("invalid params JSON → fixable_by:agent", func(t *testing.T) {
		_, err := buildPayload(&opts{method: "x", params: `not-json`, id: "1"})
		if err == nil {
			t.Fatal("expected parse error")
		}
		var apiErr *agenterrors.APIError
		if !asAPIError(err, &apiErr) {
			t.Fatalf("want APIError, got %T: %v", err, err)
		}
		if apiErr.FixableBy != agenterrors.FixableByAgent {
			t.Errorf("want fixable_by=agent, got %q", apiErr.FixableBy)
		}
	})
}

// asAPIError is a local errors.As shim — keeps this test file free of
// the extra import line (the APIError type already appears here).
func asAPIError(err error, target **agenterrors.APIError) bool {
	if ae, ok := err.(*agenterrors.APIError); ok {
		*target = ae
		return true
	}
	return false
}

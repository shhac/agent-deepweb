package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shhac/agent-deepweb/internal/api"
	"github.com/shhac/agent-deepweb/internal/config"
	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/mockdeep"
)

// setup isolates the config dir so audit entries from one test don't
// spill into the next. Returns the mockdeep test server URL.
func setup(t *testing.T) string {
	t.Helper()
	config.SetConfigDir(t.TempDir())
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	srv := httptest.NewServer(mockdeep.New())
	t.Cleanup(srv.Close)
	return srv.URL
}

// doJSONRPC is a tiny shim that packages what buildPayload + api.Do
// do in the real verb, so these e2e tests don't have to drive the
// cobra surface (which would tangle them with stdout capture etc).
// It exercises the same wire format the CLI produces.
func doJSONRPC(t *testing.T, url string, auth *credential.Resolved, o *opts) (*api.Response, *rpcResponse, error) {
	t.Helper()
	body, err := buildPayload(o)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := api.Do(ctx, api.Request{
		Method:  "POST",
		URL:     url,
		Headers: map[string]string{"Content-Type": "application/json", "Accept": "application/json"},
		Body:    bytes.NewReader(body),
		Auth:    auth,
	}, api.ClientOptions{Timeout: 5 * time.Second, MaxBytes: 1 << 20, FollowRedirects: true})
	if err != nil && resp == nil {
		return nil, nil, err
	}
	var parsed rpcResponse
	if resp != nil && len(resp.Body) > 0 {
		_ = json.Unmarshal(resp.Body, &parsed)
	}
	return resp, &parsed, err
}

// TestJSONRPC_E2E_Echo — happy-path round trip. `echo` returns the
// params verbatim as result; the response must carry jsonrpc:"2.0" +
// the id we sent + a result equal to the params we sent.
func TestJSONRPC_E2E_Echo(t *testing.T) {
	base := setup(t)
	_, parsed, err := doJSONRPC(t, base+"/jsonrpc", nil, &opts{
		method: "echo",
		params: `["hello",42]`,
		id:     "7",
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if parsed.Error != nil {
		t.Fatalf("unexpected error: %+v", parsed.Error)
	}
	if string(parsed.Result) == "" {
		t.Fatal("empty result")
	}
	var got []any
	if err := json.Unmarshal(parsed.Result, &got); err != nil {
		t.Fatalf("result not an array: %s", parsed.Result)
	}
	if len(got) != 2 || got[0] != "hello" {
		t.Errorf("round-trip mismatch: %v", got)
	}
}

// TestJSONRPC_E2E_MethodNotFound — unknown method should land the
// client-side -32601 classification. Locks in the spec-code mapping
// against a real server response, not just a unit test.
func TestJSONRPC_E2E_MethodNotFound(t *testing.T) {
	base := setup(t)
	_, parsed, err := doJSONRPC(t, base+"/jsonrpc", nil, &opts{
		method: "no_such_method",
		id:     "1",
	})
	if err != nil {
		t.Fatalf("transport error unexpected here (RPC errors ride on HTTP 200): %v", err)
	}
	if parsed.Error == nil {
		t.Fatal("expected RPC error for unknown method")
	}
	if parsed.Error.Code != -32601 {
		t.Errorf("want code=-32601, got %d", parsed.Error.Code)
	}
	// The CLI layer would run classifyRPC on this — verify it maps to agent-fixable.
	ae := classifyRPC(parsed.Error.Code, parsed.Error.Message)
	if ae.FixableBy != agenterrors.FixableByAgent {
		t.Errorf("method-not-found should be agent-fixable, got %q", ae.FixableBy)
	}
}

// TestJSONRPC_E2E_InvalidParams — `add` with wrong arity surfaces
// -32602. Exercises the server's param validation + the classifier.
func TestJSONRPC_E2E_InvalidParams(t *testing.T) {
	base := setup(t)
	_, parsed, err := doJSONRPC(t, base+"/jsonrpc", nil, &opts{
		method: "add",
		params: `[1]`, // needs 2
		id:     "1",
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if parsed.Error == nil || parsed.Error.Code != -32602 {
		t.Fatalf("want -32602, got %+v", parsed.Error)
	}
}

// TestJSONRPC_E2E_AuthedMethod — the `whoami` method returns -32001
// when no Bearer is attached and a result when a matching bearer
// profile is used. Confirms that the jsonrpc verb's profile plumbing
// reaches the server over POST (not just GET).
func TestJSONRPC_E2E_AuthedMethod(t *testing.T) {
	base := setup(t)

	t.Run("unauthenticated returns server error", func(t *testing.T) {
		_, parsed, err := doJSONRPC(t, base+"/jsonrpc", nil, &opts{method: "whoami", id: "1"})
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Error == nil || parsed.Error.Code != -32001 {
			t.Errorf("want -32001 whoami, got %+v", parsed.Error)
		}
	})

	t.Run("bearer profile succeeds", func(t *testing.T) {
		auth := testBearerProfile(t, base)
		_, parsed, err := doJSONRPC(t, base+"/jsonrpc", auth, &opts{method: "whoami", id: "1"})
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Error != nil {
			t.Fatalf("auth should have succeeded: %+v", parsed.Error)
		}
		var user map[string]any
		if err := json.Unmarshal(parsed.Result, &user); err != nil {
			t.Fatalf("result shape: %s", parsed.Result)
		}
		if user["user"] != mockdeep.ValidUsername {
			t.Errorf("whoami result: %+v", user)
		}
	})
}

// TestJSONRPC_E2E_Notify — --notify sends no id and the server
// replies with HTTP 204, empty body. Verifies the CLI's coerceID
// skip logic + server's notification handling are in lockstep.
func TestJSONRPC_E2E_Notify(t *testing.T) {
	base := setup(t)
	resp, _, err := doJSONRPC(t, base+"/jsonrpc", nil, &opts{
		method: "echo",
		params: `["fire-and-forget"]`,
		notify: true,
	})
	if err != nil {
		// 204 with no body is valid — api.Do may surface this as non-nil
		// resp; allow either as long as resp.Status == 204.
		if resp == nil {
			t.Fatalf("unexpected transport error: %v", err)
		}
	}
	if resp == nil || resp.Status != 204 {
		t.Errorf("notify should yield 204, got %v", resp)
	}
}

// testBearerProfile registers a bearer profile whose domain matches
// the mockdeep test server, so api.Do's allowlist check passes.
func testBearerProfile(t *testing.T, serverURL string) *credential.Resolved {
	t.Helper()
	// Parse the host out of the test-server URL.
	host := serverURL
	for _, prefix := range []string{"http://", "https://"} {
		if len(host) >= len(prefix) && host[:len(prefix)] == prefix {
			host = host[len(prefix):]
			break
		}
	}
	return &credential.Resolved{
		Credential: credential.Credential{
			Name:      "mock-bearer",
			Type:      credential.AuthBearer,
			Domains:   []string{host},
			AllowHTTP: true,
		},
		Secrets: credential.Secrets{Token: mockdeep.ValidBearerToken},
	}
}

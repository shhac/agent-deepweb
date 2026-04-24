package importers

import (
	"github.com/shhac/agent-deepweb/internal/template"

	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shhac/agent-deepweb/internal/api"
	"github.com/shhac/agent-deepweb/internal/credential"
	"github.com/shhac/agent-deepweb/internal/mockdeep"
)

// TestGraphQL_E2E_IntrospectionAndRun — the full introspection →
// import → run round trip. The test POSTs our canonical introspection
// query to mockdeep, parses the response into the schema IR, builds
// templates for `me` and `ping`, and runs each one against the same
// server. This is the single integration test that proves
// ParseGraphQLSchema + BuildTemplates + the resulting template shape
// all line up with a real GraphQL endpoint's wire format.
func TestGraphQL_E2E_IntrospectionAndRun(t *testing.T) {
	base := setupMockdeep(t)

	// Step 1: fetch the schema via mockdeep's graphql handler.
	introspection := postGraphQL(t, base+"/graphql", nil, map[string]any{
		"query": IntrospectionQuery,
	})

	schema, err := ParseGraphQLSchema(introspection)
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	if schema.QueryTypeName != "Query" {
		t.Fatalf("queryType: %q", schema.QueryTypeName)
	}

	// Step 2: build templates. mockdeep exposes `me` (needs auth) and
	// `ping` (does not) — both become one template each.
	tpls, err := schema.BuildTemplates(base+"/graphql", ImportGraphQLOptions{
		Prefix: "mg",
	})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]template.Template{}
	for _, tpl := range tpls {
		byName[tpl.Name] = tpl
	}
	if _, ok := byName["mg.me"]; !ok {
		t.Fatalf("mg.me missing from %v", namesList(tpls))
	}
	if _, ok := byName["mg.ping"]; !ok {
		t.Fatalf("mg.ping missing from %v", namesList(tpls))
	}

	// Step 3: run the unauthenticated `ping` field. The template's
	// body is `query Ping { ping { __typename } }` — mockdeep's
	// substring-matcher routes any query containing "ping" to
	// result=pong, so we expect a data.ping result.
	t.Run("ping (unauthed) succeeds", func(t *testing.T) {
		tpl := byName["mg.ping"]
		resp := runGraphQLTemplate(t, &tpl, nil, nil)
		if resp.Status != 200 {
			t.Fatalf("status %d: %s", resp.Status, resp.Body)
		}
		// Must NOT contain the UNAUTHENTICATED error — the introspected
		// doc + params shouldn't accidentally land an empty query.
		if strings.Contains(string(resp.Body), "unauthenticated") {
			t.Errorf("ping should not trip auth: %s", resp.Body)
		}
	})

	// Step 4: `me` requires a bearer. Call it with mockdeep's canonical
	// bearer profile; expect a `data.me` payload.
	t.Run("me with bearer profile", func(t *testing.T) {
		tpl := byName["mg.me"]
		auth := bearerProfile(t, base)
		resp := runGraphQLTemplate(t, &tpl, nil, auth)
		if resp.Status != 200 {
			t.Fatalf("status %d: %s", resp.Status, resp.Body)
		}
		// mockdeep matches the substring "me" in the query, so any
		// template doc containing "me" routes to the me resolver.
		var env map[string]any
		if err := json.Unmarshal(resp.Body, &env); err != nil {
			t.Fatalf("envelope parse: %v — %s", err, resp.Body)
		}
		if errs, ok := env["errors"].([]any); ok && len(errs) > 0 {
			t.Errorf("unexpected graphql errors: %+v", errs)
		}
	})
}

// runGraphQLTemplate executes a template whose body is a GraphQL POST.
// Lives beside the OpenAPI runner in this file rather than in a shared
// helper because the GraphQL case needs the specific Content-Type and
// isn't driven by body_format alone.
func runGraphQLTemplate(t *testing.T, tpl *template.Template, rawParams map[string]string, auth *credential.Resolved) *api.Response {
	t.Helper()
	typed, err := tpl.Validate(rawParams)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	body, err := template.SubstituteBody(tpl.BodyTemplate, typed)
	if err != nil {
		t.Fatalf("substitute body: %v", err)
	}
	headers := map[string]string{"Content-Type": "application/json", "Accept": "application/json"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := api.Do(ctx, api.Request{
		Method:  tpl.Method,
		URL:     tpl.URL,
		Headers: headers,
		Body:    bytes.NewReader(body),
		Auth:    auth,
	}, api.ClientOptions{Timeout: 5 * time.Second, MaxBytes: 1 << 20, FollowRedirects: true})
	if err != nil && resp == nil {
		t.Fatalf("api.Do: %v", err)
	}
	return resp
}

// postGraphQL is the helper for step 1 — a single authenticated-or-
// not POST of a GraphQL document. We can't use runGraphQLTemplate
// because we haven't built the template yet.
func postGraphQL(t *testing.T, url string, auth *credential.Resolved, payload map[string]any) []byte {
	t.Helper()
	body, _ := json.Marshal(payload)
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
		t.Fatalf("introspection POST: %v", err)
	}
	return resp.Body
}

// bearerProfile builds a bearer credential scoped to the test server.
// Parallels openapi_e2e_test's anonProfile but attaches the bearer
// token mockdeep expects on `me`.
func bearerProfile(t *testing.T, base string) *credential.Resolved {
	t.Helper()
	host := strings.TrimPrefix(strings.TrimPrefix(base, "http://"), "https://")
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

// namesList is a local shim so this file doesn't import from the
// graphql_test.go file (same package, but the helper lives in a
// _test file which can't be cross-referenced outside tests anyway —
// this keeps the file self-contained).
func namesList(ts []template.Template) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Name
	}
	return out
}


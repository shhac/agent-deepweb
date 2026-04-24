package mockdeep

import (
	"net/http"
	"strings"
)

// openapiSpec serves a static OpenAPI v3 spec describing mockdeep's
// handlers. The `servers[0].url` is rewritten at request time to match
// the host the client reached us on, so tests running against
// httptest.NewServer() (random port) get a spec they can import and
// run without manual patching.
//
// The spec intentionally covers a narrow slice of mockdeep: enough to
// exercise import-openapi's core translation (path params, query
// params, headers, JSON request bodies) but not so much that the
// spec itself becomes a test-maintenance burden.
func (s *Server) openapiSpec(w http.ResponseWriter, r *http.Request) {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	base := scheme + "://" + r.Host
	body := strings.ReplaceAll(openapiSpecTemplate, "__SERVER_URL__", base)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

const openapiSpecTemplate = `{
  "openapi": "3.0.3",
  "info": { "title": "mockdeep", "version": "1.0.0" },
  "servers": [{"url": "__SERVER_URL__"}],
  "paths": {
    "/healthz": {
      "get": {
        "operationId": "healthz",
        "summary": "Liveness check"
      }
    },
    "/echo": {
      "post": {
        "operationId": "echo",
        "summary": "Echo the request back as JSON",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": { "schema": { "type": "object" } }
          }
        }
      }
    },
    "/status/{code}": {
      "get": {
        "operationId": "status",
        "summary": "Return the given HTTP status code",
        "parameters": [
          { "name": "code", "in": "path", "required": true, "schema": { "type": "integer" } }
        ]
      }
    },
    "/headers": {
      "get": {
        "operationId": "headers",
        "summary": "Echo request headers",
        "parameters": [
          { "name": "X-Trace-ID", "in": "header", "schema": { "type": "string" } }
        ]
      }
    }
  }
}`

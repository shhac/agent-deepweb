package fetch

const usageText = `fetch — authenticated HTTP request

USAGE
  agent-deepweb fetch <url> [flags]

SUMMARY
  Performs an HTTP request with auth attached from a saved credential.
  The credential is resolved by --auth <name>, or by matching the URL's
  host against the domain allowlist of saved credentials. If a credential
  applies to the host but you don't want auth, pass --no-auth.

OUTPUT
  By default emits a JSON envelope:
    {
      "status":       200,
      "status_text":  "200 OK",
      "url":          "https://...",
      "auth":         "myapi" or null,
      "headers":      { ... redacted ... },
      "content_type": "application/json",
      "truncated":    false,
      "body":         <decoded JSON> | <string>
    }
  With --format raw, the response body is written directly to stdout.
  With --format text, a short header precedes the body.

FLAGS
  --auth <name>                  Credential alias (else resolved by host)
  --no-auth                      Skip credential attachment even if a match exists
  --method GET|POST|...          HTTP method (default: GET, or POST if body given)
  --header 'K: V'                Extra request header (repeatable; no secrets)
  --query key=value              URL query parameter (repeatable)
  --data <string|@file|@->       Raw request body
  --json <string|@file|@->       JSON body; sets Content-Type: application/json
  --form key=value               Form body field (repeatable); sets x-www-form-urlencoded
  --timeout <ms>                 Per-request timeout
  --max-size <bytes>             Cap response body size (default 10 MiB)
  --follow-redirects             Follow redirects (default: true)
  --format json|jsonl|raw|text   Output format (default json)
  --no-redact                    HUMAN-ONLY: print headers/body unredacted

EXAMPLES
  # GET with auto-resolved credential
  agent-deepweb fetch https://api.example.com/v1/me

  # Explicit credential
  agent-deepweb fetch https://api.example.com/v1/me --auth myapi

  # POST JSON
  agent-deepweb fetch https://api.example.com/v1/items \
    --method POST --json '{"name":"widget"}' --auth myapi

  # Raw body (for piping to jq, for instance)
  agent-deepweb fetch https://api.example.com/v1/me --format raw | jq .

ERRORS
  fixable_by: agent   URL malformed, wrong method, body too large, bad JSON
  fixable_by: human   Credential not on allowlist, 401/403, session expired
  fixable_by: retry   Network error, 429, 5xx, timeout
`

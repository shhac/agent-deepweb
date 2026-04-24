package fetch

const usageText = `fetch — authenticated HTTP request

USAGE
  agent-deepweb fetch <url> [flags]

SUMMARY
  Performs an HTTP request with auth attached from a saved profile.
  The profile is resolved by --profile <name>, or by matching the URL's
  host against the domain allowlist of saved profiles. If a profile
  applies to the host but you don't want auth, pass --profile none
  (the explicit anonymous opt-in).

OUTPUT
  By default emits a JSON envelope:
    {
      "status":       200,
      "status_text":  "200 OK",
      "url":          "https://...",
      "profile":      "myapi" or null,
      "headers":      { ... redacted ... },
      "content_type": "application/json",
      "truncated":    false,
      "body":         <decoded JSON> | <string>
    }
  With --format raw, the response body is written directly to stdout.
  With --format text, a short header precedes the body.

FLAGS
  --profile <name|none>          Profile alias, or 'none' for anonymous
  --cookiejar <path>             Bring-your-own plaintext cookie jar; overrides
                                 the profile's encrypted default. Use with
                                 --profile none for novel LLM-authored flows.
  --method GET|POST|...          HTTP method (default: GET, or POST if body given)
  --header 'K: V'                Extra request header (repeatable; no secrets)
  -H 'K: V'                      Short form for --header
  --query key=value              URL query parameter (repeatable)
  --data <string|@file|@->       Raw request body
  --json <string|@file|@->       JSON body; sets Content-Type: application/json
  --form key=value               Form body field (repeatable); x-www-form-urlencoded
                                 OR, combined with --file, a text part in multipart
  --file field=@path[;type=MIME][;filename=NAME]
                                 Multipart file part (repeatable); sets
                                 multipart/form-data. Mix with --form for text parts.
  --timeout <ms>                 Per-request timeout
  --max-size <bytes>             Cap response body size (default 10 MiB)
  --follow-redirects             Follow redirects (default: true)
  --format json|jsonl|raw|text   Output format (default json)
  --user-agent <s>               Per-request UA; beats profile UA and config default
  -A <s>                         Short form for --user-agent
  --track                        Persist a full redacted request/response record;
                                 envelope gains audit_id. Retrieve later with
                                 'agent-deepweb audit show <id>'.
  --hide-request                 Drop the 'request' block from the envelope
  --hide-response                Drop response headers/body; keep status + profile
                                 + audit_id (useful for "did it work?" calls)

EXAMPLES
  # GET with auto-resolved profile
  agent-deepweb fetch https://api.example.com/v1/me

  # Explicit profile
  agent-deepweb fetch https://api.example.com/v1/me --profile myapi

  # POST JSON
  agent-deepweb fetch https://api.example.com/v1/items \
    --method POST --json '{"name":"widget"}' --profile myapi

  # Raw body (for piping to jq, for instance)
  agent-deepweb fetch https://api.example.com/v1/me --format raw | jq .

  # Anonymous fetch
  agent-deepweb fetch https://example.com/healthz --profile none

  # Bring-your-own jar (LLM owns the credentials)
  agent-deepweb fetch https://test.example.com/login --profile none \
    --cookiejar /tmp/test.json --method POST \
    --json '{"username":"...","password":"..."}'

ERRORS
  fixable_by: agent   URL malformed, wrong method, body too large, bad JSON
  fixable_by: human   Profile not on allowlist, 401/403, session expired
  fixable_by: retry   Network error, 429, 5xx, timeout
`

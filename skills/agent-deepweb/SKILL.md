---
name: agent-deepweb
description: |
  Authenticated HTTP fetcher for AI agents — like curl/wget but for hosts that need a Bearer
  token, basic auth, or session cookie. Credentials are stored by the user under names;
  the tool attaches them to outgoing requests. The agent references credentials by name
  and never sees secret values. Responses are redacted. Use when:
  - Fetching a page, JSON API, or GraphQL endpoint that rejects anonymous requests
  - Calling an internal API that needs a Bearer token
  - Sending a GraphQL query against an authenticated endpoint
  - Listing which credentials are available (not their values)
  Triggers: "authenticated fetch", "bearer token", "graphql", "login wall", "protected url",
    "curl with auth", "api key request", "auth'd request", "https with cookie", "session api".
tools:
  allowed:
    - Bash
    - Read
    - Grep
    - Glob
---

# agent-deepweb — authenticated HTTP for AI agents

`agent-deepweb` is a `curl`-with-auth on `$PATH`. The user stores credentials under
names; you reference them by name. You never see the secret values.

Structured JSON goes to stdout. Errors are JSON on stderr with `fixable_by`:

```
{ "error": "...", "hint": "...", "fixable_by": "agent|human|retry" }
```

## When to use

- User asks you to fetch a URL that requires authentication.
- User asks for data from a GraphQL API behind a Bearer token.
- User asks what authenticated services are configured.

## Process

### 1. Check what credentials exist

```bash
agent-deepweb creds list             # names, auth type, allowed domains
agent-deepweb creds show <name>      # metadata (no secret values)
```

### 2. Make the request

```bash
# Auto-resolve credential by URL host
agent-deepweb fetch https://api.example.com/v1/me

# Explicit
agent-deepweb fetch https://api.example.com/v1/me --auth myapi

# GraphQL
agent-deepweb graphql https://api.example.com/graphql \
  --auth myapi \
  --query 'query { me { id } }' \
  --variables '{"limit":10}'

# POST JSON
agent-deepweb fetch https://api.example.com/v1/items \
  --method POST --json '{"name":"x"}' --auth myapi
```

### 3. Read the output

Fetch returns a JSON envelope:
```json
{
  "status": 200,
  "status_text": "200 OK",
  "url": "https://...",
  "auth": "myapi",
  "headers": { "...redacted authorization/cookie/token headers..." },
  "content_type": "application/json",
  "truncated": false,
  "body": { "...decoded JSON response..." }
}
```

Use `--format raw` when piping the body to another tool.

### 4. Handle errors by `fixable_by`

- `agent` — you made a mistake (bad URL, 404, malformed JSON body, multiple credentials match). Fix and retry.
- `human` — user action required (missing credential, allowlist mismatch, 401/403, expired session). Tell the user what to run:
  - `agent-deepweb creds add <name> --type bearer --token ... --domain <host>`
  - `agent-deepweb creds allow <name> <host>`
  - `agent-deepweb login <name>`
- `retry` — transient (429, 5xx, network). Retry once with backoff. Respect `Retry-After` when present.

## What you cannot do

These commands are **human-only** and will refuse in agent mode (they write or reveal secrets):
- `creds add`, `creds remove`, `creds allow`, `creds disallow`, `creds set-health`
- `login`, `session clear`
- `fetch --no-redact`, `graphql --no-redact`

If a task needs one of these, stop and tell the user the exact command to run. Don't try to work around the refusal (e.g. by writing shell scripts that pipe values).

## Safety guarantees you can rely on

- `creds list` and `creds show` never print secret values.
- Credentials only apply on their allowlisted domains. If a URL's host isn't on the list, the fetch fails with `fixable_by: human` (ask the user to run `creds allow`).
- Response headers matching `authorization|cookie|set-cookie|x-*-token|api-*-key` are replaced with `<redacted>`.
- JSON response fields whose names contain `access_token|refresh_token|client_secret|password|api_key|bearer` are replaced with `<redacted>`.
- Max response size is capped (default 10 MiB). Oversized responses return `fixable_by: agent` so you can narrow the request.

## Detailed reference

Run these for per-command help with examples:
```bash
agent-deepweb llm-help
agent-deepweb fetch llm-help
agent-deepweb graphql llm-help
agent-deepweb creds llm-help
agent-deepweb login llm-help
```

## Setup (first-time, human runs this)

```bash
# Bearer token (most common)
agent-deepweb creds add github --type bearer --token ghp_xxx --domain api.github.com

# Basic auth
agent-deepweb creds add intranet --type basic --username alice --password 'pw' \
  --domain intranet.example.com

# Optional: health URL for 'creds test <name>'
agent-deepweb creds set-health github https://api.github.com/user
agent-deepweb creds test github
```

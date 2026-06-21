# agent-deepweb v1 Design

## Problem

LLMs need to fetch resources that require authentication — GraphQL endpoints with Bearer tokens,
admin dashboards behind cookie-based logins, internal APIs with basic auth. Today the options are:

1. Put the token in the LLM's context → the LLM can leak it into logs, commits, chat transcripts.
2. Put the token in an env var → the LLM can still read it via `echo $TOKEN`.
3. Ask the user to run every request manually → defeats the point of an agent.

`agent-deepweb` is a `curl`-with-auth that the LLM can use without ever seeing the secrets.
The user owns credentials; the binary performs substitution at request time; the LLM only ever
references credentials by name.

## Goals

- LLM-safe: the model cannot read, print, or exfiltrate stored secret values.
- Domain-scoped: a credential only applies to its allowlisted domains; requests outside the
  allowlist fail loudly.
- Fixable-by hints: every error explains whether the LLM should retry, fix syntax, or ask the
  human for help.
- Ergonomic: feels like `curl` — `agent-deepweb fetch <url>` is the main verb.
- Single binary, zero runtime deps (pure Go, `CGO_ENABLED=0`).

## Non-goals (v1)

- Headed browser automation / MFA flows. v1 supports Bearer, Basic, Cookie, and Form-login.
  Browser-assisted login (Playwright/chromedp) is a future extension with a designed interface
  (see "Future").
- Full scraping / HTML parsing. The output is the raw body + structured metadata; scraping
  can be done by the LLM after `fetch`.
- Multiple identities per domain. A credential is one identity. If a user has two accounts on
  a domain, they register two credentials with different names.

## LLM-safety model

There are four classes of information:

| Class | Who can read | Example |
|-------|--------------|---------|
| **Secret value** | Binary only | `Authorization: Bearer eyJ...` |
| **Credential metadata** | LLM | Name, auth type, domain allowlist, last-used timestamp |
| **Request shape** | LLM | URL, method, non-secret headers, body |
| **Response body** | LLM (redacted) | Response JSON/HTML with known-secret fields stripped |

Rules the binary enforces:

1. **Names, not values.** All LLM-facing commands reference credentials by name (`--auth <name>`)
   or resolve by URL host → credential mapping. There is no command that prints a credential's
   secret value. `creds show <name>` prints only metadata.
2. **Host allowlist.** Each credential has an explicit `domains: [host, ...]` allowlist. The
   binary checks `url.Host` against the allowlist before attaching any auth. A mismatch aborts
   the request with a `fixable_by: human` error (the LLM cannot silently "expand" a credential's
   scope — only the human can, via `creds allow <name> <domain>`).
3. **Echo protection.** Response bodies are passed through a configurable redactor that:
   - Strips response headers matching `authorization|cookie|set-cookie|x-*-token|api-?key`
     (case-insensitive) before printing.
   - Redacts JSON fields matching the same patterns in the response body (opt-out with
     `--no-redact`, which is marked human-only — see "Human-only flags" below).
   - Never substitutes the credential's secret back into the response (belt-and-braces: if
     the remote server echoes the token, we redact it on the way out).
4. **No template substitution from LLM input.** The CLI does *not* interpolate `{{secret.foo}}`
   placeholders in URLs, headers, or bodies that the LLM provides. Substitution happens only in
   the credential definition itself (which is human-authored) and only for the target domain.
   An LLM that writes `--header "X-Token: {{secrets.api.token}}"` gets a literal string, not
   substitution.
5. **Human-only flags (default).** Mutating commands (`creds add`, `creds remove`, `creds set-*`,
   `login`, `--show-secrets`, `--no-redact`) refuse by default. Humans explicitly opt out
   of the agent-mode safety rails with `AGENT_DEEPWEB_MODE=human` to run setup interactively.
   Refusals emit `fixable_by: human` with a hint telling the LLM to ask the user.

   **Rationale for fail-safe default:** an earlier iteration required
   `AGENT_DEEPWEB_MODE=agent` as explicit opt-in; that meant a skill config
   missing the env var silently granted the LLM full access. Flipped the
   polarity so the safe behaviour is default and humans opt out for setup.

## Credentials

A **credential** is a named identity for one or more hosts. It has:

```
name:     string      unique, user-chosen alias (e.g. "github-gql", "letsdothis-staging")
auth:     AuthSpec    how to attach auth (bearer / basic / cookie / form)
domains:  []string    exact-match host allowlist (e.g. "api.github.com", "*.myshopify.com")
health:   string?     optional URL path to use for `creds test <name>`
notes:    string?     human description, shown in `creds show`
```

Stored at `~/.config/agent-deepweb/credentials.json`, mode `0600`. Secret values live only in
the Keychain (macOS) or `credentials.secrets.json` (Linux/Windows, mode `0600`). The index
file stores `__KEYCHAIN__` sentinels where a secret would otherwise sit — same pattern as
`agent-statsig` / `agent-sql`.

### AuthSpec variants

```go
type AuthSpec struct {
    Type    string          // "bearer" | "basic" | "cookie" | "form" | "custom"
    Bearer  *BearerAuth
    Basic   *BasicAuth
    Cookie  *CookieAuth
    Form    *FormLogin
    Custom  *CustomAuth
}

BearerAuth { Token string; Header string /* default "Authorization" */; Prefix string /* default "Bearer " */ }
BasicAuth  { Username, Password string }
CookieAuth { Jar []SetCookie /* domain-scoped */ }
FormLogin  { URL string; Fields map[string]string /* username/password etc */; SuccessMarker string; Store SessionStore }
CustomAuth { Headers map[string]string /* raw header template, rendered with no external input */ }
```

### Sessions

Form logins produce a **session** (cookie jar + expiry) that is stored separately from the
credential. Sessions are refreshed by `login <name>`. A session can expire; the binary
notices `401`/`403` and surfaces `fixable_by: human, hint: "run 'agent-deepweb login <name>'"`
rather than retrying silently (the LLM doesn't have the password to replay the login).

Future: the binary could detect a refresh-token flow and rotate silently.

## Storage layout

```
~/.config/agent-deepweb/
  config.json                     # settings: defaults, redaction rules, timeout, max-size
  credentials.json                # index: name → {auth type, domains, metadata, sentinel}
  credentials.secrets.json        # (non-macOS only) mode 0600, holds secret values
  sessions/
    <name>.json                   # per-credential derived state (cookies, expiry), mode 0600
```

macOS Keychain service: `app.paulie.agent-deepweb`, account = credential name, payload =
JSON blob of secret fields.

## CLI surface

### Top-level

```
agent-deepweb llm-help                 Reference card for LLMs
agent-deepweb fetch <url> [flags]      HTTP request with auth
agent-deepweb graphql <url> [flags]    GraphQL POST (JSON body = {query, variables})
agent-deepweb creds <sub>              Credential management
agent-deepweb login <name>             Derive a session (form auth)
agent-deepweb session <sub>            Session lifecycle
```

### fetch

```
agent-deepweb fetch <url>
  --auth <name>              Credential alias (auto-resolves by URL host if omitted)
  --method GET|POST|...      Default: GET (POST if --data/--form given)
  --header 'K: V'            Repeatable; non-secret headers only
  --data <string|@file|@->   Request body
  --form key=value           Repeatable; x-www-form-urlencoded body
  --json <string|@file|@->   JSON body, sets Content-Type
  --query key=value          Repeatable; URL query param
  --timeout <ms>             Per-request timeout (default from config)
  --max-size <bytes>         Cap response (default 10MiB; error with fixable_by:agent if exceeded)
  --follow-redirects         Default: true; can be disabled
  --dump headers|body|both   What to include in output (default: both)
  --format raw|json|text     Response formatting
```

### graphql

Convenience wrapper over `fetch` that:
- forces `POST`, `Content-Type: application/json`, `Accept: application/json`
- accepts `--query <gql>` and `--variables <json>` and `--operation-name <name>`
- validates the response envelope (`data` vs `errors`) and surfaces GraphQL errors in the
  `errors` array with `fixable_by: agent` (since most are query-level) unless any
  `extensions.code` matches `UNAUTHENTICATED|FORBIDDEN` (→ `human`).

### creds

```
creds list                                          # names + auth type + domains (no secrets)
creds show <name>                                   # metadata only
creds test <name>                                   # sends a HEAD/GET to health URL
creds add <name> --type bearer --token <t> \        # HUMAN-ONLY
                 --domain <host> [--domain <host>]
creds add <name> --type basic --username <u> --password <p> --domain <host>
creds add <name> --type form --login-url <u> \
                 --username-field <f> --password-field <f> \
                 --username <u> --password <p> --domain <host>
creds remove <name>                                 # HUMAN-ONLY
creds allow <name> <domain>                         # HUMAN-ONLY: add to allowlist
creds disallow <name> <domain>                      # HUMAN-ONLY
creds set-health <name> <url>                       # HUMAN-ONLY
```

`creds add` prefers interactive prompts for secret fields when stdin is a TTY; the user can
also pass the flag value, which is fine for a human but discouraged (it shows up in shell
history). If the user is on macOS, the secret is offered to Keychain; a "stored in: keychain"
confirmation is printed.

### login / session

```
login <name>                    Executes the credential's form flow; stores derived session
session status <name>           Is session present? Expired? (no cookie values shown)
session clear <name>            Wipe stored session (forces re-login next request)
```

## Error hint catalog

Every error is `{error, hint, fixable_by}`. Key ones:

| Condition | fixable_by | Hint |
|-----------|-----------|------|
| Unknown credential name | agent | `Use 'agent-deepweb creds list' to see available credentials` |
| URL host not in credential allowlist | human | `Ask the user to run 'agent-deepweb creds allow <name> <host>'` |
| Multiple credentials match host, none selected | agent | `Pass --auth <name>. Candidates: x, y` |
| No credential matches host, auth required | human | `No credential for <host>. Ask the user to register one with 'creds add'` |
| 401 / 403 on authenticated request | human | `Session may be expired. Ask the user to run 'agent-deepweb login <name>'` |
| 404 | agent | `Check the URL path` |
| 429 | retry | `Rate limited. Retry after backoff` (uses `Retry-After` header if present) |
| 5xx | retry | `Upstream error — retry` |
| Network / DNS | retry | `Network error — retry` |
| Body exceeds --max-size | agent | `Response too large. Retry with --max-size <bytes> or narrow the request` |
| Invalid JSON body | agent | `--json expects valid JSON — got: <short excerpt>` |
| Credential mutation in agent mode | human | `Credential changes must be made by the user. Ask them to run 'creds add ...'` |

## Flag & env conventions

Mirrors the other agent-* tools:

| Env var | Meaning |
|---------|---------|
| `AGENT_DEEPWEB_MODE=human` | Unlock human-only commands (setup). Default is agent mode — redaction on, human-only flags refused. |
| `AGENT_DEEPWEB_CONFIG_DIR` | Override `~/.config/agent-deepweb` (for tests, multi-profile) |
| `AGENT_DEEPWEB_AUTH` | Default `--auth` value |
| `AGENT_DEEPWEB_TIMEOUT` | Default timeout in ms |

## Project layout

```
cmd/agent-deepweb/main.go            Entry point (version stamped via ldflags)
internal/
  api/
    client.go                        HTTP client wrapping net/http with auth attachment,
                                     redaction, max-size, redirect policy.
    redact.go                        Header + JSON body redaction rules.
    auth.go                          Per-AuthSpec apply() that attaches headers/cookies/body.
  cli/
    root.go                          Global flags, command registration
    usage.go                         Top-level llm-help card
    fetch/                           fetch command + usage.go
    graphql/                         graphql command + usage.go
    creds/                           creds {list,show,test,add,remove,allow,disallow,set-health} + usage.go
    login/                           login + session commands + usage.go
    shared/
      shared.go                      Credential resolution by --auth or URL host,
                                     agent-mode detection, HTTP client factory (DI).
      testhelper.go                  httptest-based helpers for CLI tests
  config/config.go                   App settings I/O (~/.config/agent-deepweb/config.json)
  credential/
    credential.go                    Index file + resolution + allowlist check
    keychain.go                      macOS `security` wrapper (same pattern as agent-statsig)
    session.go                       Session file I/O (cookie jar, expiry)
  errors/errors.go                   APIError{Message, Hint, FixableBy, Cause} + HTTP classifier
  output/output.go                   PrintJSON / WriteError / NDJSON / body writers
skills/agent-deepweb/SKILL.md        Claude Code skill (tools: Bash, Read)
```

## Testing

- `httptest.Server`-based CLI tests via `shared.ClientFactory` override — verified credential
  headers are attached, response redactor is applied, HTTP status → fixable_by mapping works.
- Credential test covers the Keychain sentinel round-trip (macOS) and file-fallback paths.
- An "agent mode" test asserts that human-only commands refuse by default
  (no env var needed) and pass through when `AGENT_DEEPWEB_MODE=human` is set.
  Redaction cannot be turned off in agent mode.

## Future

- **Browser-assisted login.** New `AuthSpec.Type = "browser"` that delegates to a small
  Playwright/chromedp helper, letting the user complete MFA interactively. The helper hands
  back a cookie jar via stdin; the binary never sees the password.
- **Request templates.** Human pre-registers `gql.get_user {user_id}` with a bound
  credential and a parameterised query. The LLM invokes by name with structured args, never
  touching the URL or headers. Highest-safety mode.
- **Response diffs / caching.** Cache last response for a URL+auth+query and return a diff
  on re-fetch, saving tokens for polling.
- **Egress audit log.** Append-only log at `~/.config/agent-deepweb/audit.log` of every
  request (URL, credential name, status, bytes) so the user can inspect what the LLM did.

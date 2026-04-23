# agent-deepweb

`curl`-with-auth for AI agents. The user stores credentials under names; the LLM invokes them by name. The LLM never sees the secret values.

## Why

LLM agents routinely need to call authenticated endpoints — GraphQL behind a Bearer token, an admin API with basic auth, a dashboard behind a cookie login. Putting the token in the prompt leaks it into logs and transcripts. Putting it in an env var still lets the model `echo $TOKEN`. `agent-deepweb` keeps the secret with the user and lets the agent reference it by name.

## Install

### Build from source

```bash
git clone https://github.com/shhac/agent-deepweb.git
cd agent-deepweb
make build
./agent-deepweb llm-help
```

## Quick start

### 1. Register a credential (human, one-time)

```bash
# Bearer token (most common)
agent-deepweb creds add github --type bearer \
  --token ghp_xxx --domain api.github.com

# Basic auth
agent-deepweb creds add intranet --type basic \
  --username alice --password 'pw' --domain intranet.example.com

# Raw cookie
agent-deepweb creds add dashboard --type cookie \
  --cookie 'session=abc; other=xyz' --domain dashboard.example.com

# Custom header (API-key)
agent-deepweb creds add myapi --type custom \
  --custom-header 'X-API-Key: sk-xxx' --domain api.example.com

# Form-login (POSTs creds + harvests a session cookie + optional token)
agent-deepweb creds add myapp --type form \
  --login-url https://api.example.com/login \
  --username alice --password 'pw' \
  --token-path access_token \
  --domain api.example.com
agent-deepweb login myapp
```

On macOS the secret lives in the Keychain (service `app.paulie.agent-deepweb`). On Linux/Windows it goes to `~/.config/agent-deepweb/credentials.secrets.json` (mode 0600).

### 2. Fetch

```bash
# Auto-resolves credential by host
agent-deepweb fetch https://api.github.com/user

# Explicit
agent-deepweb fetch https://api.github.com/user --auth github

# POST JSON
agent-deepweb fetch https://api.example.com/v1/items \
  --method POST --json '{"name":"x"}' --auth myapi

# GraphQL
agent-deepweb graphql https://api.github.com/graphql \
  --auth github \
  --query 'query { viewer { login } }'
```

### 3. Run a template (highest-safety mode)

A template is a frozen request shape authored by the human. The LLM can only fill in parameter values.

```bash
# Import a template file (human-only)
agent-deepweb tpl import ./templates/github.json

# Run — LLM-safe
agent-deepweb tpl run github.get_user --param username=octocat
agent-deepweb tpl run github.create_issue \
  --param owner=shhac --param repo=x --param title='Oops'
```

### 4. Output

Structured JSON envelope on stdout:

```json
{
  "status": 200,
  "status_text": "200 OK",
  "url": "https://api.github.com/user",
  "auth": "github",
  "headers": { "Content-Type": ["application/json"], "Authorization": ["<redacted>"] },
  "content_type": "application/json",
  "truncated": false,
  "body": { "login": "shhac", "id": 12345 }
}
```

With `--format raw` the response body prints directly.

## How secrets are protected from the LLM

| Guarantee | Mechanism |
|-----------|-----------|
| LLM references credentials by name | `fetch --auth <name>`; never by value |
| LLM can't read stored secrets | `creds list/show` prints metadata only; no "reveal" command |
| LLM can't add/remove/expand creds | Human-only; refused when `AGENT_DEEPWEB_MODE=agent` |
| Credential off-URL is refused | Host+port and optional path allowlist per credential |
| Plain http:// blocked for auth | Unless loopback, `creds set-allow-http`, or per-request `--allow-http` (human-only) |
| Upstream echoing of secrets is masked | Response headers matching auth patterns → `<redacted>` |
| Token-like JSON fields are masked | Response body fields matching `access_token\|refresh_token\|client_secret\|password\|secret\|bearer\|token\|api[-_]?key` → `<redacted>` |
| Literal stored secret masked in body | Byte-level substitution matches the exact token/password/cookie value |
| Sensitive vs non-sensitive cookies | HttpOnly and name-pattern classification; `session show` reveals non-sensitive only |
| Redaction can't be disabled in agent mode | `--no-redact` refuses with `fixable_by: human` |
| Request is audited | Every request appends a JSONL line to `~/.config/agent-deepweb/audit.log` |

## Errors

Errors go to stderr as JSON with a classification so the LLM knows what to do next:

```json
{ "error": "credential \"myapi\" is not allowed on evil.example.com (host/path not in allowlist)",
  "hint": "Ask the user to run 'agent-deepweb creds allow myapi evil.example.com' or widen --path",
  "fixable_by": "human" }
```

| `fixable_by` | LLM behavior |
|-------------|--------------|
| `agent`     | Fix and retry (typo, 404, bad JSON, multiple matches, type error) |
| `human`     | Stop and ask user (missing cred, 401/403, allowlist, expired session, http-scheme refusal) |
| `retry`     | Retry once with backoff (429, 5xx, network timeout) |

## Commands

```
agent-deepweb llm-help                           Reference card
agent-deepweb fetch <url> [flags]                HTTP request with auth
agent-deepweb graphql <url> [flags]              GraphQL POST

agent-deepweb tpl list
agent-deepweb tpl show <name>
agent-deepweb tpl run <name> --param k=v         Agent-safe
agent-deepweb tpl import <file>                  HUMAN-ONLY
agent-deepweb tpl remove <name>                  HUMAN-ONLY

agent-deepweb creds list
agent-deepweb creds show <name>
agent-deepweb creds test <name>
agent-deepweb creds add <name> ...               HUMAN-ONLY
agent-deepweb creds remove <name>                HUMAN-ONLY
agent-deepweb creds allow <name> <domain>        HUMAN-ONLY
agent-deepweb creds disallow <name> <domain>     HUMAN-ONLY
agent-deepweb creds allow-path <name> <path>     HUMAN-ONLY
agent-deepweb creds disallow-path <name> <path>  HUMAN-ONLY
agent-deepweb creds set-health <name> <url>      HUMAN-ONLY
agent-deepweb creds set-default-header <name> "K: V"  HUMAN-ONLY
agent-deepweb creds unset-default-header <name> K     HUMAN-ONLY
agent-deepweb creds set-allow-http <name> true   HUMAN-ONLY
agent-deepweb creds set-user-agent <name> <ua>   HUMAN-ONLY

agent-deepweb login <name>                       HUMAN-ONLY (form-login flow)
agent-deepweb session status <name>
agent-deepweb session show <name>                Cookies with sensitive values masked
agent-deepweb session clear <name>               HUMAN-ONLY
agent-deepweb session set-expires <name> <d|ts>  HUMAN-ONLY
agent-deepweb session mark-sensitive <name> <c>  HUMAN-ONLY
agent-deepweb session mark-visible   <name> <c>  HUMAN-ONLY

agent-deepweb audit tail [-n N]
agent-deepweb audit summary
```

Per-command `llm-help` subcommands exist for all top-level verbs.

## Output formats

```
--format json     Default. Pretty JSON envelope to stdout.
--format raw      Response body written directly to stdout.
--format text     Short HTTP status header + raw body.
--format jsonl    Line-delimited JSON (where applicable).
```

## Environment variables

```
AGENT_DEEPWEB_MODE=agent         Enforce LLM-safety mode; human-only commands refuse.
AGENT_DEEPWEB_AUTH               Default credential name.
AGENT_DEEPWEB_CONFIG_DIR         Override ~/.config/agent-deepweb (useful in tests).
AGENT_DEEPWEB_TIMEOUT            Default request timeout in milliseconds.
AGENT_DEEPWEB_USER_AGENT         Fallback User-Agent string.
AGENT_DEEPWEB_AUDIT=off          Disable audit-log writes.
```

## Claude Code skill

`skills/agent-deepweb/SKILL.md` ships with the repo. Point your Claude Code skill loader at it. Set `AGENT_DEEPWEB_MODE=agent` in the skill's env so human-only commands refuse.

## Development

```bash
make build && make test
make lint              # golangci-lint (zero issues today)
make fmt               # gofmt (+ goimports if installed)
make mock              # Run mockdeep on :8765 for ad-hoc integration testing
```

See [AGENTS.md](AGENTS.md) for the repo layout, testing strategy, and guidance on adding a new subcommand or auth type.

## License

MIT — see [LICENSE](LICENSE).

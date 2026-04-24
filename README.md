# agent-deepweb

`curl`-with-auth for AI agents. The user registers profiles (auth identities) under names; the LLM invokes them by name. The LLM never sees the secret values.

## Why

LLM agents routinely need to call authenticated endpoints — GraphQL behind a Bearer token, an admin API with basic auth, a dashboard behind a cookie login. Putting the token in the prompt leaks it into logs and transcripts. Putting it in an env var still lets the model `echo $TOKEN`. `agent-deepweb` keeps the secret with the user and lets the agent reference it by name.

## Install

### Homebrew

```bash
brew install shhac/tap/agent-deepweb
```

### Build from source

```bash
git clone https://github.com/shhac/agent-deepweb.git
cd agent-deepweb
make build
./agent-deepweb llm-help
```

## Quick start

### 1. Register a profile (one-time)

```bash
# Bearer token (most common)
agent-deepweb profile add github --type bearer \
  --token ghp_xxx --domain api.github.com

# Basic auth
agent-deepweb profile add intranet --type basic \
  --username alice --password 'pw' --domain intranet.example.com

# Raw cookie
agent-deepweb profile add dashboard --type cookie \
  --cookie 'session=abc; other=xyz' --domain dashboard.example.com

# Custom header (API-key)
agent-deepweb profile add myapi --type custom \
  --custom-header 'X-API-Key: sk-xxx' --domain api.example.com

# Form-login (POSTs creds + harvests a session cookie + optional token)
agent-deepweb profile add myapp --type form \
  --login-url https://api.example.com/login \
  --username alice --password 'pw' \
  --token-path access_token \
  --domain api.example.com
agent-deepweb login myapp
```

On macOS the secret lives in the Keychain (service `app.paulie.agent-deepweb`). On Linux/Windows it goes to `~/.config/agent-deepweb/credentials.secrets.json` (mode 0600). Each profile also gets a random 32-byte key (provisioned at `profile add` time, persisted across mutations) used to encrypt its cookie jar.

### 2. Fetch

```bash
# Auto-resolves profile by host
agent-deepweb fetch https://api.github.com/user

# Explicit
agent-deepweb fetch https://api.github.com/user --profile github

# POST JSON
agent-deepweb fetch https://api.example.com/v1/items \
  --method POST --json '{"name":"x"}' --profile myapi

# GraphQL
agent-deepweb graphql https://api.github.com/graphql \
  --profile github \
  --query 'query { viewer { login } }'

# Explicit anonymous (no profile attached, no jar)
agent-deepweb fetch https://example.com/healthz --profile none

# Bring-your-own jar — LLM-authored end-to-end flow
agent-deepweb fetch https://test.example.com/signup --profile none \
  --cookiejar /tmp/flow.json --method POST \
  --json '{"email":"a@b","password":"..."}'
agent-deepweb fetch https://test.example.com/me --profile none \
  --cookiejar /tmp/flow.json
```

### 3. Run a template (highest-safety mode)

A template is a frozen request shape authored by the human. The LLM can only fill in parameter values.

```bash
agent-deepweb tpl import ./templates/github.json
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
  "profile": "github",
  "headers": { "Content-Type": ["application/json"], "Authorization": ["<redacted>"] },
  "content_type": "application/json",
  "truncated": false,
  "body": { "login": "shhac", "id": 12345 }
}
```

With `--format raw` the response body prints directly.

## How the security model works

agent-deepweb's job: don't be a hole in the harness's sandbox. The harness (Claude Code) controls which subcommands the LLM can invoke; agent-deepweb makes sure each subcommand can't be misused.

| Guarantee | Mechanism |
|-----------|-----------|
| LLM references profiles by name | `fetch --profile <name>`; never by value |
| LLM can't read stored secrets | `profile list/show` prints metadata only |
| Profile off-URL is refused | Per-profile host[:port] + optional path allowlist |
| Anonymous request must be explicit | `--profile none` opt-in; no silent fallthrough |
| Plain http:// blocked for auth | Unless loopback or per-profile `allow_http` (set with `profile set-allow-http <name> true --token ...`) |
| Redirects can't escape allowlist | `CheckRedirect` refuses cross-allowlist hops |
| Upstream echoing of secrets is masked | Headers + JSON fields + literal byte echo redaction (always on) |
| Cookie jars encrypted at rest | AES-256-GCM with per-profile random key alongside the primary secret |
| BYO jar (`--cookiejar <path>`) is plaintext | The caller picked the path; explicit ownership of the contents |
| Sensitive cookies masked in `jar show` | HttpOnly + name-pattern classification; human override via `mark-sensitive`/`mark-visible` |
| Escalation requires a passphrase | `profile allow / set-default-header / set-allow-http / change-secret / jar mark-visible` all require `--passphrase`, constant-time verified against a value stored with the profile. The passphrase defaults to the primary secret unless a friendly one was set at `profile add` time |
| Every request is audited | Append-only JSONL at `~/.config/agent-deepweb/audit.log`, including `profile` and `jar` fields |

The passphrase replaces v2's original "re-assert the primary secret" design. UX is better (a short friendly phrase beats pasting a 64-byte token) and failure modes are cleaner (wrong passphrase errors; wrong re-asserted secret used to silently break the profile). Security asymmetry is unchanged: the human knows the passphrase, the LLM doesn't.

### A note on jar encryption

Mode 0600 on the jar file only protects against *other* UNIX users. An LLM running as the same UID as the human can read mode-0600 files directly. v2 fixes this for profile-bound jars by encrypting them with a per-profile AES-256-GCM key stored alongside the primary secret. On macOS that key lives in Keychain (real ACL); on Linux/Windows it's in the same `credentials.secrets.json` (so an LLM with shell access could grab both). BYO jars (`--cookiejar <path>`) are intentionally plaintext — the caller picked the path and owns the trade-off.

## Errors

Errors go to stderr as JSON with a classification so the LLM knows what to do next:

```json
{ "error": "no profile matches https://example.com/",
  "hint": "Ask the user to register one with 'agent-deepweb profile add', or pass --profile none to make an anonymous request explicitly.",
  "fixable_by": "human" }
```

| `fixable_by` | LLM behavior |
|-------------|--------------|
| `agent`     | Fix and retry (typo, 404, bad JSON, multiple matches, type error) |
| `human`     | Stop and ask user (missing profile, 401/403, allowlist, expired session, http-scheme refusal) |
| `retry`     | Retry once with backoff (429, 5xx, network timeout) |

## Commands

```
agent-deepweb llm-help                           Reference card
agent-deepweb fetch <url> [flags]                HTTP request with auth
agent-deepweb graphql <url> [flags]              GraphQL POST

agent-deepweb tpl list
agent-deepweb tpl show <name>
agent-deepweb tpl run <name> --param k=v         Agent-safe (LLM fills values)
agent-deepweb tpl import <file>
agent-deepweb tpl remove <name>

agent-deepweb profile list
agent-deepweb profile show <name>
agent-deepweb profile test <name>
agent-deepweb profile add <name> --type <t> [--passphrase <p>] ...
agent-deepweb profile remove <name>              Clears jar too
agent-deepweb profile allow <name> <domain>      --passphrase <p>
agent-deepweb profile disallow <name> <domain>
agent-deepweb profile allow-path <name> <path>   --passphrase <p>
agent-deepweb profile disallow-path <name> <path>
agent-deepweb profile set-health <name> <url>
agent-deepweb profile set-default-header <name> "K: V" --passphrase <p>
agent-deepweb profile unset-default-header <name> K
agent-deepweb profile set-allow-http <name> true --passphrase <p>
agent-deepweb profile set-user-agent <name> <ua>
agent-deepweb profile change-secret <name> --passphrase <p> [--token T | --password P | ...]

agent-deepweb login <name>                       Form-login flow (writes to jar)
agent-deepweb jar status <name>
agent-deepweb jar show <name>                    Cookies; sensitive values masked
agent-deepweb jar clear <name>
agent-deepweb jar set-expires <name> <d|ts>
agent-deepweb jar mark-sensitive <name> <c> [c2 ...]
agent-deepweb jar mark-visible   <name> <c> [c2 ...]   --passphrase <p>

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
AGENT_DEEPWEB_PROFILE            Default profile name.
AGENT_DEEPWEB_CONFIG_DIR         Override ~/.config/agent-deepweb (useful in tests).
AGENT_DEEPWEB_TIMEOUT            Default request timeout in milliseconds.
AGENT_DEEPWEB_USER_AGENT         Fallback User-Agent string.
AGENT_DEEPWEB_AUDIT=off          Disable audit-log writes.
```

## Claude Code skill

`skills/agent-deepweb/SKILL.md` ships with the repo. Configure your harness to allowlist these commands for the LLM:

```
agent-deepweb llm-help
agent-deepweb fetch *
agent-deepweb graphql *
agent-deepweb tpl run *
agent-deepweb tpl list
agent-deepweb tpl show *
agent-deepweb profile list
agent-deepweb profile show *
agent-deepweb profile test *
agent-deepweb jar status *
agent-deepweb jar show *
agent-deepweb audit tail *
agent-deepweb audit summary
```

The harness denies everything else — including the escalation commands and direct file reads to `~/.config/agent-deepweb/`. agent-deepweb's primary-secret-re-assertion is a second line of defence in case the harness allowlist drifts.

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

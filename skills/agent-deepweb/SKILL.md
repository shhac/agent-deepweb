---
name: agent-deepweb
description: How to drive the `agent-deepweb` CLI — a separate tool (https://github.com/shhac/agent-deepweb) the user has installed for credential-gated HTTP. This skill does NOT replace `curl`, `WebFetch`, or other HTTP tools for unauthenticated requests; it is narrowly about invoking `agent-deepweb`'s subcommands when the user has already registered a named profile. You reference profiles by name via `--profile <name>`; you never see the stored secret values, and you cannot escalate without the profile's `--passphrase` (which you don't have).
---

# agent-deepweb skill

## What this skill is

This skill teaches you how to invoke `agent-deepweb`, a CLI tool the user has separately installed (https://github.com/shhac/agent-deepweb). It is **not** a replacement for general HTTP tooling — keep using `curl`, `WebFetch`, or whatever the harness provides for public URLs. The scope here is narrow: when the user has pre-registered credentials under a profile name, `agent-deepweb` lets you make authenticated requests without ever seeing the stored secret values.

The tool's design constraints are load-bearing to what this skill documents:

- The user registers profiles (auth identities) out-of-band; you reference them by name via `--profile <name>`.
- You never see the secret values — responses are redacted, allowlists are enforced.
- You cannot escalate (widen scope, un-mask secrets) without the profile's `--passphrase`, which you don't have.
- Every request is audited; opt-in `--track` persists a full redacted replay record.

## When to use

Reach for `agent-deepweb` **only** when the URL is behind auth the user has already registered in `agent-deepweb`. Run `agent-deepweb profile list` first to see what's available. For URLs that don't need auth, the harness's normal HTTP tools (`curl`, `WebFetch`) are the right choice — this skill does not try to displace them.

The narrow exception: if the user explicitly wants the audit trail + redaction on an anonymous request, `agent-deepweb fetch ... --profile none` is available. This is opt-in, not a default.

---

## 1. Quick start — read-only, safe to explore

```bash
agent-deepweb llm-help                           # Top-level reference card
agent-deepweb profile list                       # Profiles available (no secrets)
agent-deepweb profile show <name>                # Metadata for one profile
agent-deepweb profile test <name>                # Send a health-check request
agent-deepweb jar status <name>                  # Cookie count, expiry, has-token
agent-deepweb jar show <name>                    # Cookies (sensitive values redacted)
agent-deepweb template list                      # Available request templates
agent-deepweb template show <name>               # Full template definition
agent-deepweb config list-keys                   # Current config values + defaults
agent-deepweb config get <key>                   # One config value
agent-deepweb audit tail -n 50                   # Recent requests
agent-deepweb audit summary                      # Group recent activity
agent-deepweb audit show <audit-id>              # Full record for a --track'd request
```

---

## 2. Common tasks

### Authenticated GET

```bash
agent-deepweb fetch https://api.example.com/me --profile myapi
```

If you omit `--profile`, the host is matched against profile allowlists. Exactly one match → used. Zero matches → `fixable_by:human` error (ask the user to register a profile, or pass `--profile none`). Multiple matches → `fixable_by:agent` error listing the candidates so you can pick.

### POST JSON

```bash
agent-deepweb fetch https://api.example.com/v1/items \
  --profile myapi --method POST --json '{"name":"widget"}'
```

Body sources:
- `--json '...'`, `--json @./file.json`, `--json @-` (stdin)
- `--data '...'` (raw body; set Content-Type yourself via `--header`)
- `--form key=value` (repeatable; sets `application/x-www-form-urlencoded`)
- `--file field=@path[;type=MIME][;filename=NAME]` (repeatable; multipart/form-data — can combine with `--form` for text parts)

### Upload a file (or several)

```bash
agent-deepweb fetch https://api.example.com/upload --profile myapi --method POST \
  --file photo=@./cat.jpg \
  --file 'doc=@./notes.bin;type=application/pdf;filename=report.pdf' \
  --form caption='hello'
```

### GraphQL

```bash
agent-deepweb graphql https://api.example.com/graphql --profile myapi \
  --query 'query { me { id name } }'

# Introspection is usually the right first move on an unfamiliar schema:
agent-deepweb graphql https://api.example.com/graphql --profile myapi \
  --query '{ __schema { queryType { fields { name } } } }'
```

### Run a template (highest-safety mode)

When the user has registered a template, you can ONLY fill in parameter values:

```bash
agent-deepweb template run myapi.get_item --param id=abc123
```

Method, URL shape, headers, profile binding, and body shape are all frozen. You cannot change them.

### Anonymous (explicit)

```bash
agent-deepweb fetch https://example.com/healthz --profile none
```

Required. There is no implicit anonymous fallthrough — forgetting `--profile` errors out.

---

## 3. Token efficiency

Every envelope by default includes a `request` block (method, url, redacted headers, body_bytes) and the full response. Two flags to trim it:

```bash
# "Did it work?" — keep status + audit_id + request info, drop response body/headers
agent-deepweb fetch https://api.example.com/v1/jobs --profile myapi \
  --method POST --json '{"id":"x"}' --hide-response --track

# Response-only — drop the request block (you sent it, you know what it was)
agent-deepweb fetch https://api.example.com/me --profile myapi --hide-request
```

`--track` persists a full redacted request+response record so you can retrieve the dropped parts later via `audit show <id>` (see §5).

---

## 4. Advanced — BYO jar for LLM-authored flows

For multi-step flows where you provide the credentials inline (e.g. signup → login → action against a test environment), pair `--profile none` with `--cookiejar <path>`:

```bash
# Request 1: create a test account
agent-deepweb fetch https://test.example.com/signup --profile none \
  --cookiejar /tmp/flow.json --method POST \
  --json '{"email":"a@b","password":"..."}'

# Request 2: cookies from request 1 persist, re-sent automatically
agent-deepweb fetch https://test.example.com/me --profile none \
  --cookiejar /tmp/flow.json
```

The BYO jar is plaintext at the path you chose — you (and the harness) own cleanup.

---

## 5. Debugging — when something breaks

Every error goes to stderr as JSON with a `fixable_by` classification. Trust the classification:

```json
{ "error": "...", "hint": "...", "fixable_by": "agent|human|retry" }
```

| `fixable_by` | What to do |
|--------------|------------|
| `agent`      | Your input was wrong (typo, bad URL, body too large, ambiguous profile, wrong param type). Fix and retry. |
| `human`      | The user has to act (no profile registered, 401/403, allowlist denial, expired session, http-scheme refused). **Stop and surface the exact `hint` to the user.** Don't retry. |
| `retry`      | Transient (network, 429, 5xx, timeout). Retry once or twice with backoff; surface to the user if it persists. |

When a call fails mysteriously:

1. **Read `fixable_by` on the error envelope** — it tells you who must act.
2. **If `human`**: surface the exact `hint` verbatim to the user; don't retry.
3. **If `agent`**: re-read `profile show <name>` to check the allowlist / default headers.
4. **Re-run with `--track`** to capture the full request (including the redacted body you sent). Then `audit show <id>` for inspection.
5. **`audit tail -n 20`** to spot patterns — same host 401'ing repeatedly usually means an expired session; ask the user to re-run `login <name>`.

---

## 6. Output envelope

Every successful response is a JSON envelope to stdout:

```json
{
  "status": 200,
  "status_text": "200 OK",
  "url": "...",
  "profile": "myapi",
  "audit_id": "20260424T1200-abcd",
  "headers": { "Content-Type": ["..."], "Authorization": ["<redacted>"] },
  "content_type": "application/json",
  "truncated": false,
  "body": <decoded JSON or string>,
  "request": {
    "method": "POST",
    "url": "...",
    "headers": { ... redacted ... },
    "body_bytes": 128
  }
}
```

- `audit_id` only appears when `--track` was set.
- `request` disappears when `--hide-request` was set.
- `headers`/`body`/`content_type`/`status_text`/`truncated` disappear when `--hide-response` was set.

---

## 7. What you cannot productively do

The harness should deny these; `agent-deepweb` is also designed to reject them cleanly. Either way, running them fails without leaking secrets:

- **Adding / removing a profile** (`profile add`, `profile remove`) — you'd produce a profile authenticated with whatever you guessed for the secret.
- **Widening allowlist / changing outbound headers / enabling http:// / rotating the primary** (`profile allow`, `profile allow-path`, `profile set-default-header`, `profile set-allow-http`, `profile set-secret`, `profile set-passphrase`) — all require `--passphrase`, constant-time verified against a stored value. Wrong guess errors cleanly.
- **Un-masking secrets** (`jar mark-visible`, `profile mark-header-visible`) — same `--passphrase` mechanism.
- **Reading the encrypted jar directly** — `cat ~/.config/agent-deepweb/profiles/<name>/jar.json` returns AES-256-GCM ciphertext. The decryption key is stored alongside the primary secret (Keychain on macOS).
- **Performing form-login** (`login <name>`) — the profile must already have correct credentials; you can't put valid ones in.
- **Modifying persistent config** (`config set`, `config unset`) — not allowlisted for the LLM in the recommended harness config.

If you find yourself wanting to escalate, the right move is to ask the user. Every denied attempt gets audited.

---

## 8. Recommended harness permission allowlist

For Claude Code (or equivalent), allowlist these commands to the LLM:

```
Allow:
  agent-deepweb llm-help
  agent-deepweb fetch *
  agent-deepweb graphql *
  agent-deepweb template run *
  agent-deepweb template list
  agent-deepweb template show *
  agent-deepweb profile list
  agent-deepweb profile show *
  agent-deepweb profile test *
  agent-deepweb jar status *
  agent-deepweb jar show *
  agent-deepweb config list-keys
  agent-deepweb config get *
  agent-deepweb audit tail *
  agent-deepweb audit summary
  agent-deepweb audit show *

Deny (or simply not allowlisted):
  agent-deepweb profile add|remove|allow|allow-path|disallow|disallow-path|set-*|mark-header-*
  agent-deepweb login *
  agent-deepweb jar clear|set-expires|mark-*
  agent-deepweb template import|remove
  agent-deepweb config set|unset
  agent-deepweb audit prune
  Direct reads of ~/.config/agent-deepweb/  (so the encrypted jar + secrets file + config are off-limits via shell)
```

`--cookiejar <path>` on fetch/graphql is allowed (it's a flag, not a verb), but every use is audited and `audit summary` surfaces the BYO jar paths under `by_jar_path`.

---

## 9. Secret-safety rules to obey

- Reference profiles by name only. Never paste a token into a prompt or a follow-up command.
- If you receive a secret-looking value in a response despite redaction (rare; usually a bug), treat it as sensitive: don't echo it back to the user, don't include it in your reasoning text, and consider the response compromised.
- Don't combine `--profile <name>` with `--cookiejar <path>` to a path you'd later expect to be safe for non-test use — the plaintext jar will contain real session cookies.
- When blocked by an escalation gate, ask the user. Don't guess passphrases.

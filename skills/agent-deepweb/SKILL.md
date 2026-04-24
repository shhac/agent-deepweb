---
name: agent-deepweb
description: Use agent-deepweb to make authenticated HTTP requests when the URL needs auth (Bearer, Basic, cookie, or login-session). The user has registered profiles by name; you reference them via --profile <name>. Never request raw secret values from the user, and never echo a profile's secret material back.
---

# agent-deepweb skill

Authenticated HTTP for AI agents. The user registers profiles (auth identities) under names; you reference them by name. You never see secret values, and you cannot widen scope or un-mask sensitive cookies — only the human can, by re-supplying the profile's primary secret.

## When to use

Use `agent-deepweb` instead of plain `curl` (or instead of asking the user for a token) any time the URL is behind auth that the user has already registered. Run `agent-deepweb profile list` first to see what's available.

For URLs that don't need auth, the harness's normal HTTP tooling (`WebFetch`, `curl`, etc.) is fine. But if you want consistent redaction + audit trail for every outbound request, use `agent-deepweb fetch ... --profile none`.

## Quick start (read-only)

These commands are safe to explore — they never make changes:

```bash
agent-deepweb llm-help                       # Top-level reference card
agent-deepweb profile list                   # Profiles available (no secrets)
agent-deepweb profile show <name>            # Metadata for one profile
agent-deepweb profile test <name>            # Send a health-check request
agent-deepweb jar status <name>              # Cookie count, expiry, has-token
agent-deepweb jar show <name>                # Cookies (sensitive values redacted)
agent-deepweb tpl list                       # Available request templates
agent-deepweb tpl show <name>                # Full template definition
agent-deepweb audit tail -n 50               # Recent requests
agent-deepweb audit summary                  # Group recent activity
```

## Common workflows

### Authenticated GET

```bash
agent-deepweb fetch https://api.github.com/user --profile github
```

If you don't pass `--profile`, the host is matched against profile allowlists. Exactly one match → used. Zero matches → human-fixable error (the user must register a profile or you must pass `--profile none`). Multiple matches → agent-fixable error listing the candidates so you can pick one.

### POST JSON

```bash
agent-deepweb fetch https://api.example.com/v1/items \
  --profile myapi --method POST --json '{"name":"x"}'
```

Body sources: `--json '...'`, `--json @./file.json`, `--json @-` (stdin), `--data '...'`, `--form key=value` (repeatable).

### GraphQL

```bash
agent-deepweb graphql https://api.github.com/graphql \
  --profile github \
  --query 'query { viewer { login } }'
```

### Templates (highest-safety mode)

When the user has registered a template, you can ONLY fill in parameter values:

```bash
agent-deepweb tpl run github.get_user --param username=octocat
```

Method, URL shape, headers, profile binding, and body shape are all frozen by the template. You cannot change them.

### Anonymous (explicit)

```bash
agent-deepweb fetch https://example.com/healthz --profile none
```

Required. There is no implicit anonymous fallthrough — forgetting `--profile` errors out.

### Bring-your-own jar (LLM-authored flows)

For exploring a service end-to-end where you yourself supply the credentials inline (signup → login → action against a test environment), pair `--profile none` with `--cookiejar <path>`:

```bash
# Create a temp test account
agent-deepweb fetch https://test.example.com/signup --profile none \
  --cookiejar /tmp/flow.json --method POST \
  --json '{"email":"...","password":"..."}'

# Cookies persist between requests in /tmp/flow.json
agent-deepweb fetch https://test.example.com/me --profile none \
  --cookiejar /tmp/flow.json
```

The BYO jar is plaintext at the path you chose. Be deliberate about cleanup.

## Output

Every successful response is a JSON envelope to stdout:

```json
{
  "status": 200,
  "status_text": "200 OK",
  "url": "...",
  "profile": "myapi",
  "headers": { "Content-Type": ["..."], "Authorization": ["<redacted>"] },
  "content_type": "application/json",
  "truncated": false,
  "body": <decoded JSON or string>
}
```

Errors go to stderr as JSON:

```json
{ "error": "...", "hint": "...", "fixable_by": "agent|human|retry" }
```

| `fixable_by` | What to do |
|--------------|------------|
| `agent`      | Your input was wrong (typo, bad URL, body too large, ambiguous profile). Fix and retry. |
| `human`      | The user has to act (no profile registered, 401/403, allowlist denial, expired session, http-scheme refused). Stop and ask. |
| `retry`      | Transient (network, 429, 5xx, timeout). Retry with backoff once or twice; surface to the user if it persists. |

## What you cannot productively do

The harness should deny these commands; agent-deepweb's primary-secret-re-assertion is a second line of defence. Either way, running them either fails or silently breaks the profile (no exfil, but very noticeable to the user via audit log + broken subsequent requests):

- **Adding a profile** (`profile add`) — you'd produce a profile authenticated with whatever junk you guessed for the secret. Useless.
- **Widening allowlist** (`profile allow`, `profile allow-path`, `profile set-default-header`, `profile set-allow-http`) — these require re-supplying the profile's primary secret. Wrong value silently OVERWRITES the stored secret with garbage. The next legitimate fetch fails; the user investigates.
- **Un-masking cookies** (`jar mark-visible`) — same mechanism. Requires the primary secret; wrong value breaks the profile.
- **Reading the encrypted jar directly** — `cat ~/.config/agent-deepweb/profiles/<name>/jar.json` returns AES-256-GCM ciphertext. The decryption key is stored alongside the primary secret (Keychain on macOS).
- **Performing form-login** (`login`) — requires the form-auth profile to already have correct credentials; you can't put valid credentials in.

If you find yourself wanting to escalate, the right move is to ask the user. The audit log will record it either way.

## Recommended Claude Code permission allowlist

For the harness to enforce this skill cleanly, allowlist these commands:

```
Allow:
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

Deny (or simply not allowlisted):
  agent-deepweb profile add|remove|allow|allow-path|disallow|disallow-path|set-*
  agent-deepweb login *
  agent-deepweb jar clear|set-expires|mark-*
  agent-deepweb tpl import|remove
  Direct reads of ~/.config/agent-deepweb/  (so the encrypted jar + secrets file are off-limits via shell)
```

The `--cookiejar` flag is allowed (it's a per-fetch flag, not a separate verb), but its use is audited — `audit summary` surfaces every BYO jar path under `by_jar_path`.

## Secret-safety rules to obey

- Reference profiles by name only. Never paste a token into a prompt or a follow-up command.
- If you receive a secret-looking value in a response despite redaction (rare; usually a bug), treat it as sensitive: don't echo it back to the user, don't include it in your reasoning text, and consider the response compromised.
- Don't combine `--profile <name>` with `--cookiejar <path>` to a path you'd later want plaintext-readable for non-test scenarios. The plaintext jar will contain real session cookies.
- For any "this isn't working, can you escalate?" moment, ask the user. The escalation paths are designed so you can't usefully bypass them.

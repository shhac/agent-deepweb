# agent-deepweb v2 — profiles, password-protected escalation, generalised cookie jars

Supersedes the v1 mode-gated security model with a simpler, harder-to-bypass design.

## Why v2

v1 had three things that we now want to drop:

1. **`AGENT_DEEPWEB_MODE` env var + ~150 lines of mode-gating code.** It's a soft contract — an LLM with shell access can always set the env var. The real boundary is the harness's permission system (Claude Code allowlists which subcommands the LLM can invoke). The mode infrastructure is theatre that reads as security.

2. **Refusing destructive commands the LLM can sabotage but not exploit.** `creds remove` lets an LLM destroy a credential — but destruction isn't escalation. Refusing it added complexity for negligible safety value (the user's audit log catches it, and the harness can block it cleanly).

3. **One escalation gate (`creds allow` widening) that DID matter** — and the mode infrastructure protected it through the same blunt mechanism as everything else. Better to use the right tool: re-assertion of the credential's primary secret as a write-time protection.

## The new mental model

**Three layers of defence:**

1. **Tool design (agent-deepweb's job).** Sensitive operations require re-asserting the credential's primary secret. Wrong value → credential silently broken (self-punishing). Read operations stay unrestricted. Anonymous network use is opt-in via an explicit `--profile none`.
2. **Harness policy (Claude Code's job).** SKILL.md ships a recommended permission config telling Claude Code which subcommands the LLM can invoke. This is enforced at the shell-allowlist layer, not by the binary itself.
3. **Audit log (user's job to monitor).** Every request — including `--profile none`, including ones launched with bring-your-own cookiejar — is recorded in JSONL. Tripwires in `audit summary` surface anomalous activity.

**The "secret knowledge" insight.** The asymmetry between humans and LLMs is information, not access. Humans know their credentials' values; LLMs don't. So:

- Read commands work for everyone (no secret needed).
- Mutating commands that need the secret value to be useful (like `creds add`) are unguarded — an LLM running them produces a useless credential it can't authenticate with.
- Mutating commands that escalate (widen scope, un-mask cookies) require the human to *re-supply the credential's primary secret*. An LLM that doesn't have it can't successfully escalate.

## Concept rename: credentials → profiles

A "credential" was always more than just a secret — it bundled secret material, host/path allowlist, default headers, User-Agent override, etc. "Profile" captures the full identity. The CLI verb `creds` becomes `profile`.

```
profile add <name> --type <t> --token T --domain D [--domain D2 ...] [--path P ...]
profile list / show / remove / test
profile allow <name> <domain> --token T          (password-protected)
profile allow-path <name> <pattern> --token T
profile set-* ...

login <name> --password P                         re-auth; refreshes session
session show <name>                               masks sensitive cookies
session mark-visible <name> <c1> [c2 ...] --password P   variadic, password-gated
```

`--auth` is gone. `--profile <name>` replaces it on `fetch` and `graphql`. `--profile none` is the explicit opt-in for anonymous requests.

## Primary-secret re-assertion

Every escalation command requires the profile's primary secret as a flag. The mechanism is overwrite, not verify: we just always store what was supplied. Two failure modes:

| Symptom | What we do | Why |
|---------|-----------|-----|
| Required flag missing | Command errors with `fixable_by:agent` and a hint naming the flag | Helps humans who forgot |
| Required flag value wrong | Command silently succeeds; stored secret is now wrong; session (if form-auth) is invalidated | Self-punishing for an LLM that didn't have the secret. Subsequent fetches go out with garbage auth → no exfil |

The "verify by overwrite" design avoids any timing or error-leakage concerns from real verification.

**Per-type primary secret on escalation:**

| Type | Required flag(s) | Effect of wrong value |
|------|------------------|-----------------------|
| `bearer` | `--token T` | Stored token replaced |
| `basic` | `--username U --password P` | Stored basic creds replaced |
| `cookie` | `--cookie C` | Stored cookie value replaced |
| `custom` | `--custom-header 'K: V'` (one or more — overwrites the full set) | Stored header map replaced |
| `form` | `--password P` | Stored password replaced + session file deleted (forces re-login) |

Commands that need the secret (escalation paths):
- `profile allow / allow-path` — widens host or path allowlist
- `profile set-default-header / set-allow-http / set-user-agent`
- `session mark-visible <name> <cookie...>` — un-masks stored cookie value(s)

Commands that don't (destructive — not escalation):
- `profile remove`, `profile disallow`, `profile disallow-path`
- `session clear`, `session set-expires`, `session mark-sensitive`

Commands that don't (reads):
- `profile list / show / test`, `session status / show`, `audit tail / summary`
- `fetch / graphql / tpl run` (the auth flows through but isn't being modified)

## Cookie jar generalisation

In v1, only form-auth profiles had a cookie jar (the session file). v2 makes a persistent cookie jar a property of *every* profile. Bearer/basic/cookie/custom upstreams that set cookies on responses (rolling sessions, anti-CSRF tokens) accumulate them across requests just like form-auth.

**Storage:** `~/.config/agent-deepweb/profiles/<name>/jar.json` (mode 0600). Contains:
- `cookies: []PersistedCookie` — the persisted cookies with the same Sensitive flag classification as today
- `token: string` — for form-auth profiles, the bearer token extracted at login (was `Session.Token`)
- `acquired: Time, expires: Time` — same as today

This unifies what was previously two kinds of state (`credentials.json` index + `sessions/<name>.json`).

## Anonymous + bring-your-own jar: `--profile none --cookiejar <path>`

`--profile none` means: no profile, no allowlist check, no auth attached.

`--cookiejar <path>` means: use the JSONL jar at this path. Created if missing. Plain JSON, no encryption — the caller chose the file location and the contents.

Combinations:

| `--profile` | `--cookiejar` | Behaviour |
|-------------|---------------|-----------|
| `<name>` | (omitted) | Use profile's default jar |
| `<name>` | `<path>` | Use profile's auth + caller's jar (overrides the default) |
| `none` | (omitted) | Anonymous, stateless (no cookies sent or stored) |
| `none` | `<path>` | Anonymous, persistent jar at path — the LLM-authored-flow case |

This unlocks scenarios like an LLM running a complete signup → login → action flow against a test service. The LLM supplies the credentials inline (in the request body), the response sets cookies, the jar persists them, subsequent requests carry them. The "secret" the LLM ends up holding is one it chose.

## Anonymous requests are gated by explicitness, not mode

In v1, agent mode refused anonymous requests via `RequireAuthInAgentMode`. In v2, `--profile none` is required to make an anonymous request. Forgetting `--profile` (or supplying a profile that doesn't match the URL host) errors out. There's no "you're in a mode that allows anonymous by default" branch — anonymous is always explicit.

This achieves the same property as `RequireAuthInAgentMode` (prevents agent-deepweb from being a generic HTTP client by accident) without depending on mode detection.

## Migration

On first run after upgrade, agent-deepweb detects v1 layout and migrates in place:

```
~/.config/agent-deepweb/credentials.json        →  ~/.config/agent-deepweb/profiles.json
~/.config/agent-deepweb/credentials.secrets.json →  (merged into profiles.json on non-darwin)
~/.config/agent-deepweb/sessions/<name>.json    →  ~/.config/agent-deepweb/profiles/<name>/jar.json
```

Keychain entries keep their existing service name (`app.paulie.agent-deepweb`) and account (the profile name). No re-prompt.

If migration fails, the binary refuses to run with a `fixable_by:human` error pointing at the backup directory we wrote.

## Audit log changes

Drop `agent_mode` and `human_mode_requests` / `agent_mode_requests` from the entry/summary — they're dead concepts. Add:

- `profile string` — the profile name in use (or `"none"` for anonymous, or `""` for ad-hoc URL)
- `jar string` — path to the cookie jar used (when `--cookiejar` was passed) or `""` for default

These are the new tripwires. A user reviewing the audit log can grep for `"profile":"none"` to see every anonymous request, or for unexpected `"jar"` paths to spot LLM-written jars.

## SKILL.md recommended permission config

The SKILL ships with a recommended Claude Code permission rule set that allowlists exactly the LLM-safe commands:

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
  agent-deepweb session status *
  agent-deepweb session show *
  agent-deepweb audit tail *
  agent-deepweb audit summary

(Implicitly denied: profile add/remove/allow/allow-path/set-*/disallow/disallow-path,
 login, session clear/set-expires/mark-*, tpl import/remove, --cookiejar)
```

This is the actual security boundary. The binary cooperates by being well-behaved; the harness enforces.

## Open questions resolved (locked-in decisions)

1. **Keep `--auth` as alias?** No — clean break. Migration is one find-and-replace in the user's saved scripts; cleaner is better than dual-flag confusion.
2. **`--profile none` cookie jar default?** No jar by default. `--cookiejar` is opt-in.
3. **Form session vs profile jar.** Unified into one file per profile (`profiles/<name>/jar.json`), holds cookies + optional bearer token + expiry.
4. **Migration.** One-shot on first run, in place, with backup if it fails.

## Phased rollout

| Phase | Scope | Estimate |
|-------|-------|----------|
| **A** | Drop mode infra (env var, IsAgentMode, RefuseInAgentMode, HumanOnlyRunE, RefuseFlag, RequireAuthInAgentMode, --no-redact, per-request --allow-http, audit agent_mode field). Variadic `session mark-visible/sensitive`. | ~150 lines deleted |
| **B** | Password-protect escalation. Per-type primary-secret re-assertion. Session invalidation on form-secret change. | ~80 lines added |
| **C** | Rename `creds` → `profile`. CLI verbs, package layout, config file names. One-shot migrator. README/AGENTS/SKILL rewrites. | Large mechanical diff |
| **D** | Generalise cookie jar to all profile types. `--profile none`. `--cookiejar <path>`. Unify form session into per-profile jar. | ~200 lines added |

A and B are tractable now. C is mostly mechanical. D is a feature add that depends on C's storage refactor.

## Test plan

- A: existing tests are mostly the inverse of what we want — rewrite "agent mode refuses X" tests to "X works for everyone." Drop `gate_test.go` (RequireAuthInAgentMode). Drop `IsAgentMode` tests.
- B: per-type table tests for "wrong primary secret silently breaks credential." Integration test: LLM widens allowlist with garbage token, fetches attacker host, asserts request goes out with garbage auth (not real auth) and session is invalidated.
- C: migration test — fixture v1 layout in tempdir, run binary once, assert v2 layout.
- D: jar-bring-your-own test — `--profile none --cookiejar /tmp/test.json`, do a POST that sets cookies, do a GET that consumes them.

## Risks / things to watch

- **Migration failure.** A user with a complex setup (manually-edited credentials.json, dangling sessions) might hit an edge case. Mitigation: write the backup first, then migrate; on failure, restore from backup and refuse to run.
- **Documentation lag.** Three doc files (README, AGENTS.md, SKILL.md) plus the prompt in /tmp need synchronised updates per phase.
- **Community surprise.** First release is v0.1.0 — no public users yet. Free to make breaking changes.

## What we keep from v1

- The HTTP transport layer (api/) including redaction, scheme enforcement, redirect allowlist enforcement, classify*. All security-critical and well-tested. Untouched.
- The audit log infrastructure (drop `agent_mode` field; everything else stays).
- The macOS Keychain integration. Same service name.
- The template system (highest-safety mode). Renamed only if convenient; otherwise stays as `tpl`.
- The mockdeep test fixture.

## Out of scope for v2

- Browser-assisted login. Still future.
- Native Security Framework API (CGo) for true keychain ACLs. Still parked behind harness-cooperation.
- Response diffs / caching. Still future.

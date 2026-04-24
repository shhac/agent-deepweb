package profile

const usageText = `profile — profile (auth identity) management

USAGE
  agent-deepweb profile <subcommand>

A "profile" bundles secret material with its host[:port]+path allowlist,
default headers, User-Agent override, and (for form auth) the derived
session token. Per-profile encrypted cookie jars (AES-256-GCM) live at
~/.config/agent-deepweb/profiles/<name>/jar.json with a key stored
alongside the primary secret (Keychain on macOS, file 0600 elsewhere).

SUBCOMMANDS (agent-safe — read-only or non-escalating)
  list                                    All profiles (no secrets)
  show <name>                             Metadata for one profile
  test <name>                             Send a request to the profile's health URL
  remove <name>                           Delete profile + secrets + jar + tracked records
  disallow <name> <domain>                Narrow allowlist (no escalation)
  disallow-path <name> <pattern>          Narrow path allowlist (no escalation)
  unset-default-header <name> <key>       Remove a default header
  set-health <name> <url>                 Set the health-check URL
  set-user-agent <name> <ua>              Set per-profile User-Agent
  mark-header-sensitive <name> <h> [...]  Force redact headers beyond the built-in regex

RELATED — COOKIE JAR (see 'agent-deepweb login llm-help' for full details)
  jar status <name>                       Cookie count / expiry / has-token summary
  jar show <name>                         Cookies (sensitive values redacted)
  jar clear <name>                        Empty the jar (reset session state)

SUBCOMMANDS (HUMAN-ONLY — escalation; require --passphrase)
  add <name> --type <t> --domain <d> [...]  Register a new profile
  allow <name> <domain> --passphrase <p>    Widen host allowlist
  allow-path <name> <pattern> --passphrase <p>   Widen path allowlist
  set-default-header <name> "K: V" --passphrase <p>  Add an outbound default header
  set-allow-http <name> true --passphrase <p>  Permit http:// for this profile
  set-secret <name> --passphrase <p> [new-secret flags]  Rotate the primary secret
  set-passphrase <name> --passphrase <p> --new-passphrase <n>  Rotate just the passphrase
  mark-header-visible <name> <h> [...] --passphrase <p>  Force-show headers the default regex redacts

THE PASSPHRASE
  Every escalation requires --passphrase, which is constant-time
  verified against a value stored with the profile.

  At 'profile add' time, the human may optionally supply
  --passphrase <phrase> to set a short friendly string (min 12 chars).
  If not supplied, the passphrase auto-defaults to the primary secret
  (bearer token / password / cookie value / the header map for custom).
  That means a profile registered without --passphrase is still
  escalatable — by typing the primary secret into --passphrase.

  An auto-derived passphrase re-derives on set-secret (so the
  symmetry holds). A human-set passphrase persists across primary
  rotations unless explicitly changed via --new-passphrase.

  A wrong passphrase errors cleanly (fixable_by:agent). The LLM
  without the passphrase cannot perform any escalation; the harness
  allowlist (SKILL.md) denies escalation commands to the LLM in the
  first place.

AUTH TYPES
  bearer      Static Bearer token (or custom header)
  basic       Username + password
  cookie      Raw Cookie header value
  form        Form-based login producing a session (requires 'agent-deepweb login')
  custom      Arbitrary set of headers applied verbatim

EXAMPLES
  Register a bearer profile with a friendly passphrase:
    agent-deepweb profile add github --type bearer \
      --token ghp_xxx --domain api.github.com \
      --passphrase 'gh-admin-phrase-2026'

  Register a basic-auth profile (no passphrase — primary is the default):
    agent-deepweb profile add intranet --type basic \
      --username alice --password 'pw' --domain intranet.example.com

  Widen scope:
    agent-deepweb profile allow github gist.github.com --passphrase 'gh-admin-phrase-2026'

  Rotate the bearer token, keep the friendly passphrase:
    agent-deepweb profile set-secret github --passphrase 'gh-admin-phrase-2026' --token ghp_NEW

NOTES
  - 'show' and 'list' never reveal secret values or the passphrase.
  - A profile only applies to its allowlisted host[:port]/path.
  - 'remove' clears the entire profile state including the encrypted jar.
`

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
  remove <name>                           Delete profile + secrets + jar
  disallow <name> <domain>                Narrow allowlist (no escalation)
  disallow-path <name> <pattern>          Narrow path allowlist (no escalation)
  unset-default-header <name> <key>       Remove a default header
  set-health <name> <url>                 Set the health-check URL
  set-user-agent <name> <ua>              Set per-profile User-Agent

SUBCOMMANDS (HUMAN-ONLY — escalation; require re-asserting primary secret)
  add <name> --type <t> --domain <d> [...]  Register a new profile
  allow <name> <domain> --token T           Widen host allowlist
  allow-path <name> <pattern> --token T     Widen path allowlist
  set-default-header <name> "K: V" --token T  Add an outbound default header
  set-allow-http <name> true --token T      Permit http:// for this profile

The "primary secret" varies per type (passed at the same flag(s) used at add):
  bearer    --token T
  basic     --username U --password P
  cookie    --cookie C
  custom    --custom-header 'K: V' (one or more)
  form      --password P (clears the session on overwrite)

The mechanism is overwrite, not verify. A human who knows the value
produces a no-op overwrite; an LLM that doesn't ends up with a useless
broken profile and an audit-log breadcrumb.

AUTH TYPES
  bearer      Static Bearer token (or custom header)
  basic       Username + password
  cookie      Raw Cookie header value
  form        Form-based login producing a session (requires 'agent-deepweb login')
  custom      Arbitrary set of headers applied verbatim

EXAMPLES
  Register a bearer profile:
    agent-deepweb profile add github --type bearer \
      --token ghp_xxx --domain api.github.com

  Register a basic-auth profile:
    agent-deepweb profile add intranet --type basic \
      --username alice --password 'pw' --domain intranet.example.com

  Multiple domains at once:
    agent-deepweb profile add github --type bearer --token ghp_xxx \
      --domain api.github.com --domain uploads.github.com

  Widen scope (escalation — re-supply primary secret):
    agent-deepweb profile allow github gist.github.com --token ghp_xxx

NOTES
  - 'show' and 'list' never reveal secret values.
  - A profile only applies to its allowlisted host[:port]/path; off-allowlist use is refused.
  - 'remove' clears the entire profile state including the encrypted jar.
`

package creds

const usageText = `creds — credential management

USAGE
  agent-deepweb creds <subcommand>

SUBCOMMANDS (agent-safe)
  list                         List credentials (name, type, domains, storage)
  show <name>                  Metadata for one credential (no secret values)
  test <name>                  Send a request to the credential's health URL

SUBCOMMANDS (HUMAN-ONLY — refuse in agent mode)
  add <name>                   Register a new credential
  remove <name>                Delete a credential
  allow <name> <domain>        Add a domain to the allowlist
  disallow <name> <domain>     Remove a domain from the allowlist
  set-health <name> <url>      Set health-check URL

AUTH TYPES
  bearer      Static Bearer token (or custom header)
  basic       Username + password
  cookie      Raw Cookie header value
  form        Form-based login producing a session (requires 'agent-deepweb login')
  custom      Arbitrary set of headers applied verbatim

HUMAN-ONLY EXAMPLES
  Add a Bearer-token credential:
    agent-deepweb creds add github --type bearer \
      --token ghp_xxx --domain api.github.com

  Add a basic-auth credential:
    agent-deepweb creds add intranet --type basic \
      --username alice --password 'pw' --domain intranet.example.com

  Add a cookie-based credential (paste a known Cookie header):
    agent-deepweb creds add dashboard --type cookie \
      --cookie 'session=abc123; foo=bar' --domain dashboard.example.com

  Add multiple domains at once:
    agent-deepweb creds add github --type bearer --token ghp_xxx \
      --domain api.github.com --domain uploads.github.com

NOTES
  - Secret values are written to the macOS Keychain when available; otherwise
    to ~/.config/agent-deepweb/credentials.secrets.json (mode 0600).
  - 'show' and 'list' never reveal secret values.
  - A credential only applies to its allowlisted domains; off-domain use is refused.
`

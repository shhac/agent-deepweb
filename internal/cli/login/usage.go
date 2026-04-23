package login

const usageText = `login / session — form-based login flows

USAGE
  agent-deepweb login <name>
  agent-deepweb session status <name>
  agent-deepweb session clear <name>

SUMMARY
  For credentials of --type form, 'login' performs the form submission and
  stores the resulting cookies as a session (separate from the credential).
  'fetch' and 'graphql' will automatically attach session cookies when you
  reference a form-auth credential.

STATUS
  v1: form login is not yet implemented. The session-status and
  session-clear subcommands work for cookies written by future flows or
  for pre-populated session files.
`

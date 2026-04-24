package login

const usageText = `login / jar — form login + per-profile cookie jar

USAGE
  agent-deepweb login <name>                    Run a form-login (HUMAN-ONLY)
  agent-deepweb jar status <name>               Cookie count, expiry, has-token
  agent-deepweb jar show <name>                 Cookies (sensitive values redacted)
  agent-deepweb jar clear <name>                Wipe the jar
  agent-deepweb jar set-expires <name> <when>   Override expiry (duration or RFC3339)
  agent-deepweb jar mark-sensitive <name> <c1> [c2 ...]
  agent-deepweb jar mark-visible   <name> <c1> [c2 ...] --passphrase <p>   (escalation)

SUMMARY
  For profiles of --type form, 'login' performs the form submission and
  stores the resulting cookies + (optional) bearer token in the profile's
  encrypted jar. 'fetch' and 'graphql' will automatically attach the
  session cookies (and token) when you reference the form profile.

  In v2 the jar is universal: bearer/basic/cookie/custom profiles also
  accumulate Set-Cookie cookies into their jar across requests. Inspect
  with 'jar show <profile-name>'.

  All sensitive cookie values (HttpOnly, or matching common auth-name
  patterns like session/csrf/auth/token) are redacted in 'jar show'.
  Use 'jar mark-sensitive' to force more cookies into the redacted set
  (no escalation), or 'jar mark-visible' to un-mask one (escalation —
  requires --passphrase).

STATUS
  Form login is implemented end-to-end. The flow:
    1. POST username/password (form or JSON) to the credential's login_url.
    2. Harvest Set-Cookie headers; classify each cookie sensitive or visible.
    3. Optionally extract a bearer token from a JSON body via --token-path.
    4. Compute jar expiry as min(session-ttl, latest-cookie-expiry, +24h).

CUSTOM LOGIN BODIES (--login-body-template)
  Some APIs don't accept a flat {username, password} body — GraphQL-mutation
  logins and OAuth2 password-grant bodies are common examples. Use
  --login-body-template to supply the full JSON body as a template with
  {{username}} / {{password}} / {{<extra-field>}} placeholders; substituted
  values are JSON-string-escaped so embedded quotes don't corrupt the
  output. Content-Type is forced to application/json when this is set.

  Example — GraphQL mutation login:
    agent-deepweb profile add myapi --type form \
      --login-url https://api.example.com/graphql \
      --username alice --password 'pw' \
      --login-body-template '{"query":"mutation($u:String!,$p:String!){ signIn(input:{username:$u,password:$p}){ tokens { bearer }}}","variables":{"u":"{{username}}","p":"{{password}}"}}' \
      --token-path data.signIn.tokens.bearer \
      --domain api.example.com
    agent-deepweb login myapi

  Example — OAuth2 password-grant body:
    agent-deepweb profile add myapi --type form \
      --login-url https://auth.example.com/oauth/token \
      --username alice --password 'pw' \
      --extra-field grant_type=password --extra-field client_id=abc \
      --login-body-template '{"grant_type":"{{grant_type}}","client_id":"{{client_id}}","username":"{{username}}","password":"{{password}}"}' \
      --token-path access_token \
      --domain auth.example.com

  Rules of thumb:
    - Always place placeholders INSIDE JSON string quotes: "u":"{{username}}".
    - An unknown {{placeholder}} fails loudly with fixable_by:human —
      typos don't silently produce broken logins.
    - The whole body must be valid JSON; we validate post-substitution
      and fail before making the request if it isn't.
`

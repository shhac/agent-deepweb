package login

const usageText = `login / jar — form login + per-profile cookie jar

USAGE
  agent-deepweb login <name>                    Run a form-login (HUMAN-ONLY)
  agent-deepweb jar status <name>               Cookie count, expiry, has-token
  agent-deepweb jar show <name>                 Cookies (sensitive values redacted)
  agent-deepweb jar clear <name>                Wipe the jar
  agent-deepweb jar set-expires <name> <when>   Override expiry (duration or RFC3339)
  agent-deepweb jar mark-sensitive <name> <c1> [c2 ...]
  agent-deepweb jar mark-visible   <name> <c1> [c2 ...] --token T   (escalation)

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
  re-supply the profile's primary secret; wrong value silently breaks
  the profile).

STATUS
  Form login is implemented end-to-end. The flow:
    1. POST username/password (form or JSON) to the credential's login_url.
    2. Harvest Set-Cookie headers; classify each cookie sensitive or visible.
    3. Optionally extract a bearer token from a JSON body via --token-path.
    4. Compute jar expiry as min(session-ttl, latest-cookie-expiry, +24h).
`

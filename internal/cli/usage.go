package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func registerUsageCommand(root *cobra.Command) {
	usage := &cobra.Command{
		Use:   "usage",
		Short: "Show LLM-optimized reference card",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Print(referenceCard)
		},
	}
	root.AddCommand(usage)
}

const referenceCard = `agent-deepweb — authenticated HTTP for AI agents

curl-with-auth: the user registers profiles (auth identities) under names;
this tool attaches them to outgoing requests. You (the agent) reference
profiles by name but never see the secret values.

All output is JSON to stdout. Errors are JSON to stderr:
  { "error": "...", "hint": "...", "fixable_by": "agent|human|retry" }

QUICK START (read-only — safe to explore)
  profile list                       List profiles (name, type, domains; no secrets)
  profile show <name>                Metadata for one profile
  profile test <name>                Send a health-check request
  jar status <name>                  Per-profile jar summary (cookie count, expiry)
  jar show <name>                    Per-profile jar (sensitive cookies redacted)
  fetch <url>                        Authenticated GET (picks profile by host)
  fetch <url> --profile <name>       Use a specific profile
  fetch <url> --profile none         Explicit anonymous (no profile attached)

COMMON WORKFLOWS
  Call an authenticated JSON API:
    fetch https://api.example.com/v1/me --profile myapi

  Send a GraphQL query:
    graphql https://api.example.com/graphql \
      --profile myapi \
      --query 'query { me { id name } }'

  Send a JSON-RPC 2.0 call:
    jsonrpc https://rpc.example.com --profile myrpc \
      --method getThing --params '["arg"]'

  POST JSON:
    fetch https://api.example.com/v1/items --profile myapi \
      --method POST --json '{"name":"x"}'

  Explicit anonymous fetch (no profile, no jar):
    fetch https://example.com/healthz --profile none

  LLM-authored end-to-end flow with bring-your-own jar:
    fetch https://test.example.com/signup --profile none --cookiejar /tmp/flow.json \
      --method POST --json '{"email":"...","password":"..."}'
    fetch https://test.example.com/me     --profile none --cookiejar /tmp/flow.json
    # Cookies persist between requests in the BYO jar (plaintext at the path).

GLOBAL FLAGS
  --profile <name>                   Profile name, or 'none' for explicit anonymous
                                     (falls back to config 'default.profile')
  --cookiejar <path>                 Bring-your-own cookie jar (plaintext JSON file).
                                     Overrides the profile's encrypted default.
  --format json|jsonl|raw|text       Output format (default: json)
  --timeout <ms>                     Request timeout in ms (falls back to config 'default.timeout-ms')

CONFIG
  Persistent user config lives at ~/.config/agent-deepweb/config.json
  and is managed via 'agent-deepweb config {list-keys,get,set,unset}'.
  Precedence: per-invocation flag > config value > built-in default.

ERROR HANDLING
  fixable_by: agent  — your input was wrong (typo, bad URL, bad JSON); fix and retry
  fixable_by: human  — needs the user (missing profile, expired session, denied scope)
  fixable_by: retry  — transient (network, 429, 5xx); retry with backoff

SECRET-SAFETY RULES
  - Profiles are referenced by name; the tool never prints secret values.
  - Responses are redacted: auth headers and common token fields are replaced with "<redacted>".
  - A profile only applies on its allowed host[:port] / path. Off-allowlist requests are refused.
  - Anonymous requests must be opt-in: pass --profile none (or no profile matches the host → error).
  - Profile-default jars are encrypted at rest with a per-profile key (AES-256-GCM)
    stored alongside the primary secret. BYO jars (--cookiejar <path>) are plaintext —
    the caller picked the path.
  - Escalation commands (profile allow / set-default-header / set-allow-http /
    set-secret / jar mark-visible) require --passphrase, which is
    verified (constant-time) against a value stored with the profile. A
    wrong passphrase errors cleanly; the LLM without it can't escalate.
    The passphrase defaults to the primary secret unless the human set
    a friendly one at 'profile add' time via --passphrase.

PER-VERB REFERENCE (run these for detailed help)
  fetch usage                        fetch command reference
  graphql usage                      graphql command reference
  jsonrpc usage                      jsonrpc command reference
  template usage                     template commands reference
  profile usage                      profile commands reference
  login usage                        login / jar commands reference
  audit usage                        audit log commands reference
  config usage                       persistent user config

PROFILE MANAGEMENT (typically human-run; LLMs without secret values
get useless results which are themselves an audit signal)

  profile list
  profile show <name>
  profile test <name>
  profile add <name> --type <t> --domain <host> [--passphrase <p>] [...]
  profile remove <name>                            (clears jar too)
  profile allow <name> <domain> --passphrase <p>
  profile disallow <name> <domain>
  profile allow-path <name> <pattern> --passphrase <p>
  profile set-default-header <name> "K: V" --passphrase <p>
  profile set-allow-http <name> true --passphrase <p>
  profile set-secret <name> --passphrase <p> [--token T | --password P | ...]
  profile set-passphrase <name> --passphrase <p> --new-passphrase <n>
  profile set-health <name> <url>
  profile set-user-agent <name> <ua>

JAR MANAGEMENT
  jar status <name>                                Cookie count, expiry, has-token
  jar show <name>                                  Cookies (sensitive values redacted)
  jar clear <name>                                 Wipe the jar
  jar set-expires <name> <duration|RFC3339>
  jar mark-sensitive <name> <c1> [c2 ...]          Force redaction in jar show
  jar mark-visible   <name> <c1> [c2 ...] --passphrase <p>
`

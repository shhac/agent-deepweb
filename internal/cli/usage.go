package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func registerUsageCommand(root *cobra.Command) {
	usage := &cobra.Command{
		Use:   "llm-help",
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
  fetch <url>                        Authenticated GET (picks profile by host)
  fetch <url> --auth <name>          Use a specific profile

COMMON WORKFLOWS
  Call an authenticated JSON API:
    fetch https://api.example.com/v1/me --auth myapi

  Send a GraphQL query:
    graphql https://api.example.com/graphql \
      --auth myapi \
      --query 'query { me { id name } }'

  POST JSON:
    fetch https://api.example.com/v1/items --auth myapi \
      --method POST --json '{"name":"x"}'

  Anonymous fetch (no profile):
    fetch https://example.com/healthz --no-auth

GLOBAL FLAGS
  --auth <name>                      Profile name (or AGENT_DEEPWEB_AUTH env)
  --format json|jsonl|raw|text       Output format (default: json)
  --timeout <ms>                     Request timeout in milliseconds

ERROR HANDLING
  fixable_by: agent  — your input was wrong (typo, bad URL, bad JSON); fix and retry
  fixable_by: human  — needs the user (missing profile, expired session, denied scope)
  fixable_by: retry  — transient (network, 429, 5xx); retry with backoff

SECRET-SAFETY RULES
  - Profiles are referenced by name; the tool never prints secret values.
  - Responses are redacted: auth headers and common token fields are replaced with "<redacted>".
  - A profile only applies on its allowed host[:port] / path. Off-allowlist requests are refused.
  - Anonymous requests must be opt-in: pass --no-auth (or no profile matches the host → error).
  - Escalation commands (profile allow / set-default-header / set-allow-http /
    session mark-visible) require re-supplying the profile's primary secret. The
    LLM, which doesn't have it, can't widen scope or un-mask cookies usefully:
    a wrong value silently overwrites the stored secret with garbage.

PER-VERB REFERENCE (run these for detailed help)
  fetch llm-help                     fetch command reference
  graphql llm-help                   graphql command reference
  tpl llm-help                       template commands reference
  profile llm-help                   profile commands reference
  login llm-help                     login / session commands reference
  audit llm-help                     audit log commands reference

PROFILE MANAGEMENT (typically human-run; LLMs without secret values
get useless results which are themselves an audit signal)

  profile list
  profile show <name>
  profile test <name>
  profile add <name> --type <t> --domain <host> [...]
  profile remove <name>
  profile allow <name> <domain> --token T          (re-supply primary secret)
  profile disallow <name> <domain>
  profile allow-path <name> <pattern> --token T    (re-supply primary secret)
  profile set-default-header <name> "K: V" --token T
  profile set-allow-http <name> true --token T
  profile set-health <name> <url>
  profile set-user-agent <name> <ua>
`

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

curl-with-auth: the user stores credentials under names; this tool attaches
them to outgoing requests. You (the agent) can reference credentials by name
but never see the secret values.

All output is JSON to stdout. Errors are JSON to stderr:
  { "error": "...", "hint": "...", "fixable_by": "agent|human|retry" }

QUICK START (read-only — safe to explore)
  creds list                         List credentials (name, type, domains; no secrets)
  creds show <name>                  Metadata for one credential
  creds test <name>                  Send a health check request
  fetch <url>                        Authenticated GET (picks credential by host)
  fetch <url> --auth <name>          Use a specific credential

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

GLOBAL FLAGS
  --auth <name>                      Credential alias (or AGENT_DEEPWEB_AUTH env)
  --format json|jsonl|raw|text       Output format (default: json)
  --timeout <ms>                     Request timeout in milliseconds

ERROR HANDLING
  fixable_by: agent  — your input was wrong (typo, bad URL, bad JSON); fix and retry
  fixable_by: human  — needs the user (missing credential, expired session, denied scope)
  fixable_by: retry  — transient (network, 429, 5xx); retry with backoff

SECRET-SAFETY RULES
  - Credentials are referenced by name; the tool never prints secret values.
  - Responses are redacted: auth headers and common token fields are replaced with "<redacted>".
  - Credential add/remove/allow is human-only. In agent mode, those commands refuse
    with fixable_by: human. Ask the user to run them.
  - A credential only applies on its allowed domains. Using it off-domain is refused.

PER-ENTITY REFERENCE (run these for detailed help)
  fetch llm-help                     fetch command reference
  graphql llm-help                   graphql command reference
  creds llm-help                     credential commands reference
  login llm-help                     login / session commands reference

CREDENTIAL MANAGEMENT (most are human-only)
  creds list
  creds show <name>
  creds test <name>
  creds add <name> --type <t> --domain <host> [...]   (human-only)
  creds remove <name>                                 (human-only)
  creds allow <name> <domain>                         (human-only)
  creds disallow <name> <domain>                      (human-only)
  creds set-health <name> <url>                       (human-only)
`

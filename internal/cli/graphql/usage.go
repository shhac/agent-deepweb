package graphql

const usageText = `graphql — authenticated GraphQL request

USAGE
  agent-deepweb graphql <endpoint> [flags]

SUMMARY
  POSTs a JSON body {"query": ..., "variables": ..., "operationName": ...}
  to the endpoint with Content-Type: application/json and the profile's
  auth attached. GraphQL errors returned in the "errors" array are
  surfaced as a top-level error with fixable_by:agent (or :human when
  auth-related).

FLAGS
  --profile <name|none>      Profile alias, or 'none' for anonymous
  --cookiejar <path>         Bring-your-own plaintext cookie jar
  --query <string|@file|@->  GraphQL document (required)
  --variables <json|@file>   JSON object for variables
  --operation-name <name>    Operation name for multi-op documents
  --timeout <ms>
  --max-size <bytes>
  --format json|raw|text

EXAMPLES
  agent-deepweb graphql https://api.github.com/graphql --profile github \
    --query 'query { viewer { login } }'

  agent-deepweb graphql https://api.example.com/graphql --profile myapi \
    --query @./query.graphql --variables '{"id":"123"}'
`

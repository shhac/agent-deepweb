package jsonrpc

const usageText = `jsonrpc — authenticated JSON-RPC 2.0 request

USAGE
  agent-deepweb jsonrpc <endpoint> --method <name> [flags]

SUMMARY
  POSTs a JSON-RPC 2.0 envelope to the endpoint:
    {"jsonrpc":"2.0","method":<name>,"params":<...>,"id":<n>}
  with Content-Type: application/json and the profile's auth attached.
  The response is parsed into result vs error.{code,message,data}.
  Standard error codes (-32700 / -32600 / -32601 / -32602) surface as
  fixable_by:agent; internal (-32603) and server-defined (-32000..-32099)
  codes surface as fixable_by:human.

FLAGS
  --profile <name|none>       Profile alias, or 'none' for anonymous
  --cookiejar <path>          Bring-your-own plaintext cookie jar
  --method <name>             RPC method name (required)
  --params <json|@file|@->    Params — JSON array (positional) or object (named)
  --id <string|int>           Request id (default '1'; numeric coerced)
  --notify                    Send as a notification (no id, server won't reply)
  --timeout <ms>
  --max-size <bytes>
  --format json|raw|text
  --track                     Persist a full-fidelity record (use 'audit show <id>')
  --hide-request              Omit 'request' block from envelope
  --hide-response             Omit response headers/body (keep status + profile)

EXAMPLES
  # Ethereum node: get the current block number (no params)
  agent-deepweb jsonrpc https://mainnet.infura.io/v3/KEY --profile infura \
    --method eth_blockNumber

  # Positional params as a JSON array
  agent-deepweb jsonrpc https://mainnet.infura.io/v3/KEY --profile infura \
    --method eth_getBalance --params '["0x1234...",latest]'

  # Named params as a JSON object
  agent-deepweb jsonrpc https://rpc.example.com --profile myrpc \
    --method createItem --params '{"name":"x","qty":3}'

  # Discover available methods on an Ethereum-like node
  agent-deepweb jsonrpc https://rpc.example.com --profile myrpc \
    --method rpc_modules
`

package templatecli

const usageText = `template — parameterised request templates

USAGE
  agent-deepweb template list
  agent-deepweb template show <name>
  agent-deepweb template run <name> --param k=v [--param k=v ...]
  agent-deepweb template import <file>                  (human-only)
  agent-deepweb template import-openapi <spec-json>     (human-only)
  agent-deepweb template remove <name>                  (human-only)

SUMMARY
  A template is a frozen request shape authored by the human. The LLM can
  ONLY fill in parameter values — it cannot change URL, method, headers,
  profile binding, or body shape. This is the highest-safety mode:
  data-only input, templated substitution, no free-form URLs.

TEMPLATE FILE FORMAT
  {
    "name": "myapi.get_item",
    "description": "Fetch an item by id",
    "method": "GET",
    "url": "https://api.example.com/items/{{id}}",
    "profile": "myapi",
    "parameters": {
      "id": { "type": "string", "required": true }
    }
  }

  Or multiple at once:
  {
    "myapi.get_item": { ... },
    "myapi.list_items": { ... }
  }

PARAMETER TYPES
  string | int | number | bool | string-array
  Each parameter may specify required (bool), default (typed), enum (list),
  and description (string).

BODY TEMPLATE (type-preserving substitution)
  {
    "body_format": "json",
    "body_template": {
      "title":   "{{title}}",
      "labels":  "{{labels}}",     // string-array → JSON array
      "priority":"{{priority}}"    // int → JSON number
    }
  }

INVOCATION EXAMPLES
  agent-deepweb template run myapi.get_item --param id=abc123
  agent-deepweb template run myapi.create_widget \
    --param name=blue-widget --param priority=5 --param labels=a,b,c

OUTPUT
  Same JSON envelope as 'fetch', plus "template": "<name>" and resolved
  "url" so the caller can see exactly what was sent. Errors keep the
  fixable_by classification.

OPENAPI IMPORT
  Translate an OpenAPI v3 JSON spec into one template per operation:
    agent-deepweb template import-openapi ./spec.json \
      --prefix myapi --profile myapi

  Flags:
    --prefix <ns>     Name-space for imports (required; e.g. 'myapi' →
                      'myapi.get_user', 'myapi.list_items')
    --profile <name>  Bind every imported template to this profile
    --tag <t>         Only operations with this OpenAPI tag (repeatable)
    --server <url>    Override the spec's servers[0].url (e.g. staging)

  Notes:
    - YAML specs: convert to JSON first (e.g. 'yq -o=json . spec.yaml')
    - requestBody application/json becomes a single 'body' object param
      (pass '--param body={\"k\":\"v\"}' or '--param body=@file.json')
    - in:cookie parameters are skipped (cookies come from the profile jar)
    - Path {id} placeholders become {{id}} in the stored URL

NOTES
  - Templates are audited with their name in the audit log ("template": "...").
  - Parameters are coerced and validated BEFORE any HTTP request is made,
    so bad input fails fast with fixable_by: agent.
  - Unknown parameter names are rejected (prevents the LLM from injecting
    parameters that weren't in the schema).
`

package templatecli

const usageText = `template — parameterised request templates

USAGE
  agent-deepweb template list
  agent-deepweb template show <name>
  agent-deepweb template run <name> --param k=v [--param k=v ...]
  agent-deepweb template import <file>                  (human-only)
  agent-deepweb template import-openapi <spec-json>     (human-only)
  agent-deepweb template import-postman <collection>    (human-only)
  agent-deepweb template import-har <capture>           (human-only)
  agent-deepweb template import-http <file.http>        (human-only)
  agent-deepweb template import-curl '<curl-cmd>'       (human-only)
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

IMPORT SOURCES
  All importers share --prefix (required, namespaces names) and
  --profile (optional, binds every import to that profile).

  import-openapi <spec.json>
    OpenAPI v3.x OR Swagger 2.0 JSON. requestBody application/json
    becomes a single 'body' object param. in:cookie parameters are
    dropped (profile jar handles those). YAML → convert first with
    'yq -o=json .'. Extra flags: --tag, --server.

  import-postman <collection.json>
    Postman Collection v2.x. Folders flattened into template names
    (prefix.folder_subfolder_request). Collection + folder variables
    inherited as ParamSpec defaults. Extra flags: --folder.

  import-har <capture.har>
    Browser HTTP Archive export. Authorization, Cookie, Set-Cookie,
    X-CSRF-*, X-XSRF-*, X-API-Key, User-Agent headers stripped at
    import (your real session shouldn't ship in templates). Extra
    flags: --url-contains, --dedupe.

  import-http <file.http>
    VS Code REST Client / JetBrains HTTP Client format. '@var = value'
    declarations become ParamSpec defaults; '### name' separators
    label each request.

  import-curl '<curl-command>'
    One pasted curl invocation → one template. Flag --name required
    (no filename to derive from). Silently ignores browser "Copy as
    cURL" noise (-L, -v, --compressed, -b, -A, etc.).

  graphql import-schema <endpoint>
    Runs an introspection query (via the usual profile/track pipeline)
    and emits one template per top-level Query/Mutation field, with
    typed ParamSpec for every arg.

NOTES
  - Templates are audited with their name in the audit log ("template": "...").
  - Parameters are coerced and validated BEFORE any HTTP request is made,
    so bad input fails fast with fixable_by: agent.
  - Unknown parameter names are rejected (prevents the LLM from injecting
    parameters that weren't in the schema).
`

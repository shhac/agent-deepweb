package tpl

const usageText = `tpl — parameterised request templates

USAGE
  agent-deepweb tpl list
  agent-deepweb tpl show <name>
  agent-deepweb tpl run <name> --param k=v [--param k=v ...]
  agent-deepweb tpl import <file>        (human-only)
  agent-deepweb tpl remove <name>        (human-only)

SUMMARY
  A template is a frozen request shape authored by the human. The LLM can
  ONLY fill in parameter values — it cannot change URL, method, headers,
  credential binding, or body shape. This is the highest-safety mode:
  data-only input, templated substitution, no free-form URLs.

TEMPLATE FILE FORMAT
  {
    "name": "github.get_user",
    "description": "Fetch a user's public profile",
    "method": "GET",
    "url": "https://api.github.com/users/{{username}}",
    "auth": "github",
    "parameters": {
      "username": { "type": "string", "required": true }
    }
  }

  Or multiple at once:
  {
    "github.get_user": { ... },
    "github.list_repos": { ... }
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
  agent-deepweb tpl run github.get_user --param username=octocat
  agent-deepweb tpl run myapi.create_widget \
    --param name=blue-widget --param priority=5 --param labels=a,b,c

OUTPUT
  Same JSON envelope as 'fetch', plus "template": "<name>" and resolved
  "url" so the caller can see exactly what was sent. Errors keep the
  fixable_by classification.

NOTES
  - Templates are audited with their name in the audit log ("template": "...").
  - Parameters are coerced and validated BEFORE any HTTP request is made,
    so bad input fails fast with fixable_by: agent.
  - Unknown parameter names are rejected (prevents the LLM from injecting
    parameters that weren't in the schema).
`

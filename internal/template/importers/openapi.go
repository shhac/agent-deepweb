package importers

import (
	"github.com/shhac/agent-deepweb/internal/template"

	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// ImportOpenAPIOptions controls how ImportOpenAPIFile translates an
// OpenAPI document into agent-deepweb templates.
type ImportOpenAPIOptions struct {
	// Prefix is prepended to every imported template's Name. A typical
	// value is the profile/API nickname: `github` → `github.get_user`,
	// `github.list_repos`, etc. Required; empty prefixes would collide
	// across imports.
	Prefix string
	// Profile is the agent-deepweb profile name written into every
	// imported template's Profile field. Leave empty for "bind later
	// via `template show`/edit" — but supplying it up front is the
	// common case (one spec → one profile).
	Profile string
	// TagFilter, when non-empty, limits imports to operations carrying
	// any of the listed OpenAPI tags. Lets the user slice a giant spec
	// (hundreds of endpoints) down to the subset they actually use.
	TagFilter []string
	// ServerOverride, when non-empty, replaces the spec's first
	// `servers[].url` entry. Useful when the spec ships with a "{basePath}"
	// placeholder or points at a prod URL and you want to target staging.
	ServerOverride string
}

// ImportOpenAPIFile reads an OpenAPI v3 JSON document from disk and
// translates each operation into a template.Template. YAML specs are not
// accepted in this iteration — convert first (e.g. `yq -o=json`).
//
// Returns the list of imported template names and any parse errors.
// If the spec parses but no operations pass the TagFilter, returns
// an empty list and a nil error (signals "filter was too narrow").
func ImportOpenAPIFile(path string, opts ImportOpenAPIOptions) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ImportOpenAPI(data, opts)
}

// ImportOpenAPI parses a JSON-encoded OpenAPI v3 document from data
// and stores one template.Template per operation. The pure byte-in API lets the
// CLI layer decide where the bytes came from (file, stdin, or a
// future URL fetcher).
func ImportOpenAPI(data []byte, opts ImportOpenAPIOptions) ([]string, error) {
	if opts.Prefix == "" {
		return nil, fmt.Errorf("openapi import requires --prefix (unique name-space for this spec)")
	}

	// Quick YAML detection — refuse loudly so the user knows why.
	if looksLikeYAML(data) {
		return nil, fmt.Errorf("spec appears to be YAML; convert to JSON first (e.g. `yq -o=json . spec.yaml > spec.json`)")
	}

	// Peek at the version marker so we can dispatch between v2 (swagger)
	// and v3 (openapi) without two copies of the unmarshal logic.
	var probe struct {
		Swagger string `json:"swagger"`
		OpenAPI string `json:"openapi"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("parse openapi spec: %w", err)
	}

	var doc openapiDoc
	switch {
	case strings.HasPrefix(probe.OpenAPI, "3."):
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("parse openapi spec: %w", err)
		}
	case strings.HasPrefix(probe.Swagger, "2."):
		// Swagger 2.0: parse into the v2 shape and transform to v3 so the
		// downstream operationToTemplate code path stays single-source.
		var v2 swaggerV2Doc
		if err := json.Unmarshal(data, &v2); err != nil {
			return nil, fmt.Errorf("parse swagger 2.0 spec: %w", err)
		}
		doc = swaggerV2ToV3(v2)
	default:
		return nil, fmt.Errorf("unsupported spec version (openapi=%q swagger=%q); need openapi v3.x or swagger v2.x", probe.OpenAPI, probe.Swagger)
	}

	serverURL := strings.TrimRight(opts.ServerOverride, "/")
	if serverURL == "" && len(doc.Servers) > 0 {
		serverURL = strings.TrimRight(doc.Servers[0].URL, "/")
	}
	if serverURL == "" {
		return nil, fmt.Errorf("no servers[].url in spec and no --server override supplied; cannot form absolute URLs")
	}

	tagSet := map[string]bool{}
	for _, t := range opts.TagFilter {
		tagSet[t] = true
	}

	var imported []string
	// Sort paths so stored templates are reproducible across runs.
	paths := make([]string, 0, len(doc.Paths))
	for p := range doc.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, path := range paths {
		item := doc.Paths[path]
		verbs := []string{"get", "post", "put", "patch", "delete", "head", "options"}
		for _, verb := range verbs {
			op := item[verb]
			if op == nil {
				continue
			}
			if len(tagSet) > 0 && !anyTagMatches(op.Tags, tagSet) {
				continue
			}
			tpl, err := operationToTemplate(verb, path, op, serverURL, opts)
			if err != nil {
				return imported, fmt.Errorf("%s %s: %w", strings.ToUpper(verb), path, err)
			}
			if err := template.Store(tpl); err != nil {
				return imported, err
			}
			imported = append(imported, tpl.Name)
		}
	}
	return imported, nil
}

// openapiDoc is the minimal subset of the OpenAPI v3 JSON shape we
// consume. Fields we don't translate (components, security schemes,
// server variables, callbacks, links) are ignored — the import is
// lossy on purpose, since an agent-deepweb template is a narrower
// shape than an OpenAPI operation.
type openapiDoc struct {
	OpenAPI string                     `json:"openapi"`
	Servers []openapiServer            `json:"servers"`
	Paths   map[string]openapiPathItem `json:"paths"`
}

type openapiServer struct {
	URL string `json:"url"`
}

// openapiPathItem — HTTP method (lowercase) → operation. OpenAPI also
// allows shared parameters at the path-item level; we don't handle
// those in v1 (operation-level parameters cover ~95% of real specs).
type openapiPathItem map[string]*openapiOperation

type openapiOperation struct {
	OperationID string              `json:"operationId"`
	Summary     string              `json:"summary"`
	Description string              `json:"description"`
	Tags        []string            `json:"tags"`
	Parameters  []openapiParameter  `json:"parameters"`
	RequestBody *openapiRequestBody `json:"requestBody"`
}

type openapiParameter struct {
	Name     string         `json:"name"`
	In       string         `json:"in"` // path | query | header | cookie
	Required bool           `json:"required"`
	Schema   *openapiSchema `json:"schema"`
}

type openapiRequestBody struct {
	Required bool                        `json:"required"`
	Content  map[string]openapiMediaType `json:"content"`
}

type openapiMediaType struct {
	Schema *openapiSchema `json:"schema"`
}

type openapiSchema struct {
	Type    string         `json:"type"`
	Enum    []any          `json:"enum"`
	Default any            `json:"default"`
	Items   *openapiSchema `json:"items"`
}

// operationToTemplate is the core translation: one OpenAPI operation
// → one template.Template. Path/query/header parameters become ParamSpecs;
// application/json request bodies become a single object-typed `body`
// parameter with a pass-through template. `in:cookie` is skipped (auth
// cookies come from the profile's jar).
func operationToTemplate(verb, path string, op *openapiOperation, serverURL string, opts ImportOpenAPIOptions) (template.Template, error) {
	method := strings.ToUpper(verb)
	name := fmt.Sprintf("%s.%s", opts.Prefix, operationSlug(op.OperationID, verb, path))

	// Translate the path's {param} placeholders to our {{param}} form.
	tplURL := serverURL + pathToPlaceholders(path)

	queryTpl := map[string]string{}
	headers := map[string]string{}
	params := map[string]template.ParamSpec{}

	for _, p := range op.Parameters {
		if p.Name == "" {
			continue
		}
		// Cookies are profile-jar territory; headers come from the
		// profile's default_headers or security. Path + query are the
		// two that a template should parameterise.
		switch p.In {
		case "path":
			params[p.Name] = paramSpecFromSchema(p, true) // path params are always required per spec
		case "query":
			params[p.Name] = paramSpecFromSchema(p, p.Required)
			queryTpl[p.Name] = fmt.Sprintf("{{%s}}", p.Name)
		case "header":
			params[p.Name] = paramSpecFromSchema(p, p.Required)
			headers[p.Name] = fmt.Sprintf("{{%s}}", p.Name)
		case "cookie":
			// silently skip — cookies come from the profile jar, not the template
		}
	}

	bodyFormat := ""
	var bodyTemplate json.RawMessage
	if op.RequestBody != nil {
		if media, ok := op.RequestBody.Content["application/json"]; ok {
			bodyFormat = "json"
			// Single object-typed pass-through. The user supplies the full
			// JSON body as `--param body='{"k":"v"}'` or `--param body=@file`.
			// A more ambitious import would walk the schema and emit one
			// param per body field, but that requires full $ref resolution.
			bodyTemplate = json.RawMessage(`"{{body}}"`)
			_ = media // reserved for future schema-driven expansion
			bodyParam := template.ParamSpec{
				Type:        "object",
				Required:    op.RequestBody.Required,
				Description: "Request body (JSON object). Pass --param body='{...}' or @file.",
			}
			params["body"] = bodyParam
		}
	}

	description := strings.TrimSpace(op.Summary)
	if description == "" {
		description = strings.TrimSpace(op.Description)
	}

	return template.Template{
		Name:         name,
		Description:  description,
		Method:       method,
		URL:          tplURL,
		Query:        queryTpl,
		Headers:      headers,
		Profile:      opts.Profile,
		BodyFormat:   bodyFormat,
		BodyTemplate: bodyTemplate,
		Parameters:   params,
	}, nil
}

// paramSpecFromSchema flattens an OpenAPI parameter schema into our
// template.ParamSpec. OpenAPI has richer types (format: date-time, pattern,
// minLength, etc.) that we drop in v1 — a stricter translator could
// surface them as additional Lint() warnings.
func paramSpecFromSchema(p openapiParameter, required bool) template.ParamSpec {
	spec := template.ParamSpec{
		Required: required,
	}
	if p.Schema == nil {
		spec.Type = "string"
		return spec
	}
	switch p.Schema.Type {
	case "integer":
		spec.Type = "int"
	case "number":
		spec.Type = "number"
	case "boolean":
		spec.Type = "bool"
	case "array":
		// Our template.ParamSpec.Type only knows string-array; mixed-type arrays
		// aren't representable. Fall back to string when the items type
		// isn't a simple string.
		if p.Schema.Items != nil && p.Schema.Items.Type != "string" {
			spec.Type = "string"
		} else {
			spec.Type = "string-array"
		}
	default:
		spec.Type = "string"
	}
	if len(p.Schema.Enum) > 0 {
		spec.Enum = p.Schema.Enum
	}
	if p.Schema.Default != nil {
		spec.Default = p.Schema.Default
	}
	return spec
}

// operationSlug picks a stable, snake_case name for the template:
// operationId when set (authoritative per spec); otherwise
// method_path with punctuation normalised. OpenAPI lets operationId
// be mixed-case; we lowercase it for consistency with the rest of
// our template namespace.
func operationSlug(operationID, verb, path string) string {
	if operationID != "" {
		return sanitiseIdentifier(operationID)
	}
	return sanitiseIdentifier(verb + "_" + path)
}

func anyTagMatches(opTags []string, filter map[string]bool) bool {
	for _, t := range opTags {
		if filter[t] {
			return true
		}
	}
	return false
}

// looksLikeYAML is a conservative best-effort check. A JSON document
// starts with `{` or `[` (possibly preceded by whitespace / BOM);
// anything else is almost certainly YAML or garbage. Used only to
// emit a helpful error message, not to parse.
func looksLikeYAML(data []byte) bool {
	for _, b := range data {
		switch b {
		case ' ', '\t', '\r', '\n', 0xFE, 0xFF, 0xBB, 0xBF, 0xEF:
			continue
		case '{', '[':
			return false
		default:
			return true
		}
	}
	return false
}


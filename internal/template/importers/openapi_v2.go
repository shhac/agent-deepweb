package importers

import "strings"

// swaggerV2Doc is the minimal subset of Swagger 2.0 fields we consume
// — enough to round-trip into the OpenAPI v3 shape our emitter
// already handles. Anything that doesn't translate cleanly
// (host-level security, definitions/$ref, produces/consumes details)
// is dropped; that's the same loss posture as the v3 path.
type swaggerV2Doc struct {
	Swagger  string                      `json:"swagger"`
	Host     string                      `json:"host"`
	BasePath string                      `json:"basePath"`
	Schemes  []string                    `json:"schemes"`
	Paths    map[string]swaggerV2PathItem `json:"paths"`
}

// swaggerV2PathItem is HTTP method → operation (same map shape as v3,
// but the operation parameters live inline rather than with a `schema:`
// wrapper on each).
type swaggerV2PathItem map[string]*swaggerV2Operation

type swaggerV2Operation struct {
	OperationID string               `json:"operationId"`
	Summary     string               `json:"summary"`
	Description string               `json:"description"`
	Tags        []string             `json:"tags"`
	Parameters  []swaggerV2Parameter `json:"parameters"`
	Consumes    []string             `json:"consumes"` // e.g. ["application/json"]
}

// swaggerV2Parameter carries type information inline (v2's shape). The
// `in:body` case is special: the type lives under a `schema:` field,
// matching the shape v3 later adopted for request bodies.
type swaggerV2Parameter struct {
	Name        string          `json:"name"`
	In          string          `json:"in"` // path | query | header | body | formData
	Required    bool            `json:"required"`
	Type        string          `json:"type"` // inline for non-body params
	Enum        []any           `json:"enum"`
	Default     any             `json:"default"`
	Items       *swaggerV2Items `json:"items"`
	Schema      *openapiSchema  `json:"schema"` // body params only
	Description string          `json:"description"`
}

type swaggerV2Items struct {
	Type string `json:"type"`
}

// swaggerV2ToV3 rewrites a v2 document as an equivalent v3 one so the
// shared operationToTemplate path (openapi.go) can emit templates
// without caring which version we started with.
//
// The translation is lossy in exactly the same places the downstream
// emitter is lossy (no $ref resolution, no multi-content bodies, no
// security-scheme application). That keeps the blast radius of "v2
// support" tiny — it's a shape adapter, not a second parser.
func swaggerV2ToV3(v2 swaggerV2Doc) openapiDoc {
	v3 := openapiDoc{
		OpenAPI: "3.0.0-from-swagger-v2",
		Paths:   map[string]openapiPathItem{},
	}
	// host + basePath + schemes[0] → servers[0].url. Missing pieces get
	// sensible defaults so the downstream emitter doesn't bail.
	scheme := "https"
	if len(v2.Schemes) > 0 {
		scheme = v2.Schemes[0]
	}
	if v2.Host != "" {
		url := scheme + "://" + v2.Host + v2.BasePath
		v3.Servers = []openapiServer{{URL: strings.TrimRight(url, "/")}}
	}

	for path, item := range v2.Paths {
		v3Item := openapiPathItem{}
		for verb, op := range item {
			if op == nil {
				continue
			}
			v3Op := &openapiOperation{
				OperationID: op.OperationID,
				Summary:     op.Summary,
				Description: op.Description,
				Tags:        op.Tags,
			}
			// Walk the v2 parameters; split body/formData into
			// requestBody, carry the rest onto v3Op.Parameters.
			for _, p := range op.Parameters {
				switch p.In {
				case "body":
					// v2 body params carry a schema: field already shaped
					// like v3's. Wrap in requestBody with application/json.
					v3Op.RequestBody = &openapiRequestBody{
						Required: p.Required,
						Content: map[string]openapiMediaType{
							pickJSONContentType(op.Consumes): {Schema: p.Schema},
						},
					}
				case "formData":
					// Form fields become a requestBody with content
					// application/x-www-form-urlencoded. Our downstream
					// emitter only special-cases application/json, so
					// form-encoded bodies fall back to a single object
					// param — acceptable for v1.
					if v3Op.RequestBody == nil {
						v3Op.RequestBody = &openapiRequestBody{
							Required: p.Required || (v3Op.RequestBody != nil && v3Op.RequestBody.Required),
							Content:  map[string]openapiMediaType{},
						}
					}
					ct := "application/x-www-form-urlencoded"
					v3Op.RequestBody.Content[ct] = openapiMediaType{
						Schema: &openapiSchema{Type: "object"},
					}
				default:
					// path | query | header — lift type info into a v3 schema.
					v3Op.Parameters = append(v3Op.Parameters, openapiParameter{
						Name:     p.Name,
						In:       p.In,
						Required: p.Required,
						Schema:   v2ParamToV3Schema(p),
					})
				}
			}
			v3Item[verb] = v3Op
		}
		v3.Paths[path] = v3Item
	}
	return v3
}

// v2ParamToV3Schema lifts v2's inline type fields into a v3 schema
// object. v2 arrays carry an `items.type` instead of v3's recursive
// schema; we translate that minimally.
func v2ParamToV3Schema(p swaggerV2Parameter) *openapiSchema {
	s := &openapiSchema{
		Type:    p.Type,
		Enum:    p.Enum,
		Default: p.Default,
	}
	if p.Items != nil {
		s.Items = &openapiSchema{Type: p.Items.Type}
	}
	return s
}

// pickJSONContentType chooses the request body content-type. Swagger 2
// documents the list of acceptable request types in the `consumes`
// array; we prefer application/json when offered, else the first
// entry, else default to JSON. Matches the real-world reality that
// most v2 APIs are JSON.
func pickJSONContentType(consumes []string) string {
	for _, c := range consumes {
		if strings.EqualFold(c, "application/json") {
			return "application/json"
		}
	}
	if len(consumes) > 0 {
		return consumes[0]
	}
	return "application/json"
}


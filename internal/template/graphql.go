package template

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ImportGraphQLOptions controls BuildGraphQLTemplates. Parallels the
// OpenAPI/Postman options: --prefix namespaces imports, --profile
// binds, --only filters to a subset of field names.
type ImportGraphQLOptions struct {
	Prefix  string
	Profile string
	Only    []string // exact match against Query / Mutation field names
}

// IntrospectionQuery is the minimal introspection document we POST.
// It covers queryType + mutationType + fields + args + deep-enough
// type references to reconstruct Scalar / List / NonNull / Enum /
// InputObject types without needing a second round-trip.
//
// We do NOT fetch the full `types[]` tree — that adds ~5x payload
// for no direct win since we only read field args here, not the
// object-return-type shape (BuildTemplates selects __typename-only
// on each return; the user edits the template for deeper selection).
const IntrospectionQuery = `query IntrospectionQuery {
  __schema {
    queryType { name }
    mutationType { name }
    types {
      name
      kind
      fields(includeDeprecated: false) {
        name
        description
        args {
          name
          description
          defaultValue
          type { ...TypeRef }
        }
      }
      enumValues(includeDeprecated: false) { name }
    }
  }
}

fragment TypeRef on __Type {
  kind
  name
  ofType {
    kind
    name
    ofType {
      kind
      name
      ofType {
        kind
        name
      }
    }
  }
}`

// Schema is the parsed view of an introspection result. Only the
// fields we need for template generation are kept; the full __Type
// object is much richer.
type Schema struct {
	QueryTypeName    string
	MutationTypeName string
	Types            map[string]schemaType
}

type schemaType struct {
	Name       string
	Kind       string // OBJECT | SCALAR | ENUM | INPUT_OBJECT | ...
	Fields     []schemaField
	EnumValues []string
}

type schemaField struct {
	Name        string
	Description string
	Args        []schemaArg
}

type schemaArg struct {
	Name         string
	Description  string
	DefaultValue string
	TypeRef      typeRef
}

// typeRef is a tree-form type reference (NonNull → List → NamedType).
// Mirrors the introspection JSON 1:1 so we can walk it unambiguously.
type typeRef struct {
	Kind   string   `json:"kind"`
	Name   string   `json:"name"`
	OfType *typeRef `json:"ofType"`
}

// introspectionEnvelope — the GraphQL-level shape we unmarshal into.
type introspectionEnvelope struct {
	Data   introspectionData  `json:"data"`
	Errors []map[string]any   `json:"errors"`
}

type introspectionData struct {
	Schema rawSchema `json:"__schema"`
}

type rawSchema struct {
	QueryType    *namedTypeRef `json:"queryType"`
	MutationType *namedTypeRef `json:"mutationType"`
	Types        []rawType     `json:"types"`
}

type namedTypeRef struct {
	Name string `json:"name"`
}

type rawType struct {
	Name       string     `json:"name"`
	Kind       string     `json:"kind"`
	Fields     []rawField `json:"fields"`
	EnumValues []rawEnum  `json:"enumValues"`
}

type rawField struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Args        []rawArg   `json:"args"`
}

type rawArg struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	DefaultValue string   `json:"defaultValue"`
	Type         typeRef  `json:"type"`
}

type rawEnum struct {
	Name string `json:"name"`
}

// ParseGraphQLSchema accepts the raw introspection response body
// (the POST reply from IntrospectionQuery) and returns our Schema IR.
// GraphQL-level errors in the response are surfaced as a parse error
// so the caller never silently imports an empty schema.
func ParseGraphQLSchema(data []byte) (*Schema, error) {
	var env introspectionEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse introspection response: %w", err)
	}
	if len(env.Errors) > 0 {
		return nil, fmt.Errorf("introspection returned GraphQL errors: %v", env.Errors)
	}
	if env.Data.Schema.QueryType == nil {
		return nil, fmt.Errorf("introspection response has no queryType — is the endpoint really GraphQL?")
	}

	s := &Schema{
		QueryTypeName: env.Data.Schema.QueryType.Name,
		Types:         map[string]schemaType{},
	}
	if env.Data.Schema.MutationType != nil {
		s.MutationTypeName = env.Data.Schema.MutationType.Name
	}
	for _, t := range env.Data.Schema.Types {
		st := schemaType{Name: t.Name, Kind: t.Kind}
		for _, e := range t.EnumValues {
			st.EnumValues = append(st.EnumValues, e.Name)
		}
		for _, f := range t.Fields {
			sf := schemaField{Name: f.Name, Description: f.Description}
			for _, a := range f.Args {
				sf.Args = append(sf.Args, schemaArg{
					Name:         a.Name,
					Description:  a.Description,
					DefaultValue: a.DefaultValue,
					TypeRef:      a.Type,
				})
			}
			st.Fields = append(st.Fields, sf)
		}
		s.Types[t.Name] = st
	}
	return s, nil
}

// BuildTemplates emits one Template per top-level Query and Mutation
// field. Each template POSTs a GraphQL document that selects only
// __typename on the return — the user is expected to edit body_template
// to choose fields (the point of this import is to remove the
// boilerplate of plumbing args + variables, not to guess the shape
// the caller wants back).
func (s *Schema) BuildTemplates(endpoint string, opts ImportGraphQLOptions) ([]Template, error) {
	if opts.Prefix == "" {
		return nil, fmt.Errorf("graphql schema import requires --prefix")
	}
	if endpoint == "" {
		return nil, fmt.Errorf("graphql schema import requires the endpoint URL")
	}
	onlySet := map[string]bool{}
	for _, o := range opts.Only {
		onlySet[o] = true
	}

	var out []Template
	emit := func(opType string, fields []schemaField) {
		// Stable order — makes imports reproducible.
		sorted := append([]schemaField(nil), fields...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
		for _, f := range sorted {
			if len(onlySet) > 0 && !onlySet[f.Name] {
				continue
			}
			tpl := fieldToTemplate(endpoint, opType, f, s, opts)
			out = append(out, tpl)
		}
	}
	if qt, ok := s.Types[s.QueryTypeName]; ok {
		emit("query", qt.Fields)
	}
	if s.MutationTypeName != "" {
		if mt, ok := s.Types[s.MutationTypeName]; ok {
			emit("mutation", mt.Fields)
		}
	}
	return out, nil
}

// fieldToTemplate builds the GraphQL document for one top-level field:
//
//	query($a: T!, $b: S) { fieldName(a: $a, b: $b) { __typename } }
//
// `variables` is populated from our {{var}} placeholders, type-
// preserving so an int arg lands as a JSON number in the variables
// object at run time.
func fieldToTemplate(endpoint, opType string, f schemaField, s *Schema, opts ImportGraphQLOptions) Template {
	var (
		argDecls []string // `$a: T!`
		argCall  []string // `a: $a`
		varObj   = map[string]any{}
		params   = map[string]ParamSpec{}
	)
	for _, a := range f.Args {
		typeStr := renderTypeRef(a.TypeRef)
		argDecls = append(argDecls, fmt.Sprintf("$%s: %s", a.Name, typeStr))
		argCall = append(argCall, fmt.Sprintf("%s: $%s", a.Name, a.Name))
		varObj[a.Name] = fmt.Sprintf("{{%s}}", a.Name)
		params[a.Name] = argToParamSpec(a, s)
	}

	argSig := ""
	if len(argDecls) > 0 {
		argSig = "(" + strings.Join(argDecls, ", ") + ")"
	}
	fieldCall := f.Name
	if len(argCall) > 0 {
		fieldCall = fmt.Sprintf("%s(%s)", f.Name, strings.Join(argCall, ", "))
	}
	doc := fmt.Sprintf("%s %s%s { %s { __typename } }", opType, firstUpper(f.Name), argSig, fieldCall)

	body := map[string]any{
		"query":     doc,
		"variables": varObj,
	}
	bodyJSON, _ := json.Marshal(body)

	return Template{
		Name:         opts.Prefix + "." + sanitiseIdentifier(f.Name),
		Description:  strings.TrimSpace(f.Description),
		Method:       "POST",
		URL:          endpoint,
		Headers:      map[string]string{"Content-Type": "application/json", "Accept": "application/json"},
		Profile:      opts.Profile,
		BodyFormat:   "json",
		BodyTemplate: bodyJSON,
		Parameters:   params,
	}
}

// renderTypeRef reconstructs a GraphQL type signature (`[String!]!`)
// from the ofType chain. Strips past 4 levels for sanity — specs that
// nest deeper than `[[String!]!]!` are exotic enough that we punt
// with a fallback "String".
func renderTypeRef(t typeRef) string {
	switch t.Kind {
	case "NON_NULL":
		if t.OfType != nil {
			return renderTypeRef(*t.OfType) + "!"
		}
		return "String"
	case "LIST":
		if t.OfType != nil {
			return "[" + renderTypeRef(*t.OfType) + "]"
		}
		return "[String]"
	}
	if t.Name != "" {
		return t.Name
	}
	return "String"
}

// argToParamSpec maps a GraphQL arg type to our ParamSpec. GraphQL's
// built-in scalars (Int/Float/Boolean/String/ID) map cleanly; custom
// scalars fall back to string; enums carry their enumValues list.
func argToParamSpec(a schemaArg, s *Schema) ParamSpec {
	spec := ParamSpec{
		Required:    isNonNull(a.TypeRef),
		Description: strings.TrimSpace(a.Description),
	}
	if a.DefaultValue != "" {
		spec.Default = a.DefaultValue
	}

	base := unwrap(a.TypeRef)
	switch base.Kind {
	case "SCALAR":
		switch base.Name {
		case "Int":
			spec.Type = "int"
		case "Float":
			spec.Type = "number"
		case "Boolean":
			spec.Type = "bool"
		default:
			spec.Type = "string"
		}
	case "ENUM":
		spec.Type = "string"
		if enumT, ok := s.Types[base.Name]; ok {
			for _, v := range enumT.EnumValues {
				spec.Enum = append(spec.Enum, v)
			}
		}
	default:
		// INPUT_OBJECT, LIST-of-anything, or unknowns: accept a JSON
		// object/array via type=string and let the caller supply valid
		// JSON at --param time. A stricter walk could recurse into
		// input-object fields, but that's enough rope for now.
		spec.Type = "string"
	}
	// LIST-of-string gets special-cased for ergonomics.
	if isListOfString(a.TypeRef) {
		spec.Type = "string-array"
	}
	return spec
}

func isNonNull(t typeRef) bool {
	return t.Kind == "NON_NULL"
}

// unwrap walks past NON_NULL / LIST wrappers to the innermost named type.
func unwrap(t typeRef) typeRef {
	cur := t
	for cur.OfType != nil && (cur.Kind == "NON_NULL" || cur.Kind == "LIST") {
		cur = *cur.OfType
	}
	return cur
}

func isListOfString(t typeRef) bool {
	cur := t
	// Peel NON_NULL.
	if cur.Kind == "NON_NULL" && cur.OfType != nil {
		cur = *cur.OfType
	}
	if cur.Kind != "LIST" || cur.OfType == nil {
		return false
	}
	inner := *cur.OfType
	if inner.Kind == "NON_NULL" && inner.OfType != nil {
		inner = *inner.OfType
	}
	return inner.Kind == "SCALAR" && (inner.Name == "String" || inner.Name == "ID")
}

func firstUpper(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

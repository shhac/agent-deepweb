package importers

import (
	"github.com/shhac/agent-deepweb/internal/template"

	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// BuildTemplates emits one template.Template per top-level Query and Mutation
// field. Each template POSTs a GraphQL document that selects only
// __typename on the return — the user is expected to edit body_template
// to choose fields (the point of this import is to remove the
// boilerplate of plumbing args + variables, not to guess the shape
// the caller wants back).
func (s *Schema) BuildTemplates(endpoint string, opts ImportGraphQLOptions) ([]template.Template, error) {
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

	var out []template.Template
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
func fieldToTemplate(endpoint, opType string, f schemaField, s *Schema, opts ImportGraphQLOptions) template.Template {
	var (
		argDecls []string // `$a: T!`
		argCall  []string // `a: $a`
		varObj   = map[string]any{}
		params   = map[string]template.ParamSpec{}
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

	return template.Template{
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

// argToParamSpec maps a GraphQL arg type to our template.ParamSpec. GraphQL's
// built-in scalars (Int/Float/Boolean/String/ID) map cleanly; custom
// scalars fall back to string; enums carry their enumValues list.
func argToParamSpec(a schemaArg, s *Schema) template.ParamSpec {
	spec := template.ParamSpec{
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

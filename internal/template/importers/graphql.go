// Package-local file: graphql.go carries the GraphQL IR (Schema +
// schemaType / schemaField / schemaArg / typeRef) and the import
// option struct. The parse pipeline (introspection wire types +
// ParseGraphQLSchema) lives in graphql_parse.go; the emission
// pipeline (BuildTemplates + fieldToTemplate) lives in graphql_emit.go.
// Keeping the IR in a standalone file means both sides can reference
// the same types without either side owning the other.
package importers

// ImportGraphQLOptions controls BuildTemplates. Parallels the
// OpenAPI/Postman options: --prefix namespaces imports, --profile
// binds, --only filters to a subset of field names.
type ImportGraphQLOptions struct {
	Prefix  string
	Profile string
	Only    []string // exact match against Query / Mutation field names
}

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

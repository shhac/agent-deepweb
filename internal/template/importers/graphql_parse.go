package importers

import (
	"encoding/json"
	"fmt"
)

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

// introspectionEnvelope — the GraphQL-level shape we unmarshal into.
type introspectionEnvelope struct {
	Data   introspectionData `json:"data"`
	Errors []map[string]any  `json:"errors"`
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
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Args        []rawArg `json:"args"`
}

type rawArg struct {
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	DefaultValue string  `json:"defaultValue"`
	Type         typeRef `json:"type"`
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

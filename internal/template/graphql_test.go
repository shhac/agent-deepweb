package template

import (
	"strings"
	"testing"
)

// A minimal introspection response that covers the cases we care about:
// a Query type with a scalar field, a Mutation type with a required
// non-null arg, an ENUM, and a LIST-of-string arg.
const introspectionSample = `{
  "data": {
    "__schema": {
      "queryType": { "name": "Query" },
      "mutationType": { "name": "Mutation" },
      "types": [
        {
          "name": "Query",
          "kind": "OBJECT",
          "fields": [
            {
              "name": "user",
              "description": "Fetch a user by id",
              "args": [
                {
                  "name": "id",
                  "type": { "kind": "NON_NULL", "name": null, "ofType": { "kind": "SCALAR", "name": "ID", "ofType": null } }
                }
              ]
            },
            {
              "name": "search",
              "args": [
                {
                  "name": "term",
                  "type": { "kind": "SCALAR", "name": "String" }
                },
                {
                  "name": "tags",
                  "type": {
                    "kind": "NON_NULL", "name": null,
                    "ofType": { "kind": "LIST", "name": null, "ofType": { "kind": "NON_NULL", "name": null, "ofType": { "kind": "SCALAR", "name": "String" } } }
                  }
                }
              ]
            }
          ]
        },
        {
          "name": "Mutation",
          "kind": "OBJECT",
          "fields": [
            {
              "name": "createPost",
              "args": [
                {
                  "name": "status",
                  "type": { "kind": "ENUM", "name": "PostStatus" }
                },
                {
                  "name": "count",
                  "type": { "kind": "SCALAR", "name": "Int" }
                }
              ]
            }
          ]
        },
        {
          "name": "PostStatus",
          "kind": "ENUM",
          "enumValues": [{"name":"DRAFT"},{"name":"PUBLISHED"}]
        }
      ]
    }
  }
}`

// TestParseGraphQLSchema_Basics — round-trip the sample response into
// our IR and assert the top-level shape is what later emission needs.
func TestParseGraphQLSchema_Basics(t *testing.T) {
	s, err := ParseGraphQLSchema([]byte(introspectionSample))
	if err != nil {
		t.Fatal(err)
	}
	if s.QueryTypeName != "Query" || s.MutationTypeName != "Mutation" {
		t.Errorf("type names: query=%q mutation=%q", s.QueryTypeName, s.MutationTypeName)
	}
	if _, ok := s.Types["Query"]; !ok {
		t.Error("Query type missing")
	}
	if enum := s.Types["PostStatus"]; len(enum.EnumValues) != 2 {
		t.Errorf("enum values: %v", enum.EnumValues)
	}
}

// TestParseGraphQLSchema_RefusesErrorsResponse — GraphQL-level errors
// in the introspection response must abort import, not silently
// produce an empty schema.
func TestParseGraphQLSchema_RefusesErrorsResponse(t *testing.T) {
	payload := `{"errors":[{"message":"introspection disabled"}]}`
	_, err := ParseGraphQLSchema([]byte(payload))
	if err == nil || !strings.Contains(err.Error(), "introspection returned") {
		t.Errorf("want introspection error, got %v", err)
	}
}

// TestSchema_BuildTemplates — every top-level field becomes one
// template with POST method + JSON body carrying a GraphQL document
// shaped `query|mutation FieldName($args) { fieldName(...) { __typename } }`.
func TestSchema_BuildTemplates(t *testing.T) {
	s, err := ParseGraphQLSchema([]byte(introspectionSample))
	if err != nil {
		t.Fatal(err)
	}
	tpls, err := s.BuildTemplates("https://api.example.com/graphql", ImportGraphQLOptions{
		Prefix:  "gh",
		Profile: "gh-prof",
	})
	if err != nil {
		t.Fatal(err)
	}
	// 2 queries (user, search) + 1 mutation (createPost) = 3 templates.
	if len(tpls) != 3 {
		t.Fatalf("want 3 templates, got %d: %+v", len(tpls), names(tpls))
	}
	byName := map[string]Template{}
	for _, tpl := range tpls {
		byName[tpl.Name] = tpl
	}

	user, ok := byName["gh.user"]
	if !ok {
		t.Fatalf("gh.user template missing from %v", names(tpls))
	}
	if user.Method != "POST" || user.URL != "https://api.example.com/graphql" {
		t.Errorf("user template: %+v", user)
	}
	if user.Profile != "gh-prof" {
		t.Errorf("profile: %q", user.Profile)
	}
	if user.BodyFormat != "json" {
		t.Errorf("body_format: %q", user.BodyFormat)
	}

	// Required non-null arg → ParamSpec.Required, type from SCALAR ID → string.
	idSpec := user.Parameters["id"]
	if !idSpec.Required || idSpec.Type != "string" {
		t.Errorf("id param: %+v", idSpec)
	}

	// GraphQL document should carry `$id: ID!` and `user(id: $id)`.
	body := string(user.BodyTemplate)
	if !strings.Contains(body, `$id: ID!`) {
		t.Errorf("type signature missing in body: %s", body)
	}
	if !strings.Contains(body, `user(id: $id)`) {
		t.Errorf("arg call missing in body: %s", body)
	}
	if !strings.Contains(body, `__typename`) {
		t.Errorf("selection set missing: %s", body)
	}

	// Variables object references each arg via {{placeholder}}.
	if !strings.Contains(body, `"id":"{{id}}"`) {
		t.Errorf("variables object should reference {{id}}: %s", body)
	}

	// search.tags: NonNull LIST of NonNull String → string-array.
	search := byName["gh.search"]
	if spec := search.Parameters["tags"]; spec.Type != "string-array" || !spec.Required {
		t.Errorf("search.tags spec: %+v", spec)
	}
	if spec := search.Parameters["term"]; spec.Type != "string" || spec.Required {
		t.Errorf("search.term spec: %+v (nullable String, should not be required)", spec)
	}

	// createPost.status: ENUM carries its values.
	create := byName["gh.createpost"]
	status := create.Parameters["status"]
	if len(status.Enum) != 2 {
		t.Errorf("status enum values: %v", status.Enum)
	}
	if count := create.Parameters["count"]; count.Type != "int" {
		t.Errorf("count: want int, got %q", count.Type)
	}
}

// TestSchema_BuildTemplates_OnlyFilter — --only limits emitted
// templates to the listed field names.
func TestSchema_BuildTemplates_OnlyFilter(t *testing.T) {
	s, err := ParseGraphQLSchema([]byte(introspectionSample))
	if err != nil {
		t.Fatal(err)
	}
	tpls, err := s.BuildTemplates("https://x/graphql", ImportGraphQLOptions{
		Prefix: "x",
		Only:   []string{"user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tpls) != 1 || tpls[0].Name != "x.user" {
		t.Errorf("only-filter: %v", names(tpls))
	}
}

// TestSchema_BuildTemplates_RequiresPrefix — contract parity with the
// other import formats.
func TestSchema_BuildTemplates_RequiresPrefix(t *testing.T) {
	s, err := ParseGraphQLSchema([]byte(introspectionSample))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.BuildTemplates("https://x/graphql", ImportGraphQLOptions{})
	if err == nil || !strings.Contains(err.Error(), "--prefix") {
		t.Errorf("want prefix-required, got %v", err)
	}
}

// TestRenderTypeRef_NestedList — `[String!]!` should serialise back
// to `[String!]!`.
func TestRenderTypeRef_NestedList(t *testing.T) {
	inner := typeRef{Kind: "SCALAR", Name: "String"}
	nonNullString := typeRef{Kind: "NON_NULL", OfType: &inner}
	listOfNN := typeRef{Kind: "LIST", OfType: &nonNullString}
	nonNullList := typeRef{Kind: "NON_NULL", OfType: &listOfNN}
	if got := renderTypeRef(nonNullList); got != "[String!]!" {
		t.Errorf("renderTypeRef: got %q, want [String!]!", got)
	}
}

func names(ts []Template) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Name
	}
	return out
}

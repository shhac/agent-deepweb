package mockdeep

// introspectionResponse returns a minimal, spec-shaped __schema
// reply that describes mockdeep's two "real" GraphQL fields:
// `me: User` (selection returns id + name) and `ping: String`. Just
// enough surface for agent-deepweb's ParseGraphQLSchema +
// BuildTemplates to produce runnable templates; not a faithful echo
// of every introspection feature (no directives, interfaces, unions).
//
// Keeping this as a pure Go map literal (rather than loading a JSON
// fixture file) means the introspection shape lives inside the same
// package that implements the `me`/`ping` resolvers — if we add a
// new field, the schema and handler change together.
func introspectionResponse() map[string]any {
	return map[string]any{
		"data": map[string]any{
			"__schema": map[string]any{
				"queryType":    map[string]any{"name": "Query"},
				"mutationType": nil,
				"types": []map[string]any{
					{
						"name": "Query",
						"kind": "OBJECT",
						"fields": []map[string]any{
							{
								"name":        "me",
								"description": "Currently authenticated user",
								"args":        []any{},
							},
							{
								"name":        "ping",
								"description": "Unauthenticated liveness field",
								"args":        []any{},
							},
						},
					},
					{
						"name": "User",
						"kind": "OBJECT",
						"fields": []map[string]any{
							{"name": "id", "args": []any{}},
							{"name": "name", "args": []any{}},
						},
					},
				},
			},
		},
	}
}

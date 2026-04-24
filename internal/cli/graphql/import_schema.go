package graphql

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/api"
	"github.com/shhac/agent-deepweb/internal/cli/shared"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/template"
)

// registerImportSchema wires `graphql import-schema <endpoint>`. Lives
// alongside `graphql <endpoint>` because it's the same transport
// (authenticated POST of a GraphQL document) and uses the same profile
// resolution — just with the introspection query baked in and the
// response parsed into templates instead of printed.
func registerImportSchema(parent *cobra.Command) {
	var (
		profile   string
		cookieJar string
		prefix    string
		only      []string
		timeoutMS int
		maxBytes  int64
	)
	cmd := &cobra.Command{
		Use:   "import-schema <endpoint>",
		Short: "Introspect a GraphQL endpoint and store one template per Query/Mutation field (human-only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if prefix == "" {
				return shared.Fail(agenterrors.New(
					"--prefix is required (chooses the name-space for imported templates, e.g. 'gh')",
					agenterrors.FixableByHuman))
			}
			endpoint := args[0]
			auth, err := shared.ResolveProfile(endpoint, profile)
			if err != nil {
				return shared.Fail(err)
			}

			timeout, max := shared.ResolveLimits(timeoutMS, maxBytes, nil)
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			body := introspectionBody()
			resp, err := api.Do(ctx, api.Request{
				Method:  "POST",
				URL:     endpoint,
				Headers: map[string]string{"Content-Type": "application/json", "Accept": "application/json"},
				Body:    bytes.NewReader(body),
				Auth:    auth,
				JarPath: cookieJar,
			}, api.ClientOptions{
				Timeout:         timeout,
				MaxBytes:        max,
				FollowRedirects: true,
			})
			if err != nil {
				return shared.Fail(err)
			}
			if resp == nil || resp.Status >= 400 {
				status := 0
				if resp != nil {
					status = resp.Status
				}
				return shared.Fail(agenterrors.Newf(agenterrors.FixableByHuman,
					"introspection returned HTTP %d", status).
					WithHint("Check the endpoint URL + that the profile has rights to introspect (some servers disable introspection in production)"))
			}

			schema, err := template.ParseGraphQLSchema(resp.Body)
			if err != nil {
				return shared.Fail(agenterrors.Wrap(err, agenterrors.FixableByHuman).
					WithHint("Server may have introspection disabled, or returned a non-standard envelope"))
			}
			tpls, err := schema.BuildTemplates(endpoint, template.ImportGraphQLOptions{
				Prefix:  prefix,
				Profile: profileNameOrEmpty(profile),
				Only:    only,
			})
			if err != nil {
				return shared.Fail(agenterrors.Wrap(err, agenterrors.FixableByHuman))
			}

			var stored []string
			for _, t := range tpls {
				if err := template.Store(t); err != nil {
					return shared.FailHuman(err)
				}
				stored = append(stored, t.Name)
			}
			shared.PrintOK(map[string]any{
				"imported":       stored,
				"count":          len(stored),
				"prefix":         prefix,
				"profile":        profileNameOrEmpty(profile),
				"query_type":     schema.QueryTypeName,
				"mutation_type":  schema.MutationTypeName,
				"only_filter":    only,
			})
			return nil
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "Profile to auth the introspection request AND bind to every imported template")
	cmd.Flags().StringVar(&cookieJar, "cookiejar", "", "Bring-your-own cookie jar for the introspection request")
	cmd.Flags().StringVar(&prefix, "prefix", "", "Name-space for imported templates (required, e.g. 'gh' → 'gh.viewer', 'gh.createIssue')")
	cmd.Flags().StringSliceVar(&only, "only", nil, "Only import these top-level field names (repeatable; comma-separated also allowed)")
	cmd.Flags().IntVar(&timeoutMS, "timeout", 0, "Introspection request timeout (ms)")
	cmd.Flags().Int64Var(&maxBytes, "max-size", 0, "Introspection response cap (bytes). Large schemas may need a bump (default 10 MiB).")
	parent.AddCommand(cmd)
}

// introspectionBody returns the JSON-encoded GraphQL body for the
// standard introspection query.
func introspectionBody() []byte {
	b, _ := json.Marshal(map[string]string{"query": template.IntrospectionQuery})
	return b
}

// profileNameOrEmpty keeps the CLI UX honest: --profile="" should land
// as empty (not "none") so downstream output doesn't claim we bound
// the templates to a profile when we didn't.
func profileNameOrEmpty(s string) string {
	if s == "none" {
		return ""
	}
	return s
}


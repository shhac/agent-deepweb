// Package tpl implements the `tpl` command tree.
//
// File layout:
//
//	tpl.go    Register wiring + list/show/import/remove registrations.
//	run.go    `tpl run` — the agent-facing verb. Orchestrates parameter
//	          validation, URL/header/body expansion, credential resolution,
//	          and dispatch to api.Do.
//	body.go   buildTemplateBody — the json/form/raw body encoder.
package tpl

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/output"
	"github.com/shhac/agent-deepweb/internal/template"
)

func Register(root *cobra.Command, _ shared.Globals) {
	cmd := &cobra.Command{
		Use:   "tpl",
		Short: "Parameterised request templates (highest-safety mode)",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "llm-help",
		Short: "Show detailed reference for tpl",
		Run:   func(cmd *cobra.Command, args []string) { fmt.Print(usageText) },
	})

	registerList(cmd)
	registerShow(cmd)
	registerRun(cmd)
	registerImport(cmd)
	registerRemove(cmd)

	root.AddCommand(cmd)
}

func registerList(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List templates (no parameter values, just the schema)",
		RunE: func(cmd *cobra.Command, args []string) error {
			tpls, err := template.List()
			if err != nil {
				return shared.FailHuman(err)
			}
			type row struct {
				Name        string `json:"name"`
				Description string `json:"description,omitempty"`
				Method      string `json:"method"`
				URL         string `json:"url"`
				Auth        string `json:"auth,omitempty"`
				Params      int    `json:"parameter_count"`
			}
			rows := make([]row, 0, len(tpls))
			for _, t := range tpls {
				rows = append(rows, row{
					Name:        t.Name,
					Description: t.Description,
					Method:      t.Method,
					URL:         t.URL,
					Auth:        t.Auth,
					Params:      len(t.Parameters),
				})
			}
			output.PrintJSON(map[string]any{"templates": rows})
			return nil
		},
	})
}

func registerShow(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "show <name>",
		Short: "Show a template's full definition",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := template.Get(args[0])
			if err != nil {
				return shared.Fail(template.ClassifyLookupErr(err, args[0]))
			}
			output.PrintJSON(map[string]any{
				"template": t,
				"lint":     t.Lint(),
			})
			return nil
		},
	})
}

func registerImport(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "import <file>",
		Short: "Import template(s) from a JSON file (human-only)",
		Args:  cobra.ExactArgs(1),
		RunE: shared.HumanOnlyRunE("tpl import", func(cmd *cobra.Command, args []string) error {
			stored, err := template.ImportFile(args[0])
			if err != nil {
				return shared.Fail(agenterrors.Wrap(err, agenterrors.FixableByHuman).
					WithHint("Check JSON syntax and template shape (method, url, parameters)"))
			}
			shared.PrintOK(map[string]any{"imported": stored})
			return nil
		}),
	})
}

func registerRemove(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a template (human-only)",
		Args:  cobra.ExactArgs(1),
		RunE: shared.HumanOnlyRunE("tpl remove", func(cmd *cobra.Command, args []string) error {
			if err := template.Remove(args[0]); err != nil {
				return shared.Fail(template.ClassifyLookupErr(err, args[0]))
			}
			shared.PrintOK(map[string]any{"name": args[0]})
			return nil
		}),
	})
}

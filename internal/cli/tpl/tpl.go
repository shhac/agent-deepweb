package tpl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/api"
	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/credential"
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

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List templates (no parameter values, just the schema)",
		RunE: func(cmd *cobra.Command, args []string) error {
			tpls, err := template.List()
			if err != nil {
				return shared.Fail(agenterrors.Wrap(err, agenterrors.FixableByHuman))
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

	cmd.AddCommand(&cobra.Command{
		Use:   "show <name>",
		Short: "Show a template's full definition",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := template.Get(args[0])
			if err != nil {
				if ae := template.WrapNotFound(err, args[0]); ae != nil {
					return shared.Fail(ae)
				}
				return shared.Fail(agenterrors.Wrap(err, agenterrors.FixableByHuman))
			}
			output.PrintJSON(map[string]any{
				"template": t,
				"lint":     t.Lint(),
			})
			return nil
		},
	})

	registerRun(cmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "import <file>",
		Short: "Import template(s) from a JSON file (human-only)",
		Args:  cobra.ExactArgs(1),
		RunE: shared.HumanOnlyRunE("tpl import", func(cmd *cobra.Command, args []string) error {
			stored, err := template.ImportFile(args[0])
			if err != nil {
				return shared.Fail(agenterrors.Wrap(err, agenterrors.FixableByHuman).
					WithHint("Check JSON syntax and template shape (method, url, parameters)"))
			}
			output.PrintJSON(map[string]any{"status": "ok", "imported": stored})
			return nil
		}),
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a template (human-only)",
		Args:  cobra.ExactArgs(1),
		RunE: shared.HumanOnlyRunE("tpl remove", func(cmd *cobra.Command, args []string) error {
			if err := template.Remove(args[0]); err != nil {
				if ae := template.WrapNotFound(err, args[0]); ae != nil {
					return shared.Fail(ae)
				}
				return shared.Fail(agenterrors.Wrap(err, agenterrors.FixableByHuman))
			}
			output.PrintJSON(map[string]any{"status": "ok", "name": args[0]})
			return nil
		}),
	})

	root.AddCommand(cmd)
}

// registerRun builds the `tpl run` command, the agent-facing verb.
func registerRun(parent *cobra.Command) {
	var params []string
	var timeoutMS int
	var maxBytes int64
	var format string
	var allowHTTP bool

	cmd := &cobra.Command{
		Use:   "run <name>",
		Short: "Run a template with the given parameters",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.RefuseFlag(allowHTTP, "--allow-http"); err != nil {
				return shared.Fail(err)
			}
			return runTemplate(args[0], params, timeoutMS, maxBytes, format, allowHTTP)
		},
	}
	cmd.Flags().StringArrayVarP(&params, "param", "p", nil, "Template parameter 'name=value' (repeatable)")
	cmd.Flags().IntVar(&timeoutMS, "timeout", 0, "Request timeout in ms")
	cmd.Flags().Int64Var(&maxBytes, "max-size", 0, "Max response body size in bytes")
	cmd.Flags().StringVar(&format, "format", "", "Output format: json, raw, text")
	cmd.Flags().BoolVar(&allowHTTP, "allow-http", false, "Human-only: permit http:// for this request")
	parent.AddCommand(cmd)
}

func runTemplate(name string, rawParams []string, timeoutMS int, maxBytes int64, formatStr string, allowHTTP bool) error {
	tpl, err := template.Get(name)
	if err != nil {
		if ae := template.WrapNotFound(err, name); ae != nil {
			return shared.Fail(ae)
		}
		return shared.Fail(agenterrors.Wrap(err, agenterrors.FixableByHuman))
	}

	typed, err := parseAndValidateParams(tpl, rawParams)
	if err != nil {
		return shared.Fail(err)
	}

	expandedURL, err := template.ExpandURL(tpl.URL, tpl.Query, typed)
	if err != nil {
		return shared.Fail(agenterrors.Wrap(err, agenterrors.FixableByAgent))
	}
	headers, err := template.ExpandHeaders(tpl.Headers, typed)
	if err != nil {
		return shared.Fail(agenterrors.Wrap(err, agenterrors.FixableByAgent))
	}

	body, ct, err := buildTemplateBody(tpl, typed)
	if err != nil {
		return shared.Fail(err)
	}
	if ct != "" {
		if headers == nil {
			headers = map[string]string{}
		}
		if _, set := headers["Content-Type"]; !set {
			headers["Content-Type"] = ct
		}
	}

	var auth *credential.Resolved
	if tpl.Auth != "" {
		a, err := shared.ResolveAuth(expandedURL, tpl.Auth)
		if err != nil {
			return shared.Fail(err)
		}
		auth = a
	}

	timeout, maxBytesResolved := shared.ResolveLimits(timeoutMS, maxBytes, nil)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	method := chooseMethod(tpl.Method, body != nil)

	resp, err := api.Do(ctx, api.Request{
		Method:       method,
		URL:          expandedURL,
		Headers:      headers,
		Body:         body,
		Auth:         auth,
		AllowHTTP:    allowHTTP,
		TemplateName: name,
	}, api.ClientOptions{
		Timeout:         timeout,
		MaxBytes:        maxBytesResolved,
		Redact:          true,
		FollowRedirects: true,
	})

	writeOutput(name, expandedURL, auth, resp, formatStr)
	if err != nil {
		return shared.Fail(err)
	}
	return nil
}

// parseAndValidateParams turns --param k=v strings into typed values via
// the template's ParamSpec map. Errors are fixable_by:agent with a hint
// pointing to `tpl show`.
func parseAndValidateParams(tpl *template.Template, rawParams []string) (map[string]any, error) {
	kv := map[string]string{}
	for _, p := range rawParams {
		k, v, err := shared.SplitKV(p, "--param")
		if err != nil {
			return nil, err
		}
		kv[k] = v
	}
	typed, err := tpl.Validate(kv)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent).
			WithHint("Run 'agent-deepweb tpl show " + tpl.Name + "' to see the parameter schema")
	}
	return typed, nil
}

// buildTemplateBody renders the template's body_template according to
// body_format. Returns the reader and suggested Content-Type (empty when
// caller shouldn't override). Pure given the Template + typed params.
func buildTemplateBody(tpl *template.Template, typed map[string]any) (io.Reader, string, error) {
	switch strings.ToLower(tpl.BodyFormat) {
	case "":
		// No body_format → no body, even if body_template is set. Author must opt in.
		return nil, "", nil
	case "json":
		if len(tpl.BodyTemplate) == 0 {
			return nil, "", nil
		}
		b, err := template.SubstituteBody(tpl.BodyTemplate, typed)
		if err != nil {
			return nil, "", agenterrors.Wrap(err, agenterrors.FixableByAgent)
		}
		if len(b) == 0 {
			return nil, "", nil
		}
		return bytes.NewReader(b), "application/json", nil
	case "form":
		if len(tpl.BodyTemplate) == 0 {
			return nil, "", nil
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(tpl.BodyTemplate, &raw); err != nil {
			return nil, "", agenterrors.Wrap(err, agenterrors.FixableByAgent)
		}
		values := url.Values{}
		for k, v := range raw {
			var str string
			if err := json.Unmarshal(v, &str); err == nil {
				s, err := template.SubstituteString(str, typed, false)
				if err != nil {
					return nil, "", agenterrors.Wrap(err, agenterrors.FixableByAgent)
				}
				values.Add(k, s)
			}
		}
		return strings.NewReader(values.Encode()), "application/x-www-form-urlencoded", nil
	case "raw":
		if len(tpl.BodyTemplate) == 0 {
			return nil, "", nil
		}
		var raw string
		if err := json.Unmarshal(tpl.BodyTemplate, &raw); err != nil {
			return nil, "", agenterrors.Wrap(err, agenterrors.FixableByAgent).
				WithHint("body_format=raw expects body_template to be a JSON string")
		}
		s, err := template.SubstituteString(raw, typed, false)
		if err != nil {
			return nil, "", agenterrors.Wrap(err, agenterrors.FixableByAgent)
		}
		return strings.NewReader(s), "", nil
	default:
		return nil, "", agenterrors.Newf(agenterrors.FixableByHuman,
			"template %q: unknown body_format %q", tpl.Name, tpl.BodyFormat)
	}
}

func chooseMethod(flag string, hasBody bool) string {
	m := strings.ToUpper(flag)
	if m != "" {
		return m
	}
	if hasBody {
		return "POST"
	}
	return "GET"
}

func writeOutput(name, expandedURL string, auth *credential.Resolved, resp *api.Response, formatStr string) {
	if resp == nil {
		return
	}
	f, _ := output.ParseFormat(formatStr)
	switch f {
	case output.FormatRaw:
		_, _ = os.Stdout.Write(resp.Body)
		return
	case output.FormatText:
		fmt.Printf("HTTP %d %s\n\n", resp.Status, resp.StatusText)
		_, _ = os.Stdout.Write(resp.Body)
		return
	}
	env := output.BuildHTTPEnvelope(output.EnvelopeIn{
		URL:         expandedURL,
		Auth:        auth,
		Status:      resp.Status,
		StatusText:  resp.StatusText,
		Headers:     resp.Headers,
		ContentType: resp.ContentType,
		Body:        resp.Body,
		Truncated:   resp.Truncated,
	})
	env["template"] = name
	output.PrintJSON(env)
}

package tpl

import (
	"context"
	"fmt"
	"io"
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
		return shared.Fail(template.ClassifyLookupErr(err, name))
	}

	typed, err := parseAndValidateParams(tpl, rawParams)
	if err != nil {
		return shared.Fail(err)
	}

	expandedURL, headers, body, err := prepareRequest(tpl, typed)
	if err != nil {
		return shared.Fail(err)
	}

	var auth *credential.Resolved
	if tpl.Auth != "" {
		a, resolveErr := shared.ResolveAuth(expandedURL, tpl.Auth)
		if resolveErr != nil {
			return shared.Fail(resolveErr)
		}
		auth = a
	}

	timeout, maxBytesResolved := shared.ResolveLimits(timeoutMS, maxBytes, nil)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	resp, doErr := api.Do(ctx, api.Request{
		Method:       chooseMethod(tpl.Method, body != nil),
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
	if doErr != nil {
		return shared.Fail(doErr)
	}
	return nil
}

// prepareRequest assembles the expanded URL, headers, and body from the
// template + typed parameters. Pure given the inputs — the caller still
// owns credential resolution and dispatch to api.Do. Extracted from the
// runTemplate orchestrator so the HTTP-free stages can be tested directly.
func prepareRequest(tpl *template.Template, typed map[string]any) (string, map[string]string, io.Reader, error) {
	expandedURL, err := template.ExpandURL(tpl.URL, tpl.Query, typed)
	if err != nil {
		return "", nil, nil, agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}
	headers, err := template.ExpandHeaders(tpl.Headers, typed)
	if err != nil {
		return "", nil, nil, agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}
	body, ct, err := buildTemplateBody(tpl, typed)
	if err != nil {
		return "", nil, nil, err
	}
	if ct != "" {
		if headers == nil {
			headers = map[string]string{}
		}
		if _, set := headers["Content-Type"]; !set {
			headers["Content-Type"] = ct
		}
	}
	return expandedURL, headers, body, nil
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

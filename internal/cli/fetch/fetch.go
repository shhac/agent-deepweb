package fetch

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
)

type opts struct {
	auth            string
	noAuth          bool
	method          string
	headers         []string
	queries         []string
	data            string
	jsonBody        string
	form            []string
	timeoutMS       int
	maxBytes        int64
	followRedirects bool
	format          string
	noRedact        bool
	allowHTTP       bool
	userAgent       string
}

// Register attaches the `fetch` command to root.
func Register(root *cobra.Command, globals shared.Globals) {
	o := &opts{}
	cmd := &cobra.Command{
		Use:   "fetch <url>",
		Short: "Authenticated HTTP fetch (curl-with-auth)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(args[0], globals(), o)
		},
	}
	cmd.Flags().StringVar(&o.auth, "auth", "", "Credential alias (falls back to --auth on root or env)")
	cmd.Flags().BoolVar(&o.noAuth, "no-auth", false, "Skip auth even if a credential matches the host")
	cmd.Flags().StringVarP(&o.method, "method", "X", "", "HTTP method (default GET, or POST if body given)")
	cmd.Flags().StringArrayVarP(&o.headers, "header", "H", nil, "Extra request header (repeatable)")
	cmd.Flags().StringArrayVar(&o.queries, "query", nil, "URL query param key=value (repeatable)")
	cmd.Flags().StringVar(&o.data, "data", "", "Raw body (@file, @- for stdin)")
	cmd.Flags().StringVar(&o.jsonBody, "json", "", "JSON body (@file, @- for stdin); sets Content-Type")
	cmd.Flags().StringArrayVar(&o.form, "form", nil, "Form field key=value (repeatable); sets x-www-form-urlencoded")
	cmd.Flags().IntVar(&o.timeoutMS, "timeout", 0, "Request timeout in ms")
	cmd.Flags().Int64Var(&o.maxBytes, "max-size", 0, "Max response body size in bytes")
	cmd.Flags().BoolVar(&o.followRedirects, "follow-redirects", true, "Follow redirects")
	cmd.Flags().StringVar(&o.format, "format", "", "Output format: json, raw, text")
	cmd.Flags().BoolVar(&o.noRedact, "no-redact", false, "Human-only: disable response redaction")
	cmd.Flags().BoolVar(&o.allowHTTP, "allow-http", false, "Human-only: permit http:// for this request (overrides credential default)")
	cmd.Flags().StringVarP(&o.userAgent, "user-agent", "A", "", "User-Agent for this request (else credential's UA; else agent-deepweb/<version>)")

	cmd.AddCommand(&cobra.Command{
		Use:   "llm-help",
		Short: "Show detailed reference for fetch",
		Run:   func(cmd *cobra.Command, args []string) { fmt.Print(usageText) },
	})

	root.AddCommand(cmd)
}

func run(rawURL string, g *shared.GlobalFlags, o *opts) error {
	// --no-redact and --allow-http are human-only; refuse before touching anything.
	if err := shared.RefuseFlag(o.noRedact, "--no-redact"); err != nil {
		return shared.Fail(err)
	}
	if err := shared.RefuseFlag(o.allowHTTP, "--allow-http"); err != nil {
		return shared.Fail(err)
	}

	authName := shared.FirstNonEmpty(o.auth, g.Auth)
	if authName != "" && o.noAuth {
		return shared.Fail(agenterrors.New("--auth and --no-auth are mutually exclusive", agenterrors.FixableByAgent).
			WithHint("Drop --no-auth or drop --auth"))
	}

	var auth *credential.Resolved
	if !o.noAuth {
		a, err := shared.ResolveAuth(rawURL, authName)
		if err != nil {
			return shared.Fail(err)
		}
		auth = a
	}

	body, contentType, err := buildBody(o)
	if err != nil {
		return shared.Fail(err)
	}

	method := chooseMethod(o.method, body != nil)

	headers, err := parseHeaderFlags(o.headers)
	if err != nil {
		return shared.Fail(err)
	}
	if contentType != "" {
		headers["Content-Type"] = contentType
	}

	query, err := parseQueryFlags(o.queries)
	if err != nil {
		return shared.Fail(err)
	}

	timeout, maxBytes := shared.ResolveLimits(o.timeoutMS, o.maxBytes, g)
	redact := !o.noRedact || shared.IsAgentMode()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	resp, err := api.Do(ctx, api.Request{
		Method:    method,
		URL:       rawURL,
		Headers:   headers,
		Query:     query,
		Body:      body,
		Auth:      auth,
		AllowHTTP: o.allowHTTP,
		UserAgent: o.userAgent,
	}, api.ClientOptions{
		Timeout:         timeout,
		MaxBytes:        maxBytes,
		Redact:          redact,
		FollowRedirects: o.followRedirects,
	})

	// Even on error, `resp` is non-nil for HTTP-level errors; surface whatever we have.
	writeResponse(rawURL, auth, resp, shared.FirstNonEmpty(o.format, g.Format))
	if err != nil {
		return shared.Fail(err)
	}
	return nil
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

// parseHeaderFlags converts --header "K: V" strings into a map, failing on
// malformed entries with fixable_by:agent. Pure — testable in isolation.
func parseHeaderFlags(raw []string) (map[string]string, error) {
	headers := map[string]string{}
	for _, h := range raw {
		k, v, ok := shared.SplitHeader(h)
		if !ok {
			return nil, agenterrors.Newf(agenterrors.FixableByAgent, "malformed --header %q", h).
				WithHint("Use 'Name: value' format")
		}
		headers[k] = v
	}
	return headers, nil
}

// parseQueryFlags turns --query key=value strings into a URL-query map.
// Values are URL-encoded before storage.
func parseQueryFlags(raw []string) (map[string][]string, error) {
	query := map[string][]string{}
	for _, q := range raw {
		k, v, err := shared.SplitKV(q, "--query")
		if err != nil {
			return nil, err
		}
		query[k] = append(query[k], url.QueryEscape(v))
	}
	return query, nil
}

func writeResponse(rawURL string, auth *credential.Resolved, resp *api.Response, format string) {
	if resp == nil {
		return
	}
	f, _ := output.ParseFormat(format)
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
		URL:         rawURL,
		Auth:        auth,
		Status:      resp.Status,
		StatusText:  resp.StatusText,
		Headers:     resp.Headers,
		ContentType: resp.ContentType,
		Body:        resp.Body,
		Truncated:   resp.Truncated,
	})
	if len(resp.NewCookies) > 0 {
		env["new_cookies"] = resp.NewCookies
	}
	output.PrintJSON(env)
}

// buildBody assembles the request body from --data/--json/--form (mutually
// exclusive). Returns the body reader and a Content-Type string (empty
// when the caller should not set it).
func buildBody(o *opts) (io.Reader, string, error) {
	hasData := o.data != ""
	hasJSON := o.jsonBody != ""
	hasForm := len(o.form) > 0

	n := 0
	for _, b := range []bool{hasData, hasJSON, hasForm} {
		if b {
			n++
		}
	}
	if n == 0 {
		return nil, "", nil
	}
	if n > 1 {
		return nil, "", agenterrors.New("--data / --json / --form are mutually exclusive", agenterrors.FixableByAgent)
	}

	switch {
	case hasData:
		b, err := loadBody(o.data)
		if err != nil {
			return nil, "", err
		}
		return bytes.NewReader(b), "", nil
	case hasJSON:
		b, err := loadBody(o.jsonBody)
		if err != nil {
			return nil, "", err
		}
		var anyv any
		if err := json.Unmarshal(b, &anyv); err != nil {
			return nil, "", agenterrors.Newf(agenterrors.FixableByAgent,
				"--json is not valid JSON: %s", err.Error()).
				WithHint("Pass a valid JSON string, @file path, or @- for stdin")
		}
		return bytes.NewReader(b), "application/json", nil
	case hasForm:
		values := url.Values{}
		for _, f := range o.form {
			k, v, err := shared.SplitKV(f, "--form")
			if err != nil {
				return nil, "", err
			}
			values.Add(k, v)
		}
		return strings.NewReader(values.Encode()), "application/x-www-form-urlencoded", nil
	}
	return nil, "", nil
}

// loadBody interprets "@-" as stdin, "@path" as file contents, else literal.
func loadBody(spec string) ([]byte, error) {
	switch {
	case spec == "@-":
		return io.ReadAll(os.Stdin)
	case strings.HasPrefix(spec, "@"):
		data, err := os.ReadFile(spec[1:])
		if err != nil {
			return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent).
				WithHint("Check the path and ensure the file is readable")
		}
		return data, nil
	default:
		return []byte(spec), nil
	}
}

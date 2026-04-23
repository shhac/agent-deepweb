// Package fetch implements the `fetch` command (curl-with-auth).
//
// File layout:
//
//	fetch.go   Register + run orchestrator + writeResponse.
//	body.go    buildBody + loadBody (--data / --json / --form handling).
//	flags.go   parseHeaderFlags / parseQueryFlags / chooseMethod.
package fetch

import (
	"context"
	"fmt"
	"os"

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
	bindFlags(cmd, o)

	cmd.AddCommand(&cobra.Command{
		Use:   "llm-help",
		Short: "Show detailed reference for fetch",
		Run:   func(cmd *cobra.Command, args []string) { fmt.Print(usageText) },
	})

	root.AddCommand(cmd)
}

func bindFlags(cmd *cobra.Command, o *opts) {
	f := cmd.Flags()
	f.StringVar(&o.auth, "auth", "", "Credential alias (falls back to --auth on root or env)")
	f.BoolVar(&o.noAuth, "no-auth", false, "Skip auth even if a credential matches the host")
	f.StringVarP(&o.method, "method", "X", "", "HTTP method (default GET, or POST if body given)")
	f.StringArrayVarP(&o.headers, "header", "H", nil, "Extra request header (repeatable)")
	f.StringArrayVar(&o.queries, "query", nil, "URL query param key=value (repeatable)")
	f.StringVar(&o.data, "data", "", "Raw body (@file, @- for stdin)")
	f.StringVar(&o.jsonBody, "json", "", "JSON body (@file, @- for stdin); sets Content-Type")
	f.StringArrayVar(&o.form, "form", nil, "Form field key=value (repeatable); sets x-www-form-urlencoded")
	f.IntVar(&o.timeoutMS, "timeout", 0, "Request timeout in ms")
	f.Int64Var(&o.maxBytes, "max-size", 0, "Max response body size in bytes")
	f.BoolVar(&o.followRedirects, "follow-redirects", true, "Follow redirects")
	f.StringVar(&o.format, "format", "", "Output format: json, raw, text")
	f.StringVarP(&o.userAgent, "user-agent", "A", "", "User-Agent for this request (else credential's UA; else agent-deepweb/<version>)")
}

func run(rawURL string, g *shared.GlobalFlags, o *opts) error {
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
	// Anonymous requests must opt in via --no-auth. ResolveAuth errors
	// when no credential matches the URL, so reaching this point means
	// either an explicit credential was picked or --no-auth was set.

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

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	resp, err := api.Do(ctx, api.Request{
		Method:    method,
		URL:       rawURL,
		Headers:   headers,
		Query:     query,
		Body:      body,
		Auth:      auth,
		UserAgent: o.userAgent,
	}, api.ClientOptions{
		Timeout:         timeout,
		MaxBytes:        maxBytes,
		FollowRedirects: o.followRedirects,
	})

	// Even on error, `resp` is non-nil for HTTP-level errors; surface whatever we have.
	writeResponse(rawURL, auth, resp, shared.FirstNonEmpty(o.format, g.Format))
	if err != nil {
		return shared.Fail(err)
	}
	return nil
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

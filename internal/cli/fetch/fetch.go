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

	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/api"
	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/credential"
	"github.com/shhac/agent-deepweb/internal/output"
)

type opts struct {
	profile         string
	cookieJar       string
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
	f.StringVar(&o.profile, "profile", "", "Profile name, or 'none' for explicit anonymous (falls back to --profile on root or AGENT_DEEPWEB_PROFILE)")
	f.StringVar(&o.cookieJar, "cookiejar", "", "Bring-your-own cookie jar (plaintext JSON file). Overrides the profile's encrypted default jar.")
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
	profileName := shared.FirstNonEmpty(o.profile, g.Profile)
	auth, err := shared.ResolveProfile(rawURL, profileName)
	if err != nil {
		return shared.Fail(err)
	}
	// Anonymous requests must opt in via `--profile none`. ResolveProfile
	// errors when no profile matches the URL and the flag is empty, so
	// reaching this point means either an explicit profile was picked or
	// `--profile none` was set.

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
		JarPath:   o.cookieJar,
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
	var extras map[string]any
	if len(resp.NewCookies) > 0 {
		extras = map[string]any{"new_cookies": resp.NewCookies}
	}
	output.RenderResponse(output.EnvelopeIn{
		URL:         rawURL,
		Auth:        auth,
		Status:      resp.Status,
		StatusText:  resp.StatusText,
		Headers:     resp.Headers,
		ContentType: resp.ContentType,
		Body:        resp.Body,
		Truncated:   resp.Truncated,
	}, resp.Status, resp.StatusText, resp.Body, format, extras)
}

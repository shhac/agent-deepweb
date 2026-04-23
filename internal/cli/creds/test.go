package creds

import (
	"context"
	"net/url"
	"time"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/api"
	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/output"
)

func registerTest(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "test <name>",
		Short: "Send a health-check request using the credential",
		Args:  cobra.ExactArgs(1),
		RunE:  runTest,
	})
}

func runTest(cmd *cobra.Command, args []string) error {
	name := args[0]
	resolved, err := credential.Resolve(name)
	if err != nil {
		return shared.Fail(credential.ClassifyLookupErr(err, name))
	}
	if resolved.Health == "" {
		return shared.Fail(agenterrors.Newf(agenterrors.FixableByHuman,
			"credential %q has no health URL configured", name).
			WithHint("Ask the user to run 'agent-deepweb creds set-health " + name + " <url>'"))
	}
	u, parseErr := url.Parse(resolved.Health)
	if parseErr != nil || u.Host == "" {
		return shared.Fail(agenterrors.Newf(agenterrors.FixableByHuman,
			"credential %q has a malformed health URL", name))
	}
	if !resolved.MatchesURL(u) {
		return shared.Fail(agenterrors.Newf(agenterrors.FixableByHuman,
			"health URL %s is not in allowlist for %q", resolved.Health, name).
			WithHint("Ask the user to add the host with 'agent-deepweb creds allow " + name + " " + u.Host + "'"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	resp, err := api.Do(ctx, api.Request{
		Method: "GET",
		URL:    resolved.Health,
		Auth:   resolved,
	}, api.ClientOptions{
		Timeout:         15 * time.Second,
		MaxBytes:        32 * 1024,
		Redact:          true,
		FollowRedirects: true,
	})
	result := map[string]any{
		"credential": name,
		"url":        resolved.Health,
	}
	if resp != nil {
		result["status"] = resp.Status
	}
	if err != nil {
		result["ok"] = false
		output.PrintJSON(result)
		return shared.Fail(err)
	}
	result["ok"] = true
	output.PrintJSON(result)
	return nil
}

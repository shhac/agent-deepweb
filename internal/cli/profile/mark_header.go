package profile

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/credential"
)

// registerMarkHeader wires `profile mark-header-sensitive` and
// `profile mark-header-visible` — symmetric with `jar mark-sensitive` /
// `jar mark-visible` but for request/response headers.
//
// Default redaction is pattern-based (Authorization, Cookie, X-*-token,
// etc.); these commands are the human-controlled overrides for names
// the regex doesn't recognise or legitimately-sensitive headers the
// human wants visible for debugging.
//
// mark-sensitive narrows what the LLM can read → no escalation.
// mark-visible widens → --passphrase required.
func registerMarkHeader(parent *cobra.Command) {
	parent.AddCommand(&cobra.Command{
		Use:   "mark-header-sensitive <name> <header> [header...]",
		Short: "Force one or more headers to be redacted in envelope/track (narrowing, no passphrase)",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return markHeaderSensitive(args[0], args[1:])
		},
	})
	auth := &shared.PassphraseAssert{}
	visibleCmd := &cobra.Command{
		Use:   "mark-header-visible <name> <header> [header...]",
		Short: "Force one or more headers to be shown in envelope/track (widening; requires --passphrase)",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return markHeaderVisible(args[0], args[1:], auth)
		},
	}
	shared.BindPassphraseAssertFlags(visibleCmd, auth)
	parent.AddCommand(visibleCmd)
}

// markHeaderSensitive adds headers to the force-redact list. Narrowing
// → no passphrase check.
func markHeaderSensitive(name string, headers []string) error {
	c, err := shared.LoadProfileMetadata(name)
	if err != nil {
		return shared.Fail(err)
	}
	return applyHeaderMembership(c, headers, markToSensitive)
}

// markHeaderVisible adds headers to the force-show list. Widening —
// passphrase required; the verify runs before any mutation.
func markHeaderVisible(name string, headers []string, assert *shared.PassphraseAssert) error {
	c, err := shared.LoadAndAssert(name, assert)
	if err != nil {
		return shared.Fail(err)
	}
	return applyHeaderMembership(c, headers, markToVisible)
}

// headerTarget is a 2-state enum for which override list gains the
// named headers. Replaces the earlier (addToSensitive, addToVisible)
// bool pair that made invalid states (both-true / both-false)
// representable.
type headerTarget int

const (
	markToSensitive headerTarget = iota
	markToVisible
)

// applyHeaderMembership mutates the profile's sensitive/visible header
// sets, moving each named header to the target list and removing it
// from the other (a header can't be forced sensitive AND visible).
func applyHeaderMembership(c *credential.Credential, headers []string, target headerTarget) error {
	sens := normalisedSet(c.SensitiveHeaders)
	vis := normalisedSet(c.VisibleHeaders)
	for _, h := range headers {
		low := strings.ToLower(strings.TrimSpace(h))
		if low == "" {
			continue
		}
		switch target {
		case markToSensitive:
			sens[low] = h
			delete(vis, low)
		case markToVisible:
			vis[low] = h
			delete(sens, low)
		}
	}
	if err := credential.SetSensitiveHeaders(c.Name, mapValues(sens)); err != nil {
		return shared.FailHuman(err)
	}
	if err := credential.SetVisibleHeaders(c.Name, mapValues(vis)); err != nil {
		return shared.FailHuman(err)
	}
	shared.PrintOK(map[string]any{
		"name":              c.Name,
		"sensitive_headers": mapValues(sens),
		"visible_headers":   mapValues(vis),
	})
	return nil
}

func normalisedSet(existing []string) map[string]string {
	m := map[string]string{}
	for _, h := range existing {
		m[strings.ToLower(strings.TrimSpace(h))] = h
	}
	return m
}

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

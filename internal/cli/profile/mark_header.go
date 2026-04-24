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
			return mutateHeaderList(args[0], args[1:], true, false, nil)
		},
	})
	auth := &shared.PassphraseAssert{}
	visibleCmd := &cobra.Command{
		Use:   "mark-header-visible <name> <header> [header...]",
		Short: "Force one or more headers to be shown in envelope/track (widening; requires --passphrase)",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mutateHeaderList(args[0], args[1:], false, true, auth)
		},
	}
	shared.BindPassphraseAssertFlags(visibleCmd, auth)
	parent.AddCommand(visibleCmd)
}

// mutateHeaderList adds to the profile's SensitiveHeaders or
// VisibleHeaders list (setting one also removes the name from the
// OTHER list, since a header can't simultaneously be forced sensitive
// and forced visible).
func mutateHeaderList(name string, headers []string, addToSensitive, addToVisible bool, assert *shared.PassphraseAssert) error {
	c, err := shared.LoadProfileMetadata(name)
	if err != nil {
		return shared.Fail(err)
	}
	if assert != nil {
		if err := shared.ApplyPassphraseAssert(name, assert); err != nil {
			return shared.Fail(err)
		}
	}

	sens := normalisedSet(c.SensitiveHeaders)
	vis := normalisedSet(c.VisibleHeaders)
	for _, h := range headers {
		low := strings.ToLower(strings.TrimSpace(h))
		if low == "" {
			continue
		}
		if addToSensitive {
			sens[low] = h
			delete(vis, low)
		}
		if addToVisible {
			vis[low] = h
			delete(sens, low)
		}
	}
	if err := credential.SetSensitiveHeaders(name, mapValues(sens)); err != nil {
		return shared.FailHuman(err)
	}
	if err := credential.SetVisibleHeaders(name, mapValues(vis)); err != nil {
		return shared.FailHuman(err)
	}
	shared.PrintOK(map[string]any{
		"name":              name,
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

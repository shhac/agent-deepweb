package fetch

import (
	"net/url"
	"strings"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

// parseHeaderFlags converts --header "K: V" strings into a map, failing
// on malformed entries with fixable_by:agent. Pure — testable in isolation.
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

// chooseMethod picks the HTTP method: explicit flag wins, else POST when
// a body is present, else GET.
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

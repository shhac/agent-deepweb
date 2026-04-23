package graphql

import (
	"strings"
	"testing"

	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

func TestClassifyGraphQL(t *testing.T) {
	cases := []struct {
		name       string
		message    string
		extensions map[string]any
		want       agenterrors.FixableBy
		hintHas    string
	}{
		{"UNAUTHENTICATED → human", "who are you", map[string]any{"code": "UNAUTHENTICATED"}, agenterrors.FixableByHuman, "verify the stored credential"},
		{"FORBIDDEN → human", "go away", map[string]any{"code": "FORBIDDEN"}, agenterrors.FixableByHuman, ""},
		{"lowercase forbidden → human (case-insensitive)", "no", map[string]any{"code": "forbidden"}, agenterrors.FixableByHuman, ""},
		{"BAD_USER_INPUT → agent", "bad id", map[string]any{"code": "BAD_USER_INPUT"}, agenterrors.FixableByAgent, "query, variables"},
		{"no extensions → agent", "generic failure", nil, agenterrors.FixableByAgent, ""},
		{"extensions without code → agent", "nope", map[string]any{"other": "x"}, agenterrors.FixableByAgent, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ae := classifyGraphQL(tc.message, tc.extensions)
			if ae.FixableBy != tc.want {
				t.Errorf("FixableBy=%s, want %s", ae.FixableBy, tc.want)
			}
			if tc.hintHas != "" && !strings.Contains(ae.Hint, tc.hintHas) {
				t.Errorf("hint missing %q: %q", tc.hintHas, ae.Hint)
			}
		})
	}
}

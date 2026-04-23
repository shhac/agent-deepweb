package api

import (
	"testing"

	"github.com/shhac/agent-deepweb/internal/credential"
)

func TestResolveUserAgent_Precedence(t *testing.T) {
	// Snapshot + restore Version + env stub.
	origVersion := Version
	origEnvGet := envGet
	t.Cleanup(func() {
		Version = origVersion
		envGet = origEnvGet
	})

	Version = "1.2.3"

	credUA := &credential.Resolved{Credential: credential.Credential{UserAgent: "cred-ua/1.0"}}
	credNoUA := &credential.Resolved{Credential: credential.Credential{UserAgent: ""}}

	cases := []struct {
		name string
		req  Request
		env  string
		want string
	}{
		{
			name: "default (no overrides)",
			req:  Request{Auth: credNoUA},
			want: "agent-deepweb/1.2.3",
		},
		{
			name: "env var applies when nothing else set",
			req:  Request{Auth: credNoUA},
			env:  "env-ua/2.0",
			want: "env-ua/2.0",
		},
		{
			name: "credential UA beats env",
			req:  Request{Auth: credUA},
			env:  "env-ua/2.0",
			want: "cred-ua/1.0",
		},
		{
			name: "header (--header 'User-Agent: ...') beats env",
			req:  Request{Headers: map[string]string{"User-Agent": "header-ua/3.0"}, Auth: credNoUA},
			env:  "env-ua/2.0",
			want: "header-ua/3.0",
		},
		{
			name: "credential beats header",
			req:  Request{Headers: map[string]string{"User-Agent": "header-ua/3.0"}, Auth: credUA},
			want: "cred-ua/1.0",
		},
		{
			name: "per-request UserAgent beats everything",
			req:  Request{UserAgent: "req-ua/4.0", Headers: map[string]string{"User-Agent": "header-ua/3.0"}, Auth: credUA},
			env:  "env-ua/2.0",
			want: "req-ua/4.0",
		},
		{
			name: "empty env is treated as unset",
			req:  Request{Auth: credNoUA},
			env:  "",
			want: "agent-deepweb/1.2.3",
		},
		{
			name: "whitespace-only env is treated as unset",
			req:  Request{Auth: credNoUA},
			env:  "   ",
			want: "agent-deepweb/1.2.3",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			envGet = func(key string) string {
				if key == "AGENT_DEEPWEB_USER_AGENT" {
					return tc.env
				}
				return ""
			}
			got := resolveUserAgent(tc.req)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

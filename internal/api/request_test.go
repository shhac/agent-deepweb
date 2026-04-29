package api

import (
	"testing"

	"github.com/shhac/agent-deepweb/internal/config"
	"github.com/shhac/agent-deepweb/internal/credential"
)

func TestResolveUserAgent_Precedence(t *testing.T) {
	origVersion := Version
	t.Cleanup(func() { Version = origVersion })
	Version = "1.2.3"

	// Point config at a tempdir so we can set default.user-agent safely.
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	credUA := &credential.Resolved{Credential: credential.Credential{UserAgent: "cred-ua/1.0"}}
	credNoUA := &credential.Resolved{Credential: credential.Credential{UserAgent: ""}}

	cases := []struct {
		name     string
		req      Request
		configUA string
		want     string
	}{
		{
			name: "default (no overrides)",
			req:  Request{Auth: credNoUA},
			want: "agent-deepweb/1.2.3",
		},
		{
			name:     "config default.user-agent applies when nothing else set",
			req:      Request{Auth: credNoUA},
			configUA: "env-ua/2.0",
			want:     "env-ua/2.0",
		},
		{
			name:     "credential UA beats config",
			req:      Request{Auth: credUA},
			configUA: "env-ua/2.0",
			want:     "cred-ua/1.0",
		},
		{
			name:     "header (--header 'User-Agent: ...') beats config",
			req:      Request{Headers: map[string]string{"User-Agent": "header-ua/3.0"}, Auth: credNoUA},
			configUA: "env-ua/2.0",
			want:     "header-ua/3.0",
		},
		{
			name: "credential beats header",
			req:  Request{Headers: map[string]string{"User-Agent": "header-ua/3.0"}, Auth: credUA},
			want: "cred-ua/1.0",
		},
		{
			name:     "per-request UserAgent beats everything",
			req:      Request{UserAgent: "req-ua/4.0", Headers: map[string]string{"User-Agent": "header-ua/3.0"}, Auth: credUA},
			configUA: "env-ua/2.0",
			want:     "req-ua/4.0",
		},
		{
			name: "empty config UA falls through to default",
			req:  Request{Auth: credNoUA},
			want: "agent-deepweb/1.2.3",
		},
		{
			name:     "whitespace-only config UA is treated as unset",
			req:      Request{Auth: credNoUA},
			configUA: "   ",
			want:     "agent-deepweb/1.2.3",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := config.Read()
			c.Defaults.UserAgent = tc.configUA
			_ = config.Write(c)
			config.ClearCache()
			got := resolveUserAgent(tc.req)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

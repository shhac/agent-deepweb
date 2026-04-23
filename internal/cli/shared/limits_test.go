package shared

import (
	"testing"
	"time"

	"github.com/shhac/agent-deepweb/internal/config"
)

func TestResolveLimits_Precedence(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir("") })

	cases := []struct {
		name         string
		flagTimeout  int
		flagMaxBytes int64
		globalT      int
		wantTimeout  time.Duration
		wantMaxBytes int64
	}{
		{"flag wins over global + config", 5000, 2048, 10_000, 5 * time.Second, 2048},
		{"global wins over config when flag zero", 0, 0, 7_000, 7 * time.Second, config.DefaultMaxBytes},
		{"config default when flag+global zero", 0, 0, 0, time.Duration(config.DefaultTimeoutMS) * time.Millisecond, config.DefaultMaxBytes},
		{"max-bytes flag wins even when timeout flag zero", 0, 500, 0, time.Duration(config.DefaultTimeoutMS) * time.Millisecond, 500},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := &GlobalFlags{Timeout: tc.globalT}
			gotT, gotM := ResolveLimits(tc.flagTimeout, tc.flagMaxBytes, g)
			if gotT != tc.wantTimeout {
				t.Errorf("timeout=%v, want %v", gotT, tc.wantTimeout)
			}
			if gotM != tc.wantMaxBytes {
				t.Errorf("max-bytes=%d, want %d", gotM, tc.wantMaxBytes)
			}
		})
	}

	t.Run("nil globals is safe", func(t *testing.T) {
		gotT, gotM := ResolveLimits(1000, 100, nil)
		if gotT != time.Second || gotM != 100 {
			t.Errorf("nil globals: %v %d", gotT, gotM)
		}
	})
}

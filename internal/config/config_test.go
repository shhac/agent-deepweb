package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigDir_Precedence(t *testing.T) {
	t.Run("AGENT_DEEPWEB_CONFIG_DIR wins", func(t *testing.T) {
		SetConfigDir("")
		t.Setenv("AGENT_DEEPWEB_CONFIG_DIR", "/tmp/agentdir")
		t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
		if got := ConfigDir(); got != "/tmp/agentdir" {
			t.Errorf("got %q, want /tmp/agentdir", got)
		}
	})

	t.Run("XDG_CONFIG_HOME wins over HOME", func(t *testing.T) {
		SetConfigDir("")
		t.Setenv("AGENT_DEEPWEB_CONFIG_DIR", "")
		t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
		want := filepath.Join("/tmp/xdg", "agent-deepweb")
		if got := ConfigDir(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("HOME fallback", func(t *testing.T) {
		SetConfigDir("")
		t.Setenv("AGENT_DEEPWEB_CONFIG_DIR", "")
		t.Setenv("XDG_CONFIG_HOME", "")
		got := ConfigDir()
		// Just assert it contains agent-deepweb and isn't empty.
		if !strings.Contains(got, "agent-deepweb") {
			t.Errorf("got %q, expected a path containing agent-deepweb", got)
		}
	})

	t.Run("override wins over everything", func(t *testing.T) {
		SetConfigDir("/override")
		defer SetConfigDir("")
		t.Setenv("AGENT_DEEPWEB_CONFIG_DIR", "/tmp/agentdir")
		t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
		if got := ConfigDir(); got != "/override" {
			t.Errorf("got %q, want /override", got)
		}
	})
}

func TestApplyDefaults_UsesConstants(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)
	if cfg.Defaults.TimeoutMS != DefaultTimeoutMS {
		t.Errorf("timeout=%d, want %d", cfg.Defaults.TimeoutMS, DefaultTimeoutMS)
	}
	if cfg.Defaults.MaxBytes != DefaultMaxBytes {
		t.Errorf("max-bytes=%d, want %d", cfg.Defaults.MaxBytes, DefaultMaxBytes)
	}

	// User-provided values are preserved.
	cfg2 := &Config{Defaults: Defaults{TimeoutMS: 5000, MaxBytes: 1024}}
	applyDefaults(cfg2)
	if cfg2.Defaults.TimeoutMS != 5000 || cfg2.Defaults.MaxBytes != 1024 {
		t.Errorf("user values overwritten: %+v", cfg2.Defaults)
	}
}

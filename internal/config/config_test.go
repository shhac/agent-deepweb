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

// TestNewStore_IsolatedFromPackageGlobals — Stores constructed with
// NewStore read/write independently of SetConfigDir. This is the
// property that lets tests run in parallel without cache-sharing
// hazards; regressing would silently re-introduce flaky tests.
func TestNewStore_IsolatedFromPackageGlobals(t *testing.T) {
	// Package default points at dirA; Store instance points at dirB.
	dirA := t.TempDir()
	dirB := t.TempDir()
	SetConfigDir(dirA)
	t.Cleanup(func() { SetConfigDir(""); ClearCache() })

	s := NewStore(dirB)

	// Write via the instance; the default store MUST NOT see it.
	c := s.Read()
	c.Defaults.TimeoutMS = 12345
	if err := s.Write(c); err != nil {
		t.Fatal(err)
	}

	if got := Read().Defaults.TimeoutMS; got == 12345 {
		t.Errorf("default store saw instance write — cache cross-contamination (got %d)", got)
	}
	if got := s.Read().Defaults.TimeoutMS; got != 12345 {
		t.Errorf("instance store lost its own write (got %d)", got)
	}
}

// TestStore_ReadFallsBackToDefaultsOnMissingFile — no config.json yet →
// Read returns a fully-inflated Config (with constants), not nil.
// Callers assume non-nil everywhere.
func TestStore_ReadFallsBackToDefaultsOnMissingFile(t *testing.T) {
	s := NewStore(t.TempDir())
	c := s.Read()
	if c == nil {
		t.Fatal("Read returned nil on missing file")
	}
	if c.Defaults.TimeoutMS != DefaultTimeoutMS {
		t.Errorf("defaults not applied: %+v", c.Defaults)
	}
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

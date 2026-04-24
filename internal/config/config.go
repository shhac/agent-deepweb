// Package config handles agent-deepweb's on-disk configuration directory
// and user-tunable defaults. Values are persisted to config.json; the
// agent-deepweb config {list-keys,get,set,unset} commands manage them.
//
// Every config key has a matching per-invocation CLI flag; precedence
// is flag > config > built-in default.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type Config struct {
	Defaults Defaults `json:"defaults,omitempty"`
	Audit    Audit    `json:"audit,omitempty"`
	Track    Track    `json:"track,omitempty"`
}

type Defaults struct {
	TimeoutMS int    `json:"timeout_ms,omitempty"` // default request timeout (ms)
	MaxBytes  int64  `json:"max_bytes,omitempty"`  // response body size cap (bytes)
	UserAgent string `json:"user_agent,omitempty"` // fallback User-Agent
	Profile   string `json:"profile,omitempty"`    // fallback profile name
}

type Audit struct {
	// Enabled is a pointer so a missing value (use default true) is
	// distinguishable from an explicit false. Callers use Enabled() for
	// the effective value.
	Enabled *bool `json:"enabled,omitempty"`
}

type Track struct {
	TTL string `json:"ttl,omitempty"` // Go duration string; controls new record expires_at
}

// Built-in defaults applied when a zero-value is encountered. The only
// source of truth for "what's the baseline" — don't duplicate these
// constants elsewhere.
const (
	DefaultTimeoutMS = 30_000           // 30s
	DefaultMaxBytes  = 10 * 1024 * 1024 // 10 MiB response cap
	DefaultTrackTTL  = "168h"           // 7 days
	DefaultAudit     = true
)

var (
	cache       *Config
	cacheMu     sync.Mutex
	overrideDir string
)

// SetConfigDir overrides the config directory (used by tests).
func SetConfigDir(dir string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	overrideDir = dir
	cache = nil
}

// ConfigDir returns the directory where agent-deepweb stores its state.
// Order: AGENT_DEEPWEB_CONFIG_DIR > XDG_CONFIG_HOME/agent-deepweb > ~/.config/agent-deepweb.
// This is the ONE remaining env-var indirection in v0.4 — it has to
// exist (tests and portable setups depend on pointing the config
// somewhere non-default).
func ConfigDir() string {
	if overrideDir != "" {
		return overrideDir
	}
	if env := os.Getenv("AGENT_DEEPWEB_CONFIG_DIR"); env != "" {
		return env
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "agent-deepweb")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "agent-deepweb")
}

func configPath() string {
	return filepath.Join(ConfigDir(), "config.json")
}

// Read returns the in-memory config view, loading from disk on first
// access and caching after. ClearCache() invalidates; Write() does so
// automatically.
func Read() *Config {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cache != nil {
		return cache
	}
	data, err := os.ReadFile(configPath())
	if err != nil {
		return defaultConfig()
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaultConfig()
	}
	applyDefaults(&cfg)
	cache = &cfg
	return cache
}

func Write(cfg *Config) error {
	cacheMu.Lock()
	cache = nil
	cacheMu.Unlock()

	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), append(data, '\n'), 0o644)
}

func ClearCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cache = nil
}

func defaultConfig() *Config {
	cfg := &Config{}
	applyDefaults(cfg)
	cache = cfg
	return cfg
}

func applyDefaults(cfg *Config) {
	if cfg.Defaults.TimeoutMS == 0 {
		cfg.Defaults.TimeoutMS = DefaultTimeoutMS
	}
	if cfg.Defaults.MaxBytes == 0 {
		cfg.Defaults.MaxBytes = DefaultMaxBytes
	}
	if cfg.Track.TTL == "" {
		cfg.Track.TTL = DefaultTrackTTL
	}
}

// AuditEnabled returns the effective audit-enabled value. Default true
// when the user hasn't set audit.enabled.
func (c *Config) AuditEnabled() bool {
	if c.Audit.Enabled == nil {
		return DefaultAudit
	}
	return *c.Audit.Enabled
}

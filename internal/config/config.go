// Package config handles agent-deepweb's on-disk configuration directory.
// Also holds user-tunable defaults (timeout, max-size, redaction rules).
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type Config struct {
	Defaults Defaults `json:"defaults"`
}

type Defaults struct {
	TimeoutMS int   `json:"timeout_ms,omitempty"` // default request timeout
	MaxBytes  int64 `json:"max_bytes,omitempty"`  // cap on response body size
	Redact    bool  `json:"redact,omitempty"`     // apply redaction by default (default: true)
}

// Default values used when a zero-value TimeoutMS/MaxBytes is encountered.
// Duplicated nowhere — both config.applyDefaults and api.ClientOptions
// applyDefaults reference these so the "what's the baseline" answer has
// exactly one source of truth.
const (
	DefaultTimeoutMS = 30_000           // 30s — plenty of slack for most APIs
	DefaultMaxBytes  = 10 * 1024 * 1024 // 10 MiB response cap
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
	// Redact defaults to true. We represent that by having the boolean
	// only be "false" when explicitly disabled — read callers should call
	// RedactEnabled() rather than Defaults.Redact directly.
}

// RedactEnabled returns true unless the user has explicitly disabled it in config.
// Note: agent mode always redacts regardless of this setting — checked at call site.
func (c *Config) RedactEnabled() bool {
	// JSON zero value is false; treat explicit-false only when user wrote it.
	// For v1 simplicity, we return true always from config and let --no-redact
	// be the per-request escape hatch (human-only). A future 'config set' can
	// expose a setting; for now, config.Redact is informational.
	return true
}

// Package config handles agent-deepweb's on-disk configuration directory
// and user-tunable defaults. Values are persisted to config.json; the
// agent-deepweb config {list-keys,get,set,unset} commands manage them.
//
// Every config key has a matching per-invocation CLI flag; precedence
// is flag > config > built-in default.
//
// The package exposes two shapes:
//
//   - package-level Read/Write/ConfigDir/... functions, backed by a
//     process-wide default Store. The CLI layer uses these.
//   - a *Store struct with the same methods. Tests and the App wiring
//     (cmd/agent-deepweb/main.go) instantiate Stores with a fixed dir,
//     which eliminates test-ordering hazards around the shared cache.
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

// Store owns one config directory's persisted state. Holds the on-disk
// location plus an in-memory cache of the parsed Config, guarded by a
// mutex so concurrent readers/writers don't tear the pointer.
//
// A zero-value Store resolves its directory from the environment the
// same way ConfigDir() does (AGENT_DEEPWEB_CONFIG_DIR → XDG_CONFIG_HOME
// → ~/.config/agent-deepweb). Tests construct a Store with
// NewStore(tempDir) to get hermetic state.
type Store struct {
	dir   string
	mu    sync.Mutex
	cache *Config
}

// NewStore returns a Store rooted at dir. Pass "" to defer to the
// environment (same resolution order as the zero-value Store).
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// ConfigDir resolves the directory this store reads/writes.
// Precedence: explicit dir > AGENT_DEEPWEB_CONFIG_DIR env >
// XDG_CONFIG_HOME/agent-deepweb > ~/.config/agent-deepweb.
func (s *Store) ConfigDir() string {
	if s.dir != "" {
		return s.dir
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

func (s *Store) configPath() string {
	return filepath.Join(s.ConfigDir(), "config.json")
}

// Read returns the in-memory config view, loading from disk on first
// access and caching after. ClearCache() invalidates; Write() does so
// automatically.
func (s *Store) Read() *Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache != nil {
		return s.cache
	}
	data, err := os.ReadFile(s.configPath())
	if err != nil {
		s.cache = defaultConfig()
		return s.cache
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		s.cache = defaultConfig()
		return s.cache
	}
	applyDefaults(&cfg)
	s.cache = &cfg
	return s.cache
}

// Write persists cfg to disk and invalidates the cache so the next
// Read re-inflates via applyDefaults.
func (s *Store) Write(cfg *Config) error {
	s.mu.Lock()
	s.cache = nil
	s.mu.Unlock()

	dir := s.ConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.configPath(), append(data, '\n'), 0o644)
}

// ClearCache drops the in-memory cache. Tests use this after directly
// mutating the file on disk (bypassing Write).
func (s *Store) ClearCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = nil
}

// SetDir re-roots this Store and invalidates the cache. Used by the
// process-wide default store's SetConfigDir shim; application code
// should prefer constructing a fresh Store with NewStore instead.
func (s *Store) SetDir(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dir = dir
	s.cache = nil
}

// defaultStore is the process-wide Store used by the package-level
// functions. CLI entrypoints flow through this; tests that construct
// their own Store are unaffected.
var defaultStore = &Store{}

// SetConfigDir overrides the default store's config directory (used by
// tests). Prefer NewStore for new code.
func SetConfigDir(dir string) { defaultStore.SetDir(dir) }

// ConfigDir returns the default store's resolved directory.
func ConfigDir() string { return defaultStore.ConfigDir() }

// Read returns the default store's cached config.
func Read() *Config { return defaultStore.Read() }

// Write persists via the default store.
func Write(cfg *Config) error { return defaultStore.Write(cfg) }

// ClearCache invalidates the default store's cache.
func ClearCache() { defaultStore.ClearCache() }

func defaultConfig() *Config {
	cfg := &Config{}
	applyDefaults(cfg)
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

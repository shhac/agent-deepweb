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
	"os"
	"path/filepath"
	"sync"

	"github.com/shhac/lib-agent-cli/creds"
	"github.com/shhac/lib-agent-cli/xdg"
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
	// AGENT_DEEPWEB_CONFIG_DIR is a deepweb-specific override that wins over
	// the standard XDG resolution; keep it ahead of xdg.ConfigDir, which
	// handles the XDG_CONFIG_HOME/agent-deepweb → ~/.config/agent-deepweb tail.
	if env := os.Getenv("AGENT_DEEPWEB_CONFIG_DIR"); env != "" {
		return env
	}
	return xdg.ConfigDir("agent-deepweb")
}

func (s *Store) configPath() string {
	return filepath.Join(s.ConfigDir(), "config.json")
}

// file is config.json's backing store: 0600 writes into a 0700 parent,
// replacement by rename so a reader never sees a half-written document,
// and WithLock for a serialized read-modify-write. This used to be
// hand-rolled with os.ReadFile/os.WriteFile, which carried a lost-update
// race — two concurrent `config set` invocations each built their write
// from a snapshot taken before the other landed, so all but the last
// were erased.
func (s *Store) file() creds.Store {
	return creds.Store{Path: s.configPath()}
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
	s.cache = s.loadFresh()
	return s.cache
}

// loadFresh parses config.json straight from disk, bypassing the cache,
// falling back to the built-in defaults for a missing or unparseable
// file. It is the single definition of "what a from-scratch read looks
// like", shared by Read (which caches the result) and Update (which must
// never hand a mutate callback the cached pointer).
func (s *Store) loadFresh() *Config {
	var cfg Config
	if err := s.file().Load(&cfg); err != nil {
		return defaultConfig()
	}
	applyDefaults(&cfg)
	return &cfg
}

// Write persists cfg to disk and invalidates the cache so the next
// Read re-inflates via applyDefaults.
//
// Write is a blind overwrite; anything that derives its new value from
// the current one must go through Update instead.
func (s *Store) Write(cfg *Config) error {
	err := s.file().Save(cfg)
	s.mu.Lock()
	s.cache = nil
	s.mu.Unlock()
	return err
}

// Update applies mutate to a config loaded fresh from disk, under one
// exclusive lock spanning read, mutate and write, so two concurrent
// invocations serialize instead of each building its write from a stale
// snapshot. The cache is bypassed while the lock is held — mutate always
// sees what was just read off disk — and invalidated afterwards so a
// later Read cannot hand back the pre-write value.
//
// WithLock rather than creds.Store.Update because loadFresh has to keep
// falling back to the built-in defaults for an unparseable config.json,
// which Update's load step would instead surface as a hard error.
func (s *Store) Update(mutate func(*Config) error) error {
	err := s.file().WithLock(func() error {
		cfg := s.loadFresh()
		if err := mutate(cfg); err != nil {
			return err
		}
		return s.file().Save(cfg)
	})

	s.mu.Lock()
	s.cache = nil
	s.mu.Unlock()
	return err
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

// Update applies mutate under the default store's exclusive lock. Any
// change derived from the current config belongs here, not in
// Read-then-Write.
func Update(mutate func(*Config) error) error { return defaultStore.Update(mutate) }

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

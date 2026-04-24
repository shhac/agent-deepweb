package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// KeyDef describes one user-settable config key: its dot-notated name,
// its scalar kind, a one-line description, a display-only hint of the
// built-in default, and three closures that get/set/unset the key on a
// *Config. Centralising the per-key logic here means Get/Set/Unset at
// the top of the file become one-line map lookups — adding a new key
// is one struct literal, no drift risk across three parallel switches.
type KeyDef struct {
	Name        string
	Kind        string // "int" | "int64" | "string" | "bool" | "duration"
	Description string
	Default     string // display-only; actual default comes from package-level constants

	// get returns the current value (as a display string) plus a source
	// tag ("config" when explicitly set, "default" otherwise).
	get func(*Config) (value, source string)
	// set validates value and mutates cfg. Returns a parse error (with
	// key-name prefix) on malformed input.
	set func(*Config, string) error
	// unset reverts the key to its built-in default by writing the zero
	// value (for scalars; applyDefaults re-inflates on next Read) or
	// clearing the pointer (for *bool keys).
	unset func(*Config)
}

// Keys is the canonical list of user-settable keys. Each entry owns
// its type-specific get/set/unset logic. Get/Set/Unset at the top of
// the file dispatch via keyByName.
var Keys = []KeyDef{
	{
		Name:        "default.timeout-ms",
		Kind:        "int",
		Description: "Default request timeout in milliseconds",
		Default:     fmt.Sprint(DefaultTimeoutMS),
		get: func(c *Config) (string, string) {
			if c.Defaults.TimeoutMS == DefaultTimeoutMS {
				return fmt.Sprint(DefaultTimeoutMS), "default"
			}
			return fmt.Sprint(c.Defaults.TimeoutMS), "config"
		},
		set: func(c *Config, v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return errors.New("must be an integer number of milliseconds")
			}
			if n <= 0 {
				return errors.New("must be > 0")
			}
			c.Defaults.TimeoutMS = n
			return nil
		},
		unset: func(c *Config) { c.Defaults.TimeoutMS = 0 },
	},
	{
		Name:        "default.max-bytes",
		Kind:        "int64",
		Description: "Default response body size cap (bytes)",
		Default:     fmt.Sprint(DefaultMaxBytes),
		get: func(c *Config) (string, string) {
			if c.Defaults.MaxBytes == DefaultMaxBytes {
				return fmt.Sprint(DefaultMaxBytes), "default"
			}
			return fmt.Sprint(c.Defaults.MaxBytes), "config"
		},
		set: func(c *Config, v string) error {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return errors.New("must be an integer byte count")
			}
			if n <= 0 {
				return errors.New("must be > 0")
			}
			c.Defaults.MaxBytes = n
			return nil
		},
		unset: func(c *Config) { c.Defaults.MaxBytes = 0 },
	},
	{
		Name:        "default.user-agent",
		Kind:        "string",
		Description: "Fallback User-Agent (lowest precedence; profile/per-request can override)",
		Default:     "",
		get: func(c *Config) (string, string) {
			if c.Defaults.UserAgent == "" {
				return "", "default"
			}
			return c.Defaults.UserAgent, "config"
		},
		set:   func(c *Config, v string) error { c.Defaults.UserAgent = v; return nil },
		unset: func(c *Config) { c.Defaults.UserAgent = "" },
	},
	{
		Name:        "default.profile",
		Kind:        "string",
		Description: "Fallback profile name when --profile is omitted",
		Default:     "",
		get: func(c *Config) (string, string) {
			if c.Defaults.Profile == "" {
				return "", "default"
			}
			return c.Defaults.Profile, "config"
		},
		set:   func(c *Config, v string) error { c.Defaults.Profile = v; return nil },
		unset: func(c *Config) { c.Defaults.Profile = "" },
	},
	{
		Name:        "audit.enabled",
		Kind:        "bool",
		Description: "Write the audit log for every request",
		Default:     fmt.Sprint(DefaultAudit),
		get: func(c *Config) (string, string) {
			if c.Audit.Enabled == nil {
				return fmt.Sprint(DefaultAudit), "default"
			}
			return fmt.Sprint(*c.Audit.Enabled), "config"
		},
		set: func(c *Config, v string) error {
			b, err := parseBool(v)
			if err != nil {
				return err
			}
			c.Audit.Enabled = &b
			return nil
		},
		unset: func(c *Config) { c.Audit.Enabled = nil },
	},
	{
		Name:        "track.ttl",
		Kind:        "duration",
		Description: "How long tracked records live before being eligible for prune",
		Default:     DefaultTrackTTL,
		get: func(c *Config) (string, string) {
			if c.Track.TTL == DefaultTrackTTL {
				return DefaultTrackTTL, "default"
			}
			return c.Track.TTL, "config"
		},
		set: func(c *Config, v string) error {
			if _, err := time.ParseDuration(v); err != nil {
				return errors.New("must be a Go duration (e.g. '24h', '168h')")
			}
			c.Track.TTL = v
			return nil
		},
		unset: func(c *Config) { c.Track.TTL = "" },
	},
}

// keyByName is the dispatch table used by Get/Set/Unset. Built lazily
// the first time one of them is called; cheap to rebuild but simpler
// than thread-safe init tracking.
var keyByName = func() map[string]*KeyDef {
	m := make(map[string]*KeyDef, len(Keys))
	for i := range Keys {
		m[Keys[i].Name] = &Keys[i]
	}
	return m
}()

// ErrUnknownKey is returned for any key not in Keys. Callers surface as
// fixable_by:agent so the human sees the exact valid set.
var ErrUnknownKey = errors.New("unknown config key")

// KnownKey reports whether name is in Keys.
func KnownKey(name string) bool {
	_, ok := keyByName[name]
	return ok
}

// Get returns the current in-memory value for a key as a display
// string, plus a source tag ("config" when explicitly set, "default"
// otherwise).
func Get(cfg *Config, name string) (string, string, error) {
	def, ok := keyByName[name]
	if !ok {
		return "", "", ErrUnknownKey
	}
	v, src := def.get(cfg)
	return v, src, nil
}

// Set mutates cfg in-place with the parsed value for name. The returned
// error (on malformed input) is prefixed with the key name so the CLI
// layer can surface it verbatim.
func Set(cfg *Config, name, value string) error {
	def, ok := keyByName[name]
	if !ok {
		return ErrUnknownKey
	}
	if err := def.set(cfg, value); err != nil {
		return fmt.Errorf("%s: %s", name, err.Error())
	}
	return nil
}

// Unset reverts a key to its built-in default.
func Unset(cfg *Config, name string) error {
	def, ok := keyByName[name]
	if !ok {
		return ErrUnknownKey
	}
	def.unset(cfg)
	return nil
}

// TrackTTL returns the configured duration for tracked-record
// retention, or the built-in default on parse failure / zero / negative.
func (c *Config) TrackTTL() time.Duration {
	if c.Track.TTL == "" {
		d, _ := time.ParseDuration(DefaultTrackTTL)
		return d
	}
	d, err := time.ParseDuration(c.Track.TTL)
	if err != nil || d <= 0 {
		d2, _ := time.ParseDuration(DefaultTrackTTL)
		return d2
	}
	return d
}

func parseBool(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes", "on":
		return true, nil
	case "false", "0", "no", "off":
		return false, nil
	default:
		return false, errors.New("must be true/false (or yes/no, on/off, 1/0)")
	}
}

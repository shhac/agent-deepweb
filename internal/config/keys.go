package config

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// KeyDef describes one user-settable config key: its dot-notated name,
// its scalar kind, a one-line description, a display-only hint of the
// built-in default, and three closures that get/set/unset the key on a
// *Config. Centralising the per-key logic here means Get/Set/Unset
// become one-line map lookups — adding a new key is one struct
// literal in keys_table.go, no drift risk across three parallel
// switches.
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

package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// KeyDef describes one user-settable config key: its dot-notated name,
// its scalar kind, a one-line description for `config list-keys`, and
// a hint of the built-in default for the help text.
type KeyDef struct {
	Name        string
	Kind        string // "int" | "int64" | "string" | "bool" | "duration"
	Description string
	Default     string // display-only; the actual default comes from the package-level constants
}

// Keys is the canonical list of user-settable keys. Also the source of
// truth for which names `config set/get/unset` accept.
var Keys = []KeyDef{
	{"default.timeout-ms", "int", "Default request timeout in milliseconds", fmt.Sprint(DefaultTimeoutMS)},
	{"default.max-bytes", "int64", "Default response body size cap (bytes)", fmt.Sprint(DefaultMaxBytes)},
	{"default.user-agent", "string", "Fallback User-Agent (lowest precedence; profile/per-request can override)", ""},
	{"default.profile", "string", "Fallback profile name when --profile is omitted", ""},
	{"audit.enabled", "bool", "Write the audit log for every request", fmt.Sprint(DefaultAudit)},
	{"track.ttl", "duration", "How long tracked records live before being eligible for prune", DefaultTrackTTL},
}

// ErrUnknownKey is returned for any key not in Keys. Callers surface as
// fixable_by:agent so the human sees the exact valid set.
var ErrUnknownKey = errors.New("unknown config key")

// KnownKey reports whether name is in Keys.
func KnownKey(name string) bool {
	for _, k := range Keys {
		if k.Name == name {
			return true
		}
	}
	return false
}

// Get returns the current in-memory value for a key as a display string
// ("" for unset-string, "true"/"false" for bool, etc.), plus a source
// tag ("config" when explicitly set, "default" otherwise).
func Get(cfg *Config, name string) (string, string, error) {
	switch name {
	case "default.timeout-ms":
		if cfg.Defaults.TimeoutMS == DefaultTimeoutMS {
			return fmt.Sprint(DefaultTimeoutMS), "default", nil
		}
		return fmt.Sprint(cfg.Defaults.TimeoutMS), "config", nil
	case "default.max-bytes":
		if cfg.Defaults.MaxBytes == DefaultMaxBytes {
			return fmt.Sprint(DefaultMaxBytes), "default", nil
		}
		return fmt.Sprint(cfg.Defaults.MaxBytes), "config", nil
	case "default.user-agent":
		if cfg.Defaults.UserAgent == "" {
			return "", "default", nil
		}
		return cfg.Defaults.UserAgent, "config", nil
	case "default.profile":
		if cfg.Defaults.Profile == "" {
			return "", "default", nil
		}
		return cfg.Defaults.Profile, "config", nil
	case "audit.enabled":
		if cfg.Audit.Enabled == nil {
			return fmt.Sprint(DefaultAudit), "default", nil
		}
		return fmt.Sprint(*cfg.Audit.Enabled), "config", nil
	case "track.ttl":
		if cfg.Track.TTL == DefaultTrackTTL {
			return DefaultTrackTTL, "default", nil
		}
		return cfg.Track.TTL, "config", nil
	default:
		return "", "", ErrUnknownKey
	}
}

// Set mutates cfg in-place with the parsed value for name. Returns a
// parse error (with key-name context) on malformed input.
func Set(cfg *Config, name, value string) error {
	switch name {
	case "default.timeout-ms":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s: must be an integer number of milliseconds", name)
		}
		if v <= 0 {
			return fmt.Errorf("%s: must be > 0", name)
		}
		cfg.Defaults.TimeoutMS = v
	case "default.max-bytes":
		v, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("%s: must be an integer byte count", name)
		}
		if v <= 0 {
			return fmt.Errorf("%s: must be > 0", name)
		}
		cfg.Defaults.MaxBytes = v
	case "default.user-agent":
		cfg.Defaults.UserAgent = value
	case "default.profile":
		cfg.Defaults.Profile = value
	case "audit.enabled":
		v, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("%s: %s", name, err.Error())
		}
		cfg.Audit.Enabled = &v
	case "track.ttl":
		if _, err := time.ParseDuration(value); err != nil {
			return fmt.Errorf("%s: must be a Go duration (e.g. '24h', '168h')", name)
		}
		cfg.Track.TTL = value
	default:
		return ErrUnknownKey
	}
	return nil
}

// Unset reverts a key to its built-in default by clearing the stored
// value (for pointer-typed keys) or writing the zero value (for scalar
// types — applyDefaults re-inflates it on Read).
func Unset(cfg *Config, name string) error {
	switch name {
	case "default.timeout-ms":
		cfg.Defaults.TimeoutMS = 0
	case "default.max-bytes":
		cfg.Defaults.MaxBytes = 0
	case "default.user-agent":
		cfg.Defaults.UserAgent = ""
	case "default.profile":
		cfg.Defaults.Profile = ""
	case "audit.enabled":
		cfg.Audit.Enabled = nil
	case "track.ttl":
		cfg.Track.TTL = ""
	default:
		return ErrUnknownKey
	}
	return nil
}

// TrackTTL returns the configured duration for tracked-record retention,
// or the built-in default on parse failure.
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

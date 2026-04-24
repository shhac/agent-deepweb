package config

import (
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Keys is the canonical list of user-settable keys. Each entry owns
// its type-specific get/set/unset logic. Get/Set/Unset (in keys.go)
// dispatch via keyByName.
//
// Adding a new key is one struct literal here + one entry in the
// config.json schema docs. No drift risk across the three parallel
// Get/Set/Unset dispatchers.
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

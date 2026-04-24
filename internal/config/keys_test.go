package config

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestSet_PerKeyParseAndReject exercises each key's type-specific
// parser: happy path + representative malformed input. Guards against
// a future key landing with a set() that accepts anything.
func TestSet_PerKeyParseAndReject(t *testing.T) {
	cases := []struct {
		key, value string
		wantErr    bool
	}{
		{"default.timeout-ms", "5000", false},
		{"default.timeout-ms", "not-a-number", true},
		{"default.timeout-ms", "0", true},
		{"default.timeout-ms", "-1", true},
		{"default.max-bytes", "20000000", false},
		{"default.max-bytes", "oops", true},
		{"default.max-bytes", "0", true},
		{"default.user-agent", "custom-ua/1.0", false},
		{"default.user-agent", "", false}, // strings can be empty
		{"default.profile", "github", false},
		{"audit.enabled", "true", false},
		{"audit.enabled", "false", false},
		{"audit.enabled", "on", false},
		{"audit.enabled", "off", false},
		{"audit.enabled", "maybe", true},
		{"track.ttl", "24h", false},
		{"track.ttl", "30m", false},
		{"track.ttl", "not-a-duration", true},
		{"unknown.key", "anything", true},
	}
	for _, tc := range cases {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			c := &Config{}
			applyDefaults(c)
			err := Set(c, tc.key, tc.value)
			if tc.wantErr && err == nil {
				t.Errorf("want error for %s=%q", tc.key, tc.value)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error for %s=%q: %v", tc.key, tc.value, err)
			}
		})
	}
}

// TestSet_ErrUnknownKey — unknown keys surface the sentinel so the
// CLI layer can produce its "valid keys: [...]" hint.
func TestSet_ErrUnknownKey(t *testing.T) {
	err := Set(&Config{}, "no.such.key", "x")
	if !errors.Is(err, ErrUnknownKey) {
		t.Errorf("want ErrUnknownKey, got %v", err)
	}
}

// TestGet_SourceTagging — a key that matches the built-in default
// reports source="default"; explicit values report "config".
func TestGet_SourceTagging(t *testing.T) {
	c := &Config{}
	applyDefaults(c)

	val, src, _ := Get(c, "default.timeout-ms")
	if src != "default" {
		t.Errorf("unset → default, got %q (val=%q)", src, val)
	}

	_ = Set(c, "default.timeout-ms", "5000")
	val, src, _ = Get(c, "default.timeout-ms")
	if src != "config" || val != "5000" {
		t.Errorf("after set → config=5000, got src=%q val=%q", src, val)
	}
}

// TestUnset_RevertsToDefault — Unset followed by Read must re-inflate
// the built-in via applyDefaults. This is the invariant the CLI's
// `config unset <key>` relies on.
func TestUnset_RevertsToDefault(t *testing.T) {
	dir := t.TempDir()
	SetConfigDir(dir)
	t.Cleanup(func() { SetConfigDir(""); ClearCache() })

	c := Read()
	_ = Set(c, "default.timeout-ms", "5000")
	_ = Write(c)
	ClearCache()

	// After Write+Read, value should be 5000.
	if c := Read(); c.Defaults.TimeoutMS != 5000 {
		t.Errorf("stored value lost: %d", c.Defaults.TimeoutMS)
	}

	// Unset → revert → Read should get the built-in default.
	c2 := Read()
	_ = Unset(c2, "default.timeout-ms")
	_ = Write(c2)
	ClearCache()

	c3 := Read()
	if c3.Defaults.TimeoutMS != DefaultTimeoutMS {
		t.Errorf("after unset, got %d (want built-in %d)", c3.Defaults.TimeoutMS, DefaultTimeoutMS)
	}
	val, src, _ := Get(c3, "default.timeout-ms")
	if src != "default" {
		t.Errorf("Get source: %q (val=%q)", src, val)
	}
}

// TestAuditEnabled_PointerSemantics — `audit.enabled` uses a *bool so
// "unset → default true" is distinguishable from "explicit false".
// A serialisation bug that coerces nil to false silently disables
// auditing on every existing install.
func TestAuditEnabled_PointerSemantics(t *testing.T) {
	c := &Config{}
	if !c.AuditEnabled() {
		t.Error("audit.enabled default should be true when Audit.Enabled == nil")
	}

	f := false
	c.Audit.Enabled = &f
	if c.AuditEnabled() {
		t.Error("explicit false must return false (not default true)")
	}

	tr := true
	c.Audit.Enabled = &tr
	if !c.AuditEnabled() {
		t.Error("explicit true must return true")
	}

	// Unset reverts to nil → default true.
	_ = Unset(c, "audit.enabled")
	if !c.AuditEnabled() {
		t.Error("after unset, AuditEnabled should return default true")
	}
}

// TestTrackTTL_FallbacksOnBadInput — the TTL getter must tolerate
// garbage stored values (from hand-edits or future-version bugs) and
// fall back to the built-in. A negative duration would cause
// PruneExpired to delete every fresh record on the next Write.
func TestTrackTTL_FallbacksOnBadInput(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"empty → default", "", durationMust(DefaultTrackTTL)},
		{"parse error → default", "garbage", durationMust(DefaultTrackTTL)},
		{"negative → default", "-1h", durationMust(DefaultTrackTTL)},
		{"zero → default", "0s", durationMust(DefaultTrackTTL)},
		{"valid positive → as stored", "24h", 24 * time.Hour},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{Track: Track{TTL: tc.value}}
			if got := c.TrackTTL(); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestWriteRead_RoundTripsAllKeys — every key type must survive a
// disk round-trip (JSON-ability + field tags line up).
func TestWriteRead_RoundTripsAllKeys(t *testing.T) {
	dir := t.TempDir()
	SetConfigDir(dir)
	t.Cleanup(func() { SetConfigDir(""); ClearCache() })

	c := Read()
	for _, kv := range []struct{ key, value string }{
		{"default.timeout-ms", "12345"},
		{"default.max-bytes", "67890"},
		{"default.user-agent", "test-ua/1.0"},
		{"default.profile", "myprof"},
		{"audit.enabled", "false"},
		{"track.ttl", "48h"},
	} {
		if err := Set(c, kv.key, kv.value); err != nil {
			t.Fatalf("Set %s=%s: %v", kv.key, kv.value, err)
		}
	}
	if err := Write(c); err != nil {
		t.Fatal(err)
	}
	ClearCache()

	got := Read()
	for _, kv := range []struct{ key, value string }{
		{"default.timeout-ms", "12345"},
		{"default.max-bytes", "67890"},
		{"default.user-agent", "test-ua/1.0"},
		{"default.profile", "myprof"},
		{"audit.enabled", "false"},
		{"track.ttl", "48h"},
	} {
		v, src, _ := Get(got, kv.key)
		if v != kv.value || src != "config" {
			t.Errorf("key %s: got (%q, %q), want (%q, config)", kv.key, v, src, kv.value)
		}
	}
}

// TestWrite_InvalidatesCache — Write must invalidate the cache so a
// subsequent Read sees the new disk state (not the stale in-memory
// copy from before Write). Prevents "I set it but get still returns
// the old value" bugs.
func TestWrite_InvalidatesCache(t *testing.T) {
	dir := t.TempDir()
	SetConfigDir(dir)
	t.Cleanup(func() { SetConfigDir(""); ClearCache() })

	c := Read()
	_ = Set(c, "default.profile", "alpha")
	_ = Write(c)
	if v, _, _ := Get(Read(), "default.profile"); v != "alpha" {
		t.Errorf("after write, Read should surface alpha; got %q", v)
	}

	// Outside-of-process file edit — pretend another process wrote it.
	c2 := Read()
	_ = Set(c2, "default.profile", "beta")
	_ = Write(c2)
	if v, _, _ := Get(Read(), "default.profile"); v != "beta" {
		t.Errorf("cache not invalidated; got %q", v)
	}
}

// durationMust parses a duration string or panics — test helper for
// built-in constants that are known-valid at build time.
func durationMust(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		panic(err)
	}
	return d
}

// sanity: parseBool accepts the documented variants.
func TestParseBool(t *testing.T) {
	for _, yes := range []string{"true", "1", "yes", "on", "TRUE", "On"} {
		b, err := parseBool(yes)
		if err != nil || !b {
			t.Errorf("%q → (%v, %v), want (true, nil)", yes, b, err)
		}
	}
	for _, no := range []string{"false", "0", "no", "off", " FALSE "} {
		b, err := parseBool(no)
		if err != nil || b {
			t.Errorf("%q → (%v, %v), want (false, nil)", no, b, err)
		}
	}
	if _, err := parseBool("banana"); err == nil || !strings.Contains(err.Error(), "true/false") {
		t.Errorf("bad input should error, got %v", err)
	}
}

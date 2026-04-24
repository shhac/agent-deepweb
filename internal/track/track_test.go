package track

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shhac/agent-deepweb/internal/config"
)

// jsonMarshal is a tiny local shim so the test doesn't need to repeat
// the MarshalIndent call twice.
func jsonMarshal(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }

// TestWrite_StampsExpiresAtFromCurrentTTL covers the load-bearing v0.4
// rule: ExpiresAt is computed at write time from the CURRENT config
// TTL. A later TTL change must not retroactively expire old records.
func TestWrite_StampsExpiresAtFromCurrentTTL(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	// Set TTL to 24h.
	c := config.Read()
	c.Track.TTL = "24h"
	if err := config.Write(c); err != nil {
		t.Fatal(err)
	}
	config.ClearCache()

	now := time.Now().UTC()
	rec := &Record{ID: "test-1", Timestamp: now}
	if err := Write(rec); err != nil {
		t.Fatal(err)
	}

	got, err := Read("test-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ExpiresAt.IsZero() {
		t.Fatal("ExpiresAt should have been stamped on write")
	}
	delta := got.ExpiresAt.Sub(now)
	want := 24 * time.Hour
	// Allow small variance for the "now" passed through Write.
	if delta < want-time.Second || delta > want+time.Second {
		t.Errorf("ExpiresAt should be roughly +24h from Timestamp; delta = %v", delta)
	}
}

// TestPruneExpired_RespectsOriginalExpiresAt is the test that stops
// the "config TTL changes wipe old records" regression. We write a
// record at TTL=168h, then shorten the TTL to 1ns, and assert prune
// does NOT delete the old record (its ExpiresAt is still in the
// future).
func TestPruneExpired_RespectsOriginalExpiresAt(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	// Initial TTL: 168h (default).
	c := config.Read()
	c.Track.TTL = "168h"
	_ = config.Write(c)
	config.ClearCache()

	if err := Write(&Record{ID: "long-lived", Timestamp: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	// Shorten TTL — but old record already has its original ExpiresAt.
	c = config.Read()
	c.Track.TTL = "1ns"
	_ = config.Write(c)
	config.ClearCache()

	removed, err := PruneExpired()
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Errorf("PruneExpired removed %d records with future ExpiresAt; TTL shortening must not retroactively delete", removed)
	}
	if _, err := Read("long-lived"); err != nil {
		t.Errorf("record should still exist, got %v", err)
	}
}

// TestPruneExpired_RemovesGenuinelyExpired verifies the happy path:
// a record whose ExpiresAt is in the past gets removed. We bypass
// Write (which lazy-prunes on each call) and drop the record file
// directly so we can test PruneExpired in isolation.
func TestPruneExpired_RemovesGenuinelyExpired(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	writeDirect := func(id string, expires time.Time) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, "track"), 0o700); err != nil {
			t.Fatal(err)
		}
		data, _ := jsonMarshal(&Record{ID: id, Timestamp: expires.Add(-time.Hour), ExpiresAt: expires})
		if err := os.WriteFile(filepath.Join(dir, "track", id+".json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeDirect("expired", time.Now().Add(-1*time.Minute))
	writeDirect("fresh", time.Now().Add(time.Hour))

	removed, err := PruneExpired()
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("want 1 removed, got %d", removed)
	}
	if _, err := Read("expired"); err == nil {
		t.Error("expired record should be gone")
	}
	if _, err := Read("fresh"); err != nil {
		t.Errorf("fresh record should survive: %v", err)
	}
}

// TestPruneByProfile_ScopesByName removes only records whose Profile
// matches. Called by `profile remove` to keep the track directory from
// carrying orphan records.
func TestPruneByProfile_ScopesByName(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	now := time.Now().UTC()
	entries := []*Record{
		{ID: "a1", Timestamp: now, Profile: "alice"},
		{ID: "a2", Timestamp: now, Profile: "alice"},
		{ID: "b1", Timestamp: now, Profile: "bob"},
		{ID: "anon", Timestamp: now, Profile: "none"},
	}
	for _, r := range entries {
		if err := Write(r); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := PruneByProfile("alice")
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Errorf("want 2 removed for alice, got %d", removed)
	}
	// Unrelated profiles must survive.
	for _, id := range []string{"b1", "anon"} {
		if _, err := Read(id); err != nil {
			t.Errorf("record %s should survive PruneByProfile(alice), got %v", id, err)
		}
	}
}

// TestPruneOlderThan_UsesTimestampNotExpiresAt — the "nuke anything
// older than N regardless of per-record contract" override.
func TestPruneOlderThan_UsesTimestampNotExpiresAt(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	now := time.Now().UTC()
	old := now.Add(-2 * time.Hour)
	// Old record has a future ExpiresAt — PruneExpired would keep it,
	// but --older-than 1h should remove it.
	if err := Write(&Record{
		ID:        "old-with-future-expiry",
		Timestamp: old,
		ExpiresAt: now.Add(24 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	// Fresh record → kept.
	if err := Write(&Record{ID: "fresh", Timestamp: now}); err != nil {
		t.Fatal(err)
	}

	removed, err := PruneOlderThan(1 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("want 1 removed by Timestamp, got %d", removed)
	}
	if _, err := Read("fresh"); err != nil {
		t.Errorf("fresh record should survive: %v", err)
	}
	if _, err := Read("old-with-future-expiry"); err == nil {
		t.Error("old record should have been removed despite future ExpiresAt")
	}
}

// TestWrite_FileMode0600 verifies track records aren't world-readable.
func TestWrite_FileMode0600(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	if err := Write(&Record{ID: "perm-test", Timestamp: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "track", "perm-test.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("track record perm should be 0600, got %o", perm)
	}
}

// TestNewID_MonotonicallyPrefixedAndUnique — the ID format must be
// time-sortable (YYYYMMDDTHHMM-XXXX) and two consecutive calls must
// differ (the XXXX hex suffix prevents collisions within the same
// minute).
func TestNewID_MonotonicallyPrefixedAndUnique(t *testing.T) {
	a, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("two IDs generated back-to-back must differ")
	}
	if len(a) < 18 {
		t.Errorf("ID %q is shorter than expected YYYYMMDDTHHMM-XXXX format", a)
	}
	// Time prefix — same minute, both should share the first ~13 chars.
	if a[:13] != b[:13] {
		t.Logf("Note: IDs crossed a minute boundary during test (harmless): %q vs %q", a, b)
	}
}

// TestPruneExpired_EmptyDirIsHarmless — no track dir yet, nothing to
// remove, no error. Called implicitly on every first Write.
func TestPruneExpired_EmptyDirIsHarmless(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	removed, err := PruneExpired()
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Errorf("empty dir: want 0 removed, got %d", removed)
	}
}

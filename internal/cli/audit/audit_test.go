package audit

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	"github.com/shhac/agent-deepweb/internal/config"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/track"
)

// TestRunTrackPrune_BadDuration — `--older-than bogus` must surface a
// fixable_by:agent error with the offending value quoted, so the LLM
// can fix its own input without pinging a human.
func TestRunTrackPrune_BadDuration(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	removed, err := runTrackPrune("not-a-duration")
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if removed != 0 {
		t.Errorf("bad duration should return 0 removed, got %d", removed)
	}
	var apiErr *agenterrors.APIError
	if !errorAs(err, &apiErr) {
		t.Fatalf("want APIError, got %T: %v", err, err)
	}
	if apiErr.FixableBy != agenterrors.FixableByAgent {
		t.Errorf("want fixable_by=agent, got %q", apiErr.FixableBy)
	}
	if !strings.Contains(err.Error(), `"not-a-duration"`) {
		t.Errorf("error should quote the bad value, got %q", err.Error())
	}
}

// TestRunTrackPrune_EmptyDefaultCall — no records, no --older-than → 0, nil.
// Locks in that the default path dispatches to PruneExpired (which is
// harmless on an empty dir).
func TestRunTrackPrune_EmptyDefaultCall(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	removed, err := runTrackPrune("")
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Errorf("empty dir should return 0, got %d", removed)
	}
}

// TestRunTrackPrune_OlderThanDispatches — a valid --older-than duration
// is routed to PruneOlderThan, which matches on Timestamp (not
// ExpiresAt). Write a record with Timestamp in the past but ExpiresAt
// in the future; --older-than 1h should still remove it.
func TestRunTrackPrune_OlderThanDispatches(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	now := time.Now().UTC()
	if err := track.Write(&track.Record{
		ID:        "old",
		Timestamp: now.Add(-2 * time.Hour),
		ExpiresAt: now.Add(24 * time.Hour), // would survive PruneExpired
	}); err != nil {
		t.Fatal(err)
	}
	if err := track.Write(&track.Record{ID: "fresh", Timestamp: now}); err != nil {
		t.Fatal(err)
	}

	removed, err := runTrackPrune("1h")
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("want 1 removed (the 2h-old record), got %d", removed)
	}
	if _, err := track.Read("fresh"); err != nil {
		t.Errorf("fresh should survive, got %v", err)
	}
	if _, err := track.Read("old"); err == nil {
		t.Error("old record should have been removed")
	}
}

// TestReadTrackedRecord_NotFound — missing record is classified as
// fixable_by:human with a hint pointing at --track + TTL. This is the
// most common failure mode for `audit show <id>`: the LLM copies an
// audit_id from a non-tracked entry, or the record aged out.
func TestReadTrackedRecord_NotFound(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	_, err := readTrackedRecord("20260101T1200-abcd")
	if err == nil {
		t.Fatal("expected not-found error")
	}
	var apiErr *agenterrors.APIError
	if !errorAs(err, &apiErr) {
		t.Fatalf("want APIError, got %T: %v", err, err)
	}
	if apiErr.FixableBy != agenterrors.FixableByHuman {
		t.Errorf("missing record is human-fixable (requires re-running with --track); got %q", apiErr.FixableBy)
	}
	if !strings.Contains(err.Error(), `"20260101T1200-abcd"`) {
		t.Errorf("error should quote the missing ID, got %q", err.Error())
	}
}

// TestReadTrackedRecord_Found — happy path: a record written via
// track.Write round-trips through readTrackedRecord.
func TestReadTrackedRecord_Found(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	id := "20260101T1200-wxyz"
	if err := track.Write(&track.Record{
		ID:        id,
		Timestamp: time.Now().UTC(),
		Profile:   "alice",
		Outcome:   "ok",
	}); err != nil {
		t.Fatal(err)
	}

	rec, err := readTrackedRecord(id)
	if err != nil {
		t.Fatal(err)
	}
	if rec.ID != id || rec.Profile != "alice" || rec.Outcome != "ok" {
		t.Errorf("round-trip mismatch: %+v", rec)
	}
}

// TestPruneCmd_RefusesOnlyTracksFalse — `audit prune --only-tracks=false`
// must refuse today (the audit.log itself has no TTL). Guards against
// silently changing behaviour when log pruning lands later.
func TestPruneCmd_RefusesOnlyTracksFalse(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })

	root := &cobra.Command{Use: "root"}
	Register(root, func() *shared.GlobalFlags { return &shared.GlobalFlags{} })

	var stderr bytes.Buffer
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&stderr)
	root.SetArgs([]string{"audit", "prune", "--only-tracks=false"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected refusal error")
	}
	if !strings.Contains(err.Error(), "not supported yet") {
		t.Errorf("refusal message drift: got %q", err.Error())
	}
}

// errorAs is a local alias for errors.As so the test file stays self-
// contained without growing an extra import line.
func errorAs(err error, target any) bool {
	switch t := target.(type) {
	case **agenterrors.APIError:
		if e, ok := err.(*agenterrors.APIError); ok {
			*t = e
			return true
		}
	}
	return false
}

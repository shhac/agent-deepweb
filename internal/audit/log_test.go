package audit

import (
	"os"
	"testing"
	"time"

	"github.com/shhac/agent-deepweb/internal/config"
)

func TestAppendAndTail(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir("") })

	t.Setenv("AGENT_DEEPWEB_AUDIT", "on")
	for i := 0; i < 3; i++ {
		Append(Entry{
			Timestamp:  time.Unix(int64(i), 0).UTC(),
			Method:     "GET",
			Host:       "example.com",
			Path:       "/p",
			Profile:    "c",
			Status:     200,
			Bytes:      100 + i,
			DurationMS: int64(i),
			Outcome:    "ok",
		})
	}
	got, err := Tail(50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %d", len(got))
	}
	if got[0].Bytes != 100 || got[2].Bytes != 102 {
		t.Errorf("unexpected order/values: %+v", got)
	}
}

func TestDisabledSkipsWrites(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir("") })

	t.Setenv("AGENT_DEEPWEB_AUDIT", "off")
	Append(Entry{Method: "GET", Host: "x"})
	// File should not exist.
	if _, err := os.Stat(dir + "/audit.log"); !os.IsNotExist(err) {
		t.Fatalf("expected no audit file, stat err=%v", err)
	}
}

func TestSummarize_GroupsByHostAndOutcome(t *testing.T) {
	entries := []Entry{
		{Host: "a.com", Outcome: "ok", Profile: "k1"},
		{Host: "a.com", Outcome: "error", Profile: "k1"},
		{Host: "b.com", Outcome: "ok", Profile: "none", Jar: "/tmp/byo.json"},
	}
	s := Summarize(entries)
	if s.Total != 3 {
		t.Errorf("total: %d", s.Total)
	}
	if s.ByHost["a.com"] != 2 || s.ByHost["b.com"] != 1 {
		t.Errorf("by_host: %v", s.ByHost)
	}
	if s.ByOutcome["ok"] != 2 || s.ByOutcome["error"] != 1 {
		t.Errorf("by_outcome: %v", s.ByOutcome)
	}
	if s.ByProfile["k1"] != 2 || s.ByProfile["none"] != 1 {
		t.Errorf("by_profile: %v", s.ByProfile)
	}
	if s.AnonymousCount != 1 {
		t.Errorf("anonymous_requests: %d", s.AnonymousCount)
	}
	if s.ByJarPath["/tmp/byo.json"] != 1 {
		t.Errorf("by_jar_path: %v", s.ByJarPath)
	}
}

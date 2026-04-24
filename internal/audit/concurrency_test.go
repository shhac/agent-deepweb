package audit

import (
	"sync"
	"testing"

	"github.com/shhac/agent-deepweb/internal/config"
)

// TestAppend_Concurrent exercises a burst of parallel Append calls to
// confirm (1) nothing panics and (2) every line written is a complete,
// parseable JSON record (no interleaving). Run with `go test -race` to
// also catch data races on the file.
func TestAppend_Concurrent(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir("") })
	t.Setenv("AGENT_DEEPWEB_AUDIT", "on")

	const N = 50
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			Append(Entry{
				Method:     "GET",
				Host:       "example.com",
				Path:       "/p",
				Profile: "c",
				Status:     200,
				Outcome:    "ok",
				Bytes:      i,
			})
		}(i)
	}
	wg.Wait()

	got, err := Tail(1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != N {
		t.Fatalf("expected %d entries, got %d", N, len(got))
	}
	for i, e := range got {
		if e.Host != "example.com" {
			t.Errorf("entry %d corrupted: %+v", i, e)
		}
	}
}

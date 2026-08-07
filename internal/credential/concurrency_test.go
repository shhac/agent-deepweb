package credential

import (
	"fmt"
	"sync"
	"testing"

	"github.com/shhac/agent-deepweb/internal/config"
)

// concurrentProfiles is the writer count the lost-update race was measured
// with: twenty parallel writers against the hand-rolled read → mutate →
// write left roughly one surviving entry.
const concurrentProfiles = 20

// headlessConfigDir points both stores at a temp dir and forces the file
// fallback, so these tests never reach the real keychain (process-global,
// and on darwin a GUI prompt).
func headlessConfigDir(t *testing.T) {
	t.Helper()
	t.Setenv("AGENT_DEEPWEB_NO_KEYCHAIN", "1")
	config.SetConfigDir(t.TempDir())
	t.Cleanup(func() { config.SetConfigDir(""); config.ClearCache() })
}

func profileName(i int) string { return fmt.Sprintf("concurrent-writer-%02d", i) }

// TestStore_ConcurrentWritersAllSurvive is the regression guard for the
// race that mattered most: a profile lost from credentials.json strands
// its secret, which is still stored but no longer referenced by anything
// that could show or revoke it.
func TestStore_ConcurrentWritersAllSurvive(t *testing.T) {
	headlessConfigDir(t)

	var wg sync.WaitGroup
	errs := make(chan error, concurrentProfiles)
	for i := range concurrentProfiles {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := Store(Credential{
				Name:    profileName(i),
				Type:    AuthBearer,
				Domains: []string{"api.example.com"},
			}, Secrets{Token: fmt.Sprintf("tok-%02d-not-a-real-token", i)})
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("Store: %v", err)
	}

	idx, err := readIndex()
	if err != nil {
		t.Fatalf("readIndex: %v", err)
	}
	sec, err := readSecretsFile()
	if err != nil {
		t.Fatalf("readSecretsFile: %v", err)
	}
	for i := range concurrentProfiles {
		name := profileName(i)
		if _, ok := idx[name]; !ok {
			t.Errorf("index lost %q", name)
		}
		if _, ok := sec[name]; !ok {
			t.Errorf("secrets file lost %q — its secret is now unreferenced", name)
		}
	}
	if len(idx) != concurrentProfiles {
		t.Errorf("index has %d entries after %d concurrent writers, want %d",
			len(idx), concurrentProfiles, concurrentProfiles)
	}
}

// TestMutateEntry_ConcurrentSettersAllSurvive covers the other index
// writer: the `profile set-*` setters, which read the whole index to
// change one entry and so could erase every other entry's edit.
func TestMutateEntry_ConcurrentSettersAllSurvive(t *testing.T) {
	headlessConfigDir(t)

	for i := range concurrentProfiles {
		if _, err := Store(Credential{
			Name:    profileName(i),
			Type:    AuthBearer,
			Domains: []string{"api.example.com"},
		}, Secrets{Token: fmt.Sprintf("tok-%02d-not-a-real-token", i)}); err != nil {
			t.Fatalf("seed Store: %v", err)
		}
	}

	var wg sync.WaitGroup
	errs := make(chan error, concurrentProfiles)
	for i := range concurrentProfiles {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := SetHealth(profileName(i), fmt.Sprintf("https://api.example.com/health/%02d", i)); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("SetHealth: %v", err)
	}

	idx, err := readIndex()
	if err != nil {
		t.Fatalf("readIndex: %v", err)
	}
	for i := range concurrentProfiles {
		want := fmt.Sprintf("https://api.example.com/health/%02d", i)
		if got := idx[profileName(i)].Health; got != want {
			t.Errorf("%s health=%q, want %q — the setter's write was lost", profileName(i), got, want)
		}
	}
}

// TestRemove_ConcurrentRemovalsAllLand is the delete-side twin: every
// removal must survive, or a deleted profile reappears in the index
// pointing at a secret that was already revoked.
func TestRemove_ConcurrentRemovalsAllLand(t *testing.T) {
	headlessConfigDir(t)

	for i := range concurrentProfiles {
		if _, err := Store(Credential{
			Name:    profileName(i),
			Type:    AuthBearer,
			Domains: []string{"api.example.com"},
		}, Secrets{Token: fmt.Sprintf("tok-%02d-not-a-real-token", i)}); err != nil {
			t.Fatalf("seed Store: %v", err)
		}
	}

	var wg sync.WaitGroup
	errs := make(chan error, concurrentProfiles)
	for i := range concurrentProfiles {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Remove(profileName(i)); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("Remove: %v", err)
	}

	idx, err := readIndex()
	if err != nil {
		t.Fatalf("readIndex: %v", err)
	}
	if len(idx) != 0 {
		t.Errorf("index still holds %d entries after removing all %d: %v",
			len(idx), concurrentProfiles, keys(idx))
	}
	sec, err := readSecretsFile()
	if err != nil {
		t.Fatalf("readSecretsFile: %v", err)
	}
	if len(sec) != 0 {
		t.Errorf("secrets file still holds %d entries after removing all %d", len(sec), concurrentProfiles)
	}
}

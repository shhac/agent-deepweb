package config

import (
	"sync"
	"testing"
)

// TestUpdate_ConcurrentWritersDoNotLoseUpdates is the regression guard for
// the lost-update race that Read → mutate → Write used to carry: each
// caller built its write from a snapshot taken before the others landed,
// so all but the last increment were erased.
//
// Twenty writers each bump one counter by one. Anything short of twenty
// bumps surviving means an update was lost. Run with -race to also catch
// tearing on the cache pointer.
func TestUpdate_ConcurrentWritersDoNotLoseUpdates(t *testing.T) {
	const writers = 20
	s := NewStore(t.TempDir())

	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Update(func(c *Config) error {
				c.Defaults.TimeoutMS++
				return nil
			}); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("Update: %v", err)
	}

	s.ClearCache()
	want := DefaultTimeoutMS + writers
	if got := s.Read().Defaults.TimeoutMS; got != want {
		t.Errorf("timeout_ms=%d after %d concurrent increments, want %d — %d update(s) lost",
			got, writers, want, want-got)
	}
}

// TestUpdate_MutateSeesDiskNotCache — a stale cache inside the lock would
// reintroduce the same lost update by another route, so Update must read
// from disk even when Read has already populated the cache.
func TestUpdate_MutateSeesDiskNotCache(t *testing.T) {
	s := NewStore(t.TempDir())

	if err := s.Update(func(c *Config) error {
		c.Defaults.Profile = "from-disk"
		return nil
	}); err != nil {
		t.Fatalf("seed Update: %v", err)
	}
	// Populate the cache, then poison it with a value that was never written.
	s.Read().Defaults.Profile = "stale-cache-value"

	if err := s.Update(func(c *Config) error {
		if c.Defaults.Profile != "from-disk" {
			t.Errorf("mutate saw %q, want the on-disk %q", c.Defaults.Profile, "from-disk")
		}
		c.Defaults.UserAgent = "deepweb-test/1.0"
		return nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// And the cache must not survive the write.
	if got := s.Read().Defaults.Profile; got != "from-disk" {
		t.Errorf("Read after Update returned %q; cache was not invalidated", got)
	}
}

// TestUpdate_MutateErrorLeavesFileUntouched — an aborted mutation must not
// persist a partial config.
func TestUpdate_MutateErrorLeavesFileUntouched(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Update(func(c *Config) error {
		c.Defaults.Profile = "keep-me"
		return nil
	}); err != nil {
		t.Fatalf("seed Update: %v", err)
	}

	sentinel := errSentinel("nope")
	if err := s.Update(func(c *Config) error {
		c.Defaults.Profile = "should-not-persist"
		return sentinel
	}); err != sentinel {
		t.Fatalf("Update err=%v, want the mutate error back", err)
	}

	s.ClearCache()
	if got := s.Read().Defaults.Profile; got != "keep-me" {
		t.Errorf("profile=%q after a failed mutate, want %q", got, "keep-me")
	}
}

type errSentinel string

func (e errSentinel) Error() string { return string(e) }

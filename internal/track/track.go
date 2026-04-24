// Package track persists opt-in full-fidelity request/response records
// that `fetch`/`graphql`/`template run` produce when the `--track` flag is
// set. The audit log (internal/audit) records only lightweight metadata
// per request; tracked records additionally include the redacted
// request headers/body and response headers/body, so a human can
// inspect exactly what happened after the fact.
//
// Records are written to ~/.config/agent-deepweb/track/<id>.json, mode
// 0600. IDs are time-sortable (YYYYMMDDTHHMM-<4hex>) so a directory
// scan is cheap to filter by age.
//
// Each record stores its own ExpiresAt (= write-time + current
// track.ttl). Prune compares now against ExpiresAt, so changing the
// config later affects only new records — old ones keep their original
// lifetime. Default TTL is 168h (7 days), set via
// 'agent-deepweb config set track.ttl <duration>'.
package track

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shhac/agent-deepweb/internal/config"
)

// Record is a persisted full-fidelity view of one request/response.
// All header/body fields are already redacted — we don't re-redact
// on read. Sensitive cookies in response headers and auth headers
// (Authorization, Cookie, etc.) were masked at the api layer.
type Record struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"ts"`
	// ExpiresAt is set at write time (= ts + current config.track.ttl).
	// Prune compares now against this value rather than computing
	// ts + current-ttl, so changing the TTL later affects only new
	// records. A zero ExpiresAt (records from before this field
	// existed) is treated as "unbounded" — caller must delete manually.
	ExpiresAt  time.Time `json:"expires_at,omitempty"`
	Profile    string    `json:"profile,omitempty"` // profile name, "none" for --profile none, "" for ad-hoc
	Template   string    `json:"template,omitempty"`
	Request    Request   `json:"request"`
	Response   Response  `json:"response"`
	Outcome    string    `json:"outcome"`             // "ok" | "error"
	Error      string    `json:"error,omitempty"`     // error message when outcome=error
	FixableBy  string    `json:"fixable_by,omitempty"`
	DurationMS int64     `json:"duration_ms"`
}

// Request is the outbound side of a tracked record.
type Request struct {
	Method      string      `json:"method"`
	URL         string      `json:"url"`
	Headers     http.Header `json:"headers,omitempty"`
	Body        string      `json:"body,omitempty"`
	BodyBytes   int         `json:"body_bytes"`
	ContentType string      `json:"content_type,omitempty"`
}

// Response is the inbound side of a tracked record.
type Response struct {
	Status      int         `json:"status"`
	StatusText  string      `json:"status_text,omitempty"`
	Headers     http.Header `json:"headers,omitempty"`
	ContentType string      `json:"content_type,omitempty"`
	Body        string      `json:"body,omitempty"`
	Truncated   bool        `json:"truncated,omitempty"`
}

// Recorder is the narrow "mint an ID and persist a Record" interface
// the api package depends on. DefaultRecorder wires it to the package-
// level NewID + Write; tests pass a stub to capture records or to
// simulate persistence failures.
type Recorder interface {
	NewID() (string, error)
	Write(*Record) error
}

type defaultRecorder struct{}

func (defaultRecorder) NewID() (string, error) { return NewID() }

func (defaultRecorder) Write(r *Record) error { return Write(r) }

// DefaultRecorder is the process-wide track recorder. api.Do falls
// back to this when ClientOptions.Track is nil.
var DefaultRecorder Recorder = defaultRecorder{}

// NewID produces a time-sortable identifier suitable for filenames.
// Format: YYYYMMDDTHHMM-XXXX where XXXX is 4 random hex bytes. Short
// enough to paste, unique enough for any realistic single-human rate.
func NewID() (string, error) {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return time.Now().UTC().Format("20060102T1504") + "-" + hex.EncodeToString(b), nil
}

func trackDir() string {
	return filepath.Join(config.ConfigDir(), "track")
}

func recordPath(id string) string {
	return filepath.Join(trackDir(), id+".json")
}

// Write persists r to disk (mode 0600) and lazy-prunes expired records.
// If r.ExpiresAt is zero, it's set to r.Timestamp + current config
// track.ttl. The prune is best-effort: any scan/unlink error is
// silently ignored so a temporary filesystem hiccup doesn't fail the
// caller's request.
func Write(r *Record) error {
	if r.ExpiresAt.IsZero() {
		r.ExpiresAt = r.Timestamp.Add(config.Read().TrackTTL())
	}
	if err := os.MkdirAll(trackDir(), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(recordPath(r.ID), append(data, '\n'), 0o600); err != nil {
		return err
	}
	_, _ = PruneExpired()
	return nil
}

// Read loads the record for id, or returns os.ErrNotExist if it was
// pruned / never existed.
func Read(id string) (*Record, error) {
	data, err := os.ReadFile(recordPath(id))
	if err != nil {
		return nil, err
	}
	var r Record
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("track record %q is corrupt: %w", id, err)
	}
	return &r, nil
}

// PruneExpired removes every record whose stored ExpiresAt is in the
// past. The lazy-prune call site in Write uses this; the `audit prune`
// CLI verb calls this by default.
func PruneExpired() (int, error) {
	return pruneWhere(func(r *Record) bool {
		return !r.ExpiresAt.IsZero() && time.Now().After(r.ExpiresAt)
	})
}

// PruneOlderThan is the explicit-duration variant used by
// `audit prune --older-than <dur>`. Matches on Timestamp (not
// ExpiresAt) so a human can retro-actively delete "everything older
// than N" regardless of per-record TTL.
func PruneOlderThan(d time.Duration) (int, error) {
	cutoff := time.Now().Add(-d)
	return pruneWhere(func(r *Record) bool {
		return r.Timestamp.Before(cutoff)
	})
}

func pruneWhere(should func(*Record) bool) (int, error) {
	entries, err := os.ReadDir(trackDir())
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	removed := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		r, err := Read(id)
		if err != nil {
			continue
		}
		if should(r) {
			if err := os.Remove(recordPath(id)); err == nil {
				removed++
			}
		}
	}
	return removed, nil
}

// PruneByProfile removes every tracked record whose Profile field
// matches name. Called by `profile remove` so a deleted profile
// doesn't leave orphaned track data behind.
func PruneByProfile(name string) (int, error) {
	return pruneWhere(func(r *Record) bool { return r.Profile == name })
}


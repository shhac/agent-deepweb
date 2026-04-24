// Package track persists opt-in full-fidelity request/response records
// that `fetch`/`graphql`/`template run` produce when the `--track` flag is
// set. The audit log (internal/audit) records only lightweight metadata
// per request; tracked records additionally include the redacted
// request headers/body and response headers/body, so a human can
// inspect exactly what happened after the fact.
//
// Records are written to ~/.config/agent-deepweb/track/<id>.json, mode
// 0600. IDs are time-sortable (YYYYMMDDTHHMM-<4hex>) so a directory
// scan is cheap to filter by age. The default TTL is 7 days, overridable
// via AGENT_DEEPWEB_TRACK_TTL (Go duration string). Every Write does a
// lazy prune of expired records.
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

// DefaultTTL is how long a tracked record lives before being eligible
// for lazy pruning. Short enough that the directory stays tidy; long
// enough that "what did I do yesterday?" still works.
const DefaultTTL = 7 * 24 * time.Hour

// Record is a persisted full-fidelity view of one request/response.
// All header/body fields are already redacted — we don't re-redact
// on read. Sensitive cookies in response headers and auth headers
// (Authorization, Cookie, etc.) were masked at the api layer.
type Record struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"ts"`
	Profile   string    `json:"profile,omitempty"` // profile name, "none" for --profile none, "" for ad-hoc
	Template  string    `json:"template,omitempty"`
	Request   Request   `json:"request"`
	Response  Response  `json:"response"`
	Outcome   string    `json:"outcome"`             // "ok" | "error"
	Error     string    `json:"error,omitempty"`     // error message when outcome=error
	FixableBy string    `json:"fixable_by,omitempty"`
	DurationMS int64    `json:"duration_ms"`
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
// The prune is best-effort: any scan/unlink error is silently ignored
// so a temporary filesystem hiccup doesn't fail the caller's request.
func Write(r *Record) error {
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
	_, _ = PruneOlderThan(ttlFromEnv())
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

// PruneOlderThan removes every record whose Timestamp is older than
// now - d. Returns the count removed. Errors on individual entries are
// silently skipped so one bad file doesn't block the rest.
func PruneOlderThan(d time.Duration) (int, error) {
	entries, err := os.ReadDir(trackDir())
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	cutoff := time.Now().Add(-d)
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
		if r.Timestamp.Before(cutoff) {
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
		if r.Profile == name {
			if err := os.Remove(recordPath(id)); err == nil {
				removed++
			}
		}
	}
	return removed, nil
}

// ttlFromEnv reads AGENT_DEEPWEB_TRACK_TTL (a Go duration string like
// "168h" or "7d"-equivalent "168h") and returns the parsed duration, or
// DefaultTTL on missing/malformed input. Unparseable input is silently
// ignored rather than failing the caller's request.
func ttlFromEnv() time.Duration {
	v := os.Getenv("AGENT_DEEPWEB_TRACK_TTL")
	if v == "" {
		return DefaultTTL
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return DefaultTTL
	}
	return d
}

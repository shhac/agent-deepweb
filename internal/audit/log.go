// Package audit writes append-only JSON-lines entries for every request
// agent-deepweb makes. The human can inspect the log to see exactly what
// the LLM did. Entries never include secret values or response bodies —
// only the request shape, credential name, status, bytes, and duration.
package audit

import (
	"bufio"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shhac/agent-deepweb/internal/config"
)

// Entry is a single audited request.
type Entry struct {
	Timestamp  time.Time `json:"ts"`
	Method     string    `json:"method"`
	Scheme     string    `json:"scheme,omitempty"`
	Host       string    `json:"host"`
	Path       string    `json:"path,omitempty"`
	Credential string    `json:"credential,omitempty"`
	Template   string    `json:"template,omitempty"` // set when dispatched via `tpl run`
	Status     int       `json:"status,omitempty"`
	Bytes      int       `json:"bytes,omitempty"`
	DurationMS int64     `json:"duration_ms"`
	Outcome    string    `json:"outcome"` // "ok" | "error"
	Error      string    `json:"error,omitempty"`
	FixableBy  string    `json:"fixable_by,omitempty"`
}

// Enabled reports whether auditing is on. Controlled by AGENT_DEEPWEB_AUDIT:
// "", "on", "true", "1" → on (the default). "off", "false", "0" → off.
func Enabled() bool {
	switch strings.ToLower(os.Getenv("AGENT_DEEPWEB_AUDIT")) {
	case "off", "false", "0", "no":
		return false
	default:
		return true
	}
}

func logPath() string {
	return filepath.Join(config.ConfigDir(), "audit.log")
}

// Append writes one entry to the audit log. Failures are silently ignored
// — auditing must never block a request.
func Append(e Entry) {
	if !Enabled() {
		return
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	dir := filepath.Dir(logPath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(logPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close() //nolint:errcheck
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	_, _ = f.Write(append(data, '\n'))
}

// FromURL populates Scheme/Host/Path from a raw URL. Unparseable URLs
// leave the fields as best-effort strings.
func FromURL(raw string) (scheme, host, path string) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", ""
	}
	return u.Scheme, u.Host, u.Path
}

// Tail returns the last n entries (oldest first).
func Tail(n int) ([]Entry, error) {
	if n <= 0 {
		n = 50
	}
	f, err := os.Open(logPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close() //nolint:errcheck

	// Read all lines; audit logs are typically small. If this ever becomes
	// an issue, switch to reverse-scan from file end.
	all, err := readAllLines(f)
	if err != nil {
		return nil, err
	}
	start := 0
	if len(all) > n {
		start = len(all) - n
	}
	entries := make([]Entry, 0, len(all)-start)
	for _, line := range all[start:] {
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func readAllLines(r io.Reader) ([]string, error) {
	var lines []string
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), 8*1024*1024) // allow long lines
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines, s.Err()
}

// Summary groups recent entries for `audit summary`.
type Summary struct {
	Total     int            `json:"total"`
	ByHost    map[string]int `json:"by_host"`
	ByCred    map[string]int `json:"by_credential"`
	ByOutcome map[string]int `json:"by_outcome"`
	Since     time.Time      `json:"since,omitempty"`
	LatestTS  time.Time      `json:"latest,omitempty"`
}

func Summarize(entries []Entry) Summary {
	s := Summary{
		ByHost:    map[string]int{},
		ByCred:    map[string]int{},
		ByOutcome: map[string]int{},
	}
	for i, e := range entries {
		s.Total++
		s.ByHost[e.Host]++
		if e.Credential != "" {
			s.ByCred[e.Credential]++
		} else {
			s.ByCred["(none)"]++
		}
		s.ByOutcome[e.Outcome]++
		if i == 0 {
			s.Since = e.Timestamp
		}
		if e.Timestamp.After(s.LatestTS) {
			s.LatestTS = e.Timestamp
		}
	}
	return s
}

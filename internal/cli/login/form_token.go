package login

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/shhac/agent-deepweb/internal/credential"
)

// readCapped reads up to max bytes from r, returning a possibly-truncated
// slice. Errors (short of max bytes) are treated as end-of-input rather
// than surfaced — login is best-effort about reading the body.
func readCapped(r io.Reader, max int64) ([]byte, error) {
	limited := io.LimitReader(r, max)
	return io.ReadAll(limited)
}

// extractJSONToken walks a dot-separated path through a JSON body and
// returns the string at that location. Supports numeric indexes in arrays
// ("a.0.b"). Missing keys return "" (not an error). Non-JSON returns an
// error so the human can fix the token-path / login-url.
func extractJSONToken(body []byte, pathDot string) (string, error) {
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return "", fmt.Errorf("login response is not JSON: %w", err)
	}
	for _, seg := range strings.Split(pathDot, ".") {
		switch val := decoded.(type) {
		case map[string]any:
			decoded = val[seg]
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(val) {
				return "", fmt.Errorf("index %q out of range", seg)
			}
			decoded = val[idx]
		default:
			return "", nil
		}
	}
	switch v := decoded.(type) {
	case string:
		return v, nil
	case float64:
		return fmt.Sprint(v), nil
	case nil:
		return "", nil
	default:
		return "", fmt.Errorf("value at %q is %T, not string", pathDot, decoded)
	}
}

// computeExpiry picks the tightest expiry bound from (ttlStr, the latest
// per-cookie expiry, a 24h fallback). A session never outlives its
// cookies or the TTL the human set.
//
// The 24h fallback IS the initial value of `earliest`, so the cookie
// loop just runs `min` against it — no separate "have we picked one
// yet" flag needed.
func computeExpiry(s *credential.Jar, ttlStr string) time.Time {
	now := time.Now().UTC()
	earliest := now.Add(24 * time.Hour)
	if ttlStr != "" {
		if d, err := time.ParseDuration(ttlStr); err == nil {
			earliest = now.Add(d)
		}
	}
	for _, c := range s.Cookies {
		if !c.Expires.IsZero() && c.Expires.Before(earliest) {
			earliest = c.Expires
		}
	}
	return earliest
}

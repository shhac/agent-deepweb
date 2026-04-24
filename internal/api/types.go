package api

import (
	"io"
	"net/http"
	"time"

	"github.com/shhac/agent-deepweb/internal/config"
	"github.com/shhac/agent-deepweb/internal/credential"
)

// Version is the agent-deepweb build version used in the default User-Agent
// header ("agent-deepweb/<version>"). Set by cmd/agent-deepweb from the
// -ldflags variable.
var Version = "dev"

// Request is a high-level HTTP request description, translated into an
// *http.Request inside Do. The Resolved credential (if any) is applied
// after user headers so user-supplied headers cannot overwrite the auth.
type Request struct {
	Method  string
	URL     string
	Headers map[string]string
	Query   map[string][]string
	Body    io.Reader
	Auth    *credential.Resolved
	// JarPath, if set, overrides the profile's encrypted default jar with
	// a plaintext bring-your-own jar at this path. Used by `--cookiejar
	// <path>`, including the `--profile none --cookiejar <path>` LLM-
	// authored-flow case.
	JarPath string
	// UserAgent, if non-empty, overrides everything (credential's UA, env,
	// and the default). Empty = fall through the precedence chain.
	UserAgent string
	// TemplateName is set by `template run` so the audit log can record
	// which template produced the request. Empty for ad-hoc fetches.
	TemplateName string
	// Track, when true, tells Do to persist a full-fidelity record of
	// the request+response (via internal/track) and to stamp an AuditID
	// on the response so the caller can surface it in the envelope. The
	// CLI layer wires this up via the `--track` flag.
	Track bool
}

// Response is the redacted, size-capped response surfaced to the caller.
type Response struct {
	Status      int
	StatusText  string
	Headers     http.Header
	ContentType string
	Body        []byte
	Truncated   bool
	// NewCookies: cookies captured from Set-Cookie on this response that
	// were *not* already in the profile's jar (post-harvest diff). Visible
	// ones have values; sensitive ones are redacted. Empty when no profile
	// is attached.
	NewCookies []credential.CookieView `json:"new_cookies,omitempty"`
	// Sent captures what went out on the wire, for envelope display and
	// track-record persistence. Headers and body are redacted the same
	// way the response side is (auth headers masked, body-field secrets
	// masked, literal-value echoes masked).
	Sent SentRequest
	// AuditID is set when Request.Track was true. Empty otherwise.
	// Callers include it in the response envelope so humans can look
	// up the full record via `audit show <id>`.
	AuditID string
}

// SentRequest is the post-redaction view of what was sent to the server.
// Populated by Do so callers can display request info symmetrically with
// response info. Body is redacted; BodyBytes is the raw (pre-redaction)
// size so envelopes can show it without dumping possibly-binary payloads.
type SentRequest struct {
	Method    string
	URL       string
	Headers   http.Header
	Body      []byte
	BodyBytes int
	RequestCT string // Content-Type header of the request
}

// ClientOptions carry request-level defaults that would otherwise pile up
// as parameters to Do. Redaction is always on — there's no "no-redact"
// escape hatch in v2; if a human really wants raw output they can use curl.
type ClientOptions struct {
	Timeout         time.Duration
	MaxBytes        int64
	FollowRedirects bool
}

func (c *ClientOptions) applyDefaults() {
	if c.Timeout == 0 {
		c.Timeout = time.Duration(config.DefaultTimeoutMS) * time.Millisecond
	}
	if c.MaxBytes == 0 {
		c.MaxBytes = config.DefaultMaxBytes
	}
}

// defaultStr returns d when s is empty. Tiny helper used by the
// record/audit builders.
func defaultStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

package api

import (
	"time"

	"github.com/shhac/agent-deepweb/internal/audit"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/track"
)

// writeTrackRecord persists a full-fidelity Request/Response record via
// the provided track.Recorder when --track was set. Returns the audit
// ID for the caller to surface, or "" if persistence failed (best-
// effort; a track failure must never fail the underlying request).
//
// outErr (when non-nil) populates the record's Error/FixableBy so
// `audit show <id>` carries the full classification, not just
// "outcome:error".
func writeTrackRecord(tracker track.Recorder, req Request, resp *Response, outErr error, started time.Time) string {
	id, err := tracker.NewID()
	if err != nil {
		return ""
	}
	rec := buildTrackRecord(id, req, resp, outErr, started)
	if err := tracker.Write(rec); err != nil {
		return ""
	}
	return id
}

// buildTrackRecord is the pure "translate a completed Do into a track
// record" mapping. Extracted so track-record shape is unit-testable
// without a tempdir + FS write.
//
// Outcome is "error" when either outErr is non-nil OR the HTTP status
// is >= 400 (defensive: classifyHTTP shouldn't miss any 4xx/5xx, but
// belt-and-braces). Error/FixableBy are populated only when outErr is
// a classified APIError, mirroring buildAuditEntry.
func buildTrackRecord(id string, req Request, resp *Response, outErr error, started time.Time) *track.Record {
	profile := "none"
	if req.Auth != nil {
		profile = req.Auth.Name
	}
	outcome := "ok"
	if outErr != nil || resp.Status >= 400 {
		outcome = "error"
	}
	rec := &track.Record{
		ID:        id,
		Timestamp: started.UTC(),
		Profile:   profile,
		Template:  req.TemplateName,
		Request: track.Request{
			Method:      resp.Sent.Method,
			URL:         resp.Sent.URL,
			Headers:     resp.Sent.Headers,
			Body:        string(resp.Sent.Body),
			BodyBytes:   resp.Sent.BodyBytes,
			ContentType: resp.Sent.RequestCT,
		},
		Response: track.Response{
			Status:      resp.Status,
			StatusText:  resp.StatusText,
			Headers:     resp.Headers,
			ContentType: resp.ContentType,
			Body:        string(resp.Body),
			Truncated:   resp.Truncated,
		},
		Outcome:    outcome,
		DurationMS: time.Since(started).Milliseconds(),
	}
	if outErr != nil {
		rec.Error = outErr.Error()
		var ae *agenterrors.APIError
		if agenterrors.As(outErr, &ae) {
			rec.FixableBy = string(ae.FixableBy)
		}
	}
	return rec
}

// buildAuditEntry converts a Do invocation's inputs + outcome into an
// audit.Entry. Pure function — all state comes from its args, making
// the deferred call site in Do trivial to read.
func buildAuditEntry(req Request, started time.Time, resp *Response, outErr error) audit.Entry {
	scheme, host, path := audit.FromURL(req.URL)
	e := audit.Entry{
		Method:     defaultStr(req.Method, "GET"),
		Scheme:     scheme,
		Host:       host,
		Path:       path,
		Jar:        req.JarPath,
		Template:   req.TemplateName,
		DurationMS: time.Since(started).Milliseconds(),
	}
	if req.Auth != nil {
		e.Profile = req.Auth.Name
	} else {
		// req.Auth == nil means explicit anonymous (`--profile none`) at
		// this layer — Resolve already errored out for the no-match case.
		e.Profile = "none"
	}
	if resp != nil {
		e.Status = resp.Status
		e.Bytes = len(resp.Body)
	}
	if outErr == nil {
		e.Outcome = "ok"
	} else {
		e.Outcome = "error"
		e.Error = outErr.Error()
		var ae *agenterrors.APIError
		if agenterrors.As(outErr, &ae) {
			e.FixableBy = string(ae.FixableBy)
		}
	}
	return e
}

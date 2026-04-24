package api

import (
	"time"

	"github.com/shhac/agent-deepweb/internal/audit"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/track"
)

// writeTrackRecord persists a full-fidelity Request/Response record via
// internal/track when --track was set. Returns the audit ID for the
// caller to surface, or "" if persistence failed (best-effort; a track
// failure must never fail the underlying request).
func writeTrackRecord(req Request, resp *Response, started time.Time) string {
	id, err := track.NewID()
	if err != nil {
		return ""
	}
	rec := buildTrackRecord(id, req, resp, started)
	if err := track.Write(rec); err != nil {
		return ""
	}
	return id
}

// buildTrackRecord is the pure "translate a completed Do into a track
// record" mapping. Extracted so track-record shape is unit-testable
// without a tempdir + FS write.
func buildTrackRecord(id string, req Request, resp *Response, started time.Time) *track.Record {
	profile := "none"
	if req.Auth != nil {
		profile = req.Auth.Name
	}
	outcome := "ok"
	if resp.Status >= 400 {
		outcome = "error"
	}
	return &track.Record{
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

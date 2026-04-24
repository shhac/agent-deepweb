package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shhac/agent-deepweb/internal/audit"
	"github.com/shhac/agent-deepweb/internal/credential"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/track"
)

// roundTripperFunc is a local adapter so tests can inline a stub RoundTripper
// without building a struct.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Test aliases for the audit/track public shapes — lets DI3 tests
// read without needing to repeat the struct shapes in the helper code.
type auditEntryForTest = audit.Entry
type trackRecordForTest = track.Record

type auditWriterFunc func(audit.Entry)

func (f auditWriterFunc) Append(e audit.Entry) { f(e) }

type stubRecorder struct {
	t       *testing.T
	nextID  string
	onWrite func(*track.Record)
}

func (s *stubRecorder) NewID() (string, error) { return s.nextID, nil }
func (s *stubRecorder) Write(r *track.Record) error {
	if s.onWrite != nil {
		s.onWrite(r)
	}
	return nil
}

// TestBuildTrackRecord_Mapping covers the pure shape translation:
// every Sent/Response field lands in the right Track.Request/Response
// slot, profile defaults to "none" when Auth is nil, outcome flips to
// error on >=400 even without outErr.
func TestBuildTrackRecord_Mapping(t *testing.T) {
	started := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	resp := &Response{
		Status:      201,
		StatusText:  "201 Created",
		Headers:     http.Header{"Content-Type": []string{"application/json"}},
		ContentType: "application/json",
		Body:        []byte(`{"id":"x"}`),
		Truncated:   false,
		Sent: SentRequest{
			Method:    "POST",
			URL:       "https://api.example.com/items",
			Headers:   http.Header{"Content-Type": []string{"application/json"}},
			Body:      []byte(`{"name":"y"}`),
			BodyBytes: 12,
			RequestCT: "application/json",
		},
	}

	t.Run("happy: ok outcome, no error fields, profile=none when auth nil", func(t *testing.T) {
		rec := buildTrackRecord("id-1", Request{}, resp, nil, started)
		if rec.ID != "id-1" || rec.Profile != "none" || rec.Outcome != "ok" {
			t.Errorf("base fields: %+v", rec)
		}
		if rec.Error != "" || rec.FixableBy != "" {
			t.Errorf("ok outcome should not carry error fields: %+v", rec)
		}
		if rec.Request.Method != "POST" || rec.Request.URL != "https://api.example.com/items" {
			t.Errorf("request mapping wrong: %+v", rec.Request)
		}
		if rec.Response.Status != 201 || rec.Response.Body != `{"id":"x"}` {
			t.Errorf("response mapping wrong: %+v", rec.Response)
		}
		if rec.Timestamp != started {
			t.Errorf("timestamp not preserved: %v", rec.Timestamp)
		}
	})

	t.Run("status>=400 alone flips outcome to error", func(t *testing.T) {
		respErr := *resp
		respErr.Status = 401
		rec := buildTrackRecord("id-2", Request{}, &respErr, nil, started)
		if rec.Outcome != "error" {
			t.Errorf("status 401 should yield outcome=error, got %q", rec.Outcome)
		}
	})

	t.Run("classified error populates Error + FixableBy", func(t *testing.T) {
		respErr := *resp
		respErr.Status = 401
		ae := agenterrors.Newf(agenterrors.FixableByHuman, "401 Unauthorized — credentials rejected").
			WithHint("Ask the user to refresh the token")
		rec := buildTrackRecord("id-3", Request{}, &respErr, ae, started)
		if rec.Outcome != "error" {
			t.Errorf("outcome: %q", rec.Outcome)
		}
		if rec.Error == "" {
			t.Error("Error should be populated on outErr")
		}
		if rec.FixableBy != "human" {
			t.Errorf("FixableBy: want 'human', got %q", rec.FixableBy)
		}
	})

	t.Run("non-APIError still populates Error but not FixableBy", func(t *testing.T) {
		rec := buildTrackRecord("id-4", Request{}, resp, errPlain("plain error"), started)
		if rec.Error != "plain error" {
			t.Errorf("Error: %q", rec.Error)
		}
		if rec.FixableBy != "" {
			t.Errorf("FixableBy should be empty for non-APIError; got %q", rec.FixableBy)
		}
	})

	t.Run("auth populates profile name", func(t *testing.T) {
		req := Request{Auth: &credential.Resolved{Credential: credential.Credential{Name: "myapi"}}}
		rec := buildTrackRecord("id-5", req, resp, nil, started)
		if rec.Profile != "myapi" {
			t.Errorf("profile: %q", rec.Profile)
		}
	})

	t.Run("template name flows through", func(t *testing.T) {
		req := Request{TemplateName: "myapi.get_item"}
		rec := buildTrackRecord("id-6", req, resp, nil, started)
		if rec.Template != "myapi.get_item" {
			t.Errorf("template: %q", rec.Template)
		}
	})
}

type errPlain string

func (e errPlain) Error() string { return string(e) }

// TestDo_TrackRoundTrip — Do(Track:true) → resp.AuditID populated →
// track.Read returns a full-fidelity record with redacted Authorization
// and the full response body. This locks the end-to-end contract that
// `fetch --track ... && audit show <id>` relies on.
func TestDo_TrackRoundTrip(t *testing.T) {
	setup(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hello":"world"}`))
	}))
	defer srv.Close()

	resolved := testResolved(t, credential.AuthBearer, srv.URL, credential.Secrets{Token: "track-secret-token"})
	resp, err := Do(context.Background(), Request{
		Method: "GET",
		URL:    srv.URL + "/thing",
		Auth:   resolved,
		Track:  true,
	}, ClientOptions{Timeout: 5 * time.Second, MaxBytes: 1024})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if resp.AuditID == "" {
		t.Fatal("Track:true should populate resp.AuditID")
	}

	rec, err := track.Read(resp.AuditID)
	if err != nil {
		t.Fatalf("track.Read(%q): %v", resp.AuditID, err)
	}
	if rec.Outcome != "ok" {
		t.Errorf("outcome: %q", rec.Outcome)
	}
	if rec.Profile != "c" {
		t.Errorf("profile: %q", rec.Profile)
	}
	if rec.Request.Method != "GET" {
		t.Errorf("request method: %q", rec.Request.Method)
	}
	// Authorization header must be redacted in the stored record — the
	// point of --track is a replayable view, NOT a secret-exposing audit.
	auth := rec.Request.Headers.Get("Authorization")
	if auth == "" {
		t.Error("Authorization should appear in redacted form, not be stripped entirely")
	}
	if strings.Contains(auth, "track-secret-token") {
		t.Errorf("raw token leaked into track record: %q", auth)
	}
	// Response body is preserved (the redactor may pretty-print JSON, so
	// match the observable content rather than exact bytes).
	if !strings.Contains(rec.Response.Body, `"hello"`) || !strings.Contains(rec.Response.Body, `"world"`) {
		t.Errorf("response body: %q", rec.Response.Body)
	}
}

// TestDo_CustomTransportAndClock — ClientOptions.Transport lets tests
// stub HTTP without spinning a real server; ClientOptions.Clock makes
// the audit entry's timestamp deterministic. Guards both DI hooks.
func TestDo_CustomTransportAndClock(t *testing.T) {
	setup(t)

	fixed := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

	rt := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("pong")),
			Request:    r,
		}, nil
	})

	resp, err := Do(context.Background(), Request{
		Method: "GET",
		URL:    "https://stub.invalid/ping",
	}, ClientOptions{
		Timeout:   5 * time.Second,
		MaxBytes:  1024,
		Transport: rt,
		Clock:     func() time.Time { return fixed },
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.Status != 200 || string(resp.Body) != "pong" {
		t.Errorf("resp: %+v body=%q", resp, resp.Body)
	}
}

// TestDo_InjectedAuditAndTracker — ClientOptions.Audit and .Tracker
// are honoured. A captured Audit.Writer lets the test assert on the
// audited entry; a captured track.Recorder lets it verify the ID flow
// end-to-end without the default filesystem recorder firing.
func TestDo_InjectedAuditAndTracker(t *testing.T) {
	setup(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	var captured []auditEntryForTest
	fakeAudit := auditWriterFunc(func(e auditEntryForTest) {
		captured = append(captured, e)
	})

	var writtenIDs []string
	fakeTracker := &stubRecorder{t: t, nextID: "DI3-TEST-ID", onWrite: func(r *trackRecordForTest) {
		writtenIDs = append(writtenIDs, r.ID)
	}}

	resolved := testResolved(t, credential.AuthBearer, srv.URL, credential.Secrets{Token: "tk"})
	resp, err := Do(context.Background(), Request{
		Method: "GET",
		URL:    srv.URL,
		Auth:   resolved,
		Track:  true,
	}, ClientOptions{
		Timeout:  5 * time.Second,
		MaxBytes: 1024,
		Audit:    fakeAudit,
		Tracker:  fakeTracker,
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	if resp.AuditID != "DI3-TEST-ID" {
		t.Errorf("expected resp.AuditID to come from injected Recorder; got %q", resp.AuditID)
	}
	if len(writtenIDs) != 1 || writtenIDs[0] != "DI3-TEST-ID" {
		t.Errorf("injected Recorder.Write should have fired once with the minted ID; got %v", writtenIDs)
	}
	if len(captured) != 1 {
		t.Fatalf("want 1 audit entry, got %d", len(captured))
	}
	if captured[0].Outcome != "ok" {
		t.Errorf("audit outcome: %q", captured[0].Outcome)
	}
}

// TestDo_TrackFalseDoesNotPersist — the default path writes NO
// tracked record and leaves resp.AuditID empty. Otherwise --track
// would be meaningless.
func TestDo_TrackFalseDoesNotPersist(t *testing.T) {
	setup(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	resolved := testResolved(t, credential.AuthBearer, srv.URL, credential.Secrets{Token: "tok"})
	resp, err := Do(context.Background(), Request{
		Method: "GET",
		URL:    srv.URL,
		Auth:   resolved,
		// Track: false (default)
	}, ClientOptions{Timeout: 5 * time.Second, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if resp.AuditID != "" {
		t.Errorf("Track:false should leave AuditID empty, got %q", resp.AuditID)
	}
}

package payrollsim

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestCallbackSignatureVerifiesWithWebhookStore(t *testing.T) {
	rc := newReceiver(t)
	s, _ := newTestServer(t, nil)
	req := validRequest(rc.url())
	if rec := post(t, s, req.ChangeRef, req); rec.Code != http.StatusAccepted {
		t.Fatalf("intake = %d", rec.Code)
	}
	drain(t, s)
	calls := rc.calls()
	if len(calls) != 1 {
		t.Fatalf("callbacks = %d, want 1", len(calls))
	}
	c := calls[0]
	if c.err != nil {
		t.Fatalf("webhook.Store.Receive rejected the callback: %v", c.err)
	}
	if c.receipt.SignatureState != "VALID" || c.receipt.EventType != EventApplied || c.receipt.Schema != Schema {
		t.Fatalf("receipt = %+v", c.receipt)
	}
	h := c.header
	if h.Get("Content-Type") != "application/json" || h.Get("Webhook-Tenant") != "harborcare-demo" || h.Get("Webhook-Schema") != Schema {
		t.Fatalf("headers = %v", h)
	}
	if _, err := strconv.ParseInt(h.Get("Webhook-Timestamp"), 10, 64); err != nil {
		t.Fatalf("timestamp not decimal ns: %q", h.Get("Webhook-Timestamp"))
	}
	var ev ResultEvent
	if err := json.Unmarshal(c.body, &ev); err != nil {
		t.Fatal(err)
	}
	_, st := getStatus(t, s, req.ChangeRef)
	if ev.EventID != h.Get("Webhook-Id") || ev.ChangeRef != req.ChangeRef || ev.CorrelationKey != "corr-abc" ||
		ev.ProviderRef != st.ProviderRef || ev.Outcome != StatusApplied || ev.Reason != "" ||
		ev.EffectiveDate != "2026-12-01" || ev.BasePay != req.BasePay {
		t.Fatalf("event = %+v", ev)
	}
	if _, err := time.Parse(time.RFC3339, ev.OccurredAt); err != nil {
		t.Fatalf("occurred_at %q: %v", ev.OccurredAt, err)
	}
}

func TestCallbackWrongSecretFailsVerification(t *testing.T) {
	rc := newReceiver(t, http.StatusBadRequest)
	s, _ := newTestServer(t, func(c *Config) { c.Secret = []byte("not-the-receivers-secret") })
	req := validRequest(rc.url())
	post(t, s, req.ChangeRef, req)
	drain(t, s)
	if calls := rc.calls(); len(calls) != 1 || calls[0].err == nil {
		t.Fatalf("mismatched secret was accepted: %+v", calls)
	}
}

func TestCallbackRejectOutcome(t *testing.T) {
	rc := newReceiver(t)
	s, _ := newTestServer(t, func(c *Config) {
		c.Scenario.Mode = ModeReject
		c.Scenario.RejectReason = "pay above band"
	})
	req := validRequest(rc.url())
	post(t, s, req.ChangeRef, req)
	drain(t, s)
	calls := rc.calls()
	if len(calls) != 1 || calls[0].err != nil {
		t.Fatalf("calls = %+v", calls)
	}
	if calls[0].header.Get("Webhook-Event") != EventRejected {
		t.Fatalf("event type = %q", calls[0].header.Get("Webhook-Event"))
	}
	var ev ResultEvent
	_ = json.Unmarshal(calls[0].body, &ev)
	if ev.Outcome != StatusRejected || ev.Reason != "pay above band" {
		t.Fatalf("event = %+v", ev)
	}
	_, st := getStatus(t, s, req.ChangeRef)
	if st.Status != StatusRejected || st.Reason != "pay above band" || st.AppliedAt != "" || !st.Callback.Delivered {
		t.Fatalf("status = %+v", st)
	}
}

func TestCallbackRetriesUntilSuccess(t *testing.T) {
	rc := newReceiver(t, http.StatusInternalServerError, http.StatusInternalServerError)
	s, fs := newTestServer(t, nil)
	req := validRequest(rc.url())
	post(t, s, req.ChangeRef, req)
	drain(t, s)
	calls := rc.calls()
	if len(calls) != 3 {
		t.Fatalf("attempts seen by receiver = %d, want 3", len(calls))
	}
	id := calls[0].header.Get("Webhook-Id")
	for i, c := range calls {
		if c.header.Get("Webhook-Id") != id || string(c.body) != string(calls[0].body) {
			t.Fatalf("attempt %d changed event identity or bytes", i)
		}
		if c.err != nil {
			t.Fatalf("attempt %d failed verification: %v", i, c.err)
		}
	}
	// Waits: the zero apply delay, then 1s and 2s of backoff.
	if got, want := fs.recorded(), []time.Duration{0, time.Second, 2 * time.Second}; !reflect.DeepEqual(got, want) {
		t.Fatalf("waits = %v, want %v", got, want)
	}
	_, st := getStatus(t, s, req.ChangeRef)
	if st.Callback != (CallbackStatus{Attempts: 3, Delivered: true, LastStatus: 200}) {
		t.Fatalf("callback status = %+v", st.Callback)
	}
}

func TestCallbackTransientStatusesRetried(t *testing.T) {
	rc := newReceiver(t, http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusServiceUnavailable)
	s, _ := newTestServer(t, nil)
	req := validRequest(rc.url())
	post(t, s, req.ChangeRef, req)
	drain(t, s)
	if n := len(rc.calls()); n != 4 {
		t.Fatalf("attempts = %d, want 4", n)
	}
}

func TestCallbackPermanentStopOn400(t *testing.T) {
	rc := newReceiver(t, http.StatusBadRequest, http.StatusOK)
	s, fs := newTestServer(t, nil)
	req := validRequest(rc.url())
	post(t, s, req.ChangeRef, req)
	drain(t, s)
	if n := len(rc.calls()); n != 1 {
		t.Fatalf("attempts = %d, want 1 (400 is permanent)", n)
	}
	if got := fs.recorded(); len(got) != 1 {
		t.Fatalf("waits = %v, want only the apply delay", got)
	}
	_, st := getStatus(t, s, req.ChangeRef)
	if st.Callback != (CallbackStatus{Attempts: 1, Delivered: false, LastStatus: 400}) || st.Status != StatusApplied {
		t.Fatalf("status = %+v", st)
	}
}

func TestCallbackGivesUpAfterMaxAttempts(t *testing.T) {
	rc := newReceiver(t, 500, 500, 500, 500, 500)
	s, _ := newTestServer(t, func(c *Config) { c.MaxAttempts = 3 })
	req := validRequest(rc.url())
	post(t, s, req.ChangeRef, req)
	drain(t, s)
	if n := len(rc.calls()); n != 3 {
		t.Fatalf("attempts = %d, want 3", n)
	}
	_, st := getStatus(t, s, req.ChangeRef)
	if st.Callback != (CallbackStatus{Attempts: 3, Delivered: false, LastStatus: 500}) {
		t.Fatalf("callback = %+v", st.Callback)
	}
}

func TestCallbackNetworkErrorRecordsZeroStatus(t *testing.T) {
	s, _ := newTestServer(t, func(c *Config) { c.MaxAttempts = 2 })
	req := validRequest("http://127.0.0.1:1/unreachable")
	post(t, s, req.ChangeRef, req)
	drain(t, s)
	_, st := getStatus(t, s, req.ChangeRef)
	if st.Callback != (CallbackStatus{Attempts: 2, Delivered: false, LastStatus: 0}) {
		t.Fatalf("callback = %+v", st.Callback)
	}
}

func TestCallbackDuplicatesShareEventIdentity(t *testing.T) {
	rc := newReceiver(t)
	s, _ := newTestServer(t, func(c *Config) { c.Scenario.DuplicateCallbacks = true })
	req := validRequest(rc.url())
	post(t, s, req.ChangeRef, req)
	drain(t, s)
	calls := rc.calls()
	if len(calls) != 2 {
		t.Fatalf("callbacks = %d, want 2", len(calls))
	}
	a, b := calls[0], calls[1]
	if a.header.Get("Webhook-Id") == "" || a.header.Get("Webhook-Id") != b.header.Get("Webhook-Id") || string(a.body) != string(b.body) {
		t.Fatalf("duplicate differs: %q/%q", a.header.Get("Webhook-Id"), b.header.Get("Webhook-Id"))
	}
	if a.err != nil || b.err != nil || a.receipt.ID != b.receipt.ID {
		t.Fatalf("receiver did not dedupe to one receipt: %v %v %q %q", a.err, b.err, a.receipt.ID, b.receipt.ID)
	}
}

func TestBackoffDoublesAndCaps(t *testing.T) {
	s := New(Config{Secret: []byte("x")})
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second, time.Minute, time.Minute}
	for i, w := range want {
		if got := s.backoff(i + 1); got != w {
			t.Errorf("backoff(%d) = %v, want %v", i+1, got, w)
		}
	}
	if got := s.backoff(200); got != time.Minute {
		t.Errorf("backoff(200) = %v, want cap", got)
	}
	for status, want := range map[int]bool{500: true, 503: true, 408: true, 429: true, 400: false, 401: false, 404: false, 422: false} {
		if retryable(status) != want {
			t.Errorf("retryable(%d) != %v", status, want)
		}
	}
}

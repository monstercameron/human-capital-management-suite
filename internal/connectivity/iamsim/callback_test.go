package iamsim

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/webhook"
)

func submit(t *testing.T, h *harness, callback string) AccessChangeRequest {
	t.Helper()
	tok := issueToken(t, h.s, "")
	req := validRequest(callback)
	if rec := postChange(t, h.s, tok, req.ChangeRef, req); rec.Code != http.StatusAccepted {
		t.Fatalf("intake = %d %s", rec.Code, rec.Body)
	}
	return req
}

func statusOf(t *testing.T, h *harness, ref string) ChangeStatus {
	t.Helper()
	tok := issueToken(t, h.s, "access.read")
	code, st := getStatus(t, h.s, tok, ref)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	return st
}

func TestCallbackSignatureVerifiesWithWebhookStore(t *testing.T) {
	rc := newReceiver(t)
	h := newHarness(t, nil)
	req := submit(t, h, rc.url())
	drain(t, h.s)
	calls := rc.calls()
	if len(calls) != 1 {
		t.Fatalf("callbacks = %d, want 1", len(calls))
	}
	c := calls[0]
	if c.err != nil {
		t.Fatalf("webhook.Store.Receive rejected the callback: %v", c.err)
	}
	if c.receipt.SignatureState != "VALID" || c.receipt.EventType != EventGranted || c.receipt.Schema != Schema {
		t.Fatalf("receipt = %+v", c.receipt)
	}
	hd := c.header
	if hd.Get("Content-Type") != "application/json" || hd.Get("Webhook-Tenant") != testTenant || hd.Get("Webhook-Schema") != "iamsim.access_result/v1" || hd.Get("Webhook-Event") != "access.change.granted" {
		t.Fatalf("headers = %v", hd)
	}
	ts, err := strconv.ParseInt(hd.Get("Webhook-Timestamp"), 10, 64)
	if err != nil {
		t.Fatalf("timestamp not decimal ns: %q", hd.Get("Webhook-Timestamp"))
	}
	// Recompute the signature independently to pin the exact scheme.
	want := webhook.Sign([]byte(testWebhookSecret), webhook.Request{EventID: hd.Get("Webhook-Id"), EventType: EventGranted, Schema: Schema, Timestamp: time.Unix(0, ts), Payload: c.body})
	if hd.Get("Webhook-Signature") != want {
		t.Fatalf("signature = %q, want %q", hd.Get("Webhook-Signature"), want)
	}
	var ev ResultEvent
	if err := json.Unmarshal(c.body, &ev); err != nil {
		t.Fatal(err)
	}
	st := statusOf(t, h, req.ChangeRef)
	if ev.EventID != hd.Get("Webhook-Id") || ev.ChangeRef != req.ChangeRef || ev.CorrelationKey != "corr-abc" ||
		ev.ProviderRef != st.ProviderRef || ev.Outcome != StatusGranted || ev.Reason != "" ||
		ev.JobCode != "SAL-DIR" || ev.Grade != "M4" || ev.EffectiveDate != "2026-12-01" {
		t.Fatalf("event = %+v", ev)
	}
	if _, err := time.Parse(time.RFC3339, ev.OccurredAt); err != nil {
		t.Fatalf("occurred_at %q: %v", ev.OccurredAt, err)
	}
	var keys map[string]any
	_ = json.Unmarshal(c.body, &keys)
	for _, k := range []string{"event_id", "change_ref", "correlation_key", "provider_ref", "outcome", "reason", "job_code", "grade", "effective_date", "occurred_at"} {
		if _, ok := keys[k]; !ok {
			t.Fatalf("body missing %q: %s", k, c.body)
		}
	}
	if len(keys) != 10 {
		t.Fatalf("body has extra keys: %s", c.body)
	}
}

func TestCallbackWrongSecretFailsVerification(t *testing.T) {
	rc := newReceiver(t, http.StatusBadRequest)
	h := newHarness(t, func(c *Config) { c.WebhookSecret = []byte("not-the-receivers-secret") })
	submit(t, h, rc.url())
	drain(t, h.s)
	if calls := rc.calls(); len(calls) != 1 || calls[0].err == nil {
		t.Fatalf("mismatched secret was accepted: %+v", calls)
	}
}

func TestCallbackRejectOutcome(t *testing.T) {
	rc := newReceiver(t)
	h := newHarness(t, func(c *Config) {
		c.Scenario.Mode = ModeReject
		c.Scenario.RejectReason = "grade not in job architecture"
	})
	req := submit(t, h, rc.url())
	drain(t, h.s)
	calls := rc.calls()
	if len(calls) != 1 || calls[0].err != nil {
		t.Fatalf("calls = %+v", calls)
	}
	if calls[0].header.Get("Webhook-Event") != EventRejected {
		t.Fatalf("event type = %q", calls[0].header.Get("Webhook-Event"))
	}
	var ev ResultEvent
	_ = json.Unmarshal(calls[0].body, &ev)
	if ev.Outcome != StatusRejected || ev.Reason != "grade not in job architecture" {
		t.Fatalf("event = %+v", ev)
	}
	st := statusOf(t, h, req.ChangeRef)
	if st.Status != StatusRejected || st.GrantedAt != "" || st.Reason != "grade not in job architecture" {
		t.Fatalf("status = %+v", st)
	}
}

func TestCallbackRetriesAfterServerErrors(t *testing.T) {
	rc := newReceiver(t, 500, 503, 429, 408)
	h := newHarness(t, nil)
	req := submit(t, h, rc.url())
	drain(t, h.s)
	calls := rc.calls()
	if len(calls) != 5 {
		t.Fatalf("attempts = %d, want 5", len(calls))
	}
	for i, c := range calls {
		if c.header.Get("Webhook-Id") != calls[0].header.Get("Webhook-Id") || string(c.body) != string(calls[0].body) {
			t.Fatalf("attempt %d changed identity or bytes", i)
		}
	}
	// Waits: grant delay 0, then backoff 1s,2s,4s,8s.
	want := []time.Duration{0, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	if got := h.sleep.recorded(); !reflect.DeepEqual(got, want) {
		t.Fatalf("waits = %v, want %v", got, want)
	}
	st := statusOf(t, h, req.ChangeRef)
	if st.Callback != (CallbackStatus{Attempts: 5, Delivered: true, LastStatus: 200}) {
		t.Fatalf("callback status = %+v", st.Callback)
	}
}

func TestCallbackStopsOnPermanent4xx(t *testing.T) {
	rc := newReceiver(t, http.StatusBadRequest, 200)
	h := newHarness(t, nil)
	req := submit(t, h, rc.url())
	drain(t, h.s)
	if n := len(rc.calls()); n != 1 {
		t.Fatalf("attempts after 400 = %d, want 1", n)
	}
	st := statusOf(t, h, req.ChangeRef)
	if st.Callback != (CallbackStatus{Attempts: 1, Delivered: false, LastStatus: 400}) {
		t.Fatalf("callback status = %+v", st.Callback)
	}
}

func TestCallbackGivesUpAtMaxAttempts(t *testing.T) {
	rc := newReceiver(t, 503, 503, 503, 503)
	h := newHarness(t, func(c *Config) { c.MaxAttempts = 3 })
	req := submit(t, h, rc.url())
	drain(t, h.s)
	if n := len(rc.calls()); n != 3 {
		t.Fatalf("attempts = %d, want 3", n)
	}
	if st := statusOf(t, h, req.ChangeRef); st.Callback.Delivered || st.Callback.Attempts != 3 {
		t.Fatalf("callback = %+v", st.Callback)
	}
}

func TestCallbackNetworkErrorIsRetried(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	h := newHarness(t, func(c *Config) { c.MaxAttempts = 2 })
	req := submit(t, h, deadURL+"/cb")
	drain(t, h.s)
	st := statusOf(t, h, req.ChangeRef)
	if st.Callback != (CallbackStatus{Attempts: 2, Delivered: false, LastStatus: 0}) {
		t.Fatalf("callback = %+v", st.Callback)
	}
}

func TestDuplicateCallbacksShareEventID(t *testing.T) {
	rc := newReceiver(t)
	h := newHarness(t, func(c *Config) { c.Scenario.DuplicateCallbacks = true })
	submit(t, h, rc.url())
	drain(t, h.s)
	calls := rc.calls()
	if len(calls) != 2 {
		t.Fatalf("deliveries = %d, want 2", len(calls))
	}
	if calls[0].header.Get("Webhook-Id") == "" || calls[0].header.Get("Webhook-Id") != calls[1].header.Get("Webhook-Id") || string(calls[0].body) != string(calls[1].body) {
		t.Fatal("duplicate delivery changed event identity or bytes")
	}
	if calls[0].err != nil || calls[1].err != nil || calls[0].receipt.ID != calls[1].receipt.ID {
		t.Fatalf("receiver did not dedupe to one receipt: %v / %v", calls[0].err, calls[1].err)
	}
}

func TestBackoffSchedule(t *testing.T) {
	s := New(Config{})
	want := []time.Duration{1, 2, 4, 8, 16, 32, 60, 60}
	for i, w := range want {
		if got := s.backoff(i + 1); got != w*time.Second {
			t.Fatalf("backoff(%d) = %v, want %v", i+1, got, w*time.Second)
		}
	}
}

func TestRetryable(t *testing.T) {
	for status, want := range map[int]bool{500: true, 503: true, 408: true, 429: true, 302: true, 400: false, 401: false, 404: false, 422: false} {
		if retryable(status) != want {
			t.Fatalf("retryable(%d) = %v", status, !want)
		}
	}
	if errText(nil) != "" || errText(http.ErrServerClosed) == "" {
		t.Fatal("errText")
	}
}

package payrollsim

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// postReversal sends a reversal with the canonical idempotency key.
func postReversal(t *testing.T, h http.Handler, ref, reason string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"reason": reason})
	return postReversalRaw(t, h, testAPIKey, ref, ReversalKeyPrefix+ref, string(body))
}

func postReversalRaw(t *testing.T, h http.Handler, apiKey, ref, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/pay-changes/"+url.PathEscape(ref)+"/reversal", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set(APIKeyHeader, apiKey)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// waitStatus polls GET until the change reaches want and its callback count
// reaches attempts (the processing goroutine is still running, so drain is
// not an option before a reversal is posted).
func waitStatus(t *testing.T, s *Server, ref, want string, attempts int) ChangeStatus {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, st := getStatus(t, s, ref)
		if st.Status == want && st.Callback.Attempts >= attempts {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("status never reached %s/%d attempts: %+v", want, attempts, st)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func decodeReversalEvent(t *testing.T, body []byte) ReversalEvent {
	t.Helper()
	var ev ReversalEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestReversalOfAppliedChange(t *testing.T) {
	// Receiver: apply ok, first reversal attempt 500, retry ok.
	rc := newReceiver(t, http.StatusOK, http.StatusInternalServerError)
	s, fs := newTestServer(t, func(c *Config) { c.Scenario.ApplyDelayMS = 250 })
	req := validRequest(rc.url())
	if rec := post(t, s, req.ChangeRef, req); rec.Code != http.StatusAccepted {
		t.Fatalf("intake = %d", rec.Code)
	}
	waitStatus(t, s, req.ChangeRef, StatusApplied, 1)

	rec := postReversal(t, s, req.ChangeRef, "promotion withdrawn")
	if rec.Code != http.StatusAccepted || rec.Body.String() != `{"change_ref":"payroll:rev-1","status":"REVERSAL_PENDING"}` {
		t.Fatalf("reversal = %d %s", rec.Code, rec.Body)
	}
	drain(t, s)

	calls := rc.calls()
	if len(calls) != 3 {
		t.Fatalf("callbacks = %d, want apply + 2 reversal attempts", len(calls))
	}
	for i, c := range calls {
		if c.err != nil {
			t.Fatalf("callback %d failed webhook verification: %v", i, c.err)
		}
	}
	applyID, revID := calls[0].header.Get("Webhook-Id"), calls[1].header.Get("Webhook-Id")
	if applyID == revID || revID != calls[2].header.Get("Webhook-Id") || string(calls[1].body) != string(calls[2].body) {
		t.Fatalf("event ids: apply %q, reversal %q/%q", applyID, revID, calls[2].header.Get("Webhook-Id"))
	}
	if calls[1].header.Get("Webhook-Event") != EventReversed || calls[1].receipt.EventType != EventReversed || calls[1].header.Get("Webhook-Schema") != Schema {
		t.Fatalf("reversal headers = %v", calls[1].header)
	}
	if calls[1].receipt.ID == calls[0].receipt.ID {
		t.Fatal("receiver deduped the reversal into the apply receipt")
	}
	ev := decodeReversalEvent(t, calls[1].body)
	if ev.EventID != revID || ev.Outcome != StatusReversed || ev.ReversalReason != "promotion withdrawn" || ev.ChangeRef != req.ChangeRef || ev.BasePay != req.BasePay {
		t.Fatalf("reversal event = %+v", ev)
	}
	var keys map[string]any
	_ = json.Unmarshal(calls[1].body, &keys)
	if _, ok := keys["reversal_reason"]; !ok || len(keys) != 10 {
		t.Fatalf("reversal body keys = %s", calls[1].body)
	}
	// Apply delay, then the same delay to undo, then 1s backoff.
	if got, want := fs.recorded(), []time.Duration{250 * time.Millisecond, 250 * time.Millisecond, time.Second}; !reflect.DeepEqual(got, want) {
		t.Fatalf("waits = %v, want %v", got, want)
	}

	_, st := getStatus(t, s, req.ChangeRef)
	if st.Status != StatusReversed || st.AppliedAt == "" || st.Reversal == nil {
		t.Fatalf("status = %+v", st)
	}
	rv := *st.Reversal
	if rv.Reason != "promotion withdrawn" || rv.RequestedAt == "" || rv.CompletedAt == "" || rv.Callback != (CallbackStatus{Attempts: 2, Delivered: true, LastStatus: 200}) {
		t.Fatalf("reversal status = %+v", rv)
	}
	if st.Callback != (CallbackStatus{Attempts: 1, Delivered: true, LastStatus: 200}) {
		t.Fatalf("apply callback status changed: %+v", st.Callback)
	}
	// A replay after completion reports the current status.
	if rec := postReversal(t, s, req.ChangeRef, "promotion withdrawn"); rec.Code != http.StatusOK || rec.Body.String() != `{"change_ref":"payroll:rev-1","status":"REVERSED"}` {
		t.Fatalf("post-completion replay = %d %s", rec.Code, rec.Body)
	}
}

func TestReversalOfAcceptedCancelsPendingApply(t *testing.T) {
	rc := newReceiver(t)
	s, _ := newTestServer(t, func(c *Config) { c.Scenario.ApplyDelayMS = holdDelayMS })
	req := validRequest(rc.url())
	post(t, s, req.ChangeRef, req)
	if rec := postReversal(t, s, req.ChangeRef, "offer rescinded"); rec.Code != http.StatusAccepted {
		t.Fatalf("reversal = %d %s", rec.Code, rec.Body)
	}
	drain(t, s)
	calls := rc.calls()
	if len(calls) != 1 || calls[0].err != nil || calls[0].header.Get("Webhook-Event") != EventReversed {
		t.Fatalf("callbacks = %+v, want exactly one verified reversal", calls)
	}
	ev := decodeReversalEvent(t, calls[0].body)
	if ev.Outcome != StatusReversed || ev.ReversalReason != "offer rescinded" {
		t.Fatalf("event = %+v", ev)
	}
	_, st := getStatus(t, s, req.ChangeRef)
	if st.Status != StatusReversed || st.AppliedAt != "" || st.Callback != (CallbackStatus{}) || st.Reversal == nil || !st.Reversal.Callback.Delivered {
		t.Fatalf("status = %+v", st)
	}
}

// TestReversalBeatsAlreadyFiredApplyTimer models the worst interleaving: the
// apply timer has fired (its sleep returns nil, ignoring cancellation) but
// the apply has not yet taken the lock when the reversal lands. The late
// apply must lose: no APPLIED status, no apply callback.
func TestReversalBeatsAlreadyFiredApplyTimer(t *testing.T) {
	rc := newReceiver(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	gate := func(ctx context.Context, d time.Duration) error {
		if d == 1234*time.Millisecond { // the apply delay only
			once.Do(func() { close(entered) })
			<-release
			return nil
		}
		return ctx.Err()
	}
	s, _ := newTestServer(t, func(c *Config) {
		c.Scenario.ApplyDelayMS = 1234
		c.Sleep = gate
	})
	req := validRequest(rc.url())
	post(t, s, req.ChangeRef, req)
	<-entered
	if rec := postReversal(t, s, req.ChangeRef, "late apply must lose"); rec.Code != http.StatusAccepted {
		t.Fatalf("reversal = %d %s", rec.Code, rec.Body)
	}
	close(release)
	drain(t, s)
	for _, c := range rc.calls() {
		if c.header.Get("Webhook-Event") != EventReversed {
			t.Fatalf("late apply sent %q", c.header.Get("Webhook-Event"))
		}
	}
	if n := len(rc.calls()); n != 1 {
		t.Fatalf("callbacks = %d, want 1", n)
	}
	if _, st := getStatus(t, s, req.ChangeRef); st.Status != StatusReversed || st.AppliedAt != "" {
		t.Fatalf("status = %+v", st)
	}
}

func TestReversalReplayAndReusedKey(t *testing.T) {
	rc := newReceiver(t)
	s, _ := newTestServer(t, nil)
	req := validRequest(rc.url())
	post(t, s, req.ChangeRef, req)
	waitStatus(t, s, req.ChangeRef, StatusApplied, 1)
	// Undoing an applied change now takes "forever", parking it pending.
	control(t, s, http.MethodPut, "/v1/_control/scenario", "", fmt.Sprintf(`{"apply_delay_ms":%d}`, holdDelayMS))

	first := postReversal(t, s, req.ChangeRef, "duplicate promotion")
	if first.Code != http.StatusAccepted {
		t.Fatalf("first = %d %s", first.Code, first.Body)
	}
	replay := postReversal(t, s, req.ChangeRef, "duplicate promotion")
	if replay.Code != http.StatusOK || replay.Body.String() != first.Body.String() {
		t.Fatalf("replay = %d %s, want 200 %s", replay.Code, replay.Body, first.Body)
	}
	reuse := postReversal(t, s, req.ChangeRef, "a different reason")
	if reuse.Code != http.StatusConflict || reuse.Body.String() != `{"error":"idempotency_key_reused"}` {
		t.Fatalf("reuse = %d %s", reuse.Code, reuse.Body)
	}
	_, st := getStatus(t, s, req.ChangeRef)
	if st.Status != StatusReversalPending || st.Reversal == nil || st.Reversal.CompletedAt != "" || st.Reversal.Reason != "duplicate promotion" {
		t.Fatalf("pending status = %+v", st)
	}
	// A refusal scenario switched on later never fails the replay.
	control(t, s, http.MethodPut, "/v1/_control/scenario", "", `{"reversal_mode":"refuse"}`)
	if rec := postReversal(t, s, req.ChangeRef, "duplicate promotion"); rec.Code != http.StatusOK {
		t.Fatalf("replay under refuse = %d", rec.Code)
	}
}

func TestReversalRejectedAndUnknown(t *testing.T) {
	rc := newReceiver(t)
	s, _ := newTestServer(t, func(c *Config) { c.Scenario.Mode = ModeReject })
	req := validRequest(rc.url())
	post(t, s, req.ChangeRef, req)
	waitStatus(t, s, req.ChangeRef, StatusRejected, 1)
	if rec := postReversal(t, s, req.ChangeRef, "undo"); rec.Code != http.StatusConflict || rec.Body.String() != `{"error":"not_reversible","status":"REJECTED"}` {
		t.Fatalf("rejected = %d %s", rec.Code, rec.Body)
	}
	if rec := postReversal(t, s, "payroll:nope", "undo"); rec.Code != http.StatusNotFound || rec.Body.String() != `{"error":"not_found"}` {
		t.Fatalf("unknown = %d %s", rec.Code, rec.Body)
	}
	drain(t, s)
	if n := len(rc.calls()); n != 1 {
		t.Fatalf("callbacks = %d, want only the rejection", n)
	}
}

func TestReversalValidationAndAuth(t *testing.T) {
	s, _ := newTestServer(t, func(c *Config) { c.Scenario.ApplyDelayMS = holdDelayMS })
	req := validRequest("http://127.0.0.1:1/cb")
	post(t, s, req.ChangeRef, req)
	ref, key := req.ChangeRef, ReversalKeyPrefix+req.ChangeRef
	cases := []struct {
		name, apiKey, key, body string
		status                  int
		want                    string
	}{
		{"no api key", "", key, `{"reason":"x"}`, 401, `{"error":"invalid_api_key"}`},
		{"no api key garbage", "", "", `{`, 401, `{"error":"invalid_api_key"}`},
		{"missing key", testAPIKey, "", `{"reason":"x"}`, 400, `{"error":"invalid","field":"Idempotency-Key"}`},
		{"intake-style key", testAPIKey, ref, `{"reason":"x"}`, 400, `{"error":"invalid","field":"Idempotency-Key"}`},
		{"bad json", testAPIKey, key, `{`, 400, `{"error":"invalid","field":"body"}`},
		{"empty reason", testAPIKey, key, `{"reason":"  "}`, 400, `{"error":"invalid","field":"reason"}`},
		{"long reason", testAPIKey, key, `{"reason":"` + strings.Repeat("é", 501) + `"}`, 400, `{"error":"invalid","field":"reason"}`},
		{"too large", testAPIKey, key, `{"reason":"` + strings.Repeat("x", MaxRequestBytes) + `"}`, 413, `{"error":"too_large"}`},
	}
	for _, tc := range cases {
		rec := postReversalRaw(t, s, tc.apiKey, ref, tc.key, tc.body)
		if rec.Code != tc.status || rec.Body.String() != tc.want {
			t.Errorf("%s = %d %s, want %d %s", tc.name, rec.Code, rec.Body, tc.status, tc.want)
		}
	}
	if _, st := getStatus(t, s, ref); st.Reversal != nil || st.Status != StatusAccepted {
		t.Fatalf("refused requests reserved the reversal: %+v", st)
	}
	// 500 characters (multi-byte) is the inclusive limit.
	if rec := postReversal(t, s, ref, strings.Repeat("é", 500)); rec.Code != http.StatusAccepted {
		t.Fatalf("500-char reason = %d %s", rec.Code, rec.Body)
	}
}

func TestReversalModes(t *testing.T) {
	s, _ := newTestServer(t, func(c *Config) {
		c.Scenario.ApplyDelayMS = holdDelayMS
		c.Scenario.ReversalMode = ReversalFailTransient
		c.Scenario.RejectReason = "period closed"
	})
	req := validRequest("http://127.0.0.1:1/cb")
	post(t, s, req.ChangeRef, req)
	if rec := postReversal(t, s, req.ChangeRef, "undo"); rec.Code != http.StatusServiceUnavailable || rec.Body.String() != `{"error":"unavailable"}` {
		t.Fatalf("fail_transient = %d %s", rec.Code, rec.Body)
	}
	control(t, s, http.MethodPut, "/v1/_control/scenario", "", `{"reversal_mode":"refuse"}`)
	if rec := postReversal(t, s, req.ChangeRef, "undo"); rec.Code != http.StatusUnprocessableEntity || rec.Body.String() != `{"error":"rejected","reason":"period closed"}` {
		t.Fatalf("refuse = %d %s", rec.Code, rec.Body)
	}
	if _, st := getStatus(t, s, req.ChangeRef); st.Status != StatusAccepted || st.Reversal != nil {
		t.Fatalf("failed reversals changed state: %+v", st)
	}
	// Neither failure reserved the key: the retry is a fresh 202.
	control(t, s, http.MethodPut, "/v1/_control/scenario", "", `{"reversal_mode":"succeed"}`)
	if rec := postReversal(t, s, req.ChangeRef, "undo"); rec.Code != http.StatusAccepted {
		t.Fatalf("retry = %d %s", rec.Code, rec.Body)
	}
	if rec := control(t, s, http.MethodPut, "/v1/_control/scenario", "", `{"reversal_mode":"maybe"}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"reversal_mode"`) {
		t.Fatalf("bad mode = %d %s", rec.Code, rec.Body)
	}
}

func TestReversalRefusedWhileShuttingDown(t *testing.T) {
	s, _ := newTestServer(t, func(c *Config) { c.Scenario.ApplyDelayMS = holdDelayMS })
	req := validRequest("http://127.0.0.1:1/cb")
	post(t, s, req.ChangeRef, req)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = s.Shutdown(ctx)
	if rec := postReversal(t, s, req.ChangeRef, "undo"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("reversal after shutdown = %d", rec.Code)
	}
}

func TestDropCallbacksStillProcesses(t *testing.T) {
	rc := newReceiver(t)
	s, _ := newTestServer(t, func(c *Config) { c.Scenario.DropCallbacks = true })
	req := validRequest(rc.url())
	post(t, s, req.ChangeRef, req)
	waitStatus(t, s, req.ChangeRef, StatusApplied, 0)
	postReversal(t, s, req.ChangeRef, "undo")
	drain(t, s)
	if n := len(rc.calls()); n != 0 {
		t.Fatalf("callbacks sent = %d, want 0", n)
	}
	_, st := getStatus(t, s, req.ChangeRef)
	if st.Status != StatusReversed || st.AppliedAt == "" || st.Callback != (CallbackStatus{}) || st.Reversal.Callback != (CallbackStatus{}) {
		t.Fatalf("status = %+v", st)
	}
}

func TestDropCallbacksAppliedVisibleByPolling(t *testing.T) {
	rc := newReceiver(t)
	s, _ := newTestServer(t, func(c *Config) { c.Scenario.DropCallbacks = true })
	req := validRequest(rc.url())
	post(t, s, req.ChangeRef, req)
	drain(t, s)
	if n := len(rc.calls()); n != 0 {
		t.Fatalf("callbacks sent = %d", n)
	}
	if _, st := getStatus(t, s, req.ChangeRef); st.Status != StatusApplied || st.Callback.Attempts != 0 {
		t.Fatalf("status = %+v", st)
	}
}

func TestCallbackDelayHonored(t *testing.T) {
	rc := newReceiver(t)
	s, fs := newTestServer(t, func(c *Config) { c.Scenario.CallbackDelayMS = 2500 })
	req := validRequest(rc.url())
	post(t, s, req.ChangeRef, req)
	drain(t, s)
	if got, want := fs.recorded(), []time.Duration{0, 2500 * time.Millisecond}; !reflect.DeepEqual(got, want) {
		t.Fatalf("waits = %v, want %v", got, want)
	}
	if n := len(rc.calls()); n != 1 {
		t.Fatalf("callbacks = %d", n)
	}
	if rec := control(t, s, http.MethodPut, "/v1/_control/scenario", "", `{"callback_delay_ms":-1}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("negative delay = %d", rec.Code)
	}
}

func TestCallbackDelayCancelledByShutdown(t *testing.T) {
	rc := newReceiver(t)
	s, _ := newTestServer(t, func(c *Config) { c.Scenario.CallbackDelayMS = holdDelayMS })
	req := validRequest(rc.url())
	post(t, s, req.ChangeRef, req)
	waitStatus(t, s, req.ChangeRef, StatusApplied, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_ = s.Shutdown(ctx)
	if n := len(rc.calls()); n != 0 {
		t.Fatalf("callback sent despite held delay: %d", n)
	}
}

// eventRecorder is an HTTP transport that acknowledges every callback and
// records (change_ref, event type) pairs, so a race test can run hundreds of
// changes without sockets.
type eventRecorder struct {
	mu     sync.Mutex
	events map[string][]string
}

func (e *eventRecorder) RoundTrip(r *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(r.Body)
	var ev ResultEvent
	_ = json.Unmarshal(body, &ev)
	e.mu.Lock()
	e.events[ev.ChangeRef] = append(e.events[ev.ChangeRef], r.Header.Get("Webhook-Event"))
	e.mu.Unlock()
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: r}, nil
}

// TestReversalRacingApply fires a reversal the instant each change is
// accepted, with a real zero-delay apply racing it. Whichever wins, the
// change must end REVERSED; when the reversal found it ACCEPTED, the apply
// must never have happened nor called back.
func TestReversalRacingApply(t *testing.T) {
	const n = 200
	rec := &eventRecorder{events: map[string][]string{}}
	s, _ := newTestServer(t, func(c *Config) {
		c.Sleep = sleepContext
		c.HTTPClient = &http.Client{Transport: rec}
	})
	var wg sync.WaitGroup
	codes := make([]int, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := validRequest("http://hcm.invalid/cb")
			req.ChangeRef = fmt.Sprintf("payroll:race-%d", i)
			if r := post(t, s, req.ChangeRef, req); r.Code != http.StatusAccepted {
				t.Errorf("intake %d = %d", i, r.Code)
				return
			}
			codes[i] = postReversal(t, s, req.ChangeRef, "race").Code
		}()
	}
	wg.Wait()
	drain(t, s)
	fromAccepted := 0
	for i := range n {
		ref := fmt.Sprintf("payroll:race-%d", i)
		if codes[i] != http.StatusAccepted {
			t.Fatalf("%s reversal = %d", ref, codes[i])
		}
		s.mu.Lock()
		ch := s.changes[ref]
		status, appliedAt, from := ch.status, ch.appliedAt, ch.rev.fromStatus
		s.mu.Unlock()
		if status != StatusReversed {
			t.Fatalf("%s ended %s after a 202 reversal", ref, status)
		}
		events := rec.events[ref]
		if from == StatusAccepted {
			fromAccepted++
			if !appliedAt.IsZero() || !reflect.DeepEqual(events, []string{EventReversed}) {
				t.Fatalf("%s: apply fired after cancellation (applied_at %v, events %v)", ref, appliedAt, events)
			}
		} else if slices.Sort(events); !reflect.DeepEqual(events, []string{EventApplied, EventReversed}) {
			t.Fatalf("%s: events %v", ref, events)
		}
	}
	t.Logf("reversal found ACCEPTED in %d/%d races", fromAccepted, n)
}

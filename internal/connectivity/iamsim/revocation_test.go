package iamsim

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

const holdDelay = time.Hour

// holdSleep records waits like fakeSleep, but a wait of an hour or more
// blocks until cancelled, which is how a test parks a change in ACCEPTED.
func (f *fakeSleep) holdSleep(ctx context.Context, d time.Duration) error {
	f.mu.Lock()
	f.waits = append(f.waits, d)
	f.mu.Unlock()
	if d >= holdDelay {
		<-ctx.Done()
	}
	return ctx.Err()
}

// holdGrant parks every grant until cancelled.
func holdGrant(h **harness) func(*Config) {
	return func(c *Config) {
		c.Scenario.GrantDelayMS = int64(holdDelay / time.Millisecond)
		c.Sleep = func(ctx context.Context, d time.Duration) error { return (*h).sleep.holdSleep(ctx, d) }
	}
}

func postRevocation(t *testing.T, h http.Handler, token, ref, reason string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"reason": reason})
	return postRevocationRaw(t, h, token, ref, RevocationKeyPrefix+ref, string(body))
}

func postRevocationRaw(t *testing.T, h http.Handler, token, ref, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/access-changes/"+url.PathEscape(ref)+"/revocation", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// waitStatus polls GET until the change reaches want with at least attempts
// callback attempts; the grant goroutine is still running, so drain is not
// an option before a revocation is posted.
func waitStatus(t *testing.T, h *harness, tok, ref, want string, attempts int) ChangeStatus {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, st := getStatus(t, h.s, tok, ref)
		if st.Status == want && st.Callback.Attempts >= attempts {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("status never reached %s/%d: %+v", want, attempts, st)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestRevocationOfGrantedChange(t *testing.T) {
	rc := newReceiver(t, http.StatusOK, http.StatusServiceUnavailable)
	h := newHarness(t, func(c *Config) { c.Scenario.GrantDelayMS = 250 })
	tok := issueToken(t, h.s, "")
	req := validRequest(rc.url())
	postChange(t, h.s, tok, req.ChangeRef, req)
	waitStatus(t, h, tok, req.ChangeRef, StatusGranted, 1)

	rec := postRevocation(t, h.s, tok, req.ChangeRef, "promotion withdrawn")
	if rec.Code != http.StatusAccepted || rec.Body.String() != `{"change_ref":"iam:rev-1","status":"REVERSAL_PENDING"}` {
		t.Fatalf("revocation = %d %s", rec.Code, rec.Body)
	}
	drain(t, h.s)

	calls := rc.calls()
	if len(calls) != 3 {
		t.Fatalf("callbacks = %d, want grant + 2 revocation attempts", len(calls))
	}
	for i, c := range calls {
		if c.err != nil {
			t.Fatalf("callback %d failed webhook verification: %v", i, c.err)
		}
	}
	grantID, revID := calls[0].header.Get("Webhook-Id"), calls[1].header.Get("Webhook-Id")
	if grantID == revID || revID != calls[2].header.Get("Webhook-Id") || string(calls[1].body) != string(calls[2].body) {
		t.Fatalf("event ids: grant %q, revocation %q/%q", grantID, revID, calls[2].header.Get("Webhook-Id"))
	}
	if calls[1].header.Get("Webhook-Event") != EventRevoked || calls[1].receipt.EventType != EventRevoked || calls[1].receipt.Schema != Schema {
		t.Fatalf("revocation headers = %v receipt %+v", calls[1].header, calls[1].receipt)
	}
	if calls[1].receipt.ID == calls[0].receipt.ID {
		t.Fatal("receiver deduped the revocation into the grant receipt")
	}
	var ev RevocationEvent
	if err := json.Unmarshal(calls[1].body, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.EventID != revID || ev.Outcome != StatusRevoked || ev.ReversalReason != "promotion withdrawn" || ev.JobCode != "SAL-DIR" || ev.Grade != "M4" {
		t.Fatalf("event = %+v", ev)
	}
	var keys map[string]any
	_ = json.Unmarshal(calls[1].body, &keys)
	if _, ok := keys["reversal_reason"]; !ok || len(keys) != 11 {
		t.Fatalf("revocation body keys = %s", calls[1].body)
	}
	if got, want := h.sleep.recorded(), []time.Duration{250 * time.Millisecond, 250 * time.Millisecond, time.Second}; !reflect.DeepEqual(got, want) {
		t.Fatalf("waits = %v, want %v", got, want)
	}

	st := statusOf(t, h, req.ChangeRef)
	if st.Status != StatusRevoked || st.GrantedAt == "" || st.Reversal == nil {
		t.Fatalf("status = %+v", st)
	}
	if rv := *st.Reversal; rv.Reason != "promotion withdrawn" || rv.RequestedAt == "" || rv.CompletedAt == "" || rv.Callback != (CallbackStatus{Attempts: 2, Delivered: true, LastStatus: 200}) {
		t.Fatalf("reversal = %+v", rv)
	}
	if rec := postRevocation(t, h.s, tok, req.ChangeRef, "promotion withdrawn"); rec.Code != http.StatusOK || rec.Body.String() != `{"change_ref":"iam:rev-1","status":"REVOKED"}` {
		t.Fatalf("post-completion replay = %d %s", rec.Code, rec.Body)
	}
}

func TestRevocationOfAcceptedCancelsPendingGrant(t *testing.T) {
	rc := newReceiver(t)
	var h *harness
	h = newHarness(t, holdGrant(&h))
	tok := issueToken(t, h.s, "")
	req := validRequest(rc.url())
	postChange(t, h.s, tok, req.ChangeRef, req)
	if rec := postRevocation(t, h.s, tok, req.ChangeRef, "offer rescinded"); rec.Code != http.StatusAccepted {
		t.Fatalf("revocation = %d %s", rec.Code, rec.Body)
	}
	drain(t, h.s)
	calls := rc.calls()
	if len(calls) != 1 || calls[0].err != nil || calls[0].header.Get("Webhook-Event") != EventRevoked {
		t.Fatalf("callbacks = %+v, want exactly one verified revocation", calls)
	}
	st := statusOf(t, h, req.ChangeRef)
	if st.Status != StatusRevoked || st.GrantedAt != "" || st.Callback != (CallbackStatus{}) || st.Reversal == nil || !st.Reversal.Callback.Delivered {
		t.Fatalf("status = %+v", st)
	}
}

// TestRevocationBeatsAlreadyFiredGrantTimer: the grant timer has fired but
// the grant has not taken the lock when the revocation lands; it must lose.
func TestRevocationBeatsAlreadyFiredGrantTimer(t *testing.T) {
	rc := newReceiver(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	h := newHarness(t, func(c *Config) {
		c.Scenario.GrantDelayMS = 1234
		c.Sleep = func(ctx context.Context, d time.Duration) error {
			if d == 1234*time.Millisecond {
				once.Do(func() { close(entered) })
				<-release
				return nil
			}
			return ctx.Err()
		}
	})
	tok := issueToken(t, h.s, "")
	req := validRequest(rc.url())
	postChange(t, h.s, tok, req.ChangeRef, req)
	<-entered
	if rec := postRevocation(t, h.s, tok, req.ChangeRef, "late grant must lose"); rec.Code != http.StatusAccepted {
		t.Fatalf("revocation = %d", rec.Code)
	}
	close(release)
	drain(t, h.s)
	calls := rc.calls()
	if len(calls) != 1 || calls[0].header.Get("Webhook-Event") != EventRevoked {
		t.Fatalf("callbacks = %d (%v), want only the revocation", len(calls), calls)
	}
	if st := statusOf(t, h, req.ChangeRef); st.Status != StatusRevoked || st.GrantedAt != "" {
		t.Fatalf("status = %+v", st)
	}
}

func TestRevocationReplayAndReusedKey(t *testing.T) {
	rc := newReceiver(t)
	var h *harness
	h = newHarness(t, func(c *Config) {
		c.Sleep = func(ctx context.Context, d time.Duration) error { return h.sleep.holdSleep(ctx, d) }
	})
	tok := issueToken(t, h.s, "")
	req := validRequest(rc.url())
	postChange(t, h.s, tok, req.ChangeRef, req)
	waitStatus(t, h, tok, req.ChangeRef, StatusGranted, 1)
	control(t, h.s, http.MethodPut, "/v1/_control/scenario", "", fmt.Sprintf(`{"grant_delay_ms":%d}`, int64(holdDelay/time.Millisecond)))

	first := postRevocation(t, h.s, tok, req.ChangeRef, "duplicate promotion")
	if first.Code != http.StatusAccepted {
		t.Fatalf("first = %d %s", first.Code, first.Body)
	}
	if replay := postRevocation(t, h.s, tok, req.ChangeRef, "duplicate promotion"); replay.Code != http.StatusOK || replay.Body.String() != first.Body.String() {
		t.Fatalf("replay = %d %s", replay.Code, replay.Body)
	}
	if reuse := postRevocation(t, h.s, tok, req.ChangeRef, "other"); reuse.Code != http.StatusConflict || reuse.Body.String() != `{"error":"idempotency_key_reused"}` {
		t.Fatalf("reuse = %d %s", reuse.Code, reuse.Body)
	}
	st := statusOf(t, h, req.ChangeRef)
	if st.Status != StatusReversalPending || st.Reversal == nil || st.Reversal.CompletedAt != "" {
		t.Fatalf("pending status = %+v", st)
	}
	// Reset cancels the parked revocation job too, so cleanup drains at once.
	control(t, h.s, http.MethodPost, "/v1/_control/reset", "", "")
	drain(t, h.s)
}

func TestRevocationRejectedUnknownAndAuth(t *testing.T) {
	rc := newReceiver(t)
	h := newHarness(t, func(c *Config) { c.Scenario.Mode = ModeReject })
	tok := issueToken(t, h.s, "")
	req := validRequest(rc.url())
	postChange(t, h.s, tok, req.ChangeRef, req)
	waitStatus(t, h, tok, req.ChangeRef, StatusRejected, 1)
	if rec := postRevocation(t, h.s, tok, req.ChangeRef, "undo"); rec.Code != http.StatusConflict || rec.Body.String() != `{"error":"not_reversible","status":"REJECTED"}` {
		t.Fatalf("rejected = %d %s", rec.Code, rec.Body)
	}
	if rec := postRevocation(t, h.s, tok, "iam:nope", "undo"); rec.Code != http.StatusNotFound || rec.Body.String() != `{"error":"not_found"}` {
		t.Fatalf("unknown = %d %s", rec.Code, rec.Body)
	}
	// Auth runs first: no token is 401 even with a garbage body, a read-only
	// token is 403 insufficient_scope.
	if rec := postRevocationRaw(t, h.s, "", req.ChangeRef, "", "{"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token = %d", rec.Code)
	}
	readOnly := issueToken(t, h.s, ScopeRead)
	if rec := postRevocation(t, h.s, readOnly, req.ChangeRef, "undo"); rec.Code != http.StatusForbidden || errorCode(t, rec) != "insufficient_scope" {
		t.Fatalf("read-only = %d %s", rec.Code, rec.Body)
	}
	assertInvalidToken(t, postRevocation(t, h.s, "garbage", req.ChangeRef, "undo"))
}

func TestRevocationValidation(t *testing.T) {
	var h *harness
	h = newHarness(t, holdGrant(&h))
	tok := issueToken(t, h.s, "")
	req := validRequest("http://127.0.0.1:1/cb")
	postChange(t, h.s, tok, req.ChangeRef, req)
	ref, key := req.ChangeRef, RevocationKeyPrefix+req.ChangeRef
	for name, tc := range map[string]struct {
		key, body string
		status    int
		want      string
	}{
		"missing key":     {"", `{"reason":"x"}`, 400, `{"error":"invalid","field":"Idempotency-Key"}`},
		"payroll-style":   {"reversal:" + ref, `{"reason":"x"}`, 400, `{"error":"invalid","field":"Idempotency-Key"}`},
		"bad json":        {key, `[`, 400, `{"error":"invalid","field":"body"}`},
		"blank reason":    {key, `{"reason":""}`, 400, `{"error":"invalid","field":"reason"}`},
		"501-char reason": {key, `{"reason":"` + strings.Repeat("x", 501) + `"}`, 400, `{"error":"invalid","field":"reason"}`},
		"oversize body":   {key, `{"reason":"` + strings.Repeat("x", MaxRequestBytes) + `"}`, 413, `{"error":"too_large"}`},
	} {
		if rec := postRevocationRaw(t, h.s, tok, ref, tc.key, tc.body); rec.Code != tc.status || rec.Body.String() != tc.want {
			t.Errorf("%s = %d %s", name, rec.Code, rec.Body)
		}
	}
	if st := statusOf(t, h, ref); st.Reversal != nil {
		t.Fatalf("invalid requests reserved the revocation: %+v", st)
	}
	if rec := postRevocation(t, h.s, tok, ref, strings.Repeat("x", 500)); rec.Code != http.StatusAccepted {
		t.Fatalf("500-char reason = %d", rec.Code)
	}
}

func TestRevocationModesAndShutdown(t *testing.T) {
	var h *harness
	h = newHarness(t, func(c *Config) {
		holdGrant(&h)(c)
		c.Scenario.ReversalMode = ReversalFailTransient
		c.Scenario.RejectReason = "access frozen"
	})
	tok := issueToken(t, h.s, "")
	req := validRequest("http://127.0.0.1:1/cb")
	postChange(t, h.s, tok, req.ChangeRef, req)
	if rec := postRevocation(t, h.s, tok, req.ChangeRef, "undo"); rec.Code != http.StatusServiceUnavailable || rec.Body.String() != `{"error":"unavailable"}` {
		t.Fatalf("fail_transient = %d %s", rec.Code, rec.Body)
	}
	control(t, h.s, http.MethodPut, "/v1/_control/scenario", "", `{"reversal_mode":"refuse"}`)
	if rec := postRevocation(t, h.s, tok, req.ChangeRef, "undo"); rec.Code != http.StatusUnprocessableEntity || rec.Body.String() != `{"error":"rejected","reason":"access frozen"}` {
		t.Fatalf("refuse = %d %s", rec.Code, rec.Body)
	}
	if st := statusOf(t, h, req.ChangeRef); st.Status != StatusAccepted || st.Reversal != nil {
		t.Fatalf("failed revocations changed state: %+v", st)
	}
	if rec := control(t, h.s, http.MethodPut, "/v1/_control/scenario", "", `{"reversal_mode":"nope"}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "reversal_mode") {
		t.Fatalf("bad mode = %d %s", rec.Code, rec.Body)
	}
	control(t, h.s, http.MethodPut, "/v1/_control/scenario", "", `{"reversal_mode":"succeed"}`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = h.s.Shutdown(ctx)
	if rec := postRevocation(t, h.s, tok, req.ChangeRef, "undo"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("revocation while closing = %d", rec.Code)
	}
}

func TestDropCallbacksGrantedVisibleByPolling(t *testing.T) {
	rc := newReceiver(t)
	h := newHarness(t, func(c *Config) { c.Scenario.DropCallbacks = true })
	tok := issueToken(t, h.s, "")
	req := validRequest(rc.url())
	postChange(t, h.s, tok, req.ChangeRef, req)
	waitStatus(t, h, tok, req.ChangeRef, StatusGranted, 0)
	postRevocation(t, h.s, tok, req.ChangeRef, "undo")
	drain(t, h.s)
	if n := len(rc.calls()); n != 0 {
		t.Fatalf("callbacks sent = %d, want 0", n)
	}
	st := statusOf(t, h, req.ChangeRef)
	if st.Status != StatusRevoked || st.GrantedAt == "" || st.Callback != (CallbackStatus{}) || st.Reversal.Callback != (CallbackStatus{}) {
		t.Fatalf("status = %+v", st)
	}
}

func TestDropCallbacksGrantOnly(t *testing.T) {
	rc := newReceiver(t)
	h := newHarness(t, func(c *Config) { c.Scenario.DropCallbacks = true })
	req := submit(t, h, rc.url())
	drain(t, h.s)
	if n := len(rc.calls()); n != 0 {
		t.Fatalf("callbacks = %d", n)
	}
	if st := statusOf(t, h, req.ChangeRef); st.Status != StatusGranted || st.Callback.Attempts != 0 {
		t.Fatalf("status = %+v", st)
	}
}

func TestCallbackDelayHonored(t *testing.T) {
	rc := newReceiver(t)
	h := newHarness(t, func(c *Config) { c.Scenario.CallbackDelayMS = 2500 })
	submit(t, h, rc.url())
	drain(t, h.s)
	if got, want := h.sleep.recorded(), []time.Duration{0, 2500 * time.Millisecond}; !reflect.DeepEqual(got, want) {
		t.Fatalf("waits = %v, want %v", got, want)
	}
	if n := len(rc.calls()); n != 1 {
		t.Fatalf("callbacks = %d", n)
	}
	if rec := control(t, h.s, http.MethodPut, "/v1/_control/scenario", "", `{"callback_delay_ms":-5}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("negative delay = %d", rec.Code)
	}
}

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

// TestRevocationRacingGrant fires a revocation the instant each change is
// accepted while a real zero-delay grant races it: every change must end
// REVOKED, and when the revocation found it ACCEPTED the grant must never
// have happened nor called back.
func TestRevocationRacingGrant(t *testing.T) {
	const n = 200
	rec := &eventRecorder{events: map[string][]string{}}
	h := newHarness(t, func(c *Config) {
		c.Sleep = sleepContext
		c.HTTPClient = &http.Client{Transport: rec}
	})
	tok := issueToken(t, h.s, "")
	var wg sync.WaitGroup
	codes := make([]int, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := validRequest("http://hcm.invalid/cb")
			req.ChangeRef = fmt.Sprintf("iam:race-%d", i)
			if r := postChange(t, h.s, tok, req.ChangeRef, req); r.Code != http.StatusAccepted {
				t.Errorf("intake %d = %d", i, r.Code)
				return
			}
			codes[i] = postRevocation(t, h.s, tok, req.ChangeRef, "race").Code
		}()
	}
	wg.Wait()
	drain(t, h.s)
	fromAccepted := 0
	for i := range n {
		ref := fmt.Sprintf("iam:race-%d", i)
		if codes[i] != http.StatusAccepted {
			t.Fatalf("%s revocation = %d", ref, codes[i])
		}
		h.s.mu.Lock()
		ch := h.s.changes[ref]
		status, grantedAt, from := ch.status, ch.grantedAt, ch.rev.fromStatus
		h.s.mu.Unlock()
		if status != StatusRevoked {
			t.Fatalf("%s ended %s after a 202 revocation", ref, status)
		}
		events := rec.events[ref]
		slices.Sort(events)
		if from == StatusAccepted {
			fromAccepted++
			if !grantedAt.IsZero() || !reflect.DeepEqual(events, []string{EventRevoked}) {
				t.Fatalf("%s: grant fired after cancellation (granted_at %v, events %v)", ref, grantedAt, events)
			}
		} else if !reflect.DeepEqual(events, []string{EventGranted, EventRevoked}) {
			t.Fatalf("%s: events %v", ref, events)
		}
	}
	t.Logf("revocation found ACCEPTED in %d/%d races", fromAccepted, n)
}

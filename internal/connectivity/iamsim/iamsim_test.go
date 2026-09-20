package iamsim

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/webhook"
)

const (
	testClientID      = "test-client"
	testClientSecret  = "test-client-secret"
	testWebhookSecret = "iamsim-test-webhook-secret"
	testTenant        = "harborcare-demo"
)

// fakeClock is a settable clock so token expiry is tested without waiting.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// fakeSleep records every requested wait and returns at once, so backoff
// schedules are asserted without being waited out.
type fakeSleep struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (f *fakeSleep) sleep(ctx context.Context, d time.Duration) error {
	f.mu.Lock()
	f.waits = append(f.waits, d)
	f.mu.Unlock()
	return ctx.Err()
}

func (f *fakeSleep) recorded() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.waits...)
}

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type harness struct {
	s     *Server
	sleep *fakeSleep
	clock *fakeClock
	logs  *syncBuffer
}

// newHarness builds a simulator with zero grant delay, a fake sleep, a fake
// clock anchored at wall time (so webhook replay windows still hold) and
// captured logs.
func newHarness(t *testing.T, mutate func(*Config)) *harness {
	t.Helper()
	h := &harness{sleep: &fakeSleep{}, clock: &fakeClock{t: time.Now()}, logs: &syncBuffer{}}
	sc := DefaultScenario()
	sc.GrantDelayMS = 0
	cfg := Config{
		ClientID:      testClientID,
		ClientSecret:  testClientSecret,
		WebhookSecret: []byte(testWebhookSecret),
		Scenario:      &sc,
		Sleep:         h.sleep.sleep,
		Now:           h.clock.Now,
		Logger:        slog.New(slog.NewTextHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	if mutate != nil {
		mutate(&cfg)
	}
	h.s = New(cfg)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.s.Shutdown(ctx)
	})
	return h
}

// drain waits for all background processing and delivery to finish.
func drain(t *testing.T, s *Server) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown did not drain: %v", err)
	}
}

func basicAuth(id, secret string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(url.QueryEscape(id)+":"+url.QueryEscape(secret)))
}

// tokenCall posts a raw token request.
func tokenCall(t *testing.T, h http.Handler, form url.Values, authz, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const formType = "application/x-www-form-urlencoded"

// issueToken runs stage one with Basic auth and returns the access token.
func issueToken(t *testing.T, h http.Handler, scope string) string {
	t.Helper()
	form := url.Values{"grant_type": {"client_credentials"}}
	if scope != "" {
		form.Set("scope", scope)
	}
	rec := tokenCall(t, h, form, basicAuth(testClientID, testClientSecret), formType)
	if rec.Code != http.StatusOK {
		t.Fatalf("token = %d %s", rec.Code, rec.Body)
	}
	var tr TokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &tr); err != nil {
		t.Fatal(err)
	}
	return tr.AccessToken
}

func validRequest(callback string) AccessChangeRequest {
	return AccessChangeRequest{
		ChangeRef:      "iam:rev-1",
		Tenant:         testTenant,
		WorkerRef:      "worker-42",
		JobCode:        "SAL-DIR",
		Grade:          "M4",
		EffectiveDate:  "2026-12-01",
		CorrelationKey: "corr-abc",
		CallbackURL:    callback,
	}
}

// postChange submits an access change with the given bearer token ("" for
// none) and idempotency key ("" for none).
func postChange(t *testing.T, h http.Handler, token, key string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	switch b := body.(type) {
	case []byte:
		raw = b
	case string:
		raw = []byte(b)
	default:
		var err error
		if raw, err = json.Marshal(b); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/access-changes", bytes.NewReader(raw))
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

func getStatus(t *testing.T, h http.Handler, token, ref string) (int, ChangeStatus) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/access-changes/"+url.PathEscape(ref), nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var st ChangeStatus
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
			t.Fatalf("decode status: %v", err)
		}
	}
	return rec.Code, st
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body %q: %v", rec.Body, err)
	}
	return body["error"]
}

// receivedCallback is one callback as the receiver saw it.
type receivedCallback struct {
	header  http.Header
	body    []byte
	receipt webhook.Receipt
	err     error
}

// receiver is an httptest server that verifies callbacks through
// webhook.Store.Receive -- the HCM side's own validation path -- and answers
// with scripted statuses (then 200 once the script runs out).
type receiver struct {
	srv    *httptest.Server
	store  *webhook.Store
	mu     sync.Mutex
	got    []receivedCallback
	script []int
}

func newReceiver(t *testing.T, script ...int) *receiver {
	t.Helper()
	rc := &receiver{store: webhook.NewStore(), script: script}
	if err := rc.store.RegisterEndpoint(webhook.Endpoint{
		ID: "iam", TenantID: testTenant, ConnectionID: "iam-conn", Secret: []byte(testWebhookSecret),
		AllowedSchemas: []string{Schema}, MaxPayloadBytes: MaxRequestBytes, ReplayWindow: 5 * time.Minute,
	}); err != nil {
		t.Fatal(err)
	}
	rc.srv = httptest.NewServer(http.HandlerFunc(rc.serve))
	t.Cleanup(rc.srv.Close)
	return rc
}

func (rc *receiver) url() string { return rc.srv.URL + "/integrations/iam/v1/receipts" }

func (rc *receiver) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	c := receivedCallback{header: r.Header.Clone(), body: body}
	ts, perr := strconv.ParseInt(r.Header.Get("Webhook-Timestamp"), 10, 64)
	if perr != nil {
		c.err = perr
	} else {
		c.receipt, c.err = rc.store.Receive(webhook.Request{
			EndpointID: "iam",
			TenantID:   r.Header.Get("Webhook-Tenant"),
			EventID:    r.Header.Get("Webhook-Id"),
			EventType:  r.Header.Get("Webhook-Event"),
			Schema:     r.Header.Get("Webhook-Schema"),
			Timestamp:  time.Unix(0, ts),
			Signature:  r.Header.Get("Webhook-Signature"),
			Payload:    body,
		}, time.Now())
	}
	rc.mu.Lock()
	rc.got = append(rc.got, c)
	status := http.StatusOK
	if len(rc.script) > 0 {
		status, rc.script = rc.script[0], rc.script[1:]
	} else if c.err != nil {
		status = http.StatusBadRequest
	}
	rc.mu.Unlock()
	w.WriteHeader(status)
}

func (rc *receiver) calls() []receivedCallback {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return append([]receivedCallback(nil), rc.got...)
}

func TestHealthz(t *testing.T) {
	h := newHarness(t, nil)
	rec := httptest.NewRecorder()
	h.s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Fatalf("healthz = %d %s", rec.Code, rec.Body)
	}
}

func TestStatusUnknownIs404AndGrantedHasTimestamp(t *testing.T) {
	rc := newReceiver(t)
	h := newHarness(t, nil)
	tok := issueToken(t, h.s, "")
	if code, _ := getStatus(t, h.s, tok, "iam:nope"); code != http.StatusNotFound {
		t.Fatalf("unknown status = %d", code)
	}
	req := validRequest(rc.url())
	if rec := postChange(t, h.s, tok, req.ChangeRef, req); rec.Code != http.StatusAccepted {
		t.Fatalf("intake = %d %s", rec.Code, rec.Body)
	}
	drain(t, h.s)
	code, st := getStatus(t, h.s, tok, req.ChangeRef)
	if code != http.StatusOK || st.Status != StatusGranted || st.Reason != "" || !st.Callback.Delivered || st.Callback.Attempts != 1 || st.Callback.LastStatus != 200 {
		t.Fatalf("status = %d %+v", code, st)
	}
	if _, err := time.Parse(time.RFC3339, st.GrantedAt); err != nil {
		t.Fatalf("granted_at %q: %v", st.GrantedAt, err)
	}
}

func TestShutdownRefusesNewChangesAndDeadlineCancels(t *testing.T) {
	// A real sleep with a long grant delay keeps the job in flight, so only
	// the deadline can end the shutdown.
	h := newHarness(t, func(c *Config) {
		c.Sleep = sleepContext
		c.Scenario.GrantDelayMS = int64(time.Hour / time.Millisecond)
	})
	tok := issueToken(t, h.s, "")
	req := validRequest("http://127.0.0.1:1/cb")
	if rec := postChange(t, h.s, tok, req.ChangeRef, req); rec.Code != http.StatusAccepted {
		t.Fatalf("intake = %d", rec.Code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := h.s.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown err = %v, want deadline exceeded", err)
	}
	other := validRequest("http://127.0.0.1:1/cb")
	other.ChangeRef = "iam:rev-2"
	if rec := postChange(t, h.s, tok, other.ChangeRef, other); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("intake after shutdown = %d", rec.Code)
	}
	if _, st := getStatus(t, h.s, tok, req.ChangeRef); st.Status != StatusAccepted {
		t.Fatalf("cancelled change status = %q", st.Status)
	}
}

func TestSleepContext(t *testing.T) {
	if err := sleepContext(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("sleep = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled sleep = %v", err)
	}
	if err := sleepContext(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("zero sleep on cancelled ctx = %v", err)
	}
}

// TestTwoStageFlowEndToEnd drives the whole contract over real HTTP: stage
// one mints a token, stage two submits a change with it, the signed callback
// verifies through webhook.Store.Receive, and the status reads GRANTED.
func TestTwoStageFlowEndToEnd(t *testing.T) {
	rc := newReceiver(t)
	sim := New(Config{
		ClientID: testClientID, ClientSecret: testClientSecret, WebhookSecret: []byte(testWebhookSecret),
		Scenario: &Scenario{Mode: ModeGrant, GrantDelayMS: 1, TokenTTLSeconds: 60},
	})
	srv := httptest.NewServer(sim)
	defer srv.Close()
	client := srv.Client()

	// Stage 1.
	treq, _ := http.NewRequest(http.MethodPost, srv.URL+"/oauth2/token", strings.NewReader("grant_type=client_credentials"))
	treq.Header.Set("Content-Type", formType)
	treq.SetBasicAuth(testClientID, testClientSecret)
	tresp, err := client.Do(treq)
	if err != nil {
		t.Fatal(err)
	}
	var tr TokenResponse
	if err := json.NewDecoder(tresp.Body).Decode(&tr); err != nil {
		t.Fatal(err)
	}
	tresp.Body.Close()
	if tresp.StatusCode != http.StatusOK || tr.TokenType != "Bearer" || tr.ExpiresIn != 60 || tr.Scope != "access.read access.write" {
		t.Fatalf("token = %d %+v", tresp.StatusCode, tr)
	}

	// Stage 2.
	body, _ := json.Marshal(validRequest(rc.url()))
	areq, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/access-changes", bytes.NewReader(body))
	areq.Header.Set("Content-Type", "application/json")
	areq.Header.Set("Authorization", "Bearer "+tr.AccessToken)
	areq.Header.Set("Idempotency-Key", "iam:rev-1")
	aresp, err := client.Do(areq)
	if err != nil {
		t.Fatal(err)
	}
	var ack acceptResponse
	_ = json.NewDecoder(aresp.Body).Decode(&ack)
	aresp.Body.Close()
	if aresp.StatusCode != http.StatusAccepted || ack.Status != StatusAccepted || ack.ChangeRef != "iam:rev-1" {
		t.Fatalf("intake = %d %+v", aresp.StatusCode, ack)
	}
	drain(t, sim)

	calls := rc.calls()
	if len(calls) != 1 || calls[0].err != nil {
		t.Fatalf("callbacks = %+v", calls)
	}
	if calls[0].receipt.SignatureState != "VALID" || calls[0].receipt.EventType != EventGranted {
		t.Fatalf("receipt = %+v", calls[0].receipt)
	}
	var ev ResultEvent
	if err := json.Unmarshal(calls[0].body, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.ProviderRef != ack.ProviderRef || ev.Outcome != StatusGranted || ev.JobCode != "SAL-DIR" || ev.Grade != "M4" {
		t.Fatalf("event = %+v", ev)
	}

	sreq, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/access-changes/"+url.PathEscape("iam:rev-1"), nil)
	sreq.Header.Set("Authorization", "Bearer "+tr.AccessToken)
	sresp, err := client.Do(sreq)
	if err != nil {
		t.Fatal(err)
	}
	var st ChangeStatus
	_ = json.NewDecoder(sresp.Body).Decode(&st)
	sresp.Body.Close()
	if sresp.StatusCode != http.StatusOK || st.Status != StatusGranted || !st.Callback.Delivered {
		t.Fatalf("status = %d %+v", sresp.StatusCode, st)
	}
}

// TestConcurrentSameKeyYieldsOneChange races 20 identical submissions: the
// idempotency check and insert share one critical section, so exactly one
// wins with 202, the rest replay with 200, and one callback goes out.
func TestConcurrentSameKeyYieldsOneChange(t *testing.T) {
	rc := newReceiver(t)
	h := newHarness(t, nil)
	tok := issueToken(t, h.s, "")
	req := validRequest(rc.url())
	var wg sync.WaitGroup
	codes := make([]int, 20)
	bodies := make([]string, 20)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := postChange(t, h.s, tok, req.ChangeRef, req)
			codes[i], bodies[i] = rec.Code, rec.Body.String()
		}()
	}
	wg.Wait()
	accepted, replayed := 0, 0
	for i, c := range codes {
		switch c {
		case http.StatusAccepted:
			accepted++
		case http.StatusOK:
			replayed++
		default:
			t.Fatalf("goroutine %d got %d", i, c)
		}
		if bodies[i] != bodies[0] {
			t.Fatalf("body %d differs: %s vs %s", i, bodies[i], bodies[0])
		}
	}
	if accepted != 1 || replayed != 19 {
		t.Fatalf("accepted=%d replayed=%d", accepted, replayed)
	}
	drain(t, h.s)
	h.s.mu.Lock()
	n := len(h.s.changes)
	h.s.mu.Unlock()
	if n != 1 {
		t.Fatalf("changes stored = %d", n)
	}
	if calls := rc.calls(); len(calls) != 1 {
		t.Fatalf("callbacks = %d, want 1", len(calls))
	}
}

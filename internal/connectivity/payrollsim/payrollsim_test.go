package payrollsim

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/webhook"
)

const (
	testSecret = "payrollsim-test-secret"
	testAPIKey = "payrollsim-test-api-key"
)

// fakeSleep records every requested wait and returns at once, so backoff
// schedules are asserted without being waited out. A wait of an hour or more
// is treated as "hold": it blocks until cancelled, which is how tests keep a
// change parked in ACCEPTED without ever calling out.
type fakeSleep struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (f *fakeSleep) sleep(ctx context.Context, d time.Duration) error {
	f.mu.Lock()
	f.waits = append(f.waits, d)
	f.mu.Unlock()
	if d >= holdDelay {
		<-ctx.Done()
	}
	return ctx.Err()
}

func (f *fakeSleep) recorded() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.waits...)
}

const holdDelay = time.Hour
const holdDelayMS = int64(holdDelay / time.Millisecond)

// newTestServer builds a simulator with zero apply delay and a fake sleep.
func newTestServer(t *testing.T, mutate func(*Config)) (*Server, *fakeSleep) {
	t.Helper()
	fs := &fakeSleep{}
	sc := DefaultScenario()
	sc.ApplyDelayMS = 0
	cfg := Config{Secret: []byte(testSecret), APIKey: testAPIKey, Scenario: &sc, Sleep: fs.sleep}
	if mutate != nil {
		mutate(&cfg)
	}
	s := New(cfg)
	t.Cleanup(func() {
		// Anything still parked is abandoned rather than waited for.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_ = s.Shutdown(ctx)
	})
	return s, fs
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

func validRequest(callback string) PayChangeRequest {
	return PayChangeRequest{
		ChangeRef:      "payroll:rev-1",
		Tenant:         "harborcare-demo",
		WorkerRef:      "worker-42",
		BasePay:        Money{Amount: "160000.00", Currency: "USD"},
		EffectiveDate:  "2026-12-01",
		CorrelationKey: "corr-abc",
		CallbackURL:    callback,
	}
}

func post(t *testing.T, h http.Handler, key string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return postAs(t, h, testAPIKey, key, body)
}

func postAs(t *testing.T, h http.Handler, apiKey, key string, body any) *httptest.ResponseRecorder {
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
	req := httptest.NewRequest(http.MethodPost, "/v1/pay-changes", bytes.NewReader(raw))
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

func getStatus(t *testing.T, h http.Handler, ref string) (int, ChangeStatus) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/pay-changes/"+url.PathEscape(ref), nil)
	req.Header.Set(APIKeyHeader, testAPIKey)
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
		ID:              "payrollsim",
		TenantID:        "harborcare-demo",
		ConnectionID:    "conn-payroll",
		Secret:          []byte(testSecret),
		AllowedSchemas:  []string{Schema},
		MaxPayloadBytes: MaxRequestBytes,
		ReplayWindow:    5 * time.Minute,
	}); err != nil {
		t.Fatal(err)
	}
	rc.srv = httptest.NewServer(http.HandlerFunc(rc.handle))
	t.Cleanup(rc.srv.Close)
	return rc
}

func (rc *receiver) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	ns, _ := strconv.ParseInt(r.Header.Get("Webhook-Timestamp"), 10, 64)
	receipt, err := rc.store.Receive(webhook.Request{
		EndpointID: "payrollsim",
		TenantID:   r.Header.Get("Webhook-Tenant"),
		EventID:    r.Header.Get("Webhook-Id"),
		EventType:  r.Header.Get("Webhook-Event"),
		Schema:     r.Header.Get("Webhook-Schema"),
		Timestamp:  time.Unix(0, ns),
		Signature:  r.Header.Get("Webhook-Signature"),
		Payload:    body,
	}, time.Now())
	rc.mu.Lock()
	rc.got = append(rc.got, receivedCallback{header: r.Header.Clone(), body: body, receipt: receipt, err: err})
	status := http.StatusOK
	if len(rc.script) > 0 {
		status, rc.script = rc.script[0], rc.script[1:]
	}
	rc.mu.Unlock()
	if err != nil && status == http.StatusOK {
		status = http.StatusUnauthorized
	}
	w.WriteHeader(status)
}

func (rc *receiver) calls() []receivedCallback {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return append([]receivedCallback(nil), rc.got...)
}

func (rc *receiver) url() string { return rc.srv.URL + "/integrations/payroll/v1/receipts" }

func TestStatusReflectsAppliedAndDelivery(t *testing.T) {
	rc := newReceiver(t)
	// A pinned clock near real time keeps applied_at assertable while the
	// signed timestamp stays inside the receiver's replay window.
	fixed := time.Now().UTC().Truncate(time.Second)
	s, _ := newTestServer(t, func(c *Config) { c.Now = func() time.Time { return fixed } })
	req := validRequest(rc.url())
	if rec := post(t, s, req.ChangeRef, req); rec.Code != http.StatusAccepted {
		t.Fatalf("intake = %d %s", rec.Code, rec.Body)
	}
	drain(t, s)
	code, st := getStatus(t, s, req.ChangeRef)
	if code != http.StatusOK {
		t.Fatalf("status code = %d", code)
	}
	want := ChangeStatus{ChangeRef: req.ChangeRef, ProviderRef: st.ProviderRef, Status: StatusApplied, AppliedAt: fixed.Format(time.RFC3339), Callback: CallbackStatus{Attempts: 1, Delivered: true, LastStatus: 200}}
	if st != want {
		t.Fatalf("status = %+v, want %+v", st, want)
	}
	if len(st.ProviderRef) != len("PSIM-")+12 || st.ProviderRef[:5] != "PSIM-" {
		t.Fatalf("provider_ref = %q", st.ProviderRef)
	}
}

func TestStatusUnknownIs404(t *testing.T) {
	s, _ := newTestServer(t, nil)
	if code, _ := getStatus(t, s, "payroll:nope"); code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", code)
	}
}

func TestShutdownRefusesNewIntakeButReplays(t *testing.T) {
	rc := newReceiver(t)
	s, _ := newTestServer(t, nil)
	req := validRequest(rc.url())
	first := post(t, s, req.ChangeRef, req)
	drain(t, s)
	if rec := post(t, s, req.ChangeRef, req); rec.Code != http.StatusOK || rec.Body.String() != first.Body.String() {
		t.Fatalf("replay after shutdown = %d %s", rec.Code, rec.Body)
	}
	other := validRequest(rc.url())
	other.ChangeRef = "payroll:rev-2"
	if rec := post(t, s, other.ChangeRef, other); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("new intake after shutdown = %d, want 503", rec.Code)
	}
}

func TestShutdownTimeoutCancelsInFlight(t *testing.T) {
	sc := DefaultScenario()
	sc.ApplyDelayMS = int64(time.Hour / time.Millisecond)
	s := New(Config{Secret: []byte(testSecret), APIKey: testAPIKey, Scenario: &sc})
	req := validRequest("http://127.0.0.1:1/cb")
	if rec := post(t, s, req.ChangeRef, req); rec.Code != http.StatusAccepted {
		t.Fatalf("intake = %d", rec.Code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := s.Shutdown(ctx); err != context.DeadlineExceeded {
		t.Fatalf("shutdown err = %v, want deadline exceeded", err)
	}
	if _, st := getStatus(t, s, req.ChangeRef); st.Status != StatusAccepted {
		t.Fatalf("cancelled change status = %q, want ACCEPTED", st.Status)
	}
}

func TestHealthz(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != `{"status":"ok"}` {
		t.Fatalf("healthz = %d %s", rec.Code, rec.Body)
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	s := New(Config{Secret: []byte("x")})
	if s.maxAttempts != 10 || s.backoffBase != time.Second || s.backoffMax != time.Minute {
		t.Fatalf("defaults = %d %v %v", s.maxAttempts, s.backoffBase, s.backoffMax)
	}
	if got := s.Scenario(); got != DefaultScenario() || got.ApplyDelayMS != 1500 || got.Mode != ModeApply {
		t.Fatalf("default scenario = %+v", got)
	}
	if err := sleepContext(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("sleepContext: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepContext(ctx, time.Hour); err == nil {
		t.Fatal("sleepContext ignored cancellation")
	}
}

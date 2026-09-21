package payrollsim

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

const (
	inboundTrace   = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	inboundTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	reversalTrace  = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
)

// lockedBuffer is a goroutine-safe log sink: the simulator logs from its
// background delivery goroutines.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// records returns every JSON log record whose msg is msg.
func (b *lockedBuffer) records(t *testing.T, msg string) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.buf.String()), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		if rec["msg"] == msg {
			out = append(out, rec)
		}
	}
	return out
}

func withJSONLog(buf *lockedBuffer) func(*Config) {
	return func(c *Config) { c.Logger = slog.New(slog.NewJSONHandler(buf, nil)) }
}

func serveWith(t *testing.T, h http.Handler, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(APIKeyHeader, testAPIKey)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func spanIDOf(t *testing.T, traceparent string) (traceID, spanID string) {
	t.Helper()
	parts := strings.Split(traceparent, "-")
	if len(parts) != 4 || parts[0] != "00" || len(parts[1]) != 32 || len(parts[2]) != 16 {
		t.Fatalf("callback traceparent %q is not W3C-shaped", traceparent)
	}
	return parts[1], parts[2]
}

func TestCallbackEchoesTraceAsChildAndCorrelationUnchanged(t *testing.T) {
	var logs lockedBuffer
	rc := newReceiver(t, http.StatusInternalServerError) // first attempt fails, retry succeeds
	s, _ := newTestServer(t, withJSONLog(&logs))
	req := validRequest(rc.url())
	rec := serveWith(t, s, http.MethodPost, "/v1/pay-changes", req, map[string]string{
		"Idempotency-Key": req.ChangeRef, "traceparent": inboundTrace, "X-Correlation-Id": "corr-hcm-1",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("intake = %d %s", rec.Code, rec.Body)
	}
	drain(t, s)

	calls := rc.calls()
	if len(calls) != 2 {
		t.Fatalf("callbacks = %d, want 2", len(calls))
	}
	seen := map[string]bool{}
	for i, c := range calls {
		tid, sid := spanIDOf(t, c.header.Get("traceparent"))
		if tid != inboundTraceID || !strings.HasSuffix(c.header.Get("traceparent"), "-01") {
			t.Fatalf("callback %d traceparent %q left the caller's trace", i, c.header.Get("traceparent"))
		}
		if sid == "00f067aa0ba902b7" || seen[sid] {
			t.Fatalf("callback %d reused a span id %q", i, sid)
		}
		seen[sid] = true
		if got := c.header.Get("X-Correlation-Id"); got != "corr-hcm-1" {
			t.Fatalf("callback %d X-Correlation-Id = %q", i, got)
		}
	}
	if calls[1].err != nil {
		t.Fatalf("echo headers broke signature verification: %v", calls[1].err)
	}

	accepted := logs.records(t, "intake accepted")
	if len(accepted) != 1 || accepted[0]["traceparent"] != inboundTrace || accepted[0]["correlation_id"] != "corr-hcm-1" {
		t.Fatalf("intake log = %v", accepted)
	}

	code := serveWith(t, s, http.MethodGet, "/v1/pay-changes/"+url.PathEscape(req.ChangeRef), nil, map[string]string{
		"traceparent": inboundTrace, "X-Correlation-Id": "corr-hcm-1",
	}).Code
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	status := logs.records(t, "status read")
	if len(status) != 1 || status[0]["traceparent"] != inboundTrace || status[0]["correlation_id"] != "corr-hcm-1" || status[0]["found"] != true {
		t.Fatalf("status log = %v", status)
	}
}

func TestMalformedInboundContextIsNeitherLoggedNorEchoed(t *testing.T) {
	var logs lockedBuffer
	rc := newReceiver(t)
	s, _ := newTestServer(t, withJSONLog(&logs))
	req := validRequest(rc.url())
	rec := serveWith(t, s, http.MethodPost, "/v1/pay-changes", req, map[string]string{
		"Idempotency-Key":  req.ChangeRef,
		"traceparent":      "00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01",
		"X-Correlation-Id": strings.Repeat("c", 129),
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("intake = %d", rec.Code)
	}
	drain(t, s)
	calls := rc.calls()
	if len(calls) != 1 {
		t.Fatalf("callbacks = %d", len(calls))
	}
	if calls[0].header.Get("traceparent") != "" || calls[0].header.Get("X-Correlation-Id") != "" {
		t.Fatalf("malformed context echoed: %v", calls[0].header)
	}
	for _, r := range logs.records(t, "intake accepted") {
		if _, ok := r["traceparent"]; ok {
			t.Fatalf("malformed traceparent logged: %v", r)
		}
		if _, ok := r["correlation_id"]; ok {
			t.Fatalf("oversized correlation id logged: %v", r)
		}
	}
}

func TestReversalCallbackEchoesReversalTraceWithIntakeCorrelationFallback(t *testing.T) {
	var logs lockedBuffer
	rc := newReceiver(t)
	s, _ := newTestServer(t, withJSONLog(&logs))
	req := validRequest(rc.url())
	if rec := serveWith(t, s, http.MethodPost, "/v1/pay-changes", req, map[string]string{
		"Idempotency-Key": req.ChangeRef, "traceparent": inboundTrace, "X-Correlation-Id": "corr-hcm-1",
	}); rec.Code != http.StatusAccepted {
		t.Fatalf("intake = %d", rec.Code)
	}
	waitStatus(t, s, req.ChangeRef, StatusApplied, 1)

	rec := serveWith(t, s, http.MethodPost, "/v1/pay-changes/"+url.PathEscape(req.ChangeRef)+"/reversal",
		map[string]string{"reason": "promotion withdrawn"},
		map[string]string{"Idempotency-Key": ReversalKeyPrefix + req.ChangeRef, "traceparent": reversalTrace})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("reversal = %d %s", rec.Code, rec.Body)
	}
	drain(t, s)

	calls := rc.calls()
	if len(calls) != 2 {
		t.Fatalf("callbacks = %d, want apply + reversal", len(calls))
	}
	rev := calls[1]
	if rev.header.Get("Webhook-Event") != EventReversed {
		t.Fatalf("second callback event = %q", rev.header.Get("Webhook-Event"))
	}
	if tid, _ := spanIDOf(t, rev.header.Get("traceparent")); tid != "0af7651916cd43dd8448eb211c80319c" {
		t.Fatalf("reversal callback trace = %q, want the reversal request's trace", tid)
	}
	if got := rev.header.Get("X-Correlation-Id"); got != "corr-hcm-1" {
		t.Fatalf("reversal callback correlation = %q, want the intake's", got)
	}
	accepted := logs.records(t, "reversal accepted")
	if len(accepted) != 1 || accepted[0]["traceparent"] != reversalTrace {
		t.Fatalf("reversal log = %v", accepted)
	}
	if _, ok := accepted[0]["correlation_id"]; ok {
		t.Fatalf("reversal log invented a correlation id the request did not carry: %v", accepted[0])
	}
}

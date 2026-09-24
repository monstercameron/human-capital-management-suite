package health

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHealthAndReadinessEndpointsDistinguishProcessLifeFromAdmissionReadiness(t *testing.T) {
	readyErr := errors.New("dependency detail must not be public")
	s := New(Dependencies{Live: func() bool { return true }, ReadyCheck: func(context.Context) error { return readyErr }, CheckInterval: time.Minute})
	live := httptest.NewRecorder()
	s.Healthz(live, httptest.NewRequest("GET", "/healthz", nil))
	ready := httptest.NewRecorder()
	s.Readyz(ready, httptest.NewRequest("GET", "/readyz", nil))
	if live.Code != 200 || ready.Code != 503 {
		t.Fatalf("healthz=%d readyz=%d", live.Code, ready.Code)
	}
	if body := ready.Body.String(); body != "{\"ok\":false}\n" {
		t.Fatalf("readiness body disclosed details: %q", body)
	}
}

// TestReadinessCheckIsBoundedAndCached counts calls atomically because
// isReady runs ReadyCheck on its own goroutine and abandons it when
// CheckTimeout expires (see health.go's select on ctx.Done()). The check
// goroutine is therefore still live when this test reads the counter, which a
// plain int made a data race that `go test -race` reported on Linux CI.
func TestReadinessCheckIsBoundedAndCached(t *testing.T) {
	var calls atomic.Int64
	started := make(chan struct{})
	s := New(Dependencies{CheckTimeout: 10 * time.Millisecond, CheckInterval: time.Minute, ReadyCheck: func(ctx context.Context) error {
		calls.Add(1)
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}})

	first := make(chan bool, 1)
	go func() { first <- s.isReady(context.Background()) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("readiness check did not start")
	}
	select {
	case ready := <-first:
		if ready {
			t.Fatal("readiness admitted after its configured check timeout")
		}
	case <-time.After(time.Second):
		t.Fatal("readiness check did not return after its configured timeout")
	}
	if s.isReady(context.Background()) {
		t.Fatal("cached failed readiness was admitted")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("readiness checks = %d, want one cached check", got)
	}
}

func TestTodo_EP_HEALTH_001_Property(t *testing.T) {
	var calls atomic.Int64
	s := New(Dependencies{ReadyCheck: func(context.Context) error { calls.Add(1); return nil }, CheckInterval: time.Minute})
	if !s.isReady(context.Background()) || !s.isReady(context.Background()) {
		t.Fatal("successful readiness result was not cached")
	}
	if calls.Load() != 1 {
		t.Fatalf("readiness calls = %d, want one", calls.Load())
	}
}
func TestTodo_EP_HEALTH_001_Golden(t *testing.T) {
	s := New(Dependencies{Live: func() bool { return true }, ReadyCheck: func(context.Context) error { return errors.New("private detail") }})
	ready := httptest.NewRecorder()
	s.Readyz(ready, httptest.NewRequest("GET", "/readyz", nil))
	if got, want := ready.Body.String(), "{\"ok\":false}\n"; got != want {
		t.Fatalf("readiness wire golden = %q, want %q", got, want)
	}
}
func TestTodo_EP_HEALTH_001_Race(t *testing.T) {
	var calls atomic.Int64
	s := New(Dependencies{ReadyCheck: func(context.Context) error { calls.Add(1); return nil }, CheckInterval: time.Minute})
	const workers = 16
	results := make(chan bool, workers)
	for i := 0; i < workers; i++ {
		go func() { results <- s.isReady(context.Background()) }()
	}
	for i := 0; i < workers; i++ {
		if !<-results {
			t.Error("concurrent readiness probe failed")
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("concurrent refreshes = %d, want one", got)
	}
}
func TestTodo_EP_HEALTH_001_Integration(t *testing.T) {
	s := New(Dependencies{Live: func() bool { return true }, ReadyCheck: func(context.Context) error { return nil }})
	live, ready := httptest.NewRecorder(), httptest.NewRecorder()
	s.Healthz(live, httptest.NewRequest("GET", "/healthz", nil))
	s.Readyz(ready, httptest.NewRequest("GET", "/readyz", nil))
	if live.Code != 200 || ready.Code != 200 || live.Body.String() != "{\"ok\":true}\n" || ready.Body.String() != "{\"ok\":true}\n" {
		t.Fatalf("health integration responses: live=%d %q ready=%d %q", live.Code, live.Body.String(), ready.Code, ready.Body.String())
	}
}
func TestTodo_EP_HEALTH_001_Fault(t *testing.T) {
	s := New(Dependencies{CheckTimeout: time.Millisecond, ReadyCheck: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }})
	if s.isReady(context.Background()) {
		t.Fatal("timed out check reported ready")
	}
}
func TestTodo_EP_HEALTH_001_Security(t *testing.T) {
	secret := errors.New("database password leaked")
	s := New(Dependencies{ReadyCheck: func(context.Context) error { return secret }})
	ready := httptest.NewRecorder()
	s.Readyz(ready, httptest.NewRequest("GET", "/readyz", nil))
	if strings.Contains(ready.Body.String(), secret.Error()) || ready.Body.String() != "{\"ok\":false}\n" {
		t.Fatalf("readiness exposed internal error: %q", ready.Body.String())
	}
}
func TestTodo_EP_HEALTH_001_Conformance(t *testing.T) {
	s := New(Dependencies{Live: func() bool { return true }, ReadyCheck: func(context.Context) error { return errors.New("dependency down") }})
	live, ready := httptest.NewRecorder(), httptest.NewRecorder()
	s.Healthz(live, httptest.NewRequest("GET", "/healthz", nil))
	s.Readyz(ready, httptest.NewRequest("GET", "/readyz", nil))
	if live.Code != 200 || ready.Code != 503 {
		t.Fatalf("liveness/readiness status = %d/%d, want 200/503", live.Code, ready.Code)
	}
}
func BenchmarkTodo_EP_HEALTH_001(b *testing.B) {
	s := New(Dependencies{})
	req := httptest.NewRequest("GET", "/healthz", nil)
	for i := 0; i < b.N; i++ {
		s.Healthz(httptest.NewRecorder(), req)
	}
}

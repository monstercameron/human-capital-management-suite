package main

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/productquery"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func timerScheduler(d time.Duration, fire func()) debounceTimer { return time.AfterFunc(d, fire) }

func shellHint(t *testing.T, tenant values.TenantId, projection string, seq, revision uint64) []byte {
	t.Helper()
	raw, err := productquery.InvalidationMessage{
		ContractVersion: 1, Tenant: tenant, Projection: projection, SourceSequence: seq, Watermark: seq - 1,
		Items: []productquery.InvalidationItem{{Subject: promotion.ShellCountSubject(tenant), Revision: revision}},
	}.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// manualScheduler holds debounce windows open until the test fires them, so
// "these hints shared one window" is an observation, not a race.
type manualScheduler struct {
	mu      sync.Mutex
	pending []func()
}

type manualTimer struct{}

func (manualTimer) Stop() bool { return false }

func (m *manualScheduler) schedule(_ time.Duration, fire func()) debounceTimer {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending = append(m.pending, fire)
	return manualTimer{}
}

func (m *manualScheduler) windows() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.pending)
}

func (m *manualScheduler) fireAll() {
	m.mu.Lock()
	pending := m.pending
	m.pending = nil
	m.mu.Unlock()
	for _, fire := range pending {
		fire()
	}
}

// scriptedStream replays frames, then waits for release (or its context)
// and ends the way the server's ceiling or a dropped socket does.
type scriptedStream struct {
	ctx     context.Context
	mu      sync.Mutex
	frames  [][]byte
	release chan struct{}
	end     error
	drained atomic.Bool
}

func (s *scriptedStream) Recv() (*journeyv1.WatchPromotionInvalidationsResponse, error) {
	s.mu.Lock()
	if len(s.frames) > 0 {
		frame := s.frames[0]
		s.frames = s.frames[1:]
		s.mu.Unlock()
		return &journeyv1.WatchPromotionInvalidationsResponse{Invalidation: frame}, nil
	}
	s.mu.Unlock()
	s.drained.Store(true)
	if s.release != nil {
		select {
		case <-s.release:
		case <-s.ctx.Done():
			return nil, s.ctx.Err()
		}
	}
	return nil, s.end
}

type scriptedService struct {
	mu      sync.Mutex
	streams []*scriptedStream
	opened  []*journeyv1.WatchPromotionInvalidationsRequest
}

func (s *scriptedService) WatchPromotionInvalidations(ctx context.Context, in *journeyv1.WatchPromotionInvalidationsRequest) (journeyclient.InvalidationStream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opened = append(s.opened, in)
	index := len(s.opened) - 1
	if index >= len(s.streams) {
		index = len(s.streams) - 1
	}
	stream := s.streams[index]
	stream.ctx = ctx
	return stream, nil
}

func (s *scriptedService) requests() []*journeyv1.WatchPromotionInvalidationsRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*journeyv1.WatchPromotionInvalidationsRequest(nil), s.opened...)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestTodo_REV_091_03 drives the product shell's live-update loop against a
// scripted server: admitted hints inside one window become one quiet
// refresh; hints outside the authorized scope join nothing; a reconnect
// resumes numbering after the last committed sequence and re-reads once to
// catch up on anything missed while disconnected.
func TestTodo_REV_091_03(t *testing.T) {
	tenant := values.TenantId("acme")
	scope, err := productInvalidationScope(string(tenant))
	if err != nil {
		t.Fatal(err)
	}
	var refreshes atomic.Int64
	clock := &manualScheduler{}
	coalescer := newRefreshCoalescer(clock.schedule, productInvalidationDebounce, func() error {
		refreshes.Add(1)
		return nil
	})
	first := &scriptedStream{end: io.EOF, release: make(chan struct{}), frames: [][]byte{
		shellHint(t, tenant, scope.Projection, 1, 10),
		shellHint(t, tenant, scope.Projection, 2, 11),
		// A foreign tenant's hint and another region's hint are refused by
		// the client's scope before any read.
		shellHint(t, values.TenantId("globex"), scope.Projection, 3, 12),
		shellHint(t, tenant, "promotion_detail", 3, 12),
	}}
	second := &scriptedStream{end: io.EOF, release: make(chan struct{}), frames: [][]byte{shellHint(t, tenant, scope.Projection, 3, 13)}}
	service := &scriptedService{streams: []*scriptedStream{first, second}}
	runner := newProductInvalidationRunner(scope, service, coalescer)
	runner.reconnect.InitialBackoff = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.run(ctx) }()

	waitFor(t, "the first stream to be read", first.drained.Load)
	if clock.windows() != 1 || refreshes.Load() != 0 {
		t.Fatalf("two admitted hints opened %d windows and ran %d refreshes, want one pending window", clock.windows(), refreshes.Load())
	}
	clock.fireAll()
	if refreshes.Load() != 1 {
		t.Fatalf("refreshes after the first window = %d", refreshes.Load())
	}

	close(first.release)
	waitFor(t, "the reconnect to be read", second.drained.Load)
	requests := service.requests()
	if requests[0].GetRegion() != string(promotion.RegionShellCount) || requests[0].GetAfterSequence() != 0 {
		t.Fatalf("first open = %+v", requests[0])
	}
	if len(requests) != 2 || requests[1].GetAfterSequence() != 2 {
		t.Fatalf("reconnect requests = %+v, want one resuming after 2 (the last committed sequence)", requests)
	}
	if clock.windows() != 1 {
		t.Fatalf("the reconnect catch-up and its hint opened %d windows, want one", clock.windows())
	}
	clock.fireAll()
	if refreshes.Load() != 2 {
		t.Fatalf("refreshes = %d, want 2", refreshes.Load())
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("run ended with %v", err)
	}
}

func TestRefreshCoalescerMergesAWindow(t *testing.T) {
	var calls atomic.Int64
	want := errors.New("read failed")
	coalescer := newRefreshCoalescer(timerScheduler, 30*time.Millisecond, func() error {
		calls.Add(1)
		return want
	})
	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = coalescer.Trigger(context.Background())
		}(i)
	}
	wg.Wait()
	if calls.Load() != 1 || coalescer.Fired() != 1 {
		t.Fatalf("five hints in one window ran %d refreshes", calls.Load())
	}
	for _, err := range errs {
		if !errors.Is(err, want) {
			t.Fatalf("waiter got %v, want the refresh's own result", err)
		}
	}
	if err := coalescer.Trigger(context.Background()); !errors.Is(err, want) || calls.Load() != 2 {
		t.Fatalf("a later hint did not start a new window (%v, %d)", err, calls.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := coalescer.Trigger(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled trigger = %v", err)
	}
	var nilCoalescer *refreshCoalescer
	if err := nilCoalescer.Trigger(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// A viewer the server refuses outright stops subscribing instead of
// retrying forever.
func TestProductInvalidationRunnerStopsOnRefusal(t *testing.T) {
	scope, _ := productInvalidationScope("acme")
	refused := status.Error(codes.PermissionDenied, "no access")
	service := &scriptedService{streams: []*scriptedStream{{end: refused}}}
	runner := newProductInvalidationRunner(scope, service, newRefreshCoalescer(timerScheduler, time.Millisecond, func() error { return nil }))
	runner.reconnect.InitialBackoff = time.Millisecond
	runner.rest = time.Hour
	done := make(chan error, 1)
	go func() { done <- runner.run(context.Background()) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a refused viewer kept subscribing")
	}
	if !terminalInvalidationStatus(refused) || terminalInvalidationStatus(status.Error(codes.Unavailable, "down")) {
		t.Fatal("terminal status classification is wrong")
	}
	if _, err := productInvalidationScope(""); err == nil {
		t.Fatal("an empty tenant produced a scope")
	}
}

func TestRefreshCoalescerScheduleJoinsTheWindow(t *testing.T) {
	var calls atomic.Int64
	coalescer := newRefreshCoalescer(timerScheduler, 30*time.Millisecond, func() error { calls.Add(1); return nil })
	for range 4 {
		coalescer.Schedule()
	}
	if err := coalescer.Trigger(context.Background()); err != nil || calls.Load() != 1 {
		t.Fatalf("four scheduled hints and a waiter ran %d refreshes (%v)", calls.Load(), err)
	}
	var nilCoalescer *refreshCoalescer
	nilCoalescer.Schedule()
}

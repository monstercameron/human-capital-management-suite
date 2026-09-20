package main

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/invalidation"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// REV-091-03: the product shell's live-update subscription. The server
// streams authority-filtered promotion invalidation hints
// (WatchPromotionInvalidations); tools/uxqual/invalidation admits them
// against the shell's authorized scope, and each admitted hint -- coalesced
// with any that arrive within the debounce window -- triggers one quiet
// re-read of the affected summaries through the ordinary authorized RPCs.
// A hint never carries display data.
//
// Everything here is DOM-free so it runs under the native test runner; the
// browser composition (product_invalidation_wasm.go) supplies the refresh
// action and the timer.

const (
	// productInvalidationDebounce is how long the shell waits after a hint
	// for others to arrive before re-reading. A burst of transitions (an
	// approval that also completes a workflow step) becomes one read.
	productInvalidationDebounce = 400 * time.Millisecond
	// productInvalidationRest is the pause after the reconnect budget is
	// spent before the subscription tries again from its last checkpoint.
	productInvalidationRest = 30 * time.Second
)

// productInvalidationRegion is the one region the product shell subscribes
// to. Every transition a viewer may see produces a shell-count hint, so this
// one subscription covers the shell badge, Home, My Work, Person and the
// Journeys list; the open detail keeps its own WatchJourney stream.
const productInvalidationRegion = promotion.RegionShellCount

// productInvalidationScope is the authorized allow-list the client admits
// hints against: the viewer's own tenant and the shell-count subject, which
// is derived from the tenant alone.
func productInvalidationScope(tenant string) (invalidation.Scope, error) {
	id := values.TenantId(tenant)
	projection, err := productInvalidationRegion.Projection()
	if err != nil {
		return invalidation.Scope{}, err
	}
	scope := invalidation.Scope{Tenant: id, Projection: projection, Subjects: []values.EntityRef{promotion.ShellCountSubject(id)}}
	if err := scope.Validate(); err != nil {
		return invalidation.Scope{}, err
	}
	return scope, nil
}

// refreshCoalescer turns any number of hints inside one window into one
// refresh. The window starts at the first hint and is not extended by later
// ones, so a steady stream of hints still refreshes at a bounded rate
// instead of never.
type refreshCoalescer struct {
	schedule debounceScheduler
	delay    time.Duration
	refresh  func() error

	mu      sync.Mutex
	timer   debounceTimer
	waiters []chan error
	fired   uint64
}

func newRefreshCoalescer(schedule debounceScheduler, delay time.Duration, refresh func() error) *refreshCoalescer {
	return &refreshCoalescer{schedule: schedule, delay: delay, refresh: refresh}
}

// Trigger joins the pending refresh (scheduling one if none is pending) and
// waits for its result or for ctx to end.
func (c *refreshCoalescer) Trigger(ctx context.Context) error {
	if c == nil || c.schedule == nil || c.refresh == nil {
		return nil
	}
	result := make(chan error, 1)
	c.mu.Lock()
	c.waiters = append(c.waiters, result)
	if c.timer == nil {
		c.timer = c.schedule(c.delay, c.fire)
	}
	c.mu.Unlock()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Schedule joins the pending refresh without waiting for it. A live hint
// uses this so the subscription keeps reading: every hint that arrives
// before the window closes joins the same single refresh, which re-reads
// after all of them have committed.
func (c *refreshCoalescer) Schedule() {
	if c == nil || c.schedule == nil || c.refresh == nil {
		return
	}
	c.mu.Lock()
	if c.timer == nil {
		c.timer = c.schedule(c.delay, c.fire)
	}
	c.mu.Unlock()
}

func (c *refreshCoalescer) fire() {
	c.mu.Lock()
	waiters := c.waiters
	c.waiters = nil
	c.timer = nil
	c.fired++
	c.mu.Unlock()
	err := c.refresh()
	for _, waiter := range waiters {
		waiter <- err
	}
}

// Fired reports how many refreshes have run.
func (c *refreshCoalescer) Fired() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fired
}

// invalidationStreamAdapter is one WatchPromotionInvalidations call as the
// closeable byte stream tools/uxqual/invalidation reads. Close cancels the
// call's own context, which is what makes a blocked Recv return.
type invalidationStreamAdapter struct {
	stream journeyclient.InvalidationStream
	cancel context.CancelFunc
	fatal  *atomic.Bool
}

func (s *invalidationStreamAdapter) Recv() ([]byte, error) {
	msg, err := s.stream.Recv()
	if err != nil {
		if terminalInvalidationStatus(err) && s.fatal != nil {
			s.fatal.Store(true)
		}
		return nil, err
	}
	return msg.GetInvalidation(), nil
}

func (s *invalidationStreamAdapter) Close() error {
	s.cancel()
	return nil
}

// terminalInvalidationStatus reports a refusal no reconnect can cure: the
// viewer may not subscribe, or this server does not offer the stream.
func terminalInvalidationStatus(err error) bool {
	switch status.Code(err) {
	case codes.PermissionDenied, codes.Unauthenticated, codes.Unimplemented, codes.InvalidArgument:
		return true
	}
	return false
}

// productInvalidationRunner owns the subscription for the page's lifetime.
type productInvalidationRunner struct {
	scope     invalidation.Scope
	service   journeyclient.InvalidationService
	coalescer *refreshCoalescer
	reconnect invalidation.ReconnectOptions
	rest      time.Duration

	fatal atomic.Bool
	opens atomic.Uint64
}

func newProductInvalidationRunner(scope invalidation.Scope, service journeyclient.InvalidationService, coalescer *refreshCoalescer) *productInvalidationRunner {
	return &productInvalidationRunner{
		scope: scope, service: service, coalescer: coalescer, rest: productInvalidationRest,
		reconnect: invalidation.ReconnectOptions{MaxAttempts: 6, InitialBackoff: 500 * time.Millisecond, MaxBackoff: time.Second},
	}
}

// open starts one stream resuming after the client's committed checkpoint.
// Every open after the first also schedules one catch-up refresh: hints
// committed while the stream was down were never numbered for this viewer,
// so only an authoritative re-read can recover them.
func (r *productInvalidationRunner) open(ctx context.Context, cursor invalidation.Cursor) (invalidation.CloseStream, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := r.service.WatchPromotionInvalidations(streamCtx, &journeyv1.WatchPromotionInvalidationsRequest{
		Region: string(productInvalidationRegion), AfterSequence: cursor.Sequence(),
	})
	if err != nil {
		cancel()
		if terminalInvalidationStatus(err) {
			r.fatal.Store(true)
		}
		return nil, err
	}
	if r.opens.Add(1) > 1 {
		r.coalescer.Schedule()
	}
	return &invalidationStreamAdapter{stream: stream, cancel: cancel, fatal: &r.fatal}, nil
}

// catchUp re-reads authoritatively for a sequence gap and reports the gap
// closed at exactly the requested position.
func (r *productInvalidationRunner) catchUp(ctx context.Context, request invalidation.CatchUpRequest) (invalidation.CatchUpResult, error) {
	if err := r.coalescer.Trigger(ctx); err != nil {
		return invalidation.CatchUpResult{}, err
	}
	return invalidation.CatchUpResult{SourceSequence: request.ToSequence, Watermark: request.From.Watermark()}, nil
}

// run subscribes until ctx ends or the server refuses the viewer outright.
// When the reconnect budget is spent it rests and resumes from the same
// checkpoint, so a long outage costs one attempt per rest period, not a
// tight loop.
func (r *productInvalidationRunner) run(ctx context.Context) error {
	client, err := invalidation.New(r.scope, func(context.Context, invalidation.Refresh) error {
		r.coalescer.Schedule()
		return nil
	}, invalidation.Options{})
	if err != nil {
		return err
	}
	for {
		err := client.RunReconnect(ctx, r.open, r.catchUp, r.reconnect)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if r.fatal.Load() {
			return err
		}
		if !sleepContext(ctx, r.rest) {
			return ctx.Err()
		}
	}
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

package retrypolicy

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestBreaker(clock *fakeClock) *Breaker {
	return NewBreaker(BreakerConfig{FailureThreshold: 3, Cooldown: 10 * time.Second, MaxCooldown: 35 * time.Second, Now: clock.Now})
}

func trip(t *testing.T, b *Breaker, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if ok, _ := b.Allow(); !ok {
			t.Fatalf("allow %d refused before trip", i)
		}
		b.Record(false)
	}
}

func TestBreaker_Defaults(t *testing.T) {
	b := NewBreaker(BreakerConfig{})
	if b.threshold != 5 || b.baseCool != 30*time.Second || b.maxCool != 10*time.Minute || b.now == nil {
		t.Fatalf("defaults = %d %v %v", b.threshold, b.baseCool, b.maxCool)
	}
	if b.State() != StateClosed {
		t.Fatalf("initial state %q", b.State())
	}
}

func TestBreaker_ThresholdTripAndCooldown(t *testing.T) {
	clock := newFakeClock()
	b := newTestBreaker(clock)
	trip(t, b, 2)
	if b.State() != StateClosed {
		t.Fatalf("state after 2 failures = %q, want closed", b.State())
	}
	trip(t, b, 1)
	if b.State() != StateOpen {
		t.Fatalf("state after 3 failures = %q, want open", b.State())
	}
	ok, retryAt := b.Allow()
	if ok || !retryAt.Equal(clock.Now().Add(10*time.Second)) {
		t.Fatalf("open allow = (%v, %v)", ok, retryAt)
	}
	clock.Advance(9 * time.Second)
	if ok, _ := b.Allow(); ok {
		t.Fatal("allowed before cooldown elapsed")
	}
	clock.Advance(time.Second)
	if ok, _ := b.Allow(); !ok {
		t.Fatal("probe refused after cooldown")
	}
	if b.State() != StateHalfOpen {
		t.Fatalf("state = %q, want half_open", b.State())
	}
}

func TestBreaker_SuccessResetsCount(t *testing.T) {
	clock := newFakeClock()
	b := newTestBreaker(clock)
	trip(t, b, 2)
	b.Record(true)
	trip(t, b, 2)
	if b.State() != StateClosed {
		t.Fatalf("interleaved success did not reset count: %q", b.State())
	}
}

func TestBreaker_SingleProbeUnderConcurrency(t *testing.T) {
	clock := newFakeClock()
	b := newTestBreaker(clock)
	trip(t, b, 3)
	clock.Advance(10 * time.Second)
	var granted atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if ok, _ := b.Allow(); ok {
				granted.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if granted.Load() != 1 {
		t.Fatalf("%d probes granted, want exactly 1", granted.Load())
	}
	ok, retryAt := b.Allow()
	if ok || retryAt.IsZero() {
		t.Fatalf("non-probe caller got (%v, %v)", ok, retryAt)
	}
}

func TestBreaker_ProbeSuccessCloses(t *testing.T) {
	clock := newFakeClock()
	b := newTestBreaker(clock)
	trip(t, b, 3)
	clock.Advance(10 * time.Second)
	b.Allow()
	b.Record(true)
	if b.State() != StateClosed {
		t.Fatalf("state = %q, want closed", b.State())
	}
	if b.Cooldown() != 10*time.Second {
		t.Fatalf("cooldown not reset: %v", b.Cooldown())
	}
	trip(t, b, 2)
	if b.State() != StateClosed {
		t.Fatal("count not reset by probe success")
	}
}

func TestBreaker_ProbeFailureDoublesCooldownToMax(t *testing.T) {
	clock := newFakeClock()
	b := newTestBreaker(clock)
	trip(t, b, 3)
	want := []time.Duration{20 * time.Second, 35 * time.Second, 35 * time.Second}
	cool := 10 * time.Second
	for i, w := range want {
		clock.Advance(cool)
		if ok, _ := b.Allow(); !ok {
			t.Fatalf("round %d: probe refused", i)
		}
		b.Record(false)
		if b.State() != StateOpen || b.Cooldown() != w {
			t.Fatalf("round %d: state %q cooldown %v, want open %v", i, b.State(), b.Cooldown(), w)
		}
		ok, retryAt := b.Allow()
		if ok || !retryAt.Equal(clock.Now().Add(w)) {
			t.Fatalf("round %d: allow = (%v, %v)", i, ok, retryAt)
		}
		clock.Advance(w - time.Nanosecond)
		if ok, _ := b.Allow(); ok {
			t.Fatalf("round %d: allowed before doubled cooldown", i)
		}
		clock.Advance(-(w - time.Nanosecond))
		cool = w
	}
	// Success after the doubled cooldowns resets back to the base cooldown.
	clock.Advance(cool)
	b.Allow()
	b.Record(true)
	trip(t, b, 3)
	if b.Cooldown() != 10*time.Second {
		t.Fatalf("cooldown after recovery = %v, want base", b.Cooldown())
	}
}

func TestBreaker_StaleReportsWhileOpenIgnored(t *testing.T) {
	clock := newFakeClock()
	b := newTestBreaker(clock)
	trip(t, b, 3)
	b.Record(true)
	b.Record(false)
	if b.State() != StateOpen || b.Cooldown() != 10*time.Second {
		t.Fatalf("stale report changed open breaker: %q %v", b.State(), b.Cooldown())
	}
}

func TestBreaker_LostProbeIsReplaced(t *testing.T) {
	clock := newFakeClock()
	b := newTestBreaker(clock)
	trip(t, b, 3)
	clock.Advance(10 * time.Second)
	if ok, _ := b.Allow(); !ok {
		t.Fatal("probe refused")
	}
	clock.Advance(5 * time.Second)
	if ok, _ := b.Allow(); ok {
		t.Fatal("second probe granted while first outstanding")
	}
	clock.Advance(5 * time.Second)
	if ok, _ := b.Allow(); !ok {
		t.Fatal("abandoned probe was never replaced")
	}
}

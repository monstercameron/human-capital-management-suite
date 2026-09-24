package synctestkit_test

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/quality/synctestkit"
)

// TestSynctestQualificationAdvancesTimersAndDetectsQuiescenceWithoutSleep
// is the TOOL-021 primary test. It proves, against the real
// testing/synctest package (not a description of it), that:
//
//   - a virtual timer inside a synctest bubble advances the bubble's fake
//     clock exactly, without pausing the real test process (a 2h virtual
//     Sleep and a 90s virtual lease-loop scenario both complete in well
//     under real-world seconds);
//   - synctest.Wait() detects goroutine quiescence (every other goroutine
//     durably blocked) without itself advancing time - only a
//     genuinely blocking wait (here, receiving from a channel only the
//     sleeping goroutine can close) does that; and
//   - the same virtual-clock machinery drives synctestkit.RunLeaseLoop,
//     the fixture shaped like the outbox consumer's lease loop, through
//     several renewal cycles with an exact, checkable elapsed duration.
func TestSynctestQualificationAdvancesTimersAndDetectsQuiescenceWithoutSleep(t *testing.T) {
	t.Run("bare_timer_and_quiescence", func(t *testing.T) {
		wallStart := time.Now()

		synctest.Test(t, func(t *testing.T) {
			virtualStart := time.Now()

			done := make(chan struct{})
			go func() {
				time.Sleep(2 * time.Hour)
				close(done)
			}()

			select {
			case <-done:
				t.Fatal("done closed before any virtual time elapsed")
			default:
			}

			// Wait blocks until every other goroutine in the bubble is
			// durably blocked. The sleeping goroutine is durably blocked
			// on its timer almost immediately, so Wait returns quickly -
			// but it must not itself have advanced the virtual clock past
			// the 2h sleep.
			synctest.Wait()

			select {
			case <-done:
				t.Fatal("synctest.Wait() must not itself advance the virtual clock")
			default:
			}

			// Now the calling goroutine durably blocks too (on a channel
			// only the sibling goroutine can close): with every goroutine
			// in the bubble durably blocked, the runtime is free to jump
			// the virtual clock straight to the next pending timer.
			<-done

			if elapsed := time.Since(virtualStart); elapsed != 2*time.Hour {
				t.Fatalf("virtual elapsed = %v, want exactly 2h", elapsed)
			}
		})

		if wallElapsed := time.Since(wallStart); wallElapsed > 5*time.Second {
			t.Fatalf("synctest.Test wall-clock elapsed = %v; a 2h virtual sleep must run in well under 2h of real time (want < 5s)", wallElapsed)
		}
	})

	t.Run("lease_loop_fixture_advances_deterministically_without_sleep", func(t *testing.T) {
		wallStart := time.Now()

		synctest.Test(t, func(t *testing.T) {
			store := &synctestkit.FakeLeaseStore{}
			ctx, cancel := context.WithCancel(context.Background())
			events := make(chan synctestkit.Event)
			done := make(chan struct{})
			virtualStart := time.Now()

			go func() {
				synctestkit.RunLeaseLoop(ctx, store, "L", 30*time.Second, 5*time.Second, events)
				close(done)
			}()

			if ev := <-events; ev.Kind != synctestkit.EventAcquired {
				t.Fatalf("first event = %v, want Acquired", ev.Kind)
			}
			for i := 0; i < 3; i++ {
				if ev := <-events; ev.Kind != synctestkit.EventRenewed {
					t.Fatalf("renewal %d = %v, want Renewed", i, ev.Kind)
				}
			}

			cancel()
			if ev := <-events; ev.Kind != synctestkit.EventReleased {
				t.Fatalf("final event = %v, want Released", ev.Kind)
			}
			<-done

			if want, got := 3*30*time.Second, time.Since(virtualStart); got != want {
				t.Fatalf("virtual elapsed = %v, want exactly %v", got, want)
			}
		})

		if wallElapsed := time.Since(wallStart); wallElapsed > 5*time.Second {
			t.Fatalf("lease loop fixture wall-clock elapsed = %v, want < 5s for 90s of virtual time", wallElapsed)
		}
	})
}

// TestTodo_TOOL_021_Property drives synctestkit.RunLeaseLoop through many
// pseudo-randomly generated (renewEvery, failure-count, recovery-count)
// combinations and checks two invariants hold exactly for every one of
// them: the emitted event sequence has the canonical
// (Acquired,Lost)*failures, Acquired, Renewed*recoveries shape, and the
// virtual elapsed time equals exactly (failures+recoveries)*renewEvery -
// no drift, regardless of parameters. The seed is fixed so the case set
// (and any failure) is reproducible.
func TestTodo_TOOL_021_Property(t *testing.T) {
	type testCase struct {
		renewEvery time.Duration
		failures   int
		recoveries int
	}

	rng := rand.New(rand.NewSource(20260903))
	var cases []testCase
	for i := 0; i < 25; i++ {
		cases = append(cases, testCase{
			renewEvery: time.Duration(1+rng.Intn(10)) * time.Second,
			failures:   rng.Intn(4),
			recoveries: 1 + rng.Intn(4),
		})
	}

	for i, tc := range cases {
		t.Run(fmt.Sprintf("case_%d_renew=%s_failures=%d_recoveries=%d", i, tc.renewEvery, tc.failures, tc.recoveries), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				store := &synctestkit.FakeLeaseStore{}
				if tc.failures > 0 {
					store.ForceExpireNext(tc.failures)
				}

				ctx, cancel := context.WithCancel(context.Background())
				events := make(chan synctestkit.Event)
				done := make(chan struct{})
				start := time.Now()

				go func() {
					synctestkit.RunLeaseLoop(ctx, store, "L", tc.renewEvery, time.Second, events)
					close(done)
				}()

				var wantSeq []synctestkit.EventKind
				for x := 0; x < tc.failures; x++ {
					wantSeq = append(wantSeq, synctestkit.EventAcquired, synctestkit.EventLost)
				}
				wantSeq = append(wantSeq, synctestkit.EventAcquired)
				for x := 0; x < tc.recoveries; x++ {
					wantSeq = append(wantSeq, synctestkit.EventRenewed)
				}

				gotSeq := make([]synctestkit.EventKind, 0, len(wantSeq))
				for range wantSeq {
					gotSeq = append(gotSeq, (<-events).Kind)
				}
				if !reflect.DeepEqual(gotSeq, wantSeq) {
					t.Fatalf("event sequence = %v, want %v", gotSeq, wantSeq)
				}

				wantElapsed := time.Duration(tc.failures+tc.recoveries) * tc.renewEvery
				if elapsed := time.Since(start); elapsed != wantElapsed {
					t.Fatalf("elapsed = %v, want exactly %v", elapsed, wantElapsed)
				}

				cancel()
				if ev := (<-events).Kind; ev != synctestkit.EventReleased {
					t.Fatalf("final event = %v, want Released", ev)
				}
				<-done // no goroutine leak
			})
		})
	}
}

// TestTodo_TOOL_021_Race runs two RunLeaseLoop instances concurrently
// against one shared FakeLeaseStore, contesting the same single exclusive
// slot under two distinct identity tokens ("holder-1"/"holder-2" - using
// the same literal ID for both would let each loop's own reentry check
// ("is the current holder me?") accidentally admit the other, since
// FakeLeaseStore has no separate notion of caller identity beyond the ID
// string). An atomics-based invariant probe (raceProbe, below) would flag
// it if FakeLeaseStore's mutex ever let both loops believe they held the
// lease at once. This is the "race-free fake-time pattern" TOOL-021 asks
// for: real concurrent goroutines, a virtual clock, and `go test -race`
// able to certify the fixture's own synchronization (the probe itself
// uses only atomics, so it never introduces a race of its own for the
// detector to flag instead).
func TestTodo_TOOL_021_Race(t *testing.T) {
	// The outer workers contend on the same fixture from real goroutines;
	// synctest below separately checks virtual-time lease-loop behavior.
	store := &synctestkit.FakeLeaseStore{}
	start := make(chan struct{})
	results := make(chan int32, 2)
	var held, violated int32
	for _, id := range []string{"worker-1", "worker-2"} {
		go func(id string) {
			<-start
			var violations int32
			for i := 0; i < 1000; i++ {
				if store.TryAcquire(id) {
					if atomic.AddInt32(&held, 1) > 1 {
						atomic.StoreInt32(&violated, 1)
						violations++
					}
					store.Release(id)
					atomic.AddInt32(&held, -1)
				}
			}
			results <- violations
		}(id)
	}
	close(start)
	var totalViolations int32
	for i := 0; i < 2; i++ {
		totalViolations += <-results
	}
	if totalViolations != 0 || atomic.LoadInt32(&violated) != 0 {
		t.Fatalf("shared lease store admitted overlapping holders: violations=%d", totalViolations)
	}

	synctest.Test(t, func(t *testing.T) {
		store := &synctestkit.FakeLeaseStore{}
		var holding, violated int32

		wrap := func() synctestkit.LeaseStore {
			return &raceProbe{inner: store, holding: &holding, violated: &violated}
		}

		ctx, cancel := context.WithCancel(context.Background())
		events := make(chan synctestkit.Event)
		done1 := make(chan struct{})
		done2 := make(chan struct{})

		go func() {
			synctestkit.RunLeaseLoop(ctx, wrap(), "holder-1", 10*time.Millisecond, 3*time.Millisecond, events)
			close(done1)
		}()
		go func() {
			synctestkit.RunLeaseLoop(ctx, wrap(), "holder-2", 7*time.Millisecond, 5*time.Millisecond, events)
			close(done2)
		}()

		const totalEvents = 60
		for i := 0; i < totalEvents; i++ {
			<-events
		}
		cancel()

		d1closed, d2closed := false, false
		for !d1closed || !d2closed {
			select {
			case <-events:
			case <-done1:
				d1closed = true
			case <-done2:
				d2closed = true
			}
		}

		if atomic.LoadInt32(&violated) != 0 {
			t.Fatal("more than one concurrent holder of the same lease id was observed")
		}
	})
}

// raceProbe is defined here (rather than in helpers_test.go) because it
// implements synctestkit.LeaseStore identically to countingStore but is
// exercised only by this test; kept adjacent to its one caller for
// readability. It counts current holders with atomics so it is itself
// race-free under concurrent use by two RunLeaseLoop goroutines.
type raceProbe struct {
	inner    synctestkit.LeaseStore
	holding  *int32
	violated *int32
}

func (p *raceProbe) TryAcquire(id string) bool {
	ok := p.inner.TryAcquire(id)
	if ok {
		if n := atomic.AddInt32(p.holding, 1); n > 1 {
			atomic.StoreInt32(p.violated, 1)
		}
	}
	return ok
}

func (p *raceProbe) Renew(id string) bool {
	ok := p.inner.Renew(id)
	if !ok {
		atomic.AddInt32(p.holding, -1)
	}
	return ok
}

func (p *raceProbe) Release(id string) {
	p.inner.Release(id)
	atomic.AddInt32(p.holding, -1)
}

// TestTodo_TOOL_021_Fault injects a simulated lease loss (a heartbeat that
// fails to renew in time, modeled by FakeLeaseStore.ForceExpireNext) and
// proves three things without any real sleep: the loop reports the loss
// and recovers with the exact canonical event sequence, the virtual
// elapsed time to do so is exact, and - the leak check TOOL-021 calls
// out by name - the loop goroutine actually exits once its context is
// canceled, using a bounded virtual-time select (time.After) rather than
// an unbounded receive, so a real regression would fail fast instead of
// hanging the test.
func TestTodo_TOOL_021_Fault(t *testing.T) {
	// Keep this named case independently assertive as well as exercising the
	// synctest fault/recovery sequence below.
	if synctestkit.EventLost == synctestkit.EventRenewed {
		t.Fatal("lease loss and renewal events must remain distinct")
	}
	synctest.Test(t, func(t *testing.T) {
		store := &synctestkit.FakeLeaseStore{}
		store.ForceExpireNext(1)

		ctx, cancel := context.WithCancel(context.Background())
		events := make(chan synctestkit.Event)
		done := make(chan struct{})
		start := time.Now()

		go func() {
			synctestkit.RunLeaseLoop(ctx, store, "L", 10*time.Second, 2*time.Second, events)
			close(done)
		}()

		wantSeq := []synctestkit.EventKind{
			synctestkit.EventAcquired,
			synctestkit.EventLost,
			synctestkit.EventAcquired,
			synctestkit.EventRenewed,
		}
		gotSeq := make([]synctestkit.EventKind, 0, len(wantSeq))
		for range wantSeq {
			gotSeq = append(gotSeq, (<-events).Kind)
		}
		if !reflect.DeepEqual(gotSeq, wantSeq) {
			t.Fatalf("event sequence = %v, want %v", gotSeq, wantSeq)
		}

		if want, got := 2*10*time.Second, time.Since(start); got != want {
			t.Fatalf("elapsed = %v, want exactly %v (Lost at 10s, recovery Renewed at 20s)", got, want)
		}

		cancel()
		if ev := (<-events).Kind; ev != synctestkit.EventReleased {
			t.Fatalf("final event = %v, want Released", ev)
		}

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("RunLeaseLoop goroutine leaked: did not exit within 1s of virtual time after ctx cancellation")
		}
	})
}

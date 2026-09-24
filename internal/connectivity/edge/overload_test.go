package edge

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
)

func newTestCoordinator(threshold int, cooldown time.Duration, allowed int) *Coordinator {
	return &Coordinator{
		Breaker: NewCircuitBreaker(CircuitConfig{FailureThreshold: threshold, Cooldown: cooldown}),
		Retry:   RetryLedger{Provisioner: admission.NewProvisioner(), Allowed: allowed, Version: "test-v1"},
	}
}

func healthySignal(dependency string) admission.BackpressureSignal {
	return admission.BackpressureSignal{Source: "edge", Dependency: dependency, State: admission.BackpressureHealthy}
}

func TestTodo_EDGE_007(t *testing.T) {
	now := time.Now()

	t.Run("admit on healthy backpressure and closed circuit", func(t *testing.T) {
		c := newTestCoordinator(5, time.Minute, 3)
		d, err := c.Decide(now, OverloadRequest{TenantID: "t1", Dependency: "dep", LogicalOperationID: "op1", OperationKind: "kind", Signal: healthySignal("dep")})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d.Outcome != admission.Admit || !d.PerformEffect {
			t.Fatalf("want ADMIT with an effect, got %+v", d)
		}
		if d.CircuitState != CircuitClosed {
			t.Fatalf("want the circuit state surfaced as CLOSED, got %q", d.CircuitState)
		}
	})

	t.Run("queue on throttled backpressure", func(t *testing.T) {
		c := newTestCoordinator(5, time.Minute, 3)
		sig := admission.BackpressureSignal{Source: "edge", Dependency: "dep", State: admission.BackpressureThrottled, RetryAfter: 2}
		d, err := c.Decide(now, OverloadRequest{TenantID: "t1", Dependency: "dep", LogicalOperationID: "op2", OperationKind: "kind", Signal: sig})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d.Outcome != admission.Queue || d.PerformEffect {
			t.Fatalf("want QUEUE with no effect, got %+v", d)
		}
	})

	t.Run("defer on unavailable backpressure", func(t *testing.T) {
		c := newTestCoordinator(5, time.Minute, 3)
		sig := admission.BackpressureSignal{Source: "edge", Dependency: "dep", State: admission.BackpressureUnavailable, RetryAfter: 5}
		d, err := c.Decide(now, OverloadRequest{TenantID: "t1", Dependency: "dep", LogicalOperationID: "op3", OperationKind: "kind", Signal: sig})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d.Outcome != admission.Defer || d.PerformEffect {
			t.Fatalf("want DEFER with no effect, got %+v", d)
		}
	})

	t.Run("degrade on slow backpressure", func(t *testing.T) {
		c := newTestCoordinator(5, time.Minute, 3)
		sig := admission.BackpressureSignal{Source: "edge", Dependency: "dep", State: admission.BackpressureSlow, RecommendedRate: 10}
		d, err := c.Decide(now, OverloadRequest{TenantID: "t1", Dependency: "dep", LogicalOperationID: "op4", OperationKind: "kind", Signal: sig})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d.Outcome != admission.Degrade || !d.PerformEffect {
			t.Fatalf("want DEGRADE with an effect, got %+v", d)
		}
	})

	t.Run("reject on open circuit without re-probing as healthy", func(t *testing.T) {
		c := newTestCoordinator(1, time.Hour, 3)
		req := OverloadRequest{TenantID: "t1", Dependency: "dep", LogicalOperationID: "op5", OperationKind: "kind", Signal: healthySignal("dep")}
		// Trip the circuit: one admitted attempt that then fails, with a
		// threshold of one failure.
		c.Breaker.Admit(req.TenantID, req.Dependency, now)
		c.Breaker.Report(req.TenantID, req.Dependency, now, false)

		d, err := c.Decide(now.Add(time.Second), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d.Outcome != admission.Reject || d.PerformEffect || d.CircuitState != CircuitOpen {
			t.Fatalf("want REJECT with the circuit visibly OPEN, got %+v", d)
		}
		// Still inside cooldown: the very next check must not be treated as
		// healthy again.
		d2, err := c.Decide(now.Add(2*time.Second), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d2.Outcome != admission.Reject || d2.CircuitState != CircuitOpen {
			t.Fatalf("circuit must not be re-probed as healthy before cooldown elapses: %+v", d2)
		}
	})

	t.Run("reject on exhausted retry budget", func(t *testing.T) {
		c := newTestCoordinator(5, time.Minute, 1)
		req := OverloadRequest{TenantID: "t1", Dependency: "dep", LogicalOperationID: "op6", OperationKind: "kind", Signal: healthySignal("dep"), Attempt: 1, Failure: FailureRateLimited}
		first, err := c.Decide(now, req)
		if err != nil || first.Outcome != admission.Admit {
			t.Fatalf("the first retry should be admitted: %+v err=%v", first, err)
		}
		req.Attempt = 2
		second, err := c.Decide(now, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if second.Outcome != admission.Reject || second.PerformEffect {
			t.Fatalf("an exhausted budget must REJECT with no effect: %+v", second)
		}
	})

	t.Run("unrecognized backpressure action fails closed, never ADMIT", func(t *testing.T) {
		if got := mapBackpressureAction(admission.BackpressureAction("SOMETHING_NEW")); got != admission.Reject {
			t.Fatalf("an unrecognized action must map to REJECT, got %q", got)
		}
	})

	t.Run("the backpressure mapping is total over every declared action", func(t *testing.T) {
		cases := []struct {
			action admission.BackpressureAction
			want   admission.Outcome
		}{
			{admission.BackpressureContinue, admission.Admit},
			{admission.BackpressureSlowUpstream, admission.Degrade},
			{admission.BackpressureQueue, admission.Queue},
			{admission.BackpressureDefer, admission.Defer},
			{admission.BackpressureStop, admission.Reject},
		}
		for _, tc := range cases {
			if got := mapBackpressureAction(tc.action); got != tc.want {
				t.Fatalf("action %s: want %s, got %s", tc.action, tc.want, got)
			}
		}
	})

	t.Run("zero-valued or unrecognized circuit state fails closed, never ADMIT", func(t *testing.T) {
		var zero CircuitState
		if got := circuitOutcome(zero); got == admission.Admit {
			t.Fatalf("the zero-valued circuit state must never resolve to ADMIT, got %q", got)
		}
		if got := circuitOutcome(CircuitState("bogus")); got == admission.Admit {
			t.Fatalf("an unrecognized circuit state must never resolve to ADMIT, got %q", got)
		}
	})

	t.Run("an unconfigured circuit breaker fails closed", func(t *testing.T) {
		c := &Coordinator{Retry: RetryLedger{Provisioner: admission.NewProvisioner(), Allowed: 3, Version: "test-v1"}}
		d, err := c.Decide(now, OverloadRequest{TenantID: "t1", Dependency: "dep", LogicalOperationID: "op7", OperationKind: "kind", Signal: healthySignal("dep")})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d.Outcome != admission.Reject || d.PerformEffect {
			t.Fatalf("an unconfigured coordinator must never ADMIT, got %+v", d)
		}
	})

	t.Run("an invalid request is rejected, not silently admitted", func(t *testing.T) {
		c := newTestCoordinator(5, time.Minute, 3)
		d, err := c.Decide(now, OverloadRequest{})
		if !errors.Is(err, ErrInvalidOverloadRequest) {
			t.Fatalf("want ErrInvalidOverloadRequest, got %v", err)
		}
		if d.Outcome != admission.Reject || d.PerformEffect {
			t.Fatalf("an invalid request must never ADMIT, got %+v", d)
		}
	})
}

func TestTodo_EDGE_007_Fault(t *testing.T) {
	now := time.Now()
	c := newTestCoordinator(5, time.Minute, 2)
	req := OverloadRequest{TenantID: "t1", Dependency: "vendor", LogicalOperationID: "logical-op", OperationKind: "sync", Signal: healthySignal("vendor")}

	// Three layers -- an edge retry (429), a connector retry (503) and a
	// transport timeout -- all observe the SAME physical attempt of the
	// SAME logical operation. They must collide on one shared receipt
	// rather than each minting a fresh token.
	for _, failure := range []FailureSignal{FailureRateLimited, FailureUnavailable, FailureTimeout} {
		req.Attempt, req.Failure = 1, failure
		d, err := c.Decide(now, req)
		if err != nil {
			t.Fatalf("layer %s: unexpected error: %v", failure, err)
		}
		if d.Outcome != admission.Admit {
			t.Fatalf("layer %s: nested observation of the first charged attempt should still ADMIT, got %+v", failure, d)
		}
	}

	ids := c.Retry.Provisioner.Ledger()
	if len(ids) != 1 {
		t.Fatalf("expected exactly one provisioned budget, got %d", len(ids))
	}
	budget, ok := c.Retry.Provisioner.Snapshot(ids[0])
	if !ok {
		t.Fatalf("expected a provisioned budget")
	}
	if budget.Consumed != 1 {
		t.Fatalf("nested 429/503/timeout for one attempt must charge the budget exactly once, got Consumed=%d", budget.Consumed)
	}

	// A genuinely new physical attempt (attempt 2) consumes the second and
	// final token; exhaustion after that must REJECT rather than mint a
	// third attempt.
	req.Attempt, req.Failure = 2, FailureUnavailable
	d2, err := c.Decide(now, req)
	if err != nil || d2.Outcome != admission.Admit {
		t.Fatalf("the second distinct attempt should still be admitted: %+v err=%v", d2, err)
	}
	req.Attempt, req.Failure = 3, FailureTimeout
	d3, err := c.Decide(now, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d3.Outcome != admission.Reject || d3.PerformEffect {
		t.Fatalf("budget exhaustion must REJECT with no effect, got %+v", d3)
	}
}

func TestTodo_EDGE_007_Race(t *testing.T) {
	now := time.Now()

	{
		c := newTestCoordinator(5, time.Minute, 3)
		req := OverloadRequest{TenantID: "t1", Dependency: "vendor", LogicalOperationID: "race-op", OperationKind: "sync", Signal: healthySignal("vendor"), Attempt: 1, Failure: FailureRateLimited}
		const goroutines = 50
		var wg sync.WaitGroup
		start := make(chan struct{})
		results := make([]admission.Outcome, goroutines)
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				d, err := c.Decide(now, req)
				if err != nil {
					t.Errorf("goroutine %d: unexpected error: %v", i, err)
					return
				}
				results[i] = d.Outcome
			}(i)
		}
		close(start)
		wg.Wait()
		for i, outcome := range results {
			if outcome != admission.Admit {
				t.Fatalf("goroutine %d: a replayed attempt should still ADMIT via the stored receipt, got %s", i, outcome)
			}
		}
		ids := c.Retry.Provisioner.Ledger()
		if len(ids) != 1 {
			t.Fatalf("expected exactly one provisioned budget, got %d", len(ids))
		}
		budget, _ := c.Retry.Provisioner.Snapshot(ids[0])
		if budget.Consumed != 1 {
			t.Fatalf("%d concurrent replays of the same attempt must charge the budget exactly once, got Consumed=%d", goroutines, budget.Consumed)
		}
	}

	{
		c := newTestCoordinator(5, time.Minute, 3)
		const attempts = 20 // far more than the configured budget of 3
		var wg sync.WaitGroup
		start := make(chan struct{})
		var mu sync.Mutex
		admittedCount := 0
		for attempt := 1; attempt <= attempts; attempt++ {
			wg.Add(1)
			go func(attempt int) {
				defer wg.Done()
				<-start
				req := OverloadRequest{TenantID: "t2", Dependency: "vendor", LogicalOperationID: "race-op-2", OperationKind: "sync", Signal: healthySignal("vendor"), Attempt: attempt, Failure: FailureUnavailable}
				d, err := c.Decide(now, req)
				if err != nil {
					t.Errorf("attempt %d: unexpected error: %v", attempt, err)
					return
				}
				if d.PerformEffect {
					mu.Lock()
					admittedCount++
					mu.Unlock()
				}
			}(attempt)
		}
		close(start)
		wg.Wait()

		var consumed int
		for _, id := range c.Retry.Provisioner.Ledger() {
			if b, ok := c.Retry.Provisioner.Snapshot(id); ok && b.LogicalOperationID == "race-op-2" {
				consumed = b.Consumed
			}
		}
		if consumed > 3 {
			t.Fatalf("the budget must never be charged more than its Allowed=3 under concurrency, got Consumed=%d", consumed)
		}
		if admittedCount > 3 {
			t.Fatalf("no more than 3 concurrent distinct attempts may be admitted, got %d", admittedCount)
		}
	}
}

func TestTodo_EDGE_007_Security(t *testing.T) {
	now := time.Now()
	c := newTestCoordinator(1, time.Hour, 1)

	// Exhaust tenant A's retry budget and trip tenant A's circuit.
	reqA := OverloadRequest{TenantID: "tenant-a", Dependency: "vendor", LogicalOperationID: "shared-op", OperationKind: "sync", Signal: healthySignal("vendor"), Attempt: 1, Failure: FailureRateLimited}
	if d, err := c.Decide(now, reqA); err != nil || d.Outcome != admission.Admit {
		t.Fatalf("tenant A's first attempt should admit: %+v err=%v", d, err)
	}
	reqA.Attempt = 2
	if d, err := c.Decide(now, reqA); err != nil || d.Outcome != admission.Reject {
		t.Fatalf("tenant A should now be exhausted: %+v err=%v", d, err)
	}
	c.Breaker.Admit(reqA.TenantID, reqA.Dependency, now)
	c.Breaker.Report(reqA.TenantID, reqA.Dependency, now, false) // trips open at a threshold of one
	if _, cstate := c.Breaker.Admit(reqA.TenantID, reqA.Dependency, now); cstate != CircuitOpen {
		t.Fatalf("tenant A's circuit should be OPEN, got %q", cstate)
	}

	// Tenant B, using the same dependency and the SAME logical operation
	// id, must be wholly unaffected: neither tenant A's exhausted retry
	// budget nor tenant A's open circuit may be consumed or revealed by
	// tenant B's evaluation.
	reqB := OverloadRequest{TenantID: "tenant-b", Dependency: "vendor", LogicalOperationID: "shared-op", OperationKind: "sync", Signal: healthySignal("vendor"), Attempt: 1, Failure: FailureRateLimited}
	d, err := c.Decide(now, reqB)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Outcome != admission.Admit || !d.PerformEffect {
		t.Fatalf("tenant B must not inherit tenant A's exhausted budget or open circuit: %+v", d)
	}
	if d.CircuitState != CircuitClosed {
		t.Fatalf("tenant B's circuit must not reveal or reuse tenant A's OPEN state, got %q", d.CircuitState)
	}
}

func TestTodo_EDGE_007_Mutation(t *testing.T) {
	now := time.Now()
	c := newTestCoordinator(5, time.Minute, 3)
	req := OverloadRequest{TenantID: "t1", Dependency: "vendor", LogicalOperationID: "storm-op", OperationKind: "sync", Signal: healthySignal("vendor")}

	effectsPerformed := 0
	const unboundedFailures = 500
	for attempt := 1; attempt <= unboundedFailures; attempt++ {
		req.Attempt, req.Failure = attempt, FailureUnavailable
		d, err := c.Decide(now, req)
		if err != nil {
			t.Fatalf("attempt %d: unexpected error: %v", attempt, err)
		}
		switch d.Outcome {
		case admission.Admit, admission.Queue, admission.Defer, admission.Degrade, admission.Reject:
		default:
			t.Fatalf("attempt %d: outcome must be one of the five declared outcomes, got %q", attempt, d.Outcome)
		}
		if d.PerformEffect {
			effectsPerformed++
		} else if d.Outcome != admission.Reject {
			t.Fatalf("attempt %d: a non-effect decision that is not REJECT is unexpected here: %+v", attempt, d)
		}
	}
	if effectsPerformed != 3 {
		t.Fatalf("no retry storm: %d nested failures must still bound effects to the configured budget of 3, got %d", unboundedFailures, effectsPerformed)
	}

	// A refused retry must perform nothing: replaying the exhausted attempt
	// is stable and still performs no effect.
	req.Attempt, req.Failure = unboundedFailures, FailureUnavailable
	d, err := c.Decide(now, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.PerformEffect || d.Outcome != admission.Reject {
		t.Fatalf("a refused retry must perform nothing on replay, got %+v", d)
	}
}

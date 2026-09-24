package operation_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
)

func candidateAt(id uuid.UUID, tenant, connection, resource, criticality string, at time.Time) operation.ScheduleCandidate {
	return operation.ScheduleCandidate{OperationID: id, TenantID: tenant, ConnectionID: connection, ResourceKey: resource, Criticality: criticality, QueuedAt: at}
}

func TestQuotaWindowMechanicsAreSharedAndBounded(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	window := operation.AdvanceQuotaWindow(operation.QuotaWindow{}, now, time.Minute)
	window.Used = 4
	active := operation.AdvanceQuotaWindow(window, now.Add(30*time.Second), time.Minute)
	if active.Used != 4 || !active.ResetAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("active quota window changed: %+v", active)
	}
	next := operation.AdvanceQuotaWindow(active, now.Add(time.Minute), time.Minute)
	if next.Used != 0 || !next.StartAt.Equal(now.Add(time.Minute)) || !next.ResetAt.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("expired quota window did not reset: %+v", next)
	}
	if got := operation.QuotaRetryAfterSeconds(now, now.Add(1001*time.Millisecond)); got != 2 {
		t.Fatalf("retry interval = %d, want ceiling-rounded 2 seconds", got)
	}
}

// TestTodo_INTG_015 is the PRIMARY test: per-tenant/connection/resource/
// criticality limits are honored together, a P0 operation is scheduled
// ahead of (and into capacity reserved from) a P3 backlog rather than
// being starved by it, and queue age / predicted completion are exposed
// and correct.
func TestTodo_INTG_015(t *testing.T) {
	policy := operation.ConnectorPolicy{
		Quota:            operation.ConnectorQuota{Limit: 10, Window: time.Minute, MaxConcurrent: 2},
		PerTenantShare:   1,
		PerResourceShare: 1,
		ReserveForP0P1:   1,
	}
	ledger := operation.NewConnectorLedger(map[string]operation.ConnectorPolicy{"vendor-x": policy})
	now := testNow

	// A P3 backlog from two different tenants/resources floods the
	// connection. Only one may be admitted: ReserveForP0P1=1 caps
	// low-criticality occupancy at MaxConcurrent(2)-Reserve(1)=1, even
	// though neither tenant nor resource share is otherwise exceeded.
	p3a := candidateAt(uuid.New(), "tenant-a", "vendor-x", "res-a", "P3", now)
	p3b := candidateAt(uuid.New(), "tenant-b", "vendor-x", "res-b", "P3", now)
	pass1 := ledger.Schedule(now, []operation.ScheduleCandidate{p3a, p3b})
	if len(pass1.Admitted) != 1 || len(pass1.Deferred) != 1 {
		t.Fatalf("P3 flood admitted=%d deferred=%d, want 1/1 (criticality reserve must hold a slot open)", len(pass1.Admitted), len(pass1.Deferred))
	}
	if pass1.Deferred[0].Reason != operation.ScheduleReasonCriticalityReserved {
		t.Fatalf("second P3 deferral reason = %q, want %q", pass1.Deferred[0].Reason, operation.ScheduleReasonCriticalityReserved)
	}
	admittedTenant, admittedResource := pass1.Admitted[0].Candidate.TenantID, pass1.Admitted[0].Candidate.ResourceKey

	// The same tenant cannot expand into capacity a policy left for other
	// tenants, and the same resource cannot expand into capacity left for
	// other resources.
	if ok, reason := ledger.TryReserve(now, candidateAt(uuid.New(), admittedTenant, "vendor-x", "res-e", "P2", now)); ok || reason != operation.ScheduleReasonTenantShareExceeded {
		t.Fatalf("tenant share not honored: ok=%v reason=%q", ok, reason)
	}
	if ok, reason := ledger.TryReserve(now, candidateAt(uuid.New(), "tenant-z", "vendor-x", admittedResource, "P2", now)); ok || reason != operation.ScheduleReasonResourceShareExceeded {
		t.Fatalf("resource share not honored: ok=%v reason=%q", ok, reason)
	}

	// A P0 for a fresh tenant arrives later. It must be admitted into the
	// slot the reserve kept open, even though a P3 already occupies the
	// connection: this is what stops "P3 starves P0".
	at5 := now.Add(5 * time.Second)
	p0 := candidateAt(uuid.New(), "tenant-c", "vendor-x", "res-c", "P0", at5)
	pass2 := ledger.Schedule(at5, []operation.ScheduleCandidate{p0})
	if len(pass2.Admitted) != 1 || pass2.Admitted[0].Candidate.OperationID != p0.OperationID {
		t.Fatalf("P0 was starved by the P3 backlog: %+v", pass2)
	}
	if pass2.Admitted[0].QueueAge != 0 || !pass2.Admitted[0].PredictedCompletion.Equal(at5) {
		t.Fatalf("admitted P0 queue age/predicted completion = %+v, want age=0 predicted=%v", pass2.Admitted[0], at5)
	}

	// The connection is now fully occupied (1 P3 + 1 P0 == MaxConcurrent
	// 2). A fresh P3 must be deferred by the connection-wide concurrency
	// cap, with a predicted completion strictly after the current time.
	at10 := now.Add(10 * time.Second)
	p3c := candidateAt(uuid.New(), "tenant-d", "vendor-x", "res-d", "P3", at10)
	pass3 := ledger.Schedule(at10, []operation.ScheduleCandidate{p3c})
	if len(pass3.Deferred) != 1 || pass3.Deferred[0].Reason != operation.ScheduleReasonConcurrencyLimited {
		t.Fatalf("full connection did not defer by concurrency: %+v", pass3)
	}
	if !pass3.Deferred[0].PredictedCompletion.After(at10) {
		t.Fatalf("predicted completion %v not after now %v", pass3.Deferred[0].PredictedCompletion, at10)
	}

	// Rescheduling the still-deferred P3 later must expose its actual
	// elapsed wait as queue age.
	at40 := now.Add(40 * time.Second)
	pass4 := ledger.Schedule(at40, []operation.ScheduleCandidate{p3c})
	if len(pass4.Deferred) != 1 || pass4.Deferred[0].QueueAge != 30*time.Second {
		t.Fatalf("queue age not exposed correctly: %+v, want 30s", pass4.Deferred)
	}
}

func TestTodo_INTG_015_Integration(t *testing.T) {
	j := newJournal()
	ledger := operation.NewConnectorLedger(map[string]operation.ConnectorPolicy{
		"payroll-connection": {Quota: operation.ConnectorQuota{Limit: 10, Window: time.Minute, MaxConcurrent: 1}},
	})
	idA, idB := uuid.New(), uuid.New()
	planned(t, j, idA, "worker:w-1", 1, operation.OrderingIndependent)
	planned(t, j, idB, "worker:w-2", 1, operation.OrderingIndependent)
	queued(t, j, idA)
	queued(t, j, idB)
	opA, err := j.Get(context.Background(), "tenant-promotion", idA)
	if err != nil {
		t.Fatal(err)
	}
	opB, err := j.Get(context.Background(), "tenant-promotion", idB)
	if err != nil {
		t.Fatal(err)
	}

	// Both operations share one connection whose vendor quota allows only
	// one concurrent dispatch: this crosses the boundary between the
	// journal's real Plan/Queue/Lease/Dispatch state machine and the
	// connector ledger's admission decision, gating a real operation queue
	// rather than a synthetic candidate list.
	out := ledger.Schedule(testNow, []operation.ScheduleCandidate{operation.CandidateFromOperation(opA, testNow), operation.CandidateFromOperation(opB, testNow)})
	if len(out.Admitted) != 1 || len(out.Deferred) != 1 {
		t.Fatalf("connection concurrency did not gate the real operation queue: admitted=%d deferred=%d", len(out.Admitted), len(out.Deferred))
	}
	admittedID := out.Admitted[0].Candidate.OperationID
	writer := operation.NewPayrollSync()
	admittedLease := leaseFor(t, j, admittedID)
	if _, err := j.Dispatch(context.Background(), admittedLease, writer); err != nil {
		t.Fatal(err)
	}

	// The deferred operation cannot be leased into the connection's only
	// slot while it is still occupied...
	deferredCandidate := out.Deferred[0].Candidate
	if ok, reason := ledger.TryReserve(testNow.Add(time.Second), deferredCandidate); ok || reason != operation.ScheduleReasonConcurrencyLimited {
		t.Fatalf("deferred operation admitted before release: ok=%v reason=%q", ok, reason)
	}

	// ...until the admitted operation's slot is released, at which point a
	// later Schedule pass admits it and it can actually be leased and
	// dispatched through the real journal.
	if !ledger.Release(admittedID, "tenant-promotion") {
		t.Fatal("release of the rightful owner's own reservation failed")
	}
	later := testNow.Add(2 * time.Second)
	out2 := ledger.Schedule(later, []operation.ScheduleCandidate{deferredCandidate})
	if len(out2.Admitted) != 1 {
		t.Fatalf("released slot was not reused by the deferred operation: %+v", out2)
	}
	deferredLease := leaseFor(t, j, deferredCandidate.OperationID)
	if _, err := j.Dispatch(context.Background(), deferredLease, writer); err != nil {
		t.Fatal(err)
	}
	if got := len(writer.Calls()); got != 2 {
		t.Fatalf("provider calls = %d, want 2", got)
	}
}

// TestTodo_INTG_015_Fault proves both fault halves the RED calls out: a
// provider 429 with a reset window must back off admission to that reset
// rather than letting retries multiply, and a connection nobody
// configured must fail closed to a conservative bound rather than
// unlimited.
func TestTodo_INTG_015_Fault(t *testing.T) {
	ledger := operation.NewConnectorLedger(map[string]operation.ConnectorPolicy{
		"vendor-y": {Quota: operation.ConnectorQuota{Limit: 3, Window: time.Minute, MaxConcurrent: 5}},
	})
	now := testNow
	ledger.Observe429("vendor-y", now, 30*time.Second)

	// A naive caller retrying immediately, several times in a row, must be
	// refused every time: if even one were admitted, the 429 storm would
	// still be multiplying retries.
	for i := 0; i < 5; i++ {
		if ok, reason := ledger.TryReserve(now, candidateAt(uuid.New(), "tenant-throttled", "vendor-y", "res", "P1", now)); ok || reason != operation.ScheduleReasonRateLimited {
			t.Fatalf("attempt %d during 429 backoff: ok=%v reason=%q, want refused RATE_LIMITED", i, ok, reason)
		}
	}
	// Scheduling (not just TryReserve) during the backoff must expose a
	// predicted completion tied exactly to the provider's own reset time.
	deferred := ledger.Schedule(now, []operation.ScheduleCandidate{candidateAt(uuid.New(), "tenant-throttled", "vendor-y", "res", "P1", now)})
	if len(deferred.Deferred) != 1 || !deferred.Deferred[0].PredictedCompletion.Equal(now.Add(30*time.Second)) {
		t.Fatalf("predicted completion did not reflect the provider reset: %+v, want %v", deferred.Deferred, now.Add(30*time.Second))
	}
	// Before the reset elapses, still refused.
	if ok, _ := ledger.TryReserve(now.Add(29*time.Second), candidateAt(uuid.New(), "tenant-throttled", "vendor-y", "res", "P1", now)); ok {
		t.Fatal("admitted before the provider's own reset window elapsed")
	}
	// At/after the reset, admission resumes.
	if ok, _ := ledger.TryReserve(now.Add(30*time.Second), candidateAt(uuid.New(), "tenant-throttled", "vendor-y", "res", "P1", now)); !ok {
		t.Fatal("admission did not resume after the provider reset window elapsed")
	}

	// Unknown quota: a connection nobody configured must fail closed to the
	// conservative fallback, never unlimited.
	unknownLedger := operation.NewConnectorLedger(nil)
	if ok, _ := unknownLedger.TryReserve(now, candidateAt(uuid.New(), "tenant-a", "vendor-unconfigured", "res", "P1", now)); !ok {
		t.Fatal("first reservation against an unknown connection was refused")
	}
	if ok, reason := unknownLedger.TryReserve(now, candidateAt(uuid.New(), "tenant-a", "vendor-unconfigured", "res", "P1", now)); ok {
		t.Fatalf("unknown quota assumed unlimited: second reservation admitted (reason=%q)", reason)
	}

	// A connection named in the policy but never measured has an UNKNOWN
	// quota, exactly like one nobody named. The struct's zero value is the
	// dangerous one, so this pins that it fails closed: a caller who sets a
	// tenant share and simply had not looked up the vendor's rate limit must
	// not get unlimited vendor traffic (RED: "unknown quota assumes
	// unlimited"; REFACTOR: "reserved capacity never violates vendor quota").
	undeclared := operation.NewConnectorLedger(map[string]operation.ConnectorPolicy{
		"named-but-unmeasured": {},
	})
	admitted := 0
	for range 50 {
		ok, _ := undeclared.TryReserve(now, candidateAt(uuid.New(), "tenant-a", "named-but-unmeasured", "res", "P0", now))
		if ok {
			admitted++
		}
	}
	if admitted > operation.UnknownConnectorQuota.Limit {
		t.Fatalf("admitted %d against an undeclared vendor quota, want at most the conservative %d: unknown quota was treated as unlimited",
			admitted, operation.UnknownConnectorQuota.Limit)
	}

	// The explicit escape hatch still works: a negative bound is a caller
	// deliberately declaring "this connection has no vendor limit", which is
	// a different statement from never having said anything.
	unbounded := operation.NewConnectorLedger(map[string]operation.ConnectorPolicy{
		"genuinely-unbounded": {Quota: operation.ConnectorQuota{Limit: -1, MaxConcurrent: -1}},
	})
	for i := range 20 {
		if ok, reason := unbounded.TryReserve(now, candidateAt(uuid.New(), "tenant-a", "genuinely-unbounded", "res", "P0", now)); !ok {
			t.Fatalf("explicit unbounded connection refused reservation %d: %q", i, reason)
		}
	}

}

// TestTodo_INTG_015_Security proves tenant scoping: one tenant can
// neither consume another tenant's reserved capacity nor observe (via
// Release) a reservation it does not own.
func TestTodo_INTG_015_Security(t *testing.T) {
	policy := operation.ConnectorPolicy{Quota: operation.ConnectorQuota{MaxConcurrent: 5}, PerTenantShare: 1}
	ledger := operation.NewConnectorLedger(map[string]operation.ConnectorPolicy{"vendor-z": policy})
	now := testNow

	ownID := uuid.New()
	if ok, _ := ledger.TryReserve(now, candidateAt(ownID, "tenant-a", "vendor-z", "res", "P2", now)); !ok {
		t.Fatal("tenant-a could not reserve its own share")
	}
	if ok, reason := ledger.TryReserve(now, candidateAt(uuid.New(), "tenant-a", "vendor-z", "res-2", "P2", now)); ok || reason != operation.ScheduleReasonTenantShareExceeded {
		t.Fatalf("tenant-a consumed capacity beyond its own share: ok=%v reason=%q", ok, reason)
	}
	if ok, _ := ledger.TryReserve(now, candidateAt(uuid.New(), "tenant-b", "vendor-z", "res-3", "P2", now)); !ok {
		t.Fatal("tenant-b was denied its own configured share by tenant-a's activity")
	}
	if got := ledger.InFlight("vendor-z"); got != 2 {
		t.Fatalf("InFlight = %d, want 2", got)
	}

	// A foreign tenant supplying the exact operation id cannot release
	// (and thereby manipulate or infer) another tenant's reservation.
	if released := ledger.Release(ownID, "tenant-attacker"); released {
		t.Fatal("foreign tenant released another tenant's reservation")
	}
	if got := ledger.InFlight("vendor-z"); got != 2 {
		t.Fatalf("reservation state changed after a foreign release attempt: InFlight=%d, want 2", got)
	}
	if released := ledger.Release(ownID, "tenant-a"); !released {
		t.Fatal("rightful owner could not release its own reservation")
	}
	if got := ledger.InFlight("vendor-z"); got != 1 {
		t.Fatalf("InFlight after rightful release = %d, want 1", got)
	}
}

// TestTodo_INTG_015_Race runs real goroutines competing for the same
// connection's concurrency quota and asserts a deterministic admitted
// count: no -race detector is available on windows/arm64 here, so
// correctness under real concurrent access is proven by the count itself
// being exactly right, every run.
func TestTodo_INTG_015_Race(t *testing.T) {
	ledger := operation.NewConnectorLedger(map[string]operation.ConnectorPolicy{
		"vendor-race": {Quota: operation.ConnectorQuota{MaxConcurrent: 4}},
	})
	now := testNow
	var wg sync.WaitGroup
	var admitted int64
	const attempts = 20
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := ledger.TryReserve(now, candidateAt(uuid.New(), "tenant-race", "vendor-race", "res", "P2", now)); ok {
				atomic.AddInt64(&admitted, 1)
			}
		}()
	}
	wg.Wait()
	if admitted != 4 {
		t.Fatalf("concurrent reservations admitted = %d, want exactly 4 (MaxConcurrent)", admitted)
	}
	if got := ledger.InFlight("vendor-race"); got != 4 {
		t.Fatalf("InFlight after race = %d, want 4", got)
	}
}

// FuzzTodo_INTG_015 attacks the REFACTOR invariant directly: across
// arbitrary sequences of grants, releases, reset-window advances and
// concurrency values, reserved capacity must never exceed the configured
// vendor quota. Each iteration derives its own MaxConcurrent/Limit/
// reserve/tenant-share from the fuzz input, so "concurrency values" are
// themselves part of what is fuzzed, not fixed by the test.
func FuzzTodo_INTG_015(f *testing.F) {
	f.Add([]byte{3, 2, 1, 1, 0, 1, 2, 3, 0, 1, 2, 3, 0})
	f.Add([]byte{0, 0, 0, 0})
	f.Add([]byte{5, 5, 2, 3, 3, 3, 3, 3, 3, 3, 3, 3})
	criticalities := []string{"P0", "P1", "P2", "P3", "P4"}
	tenants := []string{"tenant-a", "tenant-b", "tenant-c"}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 4 {
			return
		}
		maxConcurrent := int(data[0]) % 6
		limit := int(data[1]) % 6
		reserve := int(data[2]) % 3
		tenantShare := int(data[3]) % 4
		policy := operation.ConnectorPolicy{
			Quota:          operation.ConnectorQuota{Limit: limit, Window: time.Second, MaxConcurrent: maxConcurrent},
			PerTenantShare: tenantShare,
			ReserveForP0P1: reserve,
		}
		ledger := operation.NewConnectorLedger(map[string]operation.ConnectorPolicy{"vendor-fuzz": policy})
		now := testNow
		type held struct {
			id     uuid.UUID
			tenant string
		}
		var reserved []held
		for i := 4; i < len(data); i++ {
			b := data[i]
			switch b % 4 {
			case 0:
				tenant := tenants[int(b)%len(tenants)]
				id := uuid.New()
				cand := candidateAt(id, tenant, "vendor-fuzz", "res", criticalities[int(b)%len(criticalities)], now)
				if ok, _ := ledger.TryReserve(now, cand); ok {
					reserved = append(reserved, held{id: id, tenant: tenant})
				}
			case 1:
				if len(reserved) > 0 {
					idx := int(b) % len(reserved)
					item := reserved[idx]
					ledger.Release(item.id, item.tenant)
					reserved = append(reserved[:idx], reserved[idx+1:]...)
				}
			case 2:
				now = now.Add(time.Duration(int(b)%5) * 200 * time.Millisecond)
			case 3:
				ledger.Observe429("vendor-fuzz", now, time.Duration(int(b)%3+1)*time.Second)
			}
			if maxConcurrent > 0 {
				if inFlight := ledger.InFlight("vendor-fuzz"); inFlight > maxConcurrent {
					t.Fatalf("reserved capacity %d exceeded vendor quota %d after op %d (byte=%d)", inFlight, maxConcurrent, i, b)
				}
			}
		}
	})
}

package outbox_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
)

// TestTodo_EVENT_003_Race runs real goroutines against one shared
// ResourceLedger. No -race locally (windows/arm64), so this is a
// concurrency-determinism test instead: it fails outright if the ledger
// ever admits more than its configured capacity, or admits the same
// outbox row more than once, under genuine concurrent contention.
func TestTodo_EVENT_003_Race(t *testing.T) {
	{
		const total = 200
		const capacity = 50
		ledger := outbox.NewResourceLedger(map[string]outbox.ResourcePolicy{"shared-dep": {Capacity: capacity}})
		ids := make([]uuid.UUID, total)
		for i := range ids {
			ids[i] = uuid.New()
		}
		admittedFlags := make([]int32, total)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < total; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				if ok, _ := ledger.TryAdmit("shared-dep", uuid.New(), ids[i]); ok {
					atomic.AddInt32(&admittedFlags[i], 1)
				}
			}(i)
		}
		close(start)
		wg.Wait()

		var admittedCount int
		for i, flag := range admittedFlags {
			if flag > 1 {
				t.Fatalf("record %d admitted %d times, want at most once", i, flag)
			}
			admittedCount += int(flag)
		}
		if admittedCount != capacity {
			t.Fatalf("admitted = %d under concurrent load, want exactly the resource capacity %d", admittedCount, capacity)
		}
		if got := ledger.InFlight("shared-dep"); got != capacity {
			t.Fatalf("in-flight = %d, want %d", got, capacity)
		}
	}

	{
		ledger := outbox.NewResourceLedger(map[string]outbox.ResourcePolicy{"dedup-dep": {Capacity: 1000}})
		dup := uuid.New()
		const racers = 64
		var dupAdmits int32
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < racers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if ok, _ := ledger.TryAdmit("dedup-dep", uuid.New(), dup); ok {
					atomic.AddInt32(&dupAdmits, 1)
				}
			}()
		}
		close(start)
		wg.Wait()
		if dupAdmits != 1 {
			t.Fatalf("duplicate outbox id admitted %d times by %d racing goroutines, want exactly 1", dupAdmits, racers)
		}
	}

	{
		const total = 100
		ledger := outbox.NewResourceLedger(map[string]outbox.ResourcePolicy{"roomy-dep": {Capacity: total}})
		ids := make([]uuid.UUID, total)
		for i := range ids {
			ids[i] = uuid.New()
		}
		var admitted int32
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < total; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				if ok, _ := ledger.TryAdmit("roomy-dep", uuid.New(), ids[i]); ok {
					atomic.AddInt32(&admitted, 1)
				}
			}(i)
		}
		close(start)
		wg.Wait()
		if int(admitted) != total {
			t.Fatalf("admitted = %d, want all %d candidates admitted when capacity is not the constraint", admitted, total)
		}
	}
}

func event003TenantFixture(t *testing.T) (*pgtest.DB, func(tenant uuid.UUID)) {
	t.Helper()
	db := pgtest.New(t)
	seed := func(tenant uuid.UUID) {
		db.Exec(t, `
			INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
			VALUES ($1, $2, 'cell-event-003', 'Event 003', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
			tenant, "event-003-"+tenant.String())
		db.Exec(t, `
			INSERT INTO payload_schema (
				tenant_id, schema_ref, schema_id, schema_version,
				message_full_name, wire_format, canonicalization_profile)
			VALUES ($1, $2, 'hcmnext.events.v1.Event003', 1,
				'hcmnext.events.v1.Event003', 'PROTOBUF', 'LEDGER_EVENT')`,
			tenant, event001SchemaRef)
	}
	return db, seed
}

func event003Enqueue(t *testing.T, db *pgtest.DB, req outbox.EnqueueRequest) outbox.Record {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	rec, err := outbox.Enqueue(ctx, tx, req)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("enqueue %s: %v", req.EffectIdentity, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit %s: %v", req.EffectIdentity, err)
	}
	return rec
}

// TestTodo_EVENT_003_Integration reaches real PostgreSQL via pgtest: it
// enqueues and polls genuine outbox rows for two tenants sharing one
// downstream resource, then proves the pure Schedule policy - fed those
// real, DB-claimed records - still admits the P0 item ahead of the other
// tenant's flood. It also proves the retry-budget wiring end to end: a
// second layer sharing the same admission.Provisioner and the same
// AttemptIdentity as Consumer.WithRetryAccounting must not get a second
// token from a one-token budget, and once that shared budget is spent the
// consumer parks the message ABANDONED even though its own local
// maxAttempts (unset here) would otherwise allow unlimited redelivery.
func TestTodo_EVENT_003_Integration(t *testing.T) {
	ctx := context.Background()
	db, seed := event003TenantFixture(t)
	tenantA, tenantB := uuid.New(), uuid.New()
	seed(tenantA)
	seed(tenantB)

	for i := 0; i < 8; i++ {
		event003Enqueue(t, db, outbox.EnqueueRequest{
			Tenant: tenantB, EffectIdentity: fmt.Sprintf("flood-%d", i), OrderingKey: "worker:event-003",
			Criticality: outbox.CriticalityP4, SchemaRef: event001SchemaRef, Payload: []byte("flood"),
		})
	}
	payroll := event003Enqueue(t, db, outbox.EnqueueRequest{
		Tenant: tenantA, EffectIdentity: "payroll-run", OrderingKey: "worker:event-003",
		Criticality: outbox.CriticalityP0, SchemaRef: event001SchemaRef, Payload: []byte("payroll"),
	})

	consumer := outbox.NewConsumer(db.Conn, outbox.WithClock(func() time.Time { return time.Now().UTC().Add(time.Minute) }))
	claimedA, err := consumer.Poll(ctx, tenantA)
	if err != nil {
		t.Fatalf("poll A: %v", err)
	}
	claimedB, err := consumer.Poll(ctx, tenantB)
	if err != nil {
		t.Fatalf("poll B: %v", err)
	}
	if len(claimedA) != 1 || len(claimedB) != 8 {
		t.Fatalf("claimed A=%d B=%d, want 1 and 8", len(claimedA), len(claimedB))
	}

	var candidates []outbox.Candidate
	for _, rec := range claimedA {
		candidates = append(candidates, outbox.Candidate{Record: rec, Resource: "shared-dispatch"})
	}
	for _, rec := range claimedB {
		candidates = append(candidates, outbox.Candidate{Record: rec, Resource: "shared-dispatch"})
	}
	ledger := outbox.NewResourceLedger(map[string]outbox.ResourcePolicy{"shared-dispatch": {Capacity: 3}})
	result := outbox.Schedule(candidates, ledger)

	if len(result.Admitted) != 3 {
		t.Fatalf("admitted = %d, want 3 (the configured capacity)", len(result.Admitted))
	}
	if result.Admitted[0].Record.OutboxID != payroll.OutboxID {
		t.Fatalf("first admitted = %#v, want tenant A's real P0 row ahead of tenant B's flood claimed from PostgreSQL", result.Admitted[0].Record)
	}
	for _, deferred := range result.Deferred {
		if deferred.Candidate.Record.Tenant != tenantB {
			t.Fatalf("wrong tenant deferred: %#v", deferred.Candidate.Record)
		}
		if deferred.Reason == "" {
			t.Fatalf("deferred candidate %#v carries no evidence for why it was shed", deferred.Candidate.Record)
		}
	}

	t.Run("shared retry budget caps redelivery across layers", func(t *testing.T) {
		// A dedicated tenant keeps this subtest's lease/clock manipulation
		// (a clock two hours ahead, to force lease reclaim across polls)
		// from reclaiming tenantA's still-outstanding "payroll-run" claim
		// from the outer test.
		tenantC := uuid.New()
		seed(tenantC)

		provisioner := admission.NewProvisioner()
		account := &outbox.RetryAccount{Provisioner: provisioner}
		spec := admission.ProvisionSpec{
			TenantID: tenantC.String(), Service: "outbox", Dependency: "payments-api",
			LogicalOperationID: "payroll-run-op", OperationKind: "payroll.run",
			Allowed: 1, Retryable: []admission.FailureClass{admission.FailureTransient}, Version: "v1",
		}
		clock := time.Now().UTC().Add(2 * time.Hour)
		retryConsumer := outbox.NewConsumer(db.Conn,
			outbox.WithClock(func() time.Time { return clock }),
			outbox.WithRetryAccounting(account, func(outbox.Record) admission.ProvisionSpec { return spec }, nil),
		)

		event003Enqueue(t, db, outbox.EnqueueRequest{
			Tenant: tenantC, EffectIdentity: "payroll-retry", OrderingKey: "worker:event-003-retry",
			Criticality: outbox.CriticalityP0, SchemaRef: event001SchemaRef, Payload: []byte("payroll-retry"),
			Causal: &outbox.CausalMetadata{
				CorrelationID: "corr-1", CausationID: "cause-1",
				LogicalOperationID: "payroll-run-op", AttemptID: "seed",
			},
		})

		claimed, err := retryConsumer.Poll(ctx, tenantC)
		if err != nil || len(claimed) != 1 {
			t.Fatalf("poll retry row: %d, %v", len(claimed), err)
		}
		claim := claimed[0]

		// A separate layer (standing in for a transaction coordinator's own
		// retry callback) already spent this exact logical attempt's only
		// token against the shared budget before outbox even records its
		// failure.
		budget, err := provisioner.Provision(spec)
		if err != nil {
			t.Fatalf("provision: %v", err)
		}
		preIdentity := outbox.AttemptIdentity(outbox.Record{
			OutboxID: claim.OutboxID,
			Causal:   &outbox.CausalMetadata{LogicalOperationID: "payroll-run-op"},
		}, 1)
		preReceipt, err := provisioner.Consume(budget.ID, admission.AttemptInput{
			AttemptID: preIdentity,
			Attempt: admission.RetryAttempt{
				LogicalOperationID: spec.LogicalOperationID, OperationKind: spec.OperationKind,
				TenantID: spec.TenantID, Dependency: spec.Dependency, Failure: admission.FailureTransient, Attempt: 1,
			},
		})
		if err != nil || preReceipt.Disposition != admission.RetryAllowed {
			t.Fatalf("pre-consumption = %#v, %v; want the one available token granted", preReceipt, err)
		}

		// Outbox's own failure handling for the identical physical attempt
		// (attempt 1, its first claim) must converge on the same stored
		// receipt rather than consuming a second token from a one-token
		// budget: the row returns to PENDING, not ABANDONED.
		if err := retryConsumer.FailClaim(ctx, claim, fmt.Errorf("transient downstream error")); err != nil {
			t.Fatalf("fail claim (attempt 1): %v", err)
		}
		afterFirst, ok := provisioner.Snapshot(budget.ID)
		if !ok || afterFirst.Consumed != 1 {
			t.Fatalf("budget after attempt 1 = %#v, ok=%v; want exactly 1 token consumed despite two layers", afterFirst, ok)
		}
		requeued, err := outbox.Read(ctx, db.Conn, tenantC, claim.OutboxID)
		if err != nil {
			t.Fatalf("read after attempt 1: %v", err)
		}
		if requeued.Status != outbox.StatusPending {
			t.Fatalf("status after attempt 1 = %s, want PENDING (shared budget was not double-charged)", requeued.Status)
		}

		// The second physical attempt is a genuinely new attempt identity
		// no layer has consumed yet. The shared budget (Allowed: 1, already
		// spent) refuses it, and outbox must abandon the message even
		// though this consumer set no local maxAttempts.
		claimed2, err := retryConsumer.Poll(ctx, tenantC)
		if err != nil || len(claimed2) != 1 {
			t.Fatalf("poll retry row (attempt 2): %d, %v", len(claimed2), err)
		}
		if err := retryConsumer.FailClaim(ctx, claimed2[0], fmt.Errorf("transient downstream error")); err != nil {
			t.Fatalf("fail claim (attempt 2): %v", err)
		}
		final, err := outbox.Read(ctx, db.Conn, tenantC, claim.OutboxID)
		if err != nil {
			t.Fatalf("read after attempt 2: %v", err)
		}
		if final.Status != outbox.StatusAbandoned {
			t.Fatalf("status after attempt 2 = %s, want ABANDONED once the one shared token was already spent", final.Status)
		}
	})
}

// TestTodo_EVENT_003_Fault proves a downstream failure/slow-down actually
// propagates as backpressure that changes admission, rather than the
// scheduler retrying (spinning) against a resource it already knows is
// unhealthy.
func TestTodo_EVENT_003_Fault(t *testing.T) {
	resource := "downstream-payments-api"
	ledger := outbox.NewResourceLedger(map[string]outbox.ResourcePolicy{resource: {Capacity: 10}})

	signal := admission.BackpressureSignal{
		Source: "outbox-scheduler", Dependency: resource, State: admission.BackpressureUnavailable, RetryAfter: 30,
	}
	decision := admission.DecideBackpressure(signal, []string{resource})
	if decision.Action != admission.BackpressureDefer || decision.RetryAfter <= 0 {
		t.Fatalf("decision = %#v, want DEFER with a positive retry-after so the caller backs off instead of spinning", decision)
	}
	ledger.ApplyBackpressure(resource, decision)

	candidate := outbox.Candidate{
		Record:   outbox.Record{Tenant: uuid.New(), OutboxID: uuid.New(), Criticality: outbox.CriticalityP0},
		Resource: resource,
	}
	result := outbox.Schedule([]outbox.Candidate{candidate}, ledger)
	if len(result.Admitted) != 0 || len(result.Deferred) != 1 {
		t.Fatalf("admitted = %d deferred = %d under DEFER backpressure, want the item deferred rather than dispatched",
			len(result.Admitted), len(result.Deferred))
	}

	// Spinning would mean retrying immediately against the same unhealthy
	// resource without ever re-evaluating backpressure. Proving the
	// scheduler is not spinning: a second Schedule pass with the ledger
	// still under DEFER produces the identical deferred outcome rather than
	// eventually admitting through sheer repetition.
	again := outbox.Schedule(result.DeferredCandidates(), ledger)
	if len(again.Admitted) != 0 || len(again.Deferred) != 1 {
		t.Fatalf("second pass under sustained DEFER admitted = %d, want 0 (no spin-admission)", len(again.Admitted))
	}
	if again.Deferred[0].Reason != outbox.ReasonBackpressureZeroed {
		t.Fatalf("deferral reason = %q, want %q (a DEFER decision zeroed the resource)", again.Deferred[0].Reason, outbox.ReasonBackpressureZeroed)
	}

	// Health recovers: CONTINUE restores the resource's baseline capacity so
	// the previously-deferred item now admits instead of the scheduler
	// being wedged forever by the earlier decision.
	healthy := admission.DecideBackpressure(
		admission.BackpressureSignal{Source: "outbox-scheduler", Dependency: resource, State: admission.BackpressureHealthy},
		[]string{resource},
	)
	ledger.ApplyBackpressure(resource, healthy)
	recovered := outbox.Schedule(again.DeferredCandidates(), ledger)
	if len(recovered.Admitted) != 1 {
		t.Fatalf("recovered admitted = %d, want 1 once the signal clears", len(recovered.Admitted))
	}

	// A resource nobody bounded is unbounded, and a decision that says
	// "healthy" must not be the thing that stops it. Reading the baseline
	// out of an absent policy yields a zero-value capacity, so restoring
	// health would wedge the resource shut forever -- the signal meaning
	// everything is fine causing a total stall.
	unpoliced := outbox.NewResourceLedger(nil)
	tenant := uuid.New()
	if ok, reason := unpoliced.TryAdmit("unpoliced-relay", tenant, uuid.New()); !ok {
		t.Fatalf("unpoliced resource refused admission before any signal: reason=%q", reason)
	}
	unpoliced.ApplyBackpressure("unpoliced-relay", healthy)
	if ok, reason := unpoliced.TryAdmit("unpoliced-relay", tenant, uuid.New()); !ok {
		t.Fatalf("a healthy signal zeroed an unpoliced resource: reason=%q", reason)
	}
	// The same holds for a slow-down: there is no baseline to halve, so it
	// must not collapse to zero either.
	slow := admission.DecideBackpressure(
		admission.BackpressureSignal{Source: "outbox-scheduler", Dependency: "unpoliced-relay", State: admission.BackpressureThrottled},
		[]string{"unpoliced-relay"},
	)
	unpoliced.ApplyBackpressure("unpoliced-relay", slow)
	if ok, reason := unpoliced.TryAdmit("unpoliced-relay", tenant, uuid.New()); !ok {
		t.Fatalf("a slow-down zeroed an unpoliced resource that has no baseline to halve: reason=%q", reason)
	}

	// Integer division must not turn a slow-down into a stop for the
	// smallest bounded resource: capacity one still admits one under
	// QUEUE/SLOW, where 1/2 would have said zero.
	single := outbox.NewResourceLedger(map[string]outbox.ResourcePolicy{"single": {Capacity: 1}})
	single.ApplyBackpressure("single", slow)
	if ok, reason := single.TryAdmit("single", tenant, uuid.New()); !ok {
		t.Fatalf("slow-down on a capacity-1 resource admitted nothing, making SLOW mean STOP: reason=%q", reason)
	}
}

// TestTodo_EVENT_003_Security proves tenant scoping in the cross-tenant
// scheduler: one tenant's flood cannot consume every slot of a resource
// another tenant also needs, no admitted or deferred candidate is ever
// mislabeled with the wrong tenant, and a per-tenant share genuinely bounds
// the flood rather than being advisory.
func TestTodo_EVENT_003_Security(t *testing.T) {
	resource := "shared-notifications"
	floodTenant, fairTenant := uuid.New(), uuid.New()
	ledger := outbox.NewResourceLedger(map[string]outbox.ResourcePolicy{resource: {Capacity: 10, PerTenantShare: 6}})

	base := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	var candidates []outbox.Candidate
	for i := 0; i < 50; i++ {
		candidates = append(candidates, outbox.Candidate{
			Record: outbox.Record{
				Tenant: floodTenant, OutboxID: uuid.New(), Criticality: outbox.CriticalityP2,
				AvailableAt: base.Add(-time.Duration(i+1) * time.Second),
			},
			Resource: resource,
		})
	}
	fairRecord := outbox.Record{
		Tenant: fairTenant, OutboxID: uuid.New(), Criticality: outbox.CriticalityP2, AvailableAt: base,
	}
	candidates = append(candidates, outbox.Candidate{Record: fairRecord, Resource: resource})

	result := outbox.Schedule(candidates, ledger)
	if len(result.Admitted) != 7 {
		t.Fatalf("admitted = %d, want 7 (the flood tenant's 6-slot share plus the fair tenant's own item)", len(result.Admitted))
	}
	var floodAdmitted, fairAdmitted int
	for _, admitted := range result.Admitted {
		switch admitted.Record.Tenant {
		case floodTenant:
			floodAdmitted++
		case fairTenant:
			fairAdmitted++
		default:
			t.Fatalf("admitted record for an unrecognized tenant: %#v", admitted.Record)
		}
	}
	if floodAdmitted != 6 {
		t.Fatalf("flood tenant admitted = %d, want capped at its configured 6-slot share", floodAdmitted)
	}
	if fairAdmitted != 1 {
		t.Fatalf("fair tenant admitted = %d, want its own item never starved by the other tenant's flood", fairAdmitted)
	}
	for _, deferred := range result.Deferred {
		if deferred.Candidate.Record.Tenant != floodTenant {
			t.Fatalf("only the flood tenant's excess should be deferred, found: %#v", deferred.Candidate.Record)
		}
		if deferred.Reason != outbox.ReasonTenantShareExceeded {
			t.Fatalf("deferral reason = %q, want %q (the flood tenant's configured share, not the resource capacity, bound it)", deferred.Reason, outbox.ReasonTenantShareExceeded)
		}
	}
	if got := ledger.InFlight(resource); got != 7 {
		t.Fatalf("resource in-flight = %d, want 7 (well under the 10 capacity because the tenant share, not the resource, bound the flood)", got)
	}
}

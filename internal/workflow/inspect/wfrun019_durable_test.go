package inspect_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/reconcile"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/inspect"
)

// The durable half of the WF-RUN-019 matrix. inspect.Build projects state a
// caller loaded; inspect.Load is the reader that loads it, and these helpers
// prove it traverses the durable records rather than caller-supplied
// references: every family below is written through its owning store and
// read back only through Load.

const (
	durableEffectRef = "effect:payroll.worker_sync/88191"
	durableSchemaRef = "hcmnext.events.v1.OutboxEvent@1"
	durableTraceID   = "4bf92f3577b34da6a3ce929d0e0e4736"
	durablePayload   = "payload:protected-salary-figure-123456"
)

// seedDurableFamilies writes, for an instance the caller already stored with
// one execution of nodeSync that recorded durableEffectRef, a pending
// RETRY_BACKOFF timer, a work item, an outbox row for the effect and a
// reconciliation job watching it.
func seedDurableFamilies(t *testing.T, db *pgtest.DB, conn *pgxadapter.Conn, tenant, instanceID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	db.Exec(t, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.events.v1.OutboxEvent', 1,
			'hcmnext.events.v1.OutboxEvent', 'PROTOBUF', 'LEDGER_EVENT')`, tenant, durableSchemaRef)

	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := (runtimestate.TimerStore{}).Set(ctx, tx, runtimestate.Timer{
			TenantID: tenant, TimerID: uuid.New(), InstanceID: instanceID, NodeID: nodeSync,
			Key: "retry-backoff/4", Kind: runtimestate.TimerRetryBackoff,
			FiresAt: fixtureEnded.Add(5 * time.Minute), CreatedAt: fixtureEnded,
		}); err != nil {
			return err
		}
		item := admin008TaskItem()
		item.TenantID, item.WorkItemID, item.WorkflowInstanceID = tenant, uuid.New(), instanceID
		item.CorrelationID, item.SubjectRefs = "corr-88191", []string{"person:jane"}
		item.OwnerRef, item.OrganizationScopeID = "route:payroll", "org:payroll"
		if _, err := (workitem.Store{}).Create(ctx, tx, item, workitem.TransitionMeta{
			ActorPrincipalID: "principal:workflow-runtime", Reason: "CREATED_BY_WORKFLOW", At: fixtureEnded,
		}); err != nil {
			return err
		}
		if _, err := outbox.Enqueue(ctx, tx, outbox.EnqueueRequest{
			Tenant: tenant, OutboxID: uuid.New(), EffectIdentity: durableEffectRef,
			OrderingKey: "worker:jane", SchemaRef: durableSchemaRef, Payload: []byte(durablePayload),
			Causal: &outbox.CausalMetadata{
				CorrelationID: "corr-88191", CausationID: "node:payroll_sync/3",
				LogicalOperationID: "payroll-sync", AttemptID: "attempt-3",
				TraceLink: &outbox.TraceLink{TraceID: durableTraceID, SpanID: "00f067aa0ba902b7",
					TraceFlags: 1, ExpiresAt: fixtureEnded.Add(24 * time.Hour)},
			},
		}); err != nil {
			return err
		}
		_, _, err := (reconcile.PostgresStore{}).Create(ctx, tx, reconcile.Job{
			TenantID: tenant, JobID: uuid.New(), EffectRef: durableEffectRef, EffectID: "node.payroll_sync",
			PolicyRef: "policy/reconcile-v1", IntendedRef: "proposal:88191",
			RequiredFreshness: observe.FreshnessFresh, NextCheckAt: fixtureEnded.Add(time.Minute),
			Deadline: fixtureEnded.Add(time.Hour), Owner: "holder:reconciler", SLARef: "sla:payroll-sync",
			RepairPolicy: "NONE", Status: reconcile.StatusPending, Version: 1,
			CreatedAt: fixtureEnded, UpdatedAt: fixtureEnded,
		})
		return err
	})
}

// loadDurable runs one inspect.Load in its own tenant-scoped transaction.
func loadDurable(conn *pgxadapter.Conn, tenant, instanceID uuid.UUID, auth inspect.Authorization) (inspect.DurableView, error) {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return inspect.DurableView{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return inspect.DurableView{}, err
	}
	return inspect.Load(ctx, tx, inspect.LoadRequest{
		TenantID: tenant, InstanceID: instanceID, Authorization: auth,
		WorkItems: inspect.WorkItemAuthorization{Disclosed: true},
	})
}

// assertDurableTraversal is the durable GREEN clause over a store-written
// instance: every family is read back from PostgreSQL, the effect reference
// is followed to its outbox row and reconciliation job, the retry state is
// the durable RETRY_BACKOFF timer, families with no store are UNAVAILABLE
// rather than empty, and the protected payload never reaches the view.
func assertDurableTraversal(t *testing.T, db *pgtest.DB, conn *pgxadapter.Conn, tenant, instanceID uuid.UUID) {
	t.Helper()
	seedDurableFamilies(t, db, conn, tenant, instanceID)

	dv, err := loadDurable(conn, tenant, instanceID, operatorAuth())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantStates := map[string]inspect.RecordState{
		inspect.FamilyInstance:            inspect.RecordLoaded,
		inspect.FamilyNodeExecution:       inspect.RecordLoaded,
		inspect.FamilyCompiledVersion:     inspect.RecordUnavailable,
		inspect.FamilyExecutionContext:    inspect.RecordIntegrityFailed,
		inspect.FamilyTimer:               inspect.RecordLoaded,
		inspect.FamilyAdvancementReceipt:  inspect.RecordNotRecorded,
		inspect.FamilyWorkItem:            inspect.RecordLoaded,
		inspect.FamilyLease:               inspect.RecordNotRecorded,
		inspect.FamilyCheckpoint:          inspect.RecordNotRecorded,
		inspect.FamilyOutbox:              inspect.RecordLoaded,
		inspect.FamilyReconciliation:      inspect.RecordLoaded,
		inspect.FamilyBusinessTransaction: inspect.RecordUnavailable,
		inspect.FamilyConnectorOperation:  inspect.RecordUnavailable,
		inspect.FamilyTrace:               inspect.RecordLoaded,
	}
	if len(dv.Records) != len(wantStates) {
		t.Errorf("manifest lists %d families, want %d: %+v", len(dv.Records), len(wantStates), dv.Records)
	}
	for family, want := range wantStates {
		rec, ok := dv.Record(family)
		if !ok {
			t.Errorf("family %s missing from the manifest", family)
			continue
		}
		if rec.State != want {
			t.Errorf("family %s = %s (%s), want %s", family, rec.State, rec.Reason, want)
		}
	}

	// Retry state is the durable backoff timer, not a caller-supplied ref.
	if len(dv.Attempts) != 1 || dv.Attempts[0].NodeID != nodeSync || !dv.Attempts[0].Current ||
		dv.Attempts[0].LatestAttempt != 3 || dv.Attempts[0].RetryTimers != 1 || dv.Attempts[0].NextRetryAt == nil ||
		!dv.Attempts[0].NextRetryAt.Equal(fixtureEnded.Add(5*time.Minute)) {
		t.Errorf("attempt history = %+v, want payroll_sync attempt 3 with one pending retry at +5m", dv.Attempts)
	}
	if len(dv.Timers) != 1 || dv.Timers[0].Kind != runtimestate.TimerRetryBackoff || dv.Timers[0].State != runtimestate.TimerPending {
		t.Errorf("timers = %+v", dv.Timers)
	}
	if len(dv.WorkItems.Items) != 1 || !dv.WorkItems.Items[0].TransitionsRecorded {
		t.Errorf("work items = %+v", dv.WorkItems.Items)
	}

	// connector -> observation/reconciliation, keyed by the recorded effect.
	if len(dv.Effects) != 1 {
		t.Fatalf("effects = %+v, want the one recorded effect", dv.Effects)
	}
	eff := dv.Effects[0]
	if eff.EffectRef != durableEffectRef || eff.OutboxState != inspect.RecordLoaded || eff.Outbox == nil ||
		eff.Outbox.Status != "PENDING" || len(eff.NodeIDs) != 1 || eff.NodeIDs[0] != nodeSync {
		t.Errorf("effect = %+v (outbox %+v)", eff, eff.Outbox)
	}
	if eff.ReconciliationState != inspect.RecordLoaded || len(eff.Reconciliation) != 1 ||
		eff.Reconciliation[0].Status != string(reconcile.StatusPending) || eff.Reconciliation[0].IntendedRef != "proposal:88191" {
		t.Errorf("reconciliation = %+v", eff.Reconciliation)
	}

	// trace: the node's trace id and the outbox trace link.
	if !contains(dv.TraceIDs.Values, "trace:payroll-sync") || !contains(dv.TraceIDs.Values, durableTraceID) {
		t.Errorf("trace ids = %v", dv.TraceIDs.Values)
	}

	// The stored context ref names no stored context: drift is a named gap.
	if dv.ExecutionContext.Verified || dv.Completeness.Complete {
		t.Errorf("a drifted context verified (%v) or the view claims completeness (%v)",
			dv.ExecutionContext.Verified, dv.Completeness.Complete)
	}
	for _, want := range []string{inspect.FamilyExecutionContext, inspect.FamilyCompiledVersion} {
		if !anyContains(dv.Completeness.Gaps, want) {
			t.Errorf("gaps = %v, want %s named", dv.Completeness.Gaps, want)
		}
	}
	if len(dv.Unavailable) != 2 || dv.Unavailable[0] != inspect.FamilyBusinessTransaction ||
		dv.Unavailable[1] != inspect.FamilyConnectorOperation {
		t.Errorf("unavailable = %v", dv.Unavailable)
	}

	rendered := renderDurable(t, dv)
	if strings.Contains(rendered, durablePayload) {
		t.Error("the rendered durable view leaks the outbox payload")
	}

	// Protected refs are redacted in the durable sections too.
	denied := operatorAuth()
	for _, f := range []string{inspect.FieldInstanceInput, inspect.FieldInstanceContext, inspect.FieldNodeInput, inspect.FieldNodeOutput} {
		denied.Fields[f] = inspect.Ruling{Effect: inspect.EffectDeny, Reason: "CLASSIFICATION_CONFIDENTIAL_HR"}
	}
	redacted, err := loadDurable(conn, tenant, instanceID, denied)
	if err != nil {
		t.Fatalf("Load with protected fields denied: %v", err)
	}
	if !redacted.ExecutionContext.ContextDigest.IsRedacted() {
		t.Error("the execution context digest was disclosed to a caller denied it")
	}
	out := renderDurable(t, redacted)
	for _, secret := range []string{"ctx:legal.us-ca/2026.1", "artifact:input/payroll-sync", "artifact:input/promotion-88191", durablePayload} {
		if strings.Contains(out, secret) {
			t.Errorf("the redacted durable view leaks %q", secret)
		}
	}

	// A denied connector stage withholds the effect traversal as a family.
	noConnector := operatorAuth()
	noConnector.Sections[inspect.SectionConnector] = inspect.Ruling{Effect: inspect.EffectDeny, Reason: "NO_CONNECTOR_SCOPE"}
	withheld, err := loadDurable(conn, tenant, instanceID, noConnector)
	if err != nil {
		t.Fatalf("Load with the connector stage denied: %v", err)
	}
	if rec, _ := withheld.Record(inspect.FamilyOutbox); rec.State != inspect.RecordRedacted || len(withheld.Effects) != 0 {
		t.Errorf("denied connector stage: outbox family %+v, effects %+v", rec, withheld.Effects)
	}
	if !contains(withheld.Completeness.Redactions, "records."+inspect.FamilyOutbox) {
		t.Errorf("redactions = %v, want the withheld outbox family named", withheld.Completeness.Redactions)
	}
}

// assertTenantIsolation proves another tenant cannot disclose the instance,
// and cannot tell it apart from one that does not exist.
func assertTenantIsolation(t *testing.T, db *pgtest.DB, tenant, instanceID uuid.UUID) {
	t.Helper()
	other := insertTenant(t, db, "wfrun019-other-"+tenant.String()[:8])
	conn := appConn(t, db)
	_, foreignErr := loadDurable(conn, other, instanceID, operatorAuth())
	if !errors.Is(foreignErr, inspect.ErrNotDisclosable) {
		t.Fatalf("another tenant's Load = %v, want ErrNotDisclosable", foreignErr)
	}
	_, missingErr := loadDurable(conn, other, uuid.New(), operatorAuth())
	if foreignErr.Error() != missingErr.Error() {
		t.Errorf("a foreign instance (%q) is distinguishable from a missing one (%q)", foreignErr, missingErr)
	}
	// Even naming the right tenant id is refused when the session is scoped
	// to another tenant: row-level security, not only the filter, holds.
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, other); err != nil {
		t.Fatal(err)
	}
	_, rlsErr := inspect.Load(ctx, tx, inspect.LoadRequest{
		TenantID: tenant, InstanceID: instanceID, Authorization: operatorAuth(),
	})
	if !errors.Is(rlsErr, inspect.ErrNotDisclosable) {
		t.Fatalf("a session scoped to another tenant read the instance: %v", rlsErr)
	}
}

// assertConcurrentLoads proves concurrent readers on separate connections
// render byte-identical durable views.
func assertConcurrentLoads(t *testing.T, db *pgtest.DB, tenant, instanceID uuid.UUID) {
	t.Helper()
	const readers = 4
	conns := make([]*pgxadapter.Conn, readers)
	for i := range conns {
		conns[i] = appConn(t, db)
	}
	results := make([]string, readers)
	var wg sync.WaitGroup
	for i := range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dv, err := loadDurable(conns[i], tenant, instanceID, operatorAuth())
			if err != nil {
				t.Errorf("concurrent Load %d: %v", i, err)
				return
			}
			results[i] = renderDurable(t, dv)
		}()
	}
	wg.Wait()
	for i := 1; i < readers; i++ {
		if results[i] != results[0] {
			t.Fatalf("concurrent durable view %d differs from view 0", i)
		}
	}
}

func renderDurable(t *testing.T, dv inspect.DurableView) string {
	t.Helper()
	b, err := dv.JSON()
	if err != nil {
		t.Fatalf("render durable view: %v", err)
	}
	return string(b)
}

func anyContains(haystack []string, sub string) bool {
	for _, s := range haystack {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

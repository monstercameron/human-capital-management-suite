package pgstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection/critical"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func contractStore(t *testing.T) (*pgtest.DB, *Store) {
	t.Helper()
	db := pgtest.New(t)
	store, err := New(db.Conn, WithClock(func() time.Time { return time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC) }))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return db, store
}

func contractRecord(tenant string) app.IntentRecord {
	now := time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
	id := uuid.NewString()
	msg := &intentsv1.IntentInstance{
		IntentId: id, Definition: &intentsv1.DefinitionReference{IntentTypeId: "contract-test", Version: 1},
		CanonicalRequestDigest: &intentsv1.CanonicalDigestReference{Digest: strings.Repeat("a", 64), AlgorithmId: "sha256"},
		IdempotencyKey:         "create-" + id,
		Lifecycle: &intentsv1.LifecycleDimensions{
			Request:     intentsv1.RequestState_REQUEST_STATE_DRAFT,
			Execution:   intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED,
			Business:    intentsv1.BusinessState_BUSINESS_STATE_NOT_STARTED,
			Consistency: intentsv1.ConsistencyState_CONSISTENCY_STATE_PENDING_OBSERVATION,
			Obligation:  intentsv1.ObligationState_OBLIGATION_STATE_PENDING,
		},
		InstanceVersion: 1, CreatedAt: timestamppb.New(now), RecordedAt: timestamppb.New(now), LastTransitionAt: timestamppb.New(now),
	}
	encoded, err := proto.Marshal(msg)
	if err != nil {
		panic(err)
	}
	return app.IntentRecord{
		Tenant: tenant, IntentID: id,
		Definition:     intent.Ref{TypeID: "contract-test", Version: 1},
		IdempotencyKey: "create-" + id, CorrelationID: uuid.NewString(),
		Lifecycle: lifecycle.Dimensions{
			Request: lifecycle.RequestDraft, Execution: lifecycle.ExecutionNotPlanned,
			Business: lifecycle.BusinessNotStarted, Consistency: lifecycle.ConsistencyPendingObservation,
			Obligation: lifecycle.ObligationPending,
		},
		InstanceVersion: 1,
		RequestDigest:   digest.Reference{AlgorithmID: "sha256", Digest: strings.Repeat("a", 64), CanonicalLength: 2},
		CreatedAt:       now, RecordedAt: now, LastTransitionAt: now,
		Envelope: encoded, EnvelopeSchemaRef: app.EnvelopeSchemaRef,
	}
}

func TestStoreContractIsolationReplayTimelineAndVerify(t *testing.T) {
	db, store := contractStore(t)
	ctx := context.Background()
	for _, tenant := range []string{"tenant-contract-a", "tenant-contract-b"} {
		if err := store.Bootstrap(ctx, tenant); err != nil {
			t.Fatalf("Bootstrap(%q): %v", tenant, err)
		}
		if err := store.Bootstrap(ctx, tenant); err != nil {
			t.Fatalf("idempotent Bootstrap(%q): %v", tenant, err)
		}
	}

	first := contractRecord("tenant-contract-a")
	created, err := store.AppendIntent(ctx, first)
	if err != nil {
		t.Fatalf("AppendIntent: %v", err)
	}
	if created.Replayed || !created.ProjectionApplied || created.OutboxID == "" {
		t.Fatalf("first append receipt = %+v", created)
	}
	changedReplay := first
	changedReplay.IntentID = uuid.NewString()
	changedReplay.Envelope = []byte("different bytes for the same idempotency key")
	replay, err := store.AppendIntent(ctx, changedReplay)
	if err != nil {
		t.Fatalf("replay AppendIntent: %v", err)
	}
	if !replay.Replayed || replay.Record.IntentID != first.IntentID {
		t.Fatalf("replay result = %+v", replay)
	}
	var events, outboxRows int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM ledger_event WHERE tenant_id=$1 AND stream_key=$2`, TenantID(first.Tenant), StreamKey(first.IntentID)).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE tenant_id=$1 AND ordering_key=$2`, TenantID(first.Tenant), StreamKey(first.IntentID)).Scan(&outboxRows); err != nil {
		t.Fatal(err)
	}
	if events != 1 || outboxRows != 1 {
		t.Fatalf("replay wrote events=%d outbox=%d; want one each", events, outboxRows)
	}

	loaded, err := store.LoadIntent(ctx, first.Tenant, first.IntentID)
	if err != nil || loaded.Envelope == nil || string(loaded.Envelope) != string(first.Envelope) || loaded.RequestDigest.CanonicalLength == 0 {
		t.Fatalf("LoadIntent = %+v, err=%v", loaded, err)
	}
	if _, err := store.LoadIntent(ctx, "tenant-contract-b", first.IntentID); !errors.Is(err, app.ErrIntentNotFound) {
		t.Fatalf("cross-tenant LoadIntent error = %v, want ErrIntentNotFound", err)
	}
	if _, err := store.LoadIntent(ctx, first.Tenant, "not-a-uuid"); !errors.Is(err, app.ErrIntentNotFound) {
		t.Fatalf("invalid identity LoadIntent error = %v, want ErrIntentNotFound", err)
	}
	if _, err := store.LoadIntent(ctx, first.Tenant, uuid.NewString()); !errors.Is(err, app.ErrIntentNotFound) {
		t.Fatalf("unknown identity LoadIntent error = %v, want ErrIntentNotFound", err)
	}
	second := contractRecord(first.Tenant)
	if _, err := store.AppendIntent(ctx, second); err != nil {
		t.Fatalf("AppendIntent second tenant A record: %v", err)
	}
	pageA, err := store.ListIntents(ctx, first.Tenant, 1, "")
	if err != nil || len(pageA.Records) != 1 || pageA.Records[0].Tenant != first.Tenant || pageA.NextCursor == "" {
		t.Fatalf("tenant A page = %+v, err=%v", pageA, err)
	}
	pageA2, err := store.ListIntents(ctx, first.Tenant, 1, pageA.NextCursor)
	if err != nil || len(pageA2.Records) != 1 || pageA2.NextCursor != "" || pageA2.Records[0].IntentID == pageA.Records[0].IntentID {
		t.Fatalf("tenant A second page = %+v, err=%v", pageA2, err)
	}
	pageB, err := store.ListIntents(ctx, "tenant-contract-b", 10, "")
	if err != nil || len(pageB.Records) != 0 {
		t.Fatalf("tenant B page = %+v, err=%v", pageB, err)
	}
	defaultPage, err := store.ListIntents(ctx, first.Tenant, 0, "")
	if err != nil || len(defaultPage.Records) != 2 {
		t.Fatalf("default sized tenant A page = %+v, err=%v", defaultPage, err)
	}
	entries, err := store.Timeline(ctx, first.Tenant, first.IntentID)
	if err != nil || len(entries) != 1 || entries[0].Sequence != 1 || entries[0].SchemaRef != first.EnvelopeSchemaRef || entries[0].Digest == "" {
		t.Fatalf("Timeline = %+v, err=%v", entries, err)
	}
	if err := store.Verify(ctx, first.Tenant, first.IntentID); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	verified, err := critical.Verify(ctx, db.Conn, ledger.NewReader(), critical.ProtoMapper{}, TenantID(first.Tenant), StreamKey(first.IntentID))
	if err != nil || !verified.OK() {
		t.Fatalf("critical.Verify = %+v, err=%v; want the creation row reconstructed from its ledger event", verified, err)
	}
}

func TestTodo_REV_012_02_Integration(t *testing.T) {
	db, store := contractStore(t)
	ctx := context.Background()
	tenant := "tenant-rev-012-02"
	if err := store.Bootstrap(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	rec := contractRecord(tenant)
	if _, err := store.AppendIntent(ctx, rec); err != nil {
		t.Fatal(err)
	}

	// The actual Store read succeeds while its own creation head is applied.
	if _, err := store.LoadIntent(ctx, tenant, rec.IntentID); err != nil {
		t.Fatalf("current intent projection was refused: %v", err)
	}
	checkpointArgs := []any{TenantID(tenant), ProjectionName, StreamKey(rec.IntentID)}
	var appliedDigest string
	if err := db.Conn.QueryRow(ctx, `SELECT last_applied_digest FROM projection_checkpoint WHERE tenant_id=$1 AND projection_name=$2 AND stream_key=$3`, checkpointArgs...).Scan(&appliedDigest); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Conn.Exec(ctx, `UPDATE projection_checkpoint SET last_applied_sequence=0, last_applied_digest=NULL WHERE tenant_id=$1 AND projection_name=$2 AND stream_key=$3`, checkpointArgs...); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadIntent(ctx, tenant, rec.IntentID)
	var barrierErr projection.BarrierError
	if !errors.As(err, &barrierErr) || barrierErr.Status != projection.BarrierStale || loaded.IntentID != "" {
		t.Fatalf("stale decision-context read = %+v, %v", loaded, err)
	}
	if _, err := db.Conn.Exec(ctx, `UPDATE projection_checkpoint SET last_applied_sequence=1, last_applied_digest=$4, status='REBUILDING' WHERE tenant_id=$1 AND projection_name=$2 AND stream_key=$3`, append(checkpointArgs, appliedDigest)...); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.LoadIntent(ctx, tenant, rec.IntentID)
	if !errors.As(err, &barrierErr) || barrierErr.Status != projection.BarrierRebuilding || loaded.IntentID != "" {
		t.Fatalf("rebuilding decision-context read = %+v, %v", loaded, err)
	}
	if _, err := db.Conn.Exec(ctx, `UPDATE projection_checkpoint SET status='CURRENT' WHERE tenant_id=$1 AND projection_name=$2 AND stream_key=$3`, checkpointArgs...); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.LoadIntent(ctx, tenant, rec.IntentID)
	if err != nil || loaded.IntentID != rec.IntentID {
		t.Fatalf("recovered decision-context read = %+v, %v", loaded, err)
	}
}

func TestTodo_REV_012_02_Fault(t *testing.T) {
	db, store := contractStore(t)
	ctx := context.Background()
	tenant := "tenant-rev-012-02-fault"
	if err := store.Bootstrap(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	rec := contractRecord(tenant)
	if _, err := store.AppendIntent(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Conn.Exec(ctx, `UPDATE projection_checkpoint SET status='REBUILDING' WHERE tenant_id=$1 AND projection_name=$2 AND stream_key=$3`, TenantID(tenant), ProjectionName, StreamKey(rec.IntentID)); err != nil {
		t.Fatal(err)
	}
	err := checkIntentReadBarrier(ctx, db.Conn, TenantID(tenant), rec.IntentID, time.Now().UTC().Add(-time.Second))
	var barrierErr projection.BarrierError
	if !errors.As(err, &barrierErr) || barrierErr.Status != projection.BarrierTimeout {
		t.Fatalf("expired decision-context barrier = %v, want typed TIMEOUT", err)
	}
	// The missing stream is hidden using the same not-found result as an
	// unknown tenant-scoped intent; it does not disclose projection metadata.
	if _, err := store.LoadIntent(ctx, tenant, uuid.NewString()); !errors.Is(err, app.ErrIntentNotFound) {
		t.Fatalf("missing intent barrier error = %v, want ErrIntentNotFound", err)
	}
}

func TestStoreContractLifecycleOutcomeAndActiveTenants(t *testing.T) {
	db, store := contractStore(t)
	ctx := context.Background()
	tenant := "tenant-lifecycle-contract"
	if err := store.Bootstrap(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	rec := contractRecord(tenant)
	if _, err := store.AppendIntent(ctx, rec); err != nil {
		t.Fatalf("AppendIntent: %v", err)
	}
	mutatedDims := lifecycle.Dimensions{
		Request: lifecycle.RequestSubmitted, Execution: lifecycle.ExecutionScheduled,
		Business: lifecycle.BusinessInProgress, Consistency: lifecycle.ConsistencyPendingObservation,
		Obligation: lifecycle.ObligationPending,
	}
	mutated, err := store.MutateLifecycle(ctx, app.LifecycleMutation{
		Tenant: tenant, IntentID: rec.IntentID, ExpectedInstanceVersion: 1,
		Lifecycle: mutatedDims, RecordedAt: rec.CreatedAt.Add(time.Minute),
	})
	if err != nil || mutated.InstanceVersion != 2 || mutated.Lifecycle != mutatedDims {
		t.Fatalf("MutateLifecycle = %+v, err=%v", mutated, err)
	}
	if _, err := store.MutateLifecycle(ctx, app.LifecycleMutation{
		Tenant: tenant, IntentID: rec.IntentID, ExpectedInstanceVersion: 1,
		Lifecycle: mutatedDims, RecordedAt: rec.CreatedAt.Add(2 * time.Minute),
	}); !errors.Is(err, app.ErrOutcomeProjectionConflict) {
		t.Fatalf("stale lifecycle mutation error = %v", err)
	}

	outcomeDims := lifecycle.Dimensions{
		Request: lifecycle.RequestApproved, Execution: lifecycle.ExecutionCommitted,
		Business: lifecycle.BusinessCompleted, Consistency: lifecycle.ConsistencyConsistent,
		Obligation: lifecycle.ObligationSatisfied,
	}
	receipt := intent.OutcomeReceipt{
		IntentID: rec.IntentID, WorkflowInstanceID: "workflow-" + rec.IntentID,
		TerminalCode: "COMMITTED", Dimensions: outcomeDims,
		Reconciliation: intent.ReconciliationPass, CommitReceiptRef: "commit-" + rec.IntentID,
		RecordedAt: rec.CreatedAt.Add(3 * time.Minute),
	}
	binding := app.OutcomeBinding{Tenant: tenant, ExpectedInstanceVersion: 2, Receipt: receipt}
	if err := store.BindOutcome(ctx, binding); err != nil {
		t.Fatalf("BindOutcome: %v", err)
	}
	if err := store.BindOutcome(ctx, binding); err != nil {
		t.Fatalf("idempotent BindOutcome: %v", err)
	}
	final, err := store.LoadIntent(ctx, tenant, rec.IntentID)
	if err != nil || final.InstanceVersion != 3 || final.Lifecycle != outcomeDims || final.CommitReceiptRef != receipt.CommitReceiptRef {
		t.Fatalf("outcome projection = %+v, err=%v", final, err)
	}
	verified, err := critical.Verify(ctx, db.Conn, ledger.NewReader(), critical.ProtoMapper{}, TenantID(tenant), StreamKey(rec.IntentID))
	if err != nil || !verified.OK() {
		t.Fatalf("critical.Verify after lifecycle and outcome writes = %+v, err=%v; want both current row families replayed from ledger", verified, err)
	}
	active, err := ActiveTenants(ctx, db.Conn)
	if err != nil {
		t.Fatalf("ActiveTenants: %v", err)
	}
	if len(active) != 1 || active[0] != TenantID(tenant) {
		t.Fatalf("active tenants = %v, want only %s", active, TenantID(tenant))
	}
}

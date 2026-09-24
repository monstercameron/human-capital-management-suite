package commit_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/conflictstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	intentmodel "github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transactioncommit "github.com/monstercameron/human-capital-management-suite/internal/transaction/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/conflict"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/plan"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type recordingConflictFence struct {
	req conflict.CommitRequest
	err error
}

func (f *recordingConflictFence) ValidateAtCommit(_ context.Context, _ dbport.Tx, req conflict.CommitRequest) (conflict.CommitResult, error) {
	f.req = req
	return conflict.CommitResult{}, f.err
}

func bindConflict(prepared plan.TransactionPlan, id, snapshot string) plan.TransactionPlan {
	prepared.ConflictIntentID = id
	prepared.ConflictSnapshotDigest = snapshot
	if id != "" && len(prepared.ConflictFootprintDigests) == 0 {
		prepared.ConflictFootprintDigests = []string{"scope-test"}
	}
	if id == "" {
		prepared.ConflictFootprintDigests = nil
	}
	sum := sha256.Sum256(prepared.CanonicalBytes())
	prepared.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return prepared
}

func TestCommitRequiresAndUsesPlanBoundConflictFence(t *testing.T) {
	db, prepared, tenant := commitFixture(t)
	prepared = bindConflict(prepared, "write-intent-1", "sha256:conflict-snapshot-1")
	if _, err := committer(t, db).Commit(context.Background(), prepared); !errors.Is(err, transactioncommit.ErrInvalidPlan) {
		t.Fatalf("commit without required durable fence = %v", err)
	}
	refused := errors.New("fence refused")
	fence := &recordingConflictFence{err: refused}
	c := transactioncommit.New(db.Conn, transactioncommit.Options{Clock: func() time.Time { return commitAt }, ConflictFence: fence})
	if _, err := c.Commit(context.Background(), prepared); !errors.Is(err, refused) {
		t.Fatalf("commit with fence refusal = %v", err)
	}
	if fence.req.TenantID != tenant.String() || fence.req.IntentID != prepared.ConflictIntentID {
		t.Fatalf("fence request = %+v", fence.req)
	}
	var events int
	if err := db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id=$1`, tenant).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 0 {
		t.Fatalf("fence refusal produced %d ledger events", events)
	}
	legacy := prepared
	legacy.ConflictIntentID = ""
	legacy.ConflictSnapshotDigest = ""
	legacy = bindConflict(legacy, "", "")
	if _, err := c.Commit(context.Background(), legacy); !errors.Is(err, transactioncommit.ErrInvalidPlan) {
		t.Fatalf("caller-supplied fence for unbound legacy plan = %v", err)
	}
}

func TestConflictFenceCommitsWithActualCoordinatorTransaction(t *testing.T) {
	db, prepared, tenant := commitFixture(t)
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, tenant.String()); err != nil {
		t.Fatal(err)
	}
	for _, stream := range prepared.Streams {
		if err := ledger.EnsureStream(ctx, tx, tenant, stream.StreamKey, "TRANSACTION", prepared.PlanID); err != nil {
			t.Fatal(err)
		}
	}
	resource, err := values.NewResourceKey(values.TenantId(tenant.String()), values.Kind("assignment"), "worker", "9001")
	if err != nil {
		t.Fatal(err)
	}
	interval, err := values.NewInstantInterval(values.NewInstant(commitAt.Add(10*time.Minute)), values.NewInstant(commitAt.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	var footprints []conflict.WriteFootprint
	for _, stream := range prepared.Streams {
		revision, revisionErr := values.NewSequenceRevision(stream.StreamKey, uint64(stream.ExpectedSequence))
		if revisionErr != nil {
			t.Fatal(revisionErr)
		}
		footprints = append(footprints, conflict.WriteFootprint{Resource: resource, Field: conflict.FieldPath("employment.assignment." + strings.TrimPrefix(stream.StreamKey, "stream:")), Interval: interval, Operation: conflict.OperationUpdate, ExpectedRevision: revision, Authority: conflict.AuthorityScope{Domain: "PEOPLE", PolicyRef: "authority.local/v1"}})
		prepared.Writes = append(prepared.Writes, intentmodel.PlannedWrite{Subject: intentmodel.SubjectReference{Kind: "EMPLOYMENT", SubjectID: "employment:9001", AuthorityDomain: "PEOPLE"}, ResourceKey: resource, FieldPath: "employment.assignment." + strings.TrimPrefix(stream.StreamKey, "stream:"), ExpectedRevision: revision, SourceAuthorityDecision: "authority.local/v1", Operation: intentmodel.WriteOperationUpdate, EffectiveInterval: interval})
	}
	for _, footprint := range footprints {
		prepared.ConflictFootprintDigests = append(prepared.ConflictFootprintDigests, footprint.ScopeDigest())
	}
	store := conflictstore.New()
	intent := conflict.WriteIntent{TenantID: tenant.String(), ID: "intent-" + tenant.String(), ProposalID: prepared.ProposalRevisionID, SnapshotDigest: "snapshot-actual", Footprints: footprints}
	if err := store.Register(ctx, tx, intent); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	prepared = bindConflict(prepared, intent.ID, intent.SnapshotDigest)
	wrongScope := prepared
	wrongScope.ConflictFootprintDigests = append([]string(nil), prepared.ConflictFootprintDigests...)
	wrongScope.ConflictFootprintDigests[0] = strings.Repeat("f", 64)
	wrongScope = bindConflict(wrongScope, intent.ID, intent.SnapshotDigest)
	scopeCommitter := transactioncommit.New(db.Conn, transactioncommit.Options{Clock: func() time.Time { return commitAt }, ConflictFence: store})
	if _, err := scopeCommitter.Commit(ctx, wrongScope); !errors.Is(err, conflict.ErrIntentConflict) {
		t.Fatalf("changed effective scope fence = %v, want intent conflict", err)
	}
	wrongField := prepared
	wrongField.Writes = append([]intentmodel.PlannedWrite(nil), prepared.Writes...)
	wrongField.Writes[0].FieldPath = "employment.assignment.unrelated"
	wrongField = bindConflict(wrongField, intent.ID, intent.SnapshotDigest)
	if _, err := scopeCommitter.Commit(ctx, wrongField); !errors.Is(err, conflict.ErrIntentConflict) {
		t.Fatalf("unrelated planned field fence = %v, want intent conflict", err)
	}
	wrongDomain := prepared
	wrongDomain.Writes = append([]intentmodel.PlannedWrite(nil), prepared.Writes...)
	wrongDomain.Writes[0].Subject.AuthorityDomain = "POSITION"
	wrongDomain = bindConflict(wrongDomain, intent.ID, intent.SnapshotDigest)
	if _, err := scopeCommitter.Commit(ctx, wrongDomain); !errors.Is(err, conflict.ErrIntentConflict) {
		t.Fatalf("transplanted authority domain fence = %v, want intent conflict", err)
	}
	wrongAuthority := prepared
	wrongAuthority.Writes = append([]intentmodel.PlannedWrite(nil), prepared.Writes...)
	wrongAuthority.Writes[0].SourceAuthorityDecision = "authority.attacker/v1"
	wrongAuthority = bindConflict(wrongAuthority, intent.ID, intent.SnapshotDigest)
	if _, err := scopeCommitter.Commit(ctx, wrongAuthority); !errors.Is(err, conflict.ErrIntentConflict) {
		t.Fatalf("substituted source authority fence = %v, want intent conflict", err)
	}
	wrongOperation := prepared
	wrongOperation.Writes = append([]intentmodel.PlannedWrite(nil), prepared.Writes...)
	wrongOperation.Writes[0].Operation = intentmodel.WriteOperationDelete
	wrongOperation = bindConflict(wrongOperation, intent.ID, intent.SnapshotDigest)
	if _, err := scopeCommitter.Commit(ctx, wrongOperation); !errors.Is(err, conflict.ErrIntentConflict) {
		t.Fatalf("substituted operation fence = %v, want intent conflict", err)
	}
	changedInterval, intervalErr := values.NewInstantInterval(values.NewInstant(commitAt.Add(time.Minute)), values.NewInstant(commitAt.Add(time.Hour)))
	if intervalErr != nil {
		t.Fatal(intervalErr)
	}
	wrongInterval := prepared
	wrongInterval.Writes = append([]intentmodel.PlannedWrite(nil), prepared.Writes...)
	wrongInterval.Writes[0].EffectiveInterval = changedInterval
	wrongInterval = bindConflict(wrongInterval, intent.ID, intent.SnapshotDigest)
	if _, err := scopeCommitter.Commit(ctx, wrongInterval); !errors.Is(err, conflict.ErrIntentConflict) {
		t.Fatalf("substituted effective interval fence = %v, want intent conflict", err)
	}
	mismatched := prepared
	mismatched.Streams = append([]plan.StreamPlan(nil), prepared.Streams...)
	mismatched.Events = append([]plan.PlannedEvent(nil), prepared.Events...)
	mismatched.Streams[0].StreamKey = "stream:unrelated"
	mismatched.Events[0].StreamKey = "stream:unrelated"
	mismatched = bindConflict(mismatched, intent.ID, intent.SnapshotDigest)
	mismatchCommitter := transactioncommit.New(db.Conn, transactioncommit.Options{Clock: func() time.Time { return commitAt }, ConflictFence: store})
	if _, err := mismatchCommitter.Commit(ctx, mismatched); !errors.Is(err, conflict.ErrIntentConflict) {
		t.Fatalf("unrelated plan stream fence = %v, want intent conflict", err)
	}

	rollback := transactioncommit.New(db.Conn, transactioncommit.Options{Clock: func() time.Time { return commitAt }, ConflictFence: store, Failpoint: func(stage string) error {
		if stage == "after-append" {
			return errors.New("rollback proof")
		}
		return nil
	}})
	if _, err := rollback.Commit(ctx, prepared); err == nil {
		t.Fatal("failpoint commit succeeded")
	}
	for table, want := range map[string]int{"ledger_event": 0, "conflict_scope_fence": 0} {
		var got int
		if err := db.Conn.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE tenant_id=$1", tenant).Scan(&got); err != nil || got != want {
			t.Fatalf("%s rows=%d err=%v", table, got, err)
		}
	}

	c := transactioncommit.New(db.Conn, transactioncommit.Options{Clock: func() time.Time { return commitAt }, ConflictFence: store})
	receipt, err := c.Commit(ctx, prepared)
	if err != nil {
		t.Fatalf("actual fenced commit: %v", err)
	}
	if len(receipt.Events) != len(prepared.Events) {
		t.Fatalf("events=%d want=%d", len(receipt.Events), len(prepared.Events))
	}
	var effectiveAt time.Time
	if err := db.Conn.QueryRow(ctx, `SELECT effective_at FROM ledger_event WHERE tenant_id=$1 ORDER BY stream_key LIMIT 1`, tenant).Scan(&effectiveAt); err != nil || !effectiveAt.Equal(commitAt.Add(10*time.Minute)) {
		t.Fatalf("ledger effective_at=%s err=%v, want typed interval start", effectiveAt, err)
	}
	var status string
	var fence int64
	if err := db.Conn.QueryRow(ctx, `SELECT status,fence FROM conflict_write_intent WHERE tenant_id=$1 AND intent_id=$2`, tenant, intent.ID).Scan(&status, &fence); err != nil || status != "COMMITTED" || fence == 0 {
		t.Fatalf("intent status=%s fence=%d err=%v", status, fence, err)
	}
}

var commitAt = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func commitFixture(t *testing.T) (*pgtest.DB, plan.TransactionPlan, uuid.UUID) {
	t.Helper()
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'Commit test', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, tenant, "commit-"+tenant.String())
	db.Exec(t, `SELECT set_config('app.tenant_id', $1, false)`, tenant.String())

	p := plan.TransactionPlan{
		PlanID: "commit-plan-" + tenant.String(), Tenant: values.TenantId(tenant.String()),
		ProposalRevisionID: "proposal:" + tenant.String(), ProposalDigest: "sha256:" + strings.Repeat("a", 64),
		IdempotencyKey: "commit:" + tenant.String(), ExpiresAt: values.NewInstant(commitAt.Add(time.Hour)),
		// Deliberately not in stream-key order: the ledger adapter owns the
		// canonical order, while plan order remains material to this digest.
		Streams: []plan.StreamPlan{{StreamKey: "stream:b", ExpectedSequence: 0}, {StreamKey: "stream:a", ExpectedSequence: 0}},
		Events: []plan.PlannedEvent{
			{StreamKey: "stream:b", Sequence: 1, EventType: "B_FACT", SchemaRef: "hcmnext.commit.b/v1", Digest: strings.Repeat("b", 64)},
			{StreamKey: "stream:a", Sequence: 1, EventType: "A_FACT", SchemaRef: "hcmnext.commit.a/v1", Digest: strings.Repeat("a", 64)},
		},
		OutboxEffects: []plan.OutboxEffect{{EffectID: "effect:payroll:" + tenant.String(), DestinationRef: "payroll", SchemaRef: "hcmnext.commit.payroll/v1", PayloadDigest: "sha256:" + strings.Repeat("c", 64), IdempotencyKey: "effect:" + tenant.String()}},
	}
	canonical := sha256.Sum256(p.CanonicalBytes())
	p.Digest = "sha256:" + hex.EncodeToString(canonical[:])
	return db, p, tenant
}

func committer(t *testing.T, db *pgtest.DB) *transactioncommit.Committer {
	t.Helper()
	return transactioncommit.New(db.Conn, transactioncommit.Options{Clock: func() time.Time { return commitAt }})
}

// TestTodo_REV_102_06_TransactionCommitPath proves the production generic
// transaction.commit capability is governed as permanent even when its
// caller supplies only a duration. The committed key compacts after expiry.
func TestTodo_REV_102_06_TransactionCommitPath(t *testing.T) {
	db, prepared, tenant := commitFixture(t)
	if _, err := committer(t, db).Commit(context.Background(), prepared); err != nil {
		t.Fatalf("commit prepared transaction: %v", err)
	}

	scope := idempotency.Scope{
		Tenant: tenant, Capability: "transaction.commit",
		EffectScope: prepared.PlanID, Key: prepared.IdempotencyKey,
	}
	store := idempotency.PostgresStore{}
	var rec idempotency.Record
	var found bool
	if err := db.Conn.QueryRow(context.Background(), `SELECT request_digest, status, retention_class
		FROM idempotency_record WHERE tenant_id=$1 AND capability_id=$2 AND effect_scope=$3 AND idempotency_key=$4`,
		scope.Tenant, scope.Capability, scope.EffectScope, scope.Key).Scan(&rec.RequestDigest, &rec.Status, &rec.RetentionClass); err != nil {
		t.Fatal(err)
	}
	if rec.Status != idempotency.StatusCompleted || rec.RetentionClass != idempotency.RetentionPermanentTombstone {
		t.Fatalf("transaction.commit record = status %q class %q", rec.Status, rec.RetentionClass)
	}

	processed, err := store.Expire(context.Background(), db.Conn, tenant, commitAt.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("expire transaction.commit record: %v", err)
	}
	if processed != 1 {
		t.Fatalf("Expire processed %d rows, want 1", processed)
	}
	rec, found, err = store.Lookup(context.Background(), db.Conn, scope)
	if err != nil {
		t.Fatal(err)
	}
	if !found || rec.Status != idempotency.StatusTombstone || rec.RequestDigest == "" || !rec.Identity.Empty() {
		t.Fatalf("transaction.commit tombstone = %+v, found %v", rec, found)
	}
	if _, err := committer(t, db).Commit(context.Background(), prepared); idempotency.CodeOf(err) != idempotency.CodeTombstoned {
		t.Fatalf("commit replay after tombstoning = %v, want %s", err, idempotency.CodeTombstoned)
	}
	var eventCount int
	if err := db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id=$1`, tenant).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 2 {
		t.Fatalf("ledger contains %d events after tombstoned replay, want 2", eventCount)
	}
}

// TestTodo_TX_004 proves ledger, critical checkpoints, outbox and the durable
// replay record are produced by one local commit and a replay returns the same
// receipt without creating a second fact.
func TestTodo_TX_004(t *testing.T) {
	db, prepared, tenant := commitFixture(t)
	c := committer(t, db)
	first, err := c.Commit(context.Background(), prepared)
	if err != nil {
		t.Fatalf("first commit: %v", err)
	}
	if first.Replayed || len(first.Events) != 2 || len(first.Projections) != 2 || len(first.Outbox) != 1 {
		t.Fatalf("first receipt = %+v, want 2 events/2 projections/1 outbox", first)
	}
	second, err := c.Commit(context.Background(), prepared)
	if err != nil {
		t.Fatalf("replay commit: %v", err)
	}
	if !second.Replayed || second.ReceiptID != first.ReceiptID {
		t.Fatalf("replay receipt = %+v, want same receipt marked replayed", second)
	}
	ctx := context.Background()
	for table, want := range map[string]int{"ledger_event": 2, "outbox": 1, "projection_checkpoint": 2, "idempotency_record": 1} {
		var got int
		if err := db.Conn.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE tenant_id = $1", tenant).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != want {
			t.Errorf("%s rows = %d, want %d", table, got, want)
		}
	}
	var firstStream, secondStream string
	if err := db.Conn.QueryRow(ctx, `SELECT stream_key FROM ledger_event WHERE tenant_id = $1 ORDER BY sequence, stream_key LIMIT 1`, tenant).Scan(&firstStream); err != nil {
		t.Fatal(err)
	}
	if err := db.Conn.QueryRow(ctx, `SELECT stream_key FROM ledger_event WHERE tenant_id = $1 ORDER BY sequence, stream_key OFFSET 1 LIMIT 1`, tenant).Scan(&secondStream); err != nil {
		t.Fatal(err)
	}
	if firstStream != "stream:a" || secondStream != "stream:b" {
		t.Fatalf("ledger order = %s, %s; want stream:a, stream:b", firstStream, secondStream)
	}
}

// TestTodo_TX_004_Race proves the same plan submitted on independent database
// sessions produces one idempotency winner and one durable event set.
func TestTodo_TX_004_Race(t *testing.T) {
	db, prepared, tenant := commitFixture(t)
	connections := []*pgxadapter.Conn{db.NewConn(t), db.NewConn(t)}
	start := make(chan struct{})
	results := make(chan error, len(connections))
	var wg sync.WaitGroup
	for _, conn := range connections {
		wg.Add(1)
		go func(conn *pgxadapter.Conn) {
			defer wg.Done()
			<-start
			c := transactioncommit.New(conn, transactioncommit.Options{Clock: func() time.Time { return commitAt }})
			_, err := c.Commit(context.Background(), prepared)
			results <- err
		}(conn)
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("racing commit: %v", err)
		}
	}
	var events, effects int
	if err := db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, tenant).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM outbox WHERE tenant_id = $1`, tenant).Scan(&effects); err != nil {
		t.Fatal(err)
	}
	if events != 2 || effects != 1 {
		t.Fatalf("racing commit rows = events %d/outbox %d, want 2/1", events, effects)
	}
}

// TestTodo_TX_004_Fault injects a failure after the ledger phase and proves
// the caller's transaction rolls back ledger, projections, outbox and receipt.
func TestTodo_TX_004_Fault(t *testing.T) {
	t.Run("before commit rolls back", func(t *testing.T) {
		db, prepared, tenant := commitFixture(t)
		injected := errors.New("injected after ledger")
		c := transactioncommit.New(db.Conn, transactioncommit.Options{
			Clock: commitClock,
			Failpoint: func(stage string) error {
				if stage == "after-append" {
					return injected
				}
				return nil
			},
		})
		if _, err := c.Commit(context.Background(), prepared); !errors.Is(err, injected) {
			t.Fatalf("fault error = %v, want injected error", err)
		}
		for _, table := range []string{"ledger_event", "projection_checkpoint", "outbox", "idempotency_record"} {
			var count int
			if err := db.Conn.QueryRow(context.Background(), "SELECT count(*) FROM "+table+" WHERE tenant_id = $1", tenant).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Errorf("%s rows after rollback = %d, want 0", table, count)
			}
		}
	})

	t.Run("after commit is durable", func(t *testing.T) {
		db, prepared, tenant := commitFixture(t)
		injected := errors.New("injected after commit")
		c := transactioncommit.New(db.Conn, transactioncommit.Options{
			Clock: commitClock,
			Failpoint: func(stage string) error {
				if stage == "after-commit" {
					return injected
				}
				return nil
			},
		})
		if _, err := c.Commit(context.Background(), prepared); !errors.Is(err, injected) {
			t.Fatalf("post-commit fault error = %v, want injected error", err)
		}
		var events int
		if err := db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, tenant).Scan(&events); err != nil {
			t.Fatal(err)
		}
		if events != 2 {
			t.Fatalf("ledger rows after post-commit fault = %d, want 2", events)
		}
		replay, err := committer(t, db).Commit(context.Background(), prepared)
		if err != nil || !replay.Replayed {
			t.Fatalf("replay after post-commit fault = %+v, %v; want durable replay", replay, err)
		}
	})
}

// TestTodo_TX_004_Mutation proves a plan digest mutation is refused before
// any transaction-local row can be created.
func TestTodo_TX_004_Mutation(t *testing.T) {
	db, prepared, tenant := commitFixture(t)
	prepared.Events[0].Digest = strings.Repeat("d", 64)
	c := committer(t, db)
	if _, err := c.Commit(context.Background(), prepared); err == nil {
		t.Fatal("tampered plan committed")
	}
	var count int
	if err := db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, tenant).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("ledger rows after invalid plan = %d, want 0", count)
	}
}

func commitClock() time.Time { return commitAt }

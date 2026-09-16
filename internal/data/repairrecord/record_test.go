package repairrecord_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/repairrecord"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// seedTenant inserts the one tenant row a repair record is scoped to.
func seedTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	tenantID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenantID, key, "repairrecord test tenant "+key)
	return tenantID
}

// beginScoped opens the tenant-scoped transaction every call in this package
// requires of its caller.
func beginScoped(t *testing.T, db *pgtest.DB, tenantID uuid.UUID) dbport.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	return tx
}

func record(tenantID uuid.UUID, stage repairrecord.Stage) repairrecord.Record {
	return repairrecord.Record{
		TenantID: tenantID, FenceKey: "repair:repair-promotion-1:effect:payroll-provision", Stage: stage,
		FenceID: "repair-fence:sha256:evidence-1", PlanDigest: "sha256:plan-1",
		OriginalSemanticKey: "promotion:worker-1:proposal-1", FailedEffectKey: "effect:payroll-provision",
		Status: "UNKNOWN", ConsistencyState: "UNKNOWN",
		RecordedAt: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
	}
}

// TestTodo_WF_RUN_016_Integration proves the durable record round-trips through
// real PostgreSQL: the three stages are written once each, read back in
// lifecycle order with every identity intact, and a repeated stage is refused
// without disturbing what is already there.
func TestTodo_WF_RUN_016_Integration(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := seedTenant(t, db, "wfrun016-integration")
	tx := beginScoped(t, db, tenantID)
	defer func() { _ = tx.Rollback(ctx) }()

	claim := record(tenantID, repairrecord.StageClaimed)
	claimed, err := repairrecord.Append(ctx, tx, claim)
	if err != nil || !claimed {
		t.Fatalf("claim = %t, %v, want the first writer to win", claimed, err)
	}
	again, err := repairrecord.Append(ctx, tx, claim)
	if err != nil || again {
		t.Fatalf("second claim = %t, %v, want false without an error", again, err)
	}

	executed := record(tenantID, repairrecord.StageExecuted)
	executed.Status, executed.Executed, executed.ConsistencyState = "RECONCILIATION_REQUIRED", true, "DEGRADED"
	executed.EffectRef, executed.EffectResultRef = "operation:payroll-1", "payroll:accepted"
	if ok, err := repairrecord.Append(ctx, tx, executed); err != nil || !ok {
		t.Fatalf("executed = %t, %v", ok, err)
	}

	settled := record(tenantID, repairrecord.StageSettled)
	settled.Status, settled.Executed, settled.ConsistencyState = "COMPLETED", true, "CONSISTENT"
	settled.EffectRef, settled.EffectResultRef = "operation:payroll-1", "payroll:accepted"
	settled.ObservationState, settled.ObservationDigest, settled.ObservationComplete = "PAYROLL_EXPECTED", "sha256:observed", true
	settled.ReconciliationStatus, settled.ReconciliationRoute = "PASS", "CONSISTENT"
	if ok, err := repairrecord.Append(ctx, tx, settled); err != nil || !ok {
		t.Fatalf("settled = %t, %v", ok, err)
	}

	stored, err := repairrecord.Load(ctx, tx, tenantID, claim.FenceKey)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(stored) != 3 {
		t.Fatalf("stored = %d rows, want three stages", len(stored))
	}
	wantStages := []repairrecord.Stage{repairrecord.StageClaimed, repairrecord.StageExecuted, repairrecord.StageSettled}
	for i, want := range wantStages {
		if stored[i].Stage != want {
			t.Fatalf("row %d stage = %s, want %s", i, stored[i].Stage, want)
		}
	}
	final := stored[2]
	if final.Status != "COMPLETED" || !final.Executed || final.ConsistencyState != "CONSISTENT" {
		t.Fatalf("settled row = %+v", final)
	}
	if final.OriginalSemanticKey != "promotion:worker-1:proposal-1" || final.FenceID != "repair-fence:sha256:evidence-1" {
		t.Fatalf("settled identities = %+v, want the parent semantic key and the repair fence kept apart", final)
	}
	if final.ObservationState != "PAYROLL_EXPECTED" || !final.ObservationComplete ||
		final.ReconciliationStatus != "PASS" || final.ReconciliationRoute != "CONSISTENT" {
		t.Fatalf("settled evidence = %+v", final)
	}
	if !final.RecordedAt.Equal(claim.RecordedAt) {
		t.Fatalf("recorded_at = %s, want %s", final.RecordedAt, claim.RecordedAt)
	}
	if empty, err := repairrecord.Load(ctx, tx, tenantID, "repair:never-claimed:effect:none"); err != nil || len(empty) != 0 {
		t.Fatalf("unclaimed fence = %d rows, %v, want none", len(empty), err)
	}
}

// TestTodo_WF_RUN_016_Race releases twelve genuinely concurrent connections at
// one repair fence's claim. What decides the single winner is the table's own
// primary key evaluated at commit, not this test's control flow, which is the
// whole reason Append is one INSERT .. ON CONFLICT DO NOTHING .. RETURNING
// rather than a SELECT followed by a write.
func TestTodo_WF_RUN_016_Race(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := seedTenant(t, db, "wfrun016-race")

	const concurrency = 12
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		winners int
		losers  int
		other   []error
	)
	start := make(chan struct{})
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			// Each goroutine holds its own connection and transaction, so the
			// exclusion proven here comes from PostgreSQL serializing commits.
			conn := db.NewConn(t)
			tx, err := conn.Begin(ctx)
			if err != nil {
				mu.Lock()
				other = append(other, err)
				mu.Unlock()
				return
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
				mu.Lock()
				other = append(other, err)
				mu.Unlock()
				return
			}
			claimed, err := repairrecord.Append(ctx, tx, record(tenantID, repairrecord.StageClaimed))
			if err == nil && claimed {
				err = tx.Commit(ctx)
			}
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil:
				other = append(other, err)
			case claimed:
				winners++
			default:
				losers++
			}
		}()
	}
	close(start)
	wg.Wait()

	for _, err := range other {
		t.Errorf("unexpected error from a concurrent claim: %v", err)
	}
	if winners != 1 {
		t.Fatalf("winners = %d of %d concurrent claims, want exactly one redrive admitted", winners, concurrency)
	}
	if winners+losers != concurrency {
		t.Fatalf("accounted = %d of %d attempts", winners+losers, concurrency)
	}

	readTx := beginScoped(t, db, tenantID)
	defer func() { _ = readTx.Rollback(ctx) }()
	stored, err := repairrecord.Load(ctx, readTx, tenantID, record(tenantID, repairrecord.StageClaimed).FenceKey)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("durable claims for the contested fence = %d, want exactly one", len(stored))
	}
}

// TestTodo_WF_RUN_016_Fault proves the table refuses the two rewrites that
// would make the record a lie: an UPDATE or a DELETE of a stage already
// written. The record is evidence that an external effect ran; it is never
// edited afterwards.
func TestTodo_WF_RUN_016_Fault(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := seedTenant(t, db, "wfrun016-fault")
	tx := beginScoped(t, db, tenantID)
	if _, err := repairrecord.Append(ctx, tx, record(tenantID, repairrecord.StageClaimed)); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	for name, statement := range map[string]string{
		"update": `UPDATE workflow_repair_execution_record SET status = 'COMPLETED' WHERE tenant_id = $1`,
		"delete": `DELETE FROM workflow_repair_execution_record WHERE tenant_id = $1`,
	} {
		t.Run(name, func(t *testing.T) {
			mutateTx := beginScoped(t, db, tenantID)
			defer func() { _ = mutateTx.Rollback(ctx) }()
			_, err := mutateTx.Exec(ctx, statement, tenantID)
			if err == nil {
				t.Fatalf("%s on an append-only repair record was accepted", name)
			}
			if !strings.Contains(err.Error(), "append-only") {
				t.Fatalf("%s refusal = %v, want the forbid_mutation trigger", name, err)
			}
		})
	}
}

// asAppRole opens a connection as the application role and scopes it to one
// tenant. The row-level-security policy is what is under test here, so the
// session must be the one production uses -- the migration owner bypasses no
// policy under FORCE, but pgtest's own superuser session does.
func asAppRole(t *testing.T, db *pgtest.DB, tenantID uuid.UUID) *pgxadapter.Conn {
	t.Helper()
	ctx := context.Background()
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE hcmnext_app"); err != nil {
		t.Fatalf("set role: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('app.tenant_id',$1,false)`, tenantID.String()); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	return conn
}

// TestTodo_WF_RUN_016_Security proves the row-level-security policy, not the
// Go layer, is what keeps one tenant's repair record out of another's reads
// and writes: a session scoped to another tenant cannot see the row even when
// it names the owner's tenant id itself, and cannot insert one on the owner's
// behalf.
func TestTodo_WF_RUN_016_Security(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	owner := seedTenant(t, db, "wfrun016-security-owner")
	other := seedTenant(t, db, "wfrun016-security-other")

	ownerConn := asAppRole(t, db, owner)
	if claimed, err := repairrecord.Append(ctx, ownerConn, record(owner, repairrecord.StageClaimed)); err != nil || !claimed {
		t.Fatalf("owner claim = %t, %v", claimed, err)
	}
	fenceKey := record(owner, repairrecord.StageClaimed).FenceKey
	if own, err := repairrecord.Load(ctx, ownerConn, owner, fenceKey); err != nil || len(own) != 1 {
		t.Fatalf("owner read back %d rows, %v, want its own claim", len(own), err)
	}

	otherConn := asAppRole(t, db, other)
	stored, err := repairrecord.Load(ctx, otherConn, other, fenceKey)
	if err != nil {
		t.Fatalf("cross-tenant load: %v", err)
	}
	if len(stored) != 0 {
		t.Fatalf("another tenant read %d rows for the same fence key", len(stored))
	}
	// Naming the owner's tenant id in the WHERE clause is not what grants
	// access; the session's own scope is.
	leaked, err := repairrecord.Load(ctx, otherConn, owner, fenceKey)
	if err != nil {
		t.Fatalf("cross-tenant load by owner id: %v", err)
	}
	if len(leaked) != 0 {
		t.Fatalf("a session scoped to another tenant read %d owner rows", len(leaked))
	}
	// A write claiming the owner's tenant from this session is refused by the
	// policy's WITH CHECK clause.
	if _, err := repairrecord.Append(ctx, otherConn, record(owner, repairrecord.StageExecuted)); err == nil {
		t.Fatal("a cross-tenant insert was accepted")
	}
	// And the owner's own record is untouched by that attempt.
	if own, err := repairrecord.Load(ctx, ownerConn, owner, fenceKey); err != nil || len(own) != 1 {
		t.Fatalf("owner rows after the refused cross-tenant write = %d, %v", len(own), err)
	}
}

// TestTodo_WF_RUN_016_Mutation names every precondition Append and Load refuse
// before touching the database, so a record that could not identify its own
// repair can never be written.
func TestTodo_WF_RUN_016_Mutation(t *testing.T) {
	ctx := context.Background()
	tenantID := uuid.New()
	cases := map[string]func(repairrecord.Record) repairrecord.Record{
		"no tenant":        func(r repairrecord.Record) repairrecord.Record { r.TenantID = uuid.Nil; return r },
		"no fence key":     func(r repairrecord.Record) repairrecord.Record { r.FenceKey = "  "; return r },
		"undeclared stage": func(r repairrecord.Record) repairrecord.Record { r.Stage = "ABORTED"; return r },
		"no fence id":      func(r repairrecord.Record) repairrecord.Record { r.FenceID = ""; return r },
		"no plan digest":   func(r repairrecord.Record) repairrecord.Record { r.PlanDigest = ""; return r },
		"no semantic key":  func(r repairrecord.Record) repairrecord.Record { r.OriginalSemanticKey = ""; return r },
		"no effect key":    func(r repairrecord.Record) repairrecord.Record { r.FailedEffectKey = ""; return r },
		"no status":        func(r repairrecord.Record) repairrecord.Record { r.Status = ""; return r },
		"bad consistency":  func(r repairrecord.Record) repairrecord.Record { r.ConsistencyState = "FINE"; return r },
		"no instant":       func(r repairrecord.Record) repairrecord.Record { r.RecordedAt = time.Time{}; return r },
	}
	// A nil executor is enough: a valid record would reach the database, and
	// every case below must be refused before it ever gets there.
	for name, mutate := range cases {
		_, err := repairrecord.Append(ctx, nil, mutate(record(tenantID, repairrecord.StageClaimed)))
		if !errors.Is(err, repairrecord.ErrInvalid) {
			t.Fatalf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	if _, err := repairrecord.Load(ctx, nil, tenantID, "repair:plan"); !errors.Is(err, repairrecord.ErrInvalid) {
		t.Fatalf("nil executor load = %v, want ErrInvalid", err)
	}
	for _, stage := range []repairrecord.Stage{repairrecord.StageClaimed, repairrecord.StageExecuted, repairrecord.StageSettled} {
		if !stage.Valid() {
			t.Fatalf("%s reported invalid", stage)
		}
	}
	if repairrecord.Stage("ABORTED").Valid() {
		t.Fatal("an undeclared stage reported valid")
	}
}

// TestLoadRefusesAnUnscopedRead pins the two read preconditions separately
// from the write ones.
func TestLoadRefusesAnUnscopedRead(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := seedTenant(t, db, "wfrun016-load-guard")
	tx := beginScoped(t, db, tenantID)
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := repairrecord.Load(ctx, tx, uuid.Nil, "repair:plan"); !errors.Is(err, repairrecord.ErrInvalid) {
		t.Fatalf("load without a tenant = %v, want ErrInvalid", err)
	}
	if _, err := repairrecord.Load(ctx, tx, tenantID, "   "); !errors.Is(err, repairrecord.ErrInvalid) {
		t.Fatalf("load without a fence key = %v, want ErrInvalid", err)
	}
}

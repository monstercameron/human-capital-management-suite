package runtime_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// quarantineInstance creates one durable instance for tenant and returns its
// id, so a quarantined-work row has the instance its foreign key requires.
func quarantineInstance(t *testing.T, db *pgtest.DB, tenant uuid.UUID) uuid.UUID {
	t.Helper()
	inst := newInstance(t, tenant, referencePlan(t))
	inTenantTx(t, appConn(t, db), tenant, func(tx dbport.Tx) error {
		_, err := (runtime.Store{}).CreateInstance(context.Background(), tx, inst)
		return err
	})
	return inst.InstanceID
}

func countRows(t *testing.T, db *pgtest.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func admittedWork(t *testing.T, node, key string) runtime.QuarantinedWork {
	t.Helper()
	work, err := runtime.Admit(quarantineSpec(node, key, terminalRoute(runtime.ReasonAttemptsExhausted, "")))
	if err != nil {
		t.Fatal(err)
	}
	return work
}

// TestTodo_WF_RUN_007_Durable proves the PostgreSQL QuarantinedWork store
// keeps the in-memory ledger's contract across a fresh connection: every
// retained field survives, refiling a key returns the stored record without
// duplicating it, a ghost key is refused, and the row is append-only.
func TestTodo_WF_RUN_007_Durable(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wfrun007-durable")
	instanceID := quarantineInstance(t, db, tenant)
	store := runtime.QuarantineStore{}

	blocked := admittedWork(t, "node/validate", "wfq-durable-1")
	spec := quarantineSpec("node/commit", "wfq-durable-2", terminalRoute(runtime.ReasonBudgetExhausted, "operations.repair.retry_budget"))
	spec.Ambiguous = true
	ambiguous, err := runtime.Admit(spec)
	if err != nil {
		t.Fatal(err)
	}

	conn := appConn(t, db)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		for i, work := range []runtime.QuarantinedWork{blocked, ambiguous} {
			filed, err := store.File(ctx, tx, tenant, instanceID, work, fixedInstant.Add(time.Duration(i)*time.Second))
			if err != nil {
				return err
			}
			if filed != work {
				return fmt.Errorf("filed %+v, want %+v", filed, work)
			}
		}
		// A refiling under the same key with different content keeps the
		// first record: idempotency is by key, never last-writer-wins.
		changed := admittedWork(t, "node/validate", "wfq-durable-1")
		changed.LastError = "a different error"
		changed.Digest = ""
		resealed, err := runtime.Admit(runtime.QuarantineSpec{
			NodeID: changed.NodeID, WorkflowID: changed.WorkflowID, Attempts: 9, LastError: changed.LastError,
			IdempotencyKey: changed.IdempotencyKey, Owner: "operator-2", SLA: time.Hour,
			Terminal: terminalRoute(runtime.ReasonNonretryable, ""),
		})
		if err != nil {
			return err
		}
		again, err := store.File(ctx, tx, tenant, instanceID, resealed, fixedInstant.Add(time.Minute))
		if err != nil {
			return err
		}
		if again != blocked {
			return fmt.Errorf("refile returned %+v, want the stored %+v", again, blocked)
		}
		return nil
	})

	restarted := appConn(t, db)
	inTenantTx(t, restarted, tenant, func(tx dbport.Tx) error {
		got, err := store.Get(ctx, tx, tenant, "wfq-durable-2")
		if err != nil {
			return err
		}
		if got != ambiguous || got.Route != runtime.WorkflowQuarantined || !got.Ambiguous || got.SLA != 30*time.Minute ||
			got.Attempts != 4 || got.Owner != "operator-1" || got.NextAction != "await-reconciliation" {
			return fmt.Errorf("durable record = %+v, want %+v", got, ambiguous)
		}
		listed, err := store.ListForInstance(ctx, tx, tenant, instanceID)
		if err != nil {
			return err
		}
		if len(listed) != 2 || listed[0] != blocked || listed[1] != ambiguous {
			return fmt.Errorf("list = %+v, want exactly the two filed records in order", listed)
		}
		if _, err := store.Get(ctx, tx, tenant, "wfq-ghost"); runtimeCode(err) != runtime.CodeQuarantineNotFound {
			return fmt.Errorf("ghost lookup = %v, want %s", err, runtime.CodeQuarantineNotFound)
		}
		none, err := store.ListForInstance(ctx, tx, tenant, uuid.New())
		if err != nil || len(none) != 0 {
			return fmt.Errorf("unknown instance list = %+v, %v", none, err)
		}
		return nil
	})
	if n := countRows(t, db, `SELECT count(*) FROM workflow_quarantined_work WHERE tenant_id = $1`, tenant); n != 2 {
		t.Fatalf("rows = %d, want 2 (refiling must not duplicate)", n)
	}

	// Append-only: the application role holds no UPDATE or DELETE, and the
	// forbid_mutation trigger refuses both even for the superuser.
	for _, stmt := range []string{
		`UPDATE workflow_quarantined_work SET route = 'BLOCKED' WHERE tenant_id = $1`,
		`DELETE FROM workflow_quarantined_work WHERE tenant_id = $1`,
	} {
		if err := inTenantTxErr(appConn(t, db), tenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, stmt, tenant)
			return err
		}); err == nil {
			t.Fatalf("application role ran %q", stmt)
		}
		if err := db.ExecErr(stmt, tenant); err == nil {
			t.Fatalf("superuser bypassed forbid_mutation with %q", stmt)
		}
	}
}

// TestTodo_WF_RUN_007_DurableRace files the same idempotency key from
// concurrent transactions on separate connections. Exactly one row lands and
// every filer reads back that one record.
func TestTodo_WF_RUN_007_DurableRace(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wfrun007-race")
	instanceID := quarantineInstance(t, db, tenant)

	const workers = 8
	results := make([]runtime.QuarantinedWork, workers)
	errs := make([]error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		conn := appConn(t, db)
		work, err := runtime.Admit(runtime.QuarantineSpec{
			NodeID: "node/commit", WorkflowID: "wf/poison", Attempts: 3, LastError: fmt.Sprintf("error from worker %d", i),
			IdempotencyKey: "wfq-race", Owner: "operator-1", SLA: time.Minute,
			Terminal: terminalRoute(runtime.ReasonAttemptsExhausted, ""),
		})
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
				var err error
				results[i], err = (runtime.QuarantineStore{}).File(ctx, tx, tenant, instanceID, work, fixedInstant)
				return err
			})
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
	for i := range results {
		if results[i] != results[0] {
			t.Fatalf("worker %d read %+v, worker 0 read %+v: concurrent filings diverged", i, results[i], results[0])
		}
	}
	if !strings.HasPrefix(results[0].LastError, "error from worker ") || results[0].Verify() != nil {
		t.Fatalf("stored record = %+v", results[0])
	}
	if n := countRows(t, db, `SELECT count(*) FROM workflow_quarantined_work WHERE tenant_id = $1 AND idempotency_key = 'wfq-race'`, tenant); n != 1 {
		t.Fatalf("concurrent filings stored %d rows, want 1", n)
	}
}

// TestTodo_WF_RUN_007_DurableTenantIsolation proves row level security: a
// second tenant neither reads nor lists the first tenant's quarantined work,
// and cannot file a row claiming the first tenant's identity.
func TestTodo_WF_RUN_007_DurableTenantIsolation(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	owner := insertTenant(t, db, "wfrun007-owner")
	other := insertTenant(t, db, "wfrun007-other")
	instanceID := quarantineInstance(t, db, owner)
	store := runtime.QuarantineStore{}
	work := admittedWork(t, "node/validate", "wfq-tenant")

	inTenantTx(t, appConn(t, db), owner, func(tx dbport.Tx) error {
		_, err := store.File(ctx, tx, owner, instanceID, work, fixedInstant)
		return err
	})
	inTenantTx(t, appConn(t, db), other, func(tx dbport.Tx) error {
		if _, err := store.Get(ctx, tx, other, "wfq-tenant"); runtimeCode(err) != runtime.CodeQuarantineNotFound {
			return fmt.Errorf("other tenant read = %v, want %s", err, runtime.CodeQuarantineNotFound)
		}
		if _, err := store.Get(ctx, tx, owner, "wfq-tenant"); runtimeCode(err) != runtime.CodeQuarantineNotFound {
			return fmt.Errorf("other tenant naming the owner read = %v, want %s", err, runtime.CodeQuarantineNotFound)
		}
		listed, err := store.ListForInstance(ctx, tx, owner, instanceID)
		if err != nil || len(listed) != 0 {
			return fmt.Errorf("other tenant listed %+v, %v", listed, err)
		}
		return nil
	})
	err := inTenantTxErr(appConn(t, db), other, func(tx dbport.Tx) error {
		_, err := store.File(ctx, tx, owner, instanceID, admittedWork(t, "node/validate", "wfq-forged"), fixedInstant)
		return err
	})
	if runtimeCode(err) != runtime.CodeStorageFailed {
		t.Fatalf("cross-tenant filing = %v, want %s from row level security", err, runtime.CodeStorageFailed)
	}
}

// TestTodo_WF_RUN_007_DurableFault covers every refusal before and after the
// database: malformed filings, a broken submitted seal, a stored row whose
// seal no longer verifies, and storage failures.
func TestTodo_WF_RUN_007_DurableFault(t *testing.T) {
	ctx := context.Background()
	store := runtime.QuarantineStore{}
	tenant, instanceID := uuid.New(), uuid.New()
	work := admittedWork(t, "node/validate", "wfq-fault")
	failing := failingExecutor{err: errors.New("connection reset")}

	unsealed := work
	unsealed.Owner = "someone-else"
	badRoute := work
	badRoute.Route = "COMPLETED"
	noKey := work
	noKey.IdempotencyKey = ""
	for name, tc := range map[string]struct {
		tenant, instance uuid.UUID
		work             runtime.QuarantinedWork
		at               time.Time
		code             string
	}{
		"nil tenant":   {uuid.Nil, instanceID, work, fixedInstant, runtime.CodeInvalidRecord},
		"nil instance": {tenant, uuid.Nil, work, fixedInstant, runtime.CodeInvalidRecord},
		"zero instant": {tenant, instanceID, work, time.Time{}, runtime.CodeInvalidRecord},
		"success":      {tenant, instanceID, badRoute, fixedInstant, runtime.CodeInvalidRecord},
		"no key":       {tenant, instanceID, noKey, fixedInstant, runtime.CodeInvalidRecord},
		"broken seal":  {tenant, instanceID, unsealed, fixedInstant, runtime.CodeQuarantineSealBroken},
		"storage":      {tenant, instanceID, work, fixedInstant, runtime.CodeStorageFailed},
	} {
		if _, err := store.File(ctx, failing, tc.tenant, tc.instance, tc.work, tc.at); runtimeCode(err) != tc.code {
			t.Errorf("%s: File = %v, want %s", name, err, tc.code)
		}
	}
	if _, err := store.Get(ctx, failing, tenant, "wfq-fault"); runtimeCode(err) != runtime.CodeStorageFailed {
		t.Errorf("Get on failing storage = %v, want %s", err, runtime.CodeStorageFailed)
	}
	if _, err := store.ListForInstance(ctx, failing, tenant, instanceID); runtimeCode(err) != runtime.CodeStorageFailed {
		t.Errorf("List on failing storage = %v, want %s", err, runtime.CodeStorageFailed)
	}
	if _, err := store.ListForInstance(ctx, failingExecutor{rows: &failingRows{err: errors.New("cursor lost")}}, tenant, instanceID); runtimeCode(err) != runtime.CodeStorageFailed {
		t.Errorf("List with a failing cursor = %v, want %s", err, runtime.CodeStorageFailed)
	}
	if _, err := store.ListForInstance(ctx, failingExecutor{rows: &failingRows{next: 1, scanErr: errors.New("bad column")}}, tenant, instanceID); runtimeCode(err) != runtime.CodeStorageFailed {
		t.Errorf("List with a failing scan = %v, want %s", err, runtime.CodeStorageFailed)
	}

	// A stored row edited behind the store's back (the superuser disables
	// the append-only trigger to simulate tampering) is refused on read.
	db := pgtest.New(t)
	realTenant := insertTenant(t, db, "wfrun007-fault")
	realInstance := quarantineInstance(t, db, realTenant)
	inTenantTx(t, appConn(t, db), realTenant, func(tx dbport.Tx) error {
		_, err := store.File(ctx, tx, realTenant, realInstance, work, fixedInstant)
		return err
	})
	db.Exec(t, `ALTER TABLE workflow_quarantined_work DISABLE TRIGGER workflow_quarantined_work_append_only`)
	db.Exec(t, `UPDATE workflow_quarantined_work SET attempts = 1 WHERE tenant_id = $1`, realTenant)
	db.Exec(t, `ALTER TABLE workflow_quarantined_work ENABLE TRIGGER workflow_quarantined_work_append_only`)
	inTenantTx(t, appConn(t, db), realTenant, func(tx dbport.Tx) error {
		if _, err := store.Get(ctx, tx, realTenant, "wfq-fault"); runtimeCode(err) != runtime.CodeQuarantineSealBroken {
			return fmt.Errorf("tampered Get = %v, want %s", err, runtime.CodeQuarantineSealBroken)
		}
		if _, err := store.ListForInstance(ctx, tx, realTenant, realInstance); runtimeCode(err) != runtime.CodeQuarantineSealBroken {
			return fmt.Errorf("tampered List = %v, want %s", err, runtime.CodeQuarantineSealBroken)
		}
		return nil
	})
}

// failingExecutor is storage that fails every statement, or serves rows.
type failingExecutor struct {
	err  error
	rows *failingRows
}

func (f failingExecutor) Exec(context.Context, string, ...any) (int64, error) {
	return 0, f.err
}

func (f failingExecutor) Query(context.Context, string, ...any) (dbport.Rows, error) {
	if f.rows != nil {
		return f.rows, nil
	}
	return nil, f.err
}

func (f failingExecutor) QueryRow(context.Context, string, ...any) dbport.Row {
	return failingRow{err: f.err}
}

type failingRow struct{ err error }

func (r failingRow) Scan(...any) error { return r.err }

type failingRows struct {
	next    int
	scanErr error
	err     error
}

func (r *failingRows) Next() bool {
	if r.next > 0 {
		r.next--
		return true
	}
	return false
}
func (r *failingRows) Scan(...any) error { return r.scanErr }
func (r *failingRows) Err() error        { return r.err }
func (r *failingRows) Close()            {}

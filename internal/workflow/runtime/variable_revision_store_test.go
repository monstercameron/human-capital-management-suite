package runtime_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestTodo_WF_COMP_007_VariableRevisionStore proves the durable variable
// write path appends WorkflowVariableRevision rows: the instance head and
// version advance together, the current-value projection follows, a stale
// writer is refused, the history survives a new connection, other tenants see
// nothing, and the table refuses UPDATE and DELETE.
func TestTodo_WF_COMP_007_VariableRevisionStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wfcomp007-variables")
	otherTenant := insertTenant(t, db, "wfcomp007-variables-other")
	plan := referencePlan(t)
	store := runtime.Store{}
	conn := appConn(t, db)

	inst := newInstance(t, tenant, plan)
	var created runtime.Instance
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		created, err = store.CreateInstance(ctx, tx, inst)
		return err
	})

	log, _ := runtime.NewVariableRevisionLog(tenant, inst.InstanceID, nil)
	first, err := log.Append(proposalWrite(`{"grade":"L5"}`), fixedInstant)
	if err != nil {
		t.Fatalf("build first revision: %v", err)
	}
	version := created.InstanceVersion
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, v, err := store.AppendVariableRevision(ctx, tx, first, version)
		version = v
		return err
	})
	if version != created.InstanceVersion+1 {
		t.Fatalf("instance version = %d, want %d", version, created.InstanceVersion+1)
	}

	// A writer holding the pre-write instance version is refused and writes nothing.
	second, err := log.Append(proposalWrite(`{"grade":"L6"}`), fixedInstant.Add(time.Minute))
	if err != nil {
		t.Fatalf("build second revision: %v", err)
	}
	err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, _, err := store.AppendVariableRevision(ctx, tx, second, created.InstanceVersion)
		return err
	})
	wantCode(t, err, runtime.CodeStaleInstance)

	// A writer at the current version whose revision was built from a stale
	// head (it thinks it is writing revision 1 again) is refused too.
	staleLog, _ := runtime.NewVariableRevisionLog(tenant, inst.InstanceID, nil)
	stale, _ := staleLog.Append(proposalWrite(`{"grade":"L7"}`), fixedInstant)
	err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, _, err := store.AppendVariableRevision(ctx, tx, stale, version)
		return err
	})
	wantCode(t, err, runtime.CodeStaleInstance)

	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, v, err := store.AppendVariableRevision(ctx, tx, second, version)
		version = v
		return err
	})

	// Read back on a fresh connection.
	fresh := appConn(t, db)
	var loaded *runtime.VariableRevisionLog
	var reread runtime.Instance
	var projected string
	var projectedRevision int64
	inTenantTx(t, fresh, tenant, func(tx dbport.Tx) error {
		var err error
		if loaded, err = store.LoadVariableRevisions(ctx, tx, tenant, inst.InstanceID); err != nil {
			return err
		}
		if reread, err = store.LoadInstance(ctx, tx, tenant, inst.InstanceID); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT variable_value::text, written_at_revision FROM workflow_variable
			WHERE tenant_id = $1 AND instance_id = $2 AND variable_name = 'proposal'`,
			tenant, inst.InstanceID).Scan(&projected, &projectedRevision)
	})
	if loaded.Head() != 2 || reread.VariableRevisionHead != 2 || reread.InstanceVersion != version {
		t.Fatalf("head %d, instance head %d version %d (want 2, 2, %d)",
			loaded.Head(), reread.VariableRevisionHead, reread.InstanceVersion, version)
	}
	if prior, ok := loaded.AsOf("proposal", 1); !ok || string(prior.Value) != `{"grade":"L5"}` || prior.RevisionDigest != first.RevisionDigest {
		t.Fatalf("revision 1 after reload = %+v", prior)
	}
	if projectedRevision != 2 || (projected != `{"grade": "L6"}` && projected != `{"grade":"L6"}`) {
		t.Fatalf("projection = %s at %d, want L6 at 2", projected, projectedRevision)
	}

	// The next revision builds on the reloaded log and still stores.
	third, err := loaded.Append(proposalWrite(`{"grade":"L8"}`), fixedInstant.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("append on reloaded log: %v", err)
	}
	inTenantTx(t, fresh, tenant, func(tx dbport.Tx) error {
		_, _, err := store.AppendVariableRevision(ctx, tx, third, version)
		return err
	})

	// Another tenant sees no history.
	inTenantTx(t, fresh, otherTenant, func(tx dbport.Tx) error {
		other, err := store.LoadVariableRevisions(ctx, tx, tenant, inst.InstanceID)
		if err != nil {
			return err
		}
		if other.Head() != 0 {
			t.Errorf("tenant isolation leaked %d revisions", other.Head())
		}
		return nil
	})

	// Append-only: the app role holds no UPDATE/DELETE, and even the
	// migration role is stopped by forbid_mutation.
	for _, stmt := range []string{
		`UPDATE workflow_variable_revision SET reason = 'rewritten'`,
		`DELETE FROM workflow_variable_revision`,
	} {
		if err := inTenantTxErr(fresh, tenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, stmt)
			return err
		}); err == nil {
			t.Errorf("app role executed %q", stmt)
		}
		if err := db.ExecErr(stmt); err == nil {
			t.Errorf("privileged role executed %q past forbid_mutation", stmt)
		}
	}

	// A revision that fails validation never reaches a statement.
	bad := third
	bad.Reason = ""
	err = inTenantTxErr(fresh, tenant, func(tx dbport.Tx) error {
		_, _, err := store.AppendVariableRevision(ctx, tx, bad, version+1)
		return err
	})
	wantCode(t, err, runtime.CodeInvalidRecord)
	err = inTenantTxErr(fresh, tenant, func(tx dbport.Tx) error {
		_, _, err := store.AppendVariableRevision(ctx, tx, third, 0)
		return err
	})
	wantCode(t, err, runtime.CodeInvalidRecord)
}

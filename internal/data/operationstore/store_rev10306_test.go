package operationstore

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// TestTodo_REV_103_06 proves the fence read is load bearing: a live
// transaction returns the stored fence token with no error, and cancelling
// an operation that does not exist surfaces ErrNotFound instead of a
// fence verdict.
func TestTodo_REV_103_06(t *testing.T) {
	db := pgtest.New(t)
	tenant := operationTenant(t, db, "operation-rev10306")
	store := New(db.NewConn(t))
	fixture := operationFixture(tenant, "op-fence", StateRunning)
	if err := store.Put(t.Context(), fixture); err != nil {
		t.Fatalf("Put: %v", err)
	}

	tx, tenantUUID, err := store.beginTenant(t.Context(), tenant.String())
	if err != nil {
		t.Fatalf("beginTenant: %v", err)
	}
	defer tx.Rollback(t.Context())
	fence, err := currentFence(t.Context(), tx, tenantUUID, fixture.OperationID)
	if err != nil {
		t.Fatalf("currentFence: %v", err)
	}
	if fence != 1 {
		t.Fatalf("fence = %d, want the Put-time token 1", fence)
	}

	if _, err := store.Cancel(t.Context(), tenant.String(), "op-missing", "cancel-1", "operator-request"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Cancel on a missing operation = %v, want ErrNotFound", err)
	}
}

// TestTodo_REV_103_06_Fault injects the failure REV-103-06 names: when the
// fence query itself fails, Cancel's compare-and-swap must report the
// backend error, never a zero fence that misreports as ErrFenced.
func TestTodo_REV_103_06_Fault(t *testing.T) {
	db := pgtest.New(t)
	tenant := operationTenant(t, db, "operation-rev10306-fault")
	store := New(db.NewConn(t))

	tx, tenantUUID, err := store.beginTenant(t.Context(), tenant.String())
	if err != nil {
		t.Fatalf("beginTenant: %v", err)
	}
	// Sabotage the transaction so the fence SELECT cannot succeed.
	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatalf("sabotage rollback: %v", err)
	}
	if _, err := currentFence(t.Context(), tx, tenantUUID, "op-any"); err == nil {
		t.Fatal("currentFence on a dead transaction returned no error; a zero fence would misreport as fenced")
	}
}

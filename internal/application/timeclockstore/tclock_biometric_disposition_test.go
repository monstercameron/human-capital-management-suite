package timeclockstore

import (
	"context"
	"testing"
)

func TestTodo_TCLOCK_006_CompositionFailsClosedWithoutDurableVault(t *testing.T) {
	a := BiometricDispositionAdapter{}
	if _, err := a.Withdraw(context.Background(), "tenant", "consent", "worker", "actor", "withdraw", "idem", 1); err == nil {
		t.Fatal("nil store withdrawal unexpectedly succeeded")
	}
	if err := a.Destroy(context.Background(), "tenant", "disposition", "actor"); err == nil {
		t.Fatal("missing durable custody unexpectedly succeeded")
	} else if err.Error() != "timeclockstore: biometric custody composition is incomplete" {
		t.Fatalf("err=%v, want explicit composition failure", err)
	}
	if _, err := a.Reconcile(context.Background(), "tenant", 10); err == nil {
		t.Fatal("nil store reconciliation unexpectedly succeeded")
	}
}

func TestTodo_TCLOCK_017_ReconcileRequiresPersistentStore(t *testing.T) {
	if _, err := (BiometricDispositionAdapter{}).Reconcile(context.Background(), "tenant", 10); err == nil {
		t.Fatal("reconcile without a persistent store unexpectedly succeeded")
	}
}

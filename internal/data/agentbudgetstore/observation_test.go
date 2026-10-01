package agentbudgetstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENT2_025_SettledTaskUsageReadsDurableTaskScopedCounters(t *testing.T) {
	e := newEnv(t, "eval-tenant-a", "eval-tenant-b")
	store := e.store(t)
	ledger, err := agentbudget.NewWithPersistence(policy(), func() time.Time { return time.Date(2026, 9, 30, 16, 0, 0, 0, time.UTC) }, store)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.OpenTask(agentbudget.TaskSpec{ID: "eval-task", TenantID: "eval-tenant-a", UserID: "user-a", Limit: policy().TaskDefault}); err != nil {
		t.Fatal(err)
	}
	reservation, err := reserve(ledger, "eval-task", "step-1", "fingerprint", call())
	if err != nil {
		t.Fatal(err)
	}
	settled := agentbudget.Usage{Steps: 1, Tokens: 77, WallClock: 250 * time.Millisecond, SpendMicros: 19}
	if err := reservation.Settle(settled); err != nil {
		t.Fatal(err)
	}

	got, err := store.SettledTaskUsage(context.Background(), values.TenantId("eval-tenant-a"), "eval-task")
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != "eval-tenant-a" || got.TaskID != "eval-task" || got.Usage != settled {
		t.Fatalf("durable settled usage = %#v, want task identity and %#v", got, settled)
	}
	if _, err := store.SettledTaskUsage(context.Background(), values.TenantId("eval-tenant-b"), "eval-task"); !errors.Is(err, ErrTaskMissing) {
		t.Fatalf("foreign tenant read error = %v, want missing task", err)
	}
	if _, err := store.SettledTaskUsage(context.Background(), values.TenantId("eval-tenant-a"), "missing-task"); !errors.Is(err, ErrTaskMissing) {
		t.Fatalf("missing task read error = %v, want missing task", err)
	}
}

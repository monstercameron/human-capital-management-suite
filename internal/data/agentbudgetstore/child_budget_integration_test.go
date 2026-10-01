package agentbudgetstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
)

func TestTodo_AGENT_050_Integration_RestartPreservesNestedBudgetLineage(t *testing.T) {
	e := newEnv(t, "nested-a", "nested-b")
	c := &clock{at: day1}
	first := boot(t, e, policy(), c, "nested-a")
	rootLimit := agentbudget.Limits{Steps: 3, Tokens: 300, WallClock: 3 * 60 * 1e9, SpendMicros: 300}
	parentLimit := agentbudget.Limits{Steps: 2, Tokens: 200, WallClock: 2 * 60 * 1e9, SpendMicros: 200}
	if err := first.OpenTask(agentbudget.TaskSpec{ID: "root", TenantID: "nested-a", UserID: "u", Limit: rootLimit}); err != nil {
		t.Fatal(err)
	}
	if err := first.OpenChildTask(agentbudget.TaskSpec{ID: "parent", Limit: parentLimit}, "root"); err != nil {
		t.Fatal(err)
	}
	if err := first.OpenChildTask(agentbudget.TaskSpec{ID: "grandchild", Limit: parentLimit}, "parent"); err != nil {
		t.Fatal(err)
	}
	r, err := first.Reserve(context.Background(), agentbudget.Request{TaskID: "grandchild", StepID: "s1", Fingerprint: "f1", Estimate: call()})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(r.Estimate); err != nil {
		t.Fatal(err)
	}
	second := boot(t, e, policy(), c, "nested-a")
	for _, id := range []string{"root", "parent", "grandchild"} {
		if got := taskSnap(t, second, id); got.Used.Steps != 1 || got.ParentTaskID == "" && id != "root" {
			t.Fatalf("restored %s = %+v", id, got)
		}
	}
	if got := taskSnap(t, second, "grandchild"); got.RootTaskID != "root" || got.Depth != 2 || got.ParentTaskID != "parent" {
		t.Fatalf("restored lineage = %+v", got)
	}
	if snap := second.Snapshot(); len(snap.UserPeriodTotals) != 1 || snap.UserPeriodTotals[0].Steps != 1 || len(snap.TenantPeriodTotals) != 1 || snap.TenantPeriodTotals[0].Steps != 1 {
		t.Fatalf("shared periods double charged = %+v", snap)
	}
	if _, err := second.Reserve(context.Background(), agentbudget.Request{TaskID: "grandchild", StepID: "s2", Fingerprint: "f2", Estimate: agentbudget.Usage{Steps: 2, Tokens: 1, WallClock: 1, SpendMicros: 1}}); !errors.Is(err, agentbudget.ErrPaused) {
		t.Fatalf("root/parent ceiling after restart = %v", err)
	}
	other := boot(t, e, policy(), c, "nested-b")
	if err := other.OpenTask(agentbudget.TaskSpec{ID: "root", TenantID: "nested-b", UserID: "u", Limit: rootLimit}); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Reserve(context.Background(), agentbudget.Request{TaskID: "root", StepID: "s1", Fingerprint: "f", Estimate: call()}); err != nil {
		t.Fatalf("cross-tenant budget leaked: %v", err)
	}
}

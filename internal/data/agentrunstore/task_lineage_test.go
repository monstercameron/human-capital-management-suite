package agentrunstore

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

func TestTodo_AGENT_050_Lineage(t *testing.T) {
	want := agentrun.AgentTask{ID: "child", ParentTaskID: "parent", RootTaskID: "parent", BudgetTaskID: "parent", DelegationDepth: 1}
	raw, err := encodeTaskLineage(want)
	if err != nil {
		t.Fatal(err)
	}
	const golden = `{"parent_task_id":"parent","root_task_id":"parent","budget_task_id":"parent","delegation_depth":1}`
	if string(raw) != golden {
		t.Fatalf("lineage bytes = %s", raw)
	}
	got := agentrun.AgentTask{ID: "child"}
	if err := decodeTaskLineage(raw, &got); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("lineage = %+v, %v", got, err)
	}
	if root, err := encodeTaskLineage(agentrun.AgentTask{ID: "root"}); err != nil || root != nil {
		t.Fatalf("root lineage = %s, %v", root, err)
	}
	for _, raw := range []string{`null`, `{}`, `{"parent_task_id":"child","root_task_id":"parent","budget_task_id":"parent","delegation_depth":1}`, golden + `{}`, `{"parent_task_id":"parent","root_task_id":"parent","budget_task_id":"other","delegation_depth":1}`, `{"parent_task_id":"parent","root_task_id":"parent","budget_task_id":"parent","delegation_depth":17}`} {
		if err := decodeTaskLineage([]byte(raw), &got); !errors.Is(err, agentrun.ErrInvalid) {
			t.Fatalf("invalid %s: %v", raw, err)
		}
	}
}

func TestTodo_AGENT_050_Lineage_Integration(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	one := e.tenantStore(t, "run-one")
	runtime, parent := newRuntimeTask(t, one, "run-one", "parent")
	child, err := runtime.CreateTask(ctx, agentrun.CreateRequest{ID: "child", TenantID: "run-one", UserID: parent.UserID, Goal: "specialist lookup", Plan: testPlan(t), Now: t0, ExpiresAt: parent.ExpiresAt, ParentTaskID: parent.ID, RootTaskID: parent.ID, BudgetTaskID: parent.ID, DelegationDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	two := e.tenantStore(t, "run-one")
	got, err := two.Get(ctx, "child")
	if err != nil || !reflect.DeepEqual(got, child) {
		t.Fatalf("cross-connection child = %+v, %v", got, err)
	}
	child.Version++
	child.Goal = "refined specialist lookup"
	if err := two.Save(ctx, child, child.Version-1); err != nil {
		t.Fatal(err)
	}
	got, err = one.Get(ctx, "child")
	if err != nil || !reflect.DeepEqual(got, child) {
		t.Fatalf("saved child = %+v, %v", got, err)
	}
	other := e.tenantStore(t, "run-two")
	if _, err := other.Get(ctx, "child"); !errors.Is(err, agentrun.ErrNotFound) {
		t.Fatalf("other tenant lineage = %v", err)
	}
}

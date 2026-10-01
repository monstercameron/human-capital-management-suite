package agentrun

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTodo_AGENT_050_SecurityTaskLineage(t *testing.T) {
	runtime, _ := newTestRuntime(t, planForTest(t, step("read", StepRead, TierRead)))
	base := CreateRequest{ID: "child", TenantID: "tenant-1", UserID: "user-1", Goal: "read", Plan: planForTest(t, step("read", StepRead, TierRead)), Now: testNow, ExpiresAt: testNow.Add(time.Hour), ParentTaskID: "parent", RootTaskID: "root", BudgetTaskID: "root", DelegationDepth: 2}
	for _, mutate := range []func(*CreateRequest){func(r *CreateRequest) { r.ParentTaskID = "" }, func(r *CreateRequest) { r.BudgetTaskID = "other" }, func(r *CreateRequest) { r.DelegationDepth = 17 }, func(r *CreateRequest) { r.RootTaskID = r.ID }} {
		req := base
		mutate(&req)
		if _, err := runtime.CreateTask(context.Background(), req); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid child lineage = %v", err)
		}
		if _, err := runtime.GetTask(context.Background(), req.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("invalid child persisted = %v", err)
		}
	}
	child, err := runtime.CreateTask(context.Background(), base)
	if err != nil || child.ParentTaskID != "parent" || child.RootTaskID != "root" || child.BudgetTaskID != "root" || child.DelegationDepth != 2 {
		t.Fatalf("child lineage = %+v, %v", child, err)
	}
}

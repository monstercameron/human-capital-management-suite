package agentbudget

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTodo_AGENT_050_ChildUsesRootBudgetAndChargesSharedPeriodsOnce(t *testing.T) {
	l, err := NewWithClock(budgetPolicy(), func() time.Time { return time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	rootLimit := Limits{Steps: 2, Tokens: 200, WallClock: time.Minute, SpendMicros: 200}
	if err := l.OpenTask(TaskSpec{ID: "root", TenantID: "tenant", UserID: "user", Limit: rootLimit}); err != nil {
		t.Fatal(err)
	}
	if err := l.OpenChildTask(TaskSpec{ID: "child", Limit: rootLimit}, "root"); err != nil {
		t.Fatal(err)
	}
	r, err := l.Reserve(context.Background(), Request{TaskID: "child", StepID: "s1", Fingerprint: "f", Estimate: Limits{Steps: 1, Tokens: 10, WallClock: time.Second, SpendMicros: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(*rUsage(r)); err != nil {
		t.Fatal(err)
	}
	var child, root TaskSnapshot
	for _, task := range l.Snapshot().Tasks {
		if task.ID == "child" {
			child = task
		}
		if task.ID == "root" {
			root = task
		}
	}
	if child.Used.Steps != 1 || root.Used.Steps != 1 {
		t.Fatalf("child/root usage = %+v / %+v", child.Used, root.Used)
	}
	s := l.Snapshot()
	if len(s.UserPeriodTotals) != 1 || s.UserPeriodTotals[0].Steps != 1 || len(s.TenantPeriodTotals) != 1 || s.TenantPeriodTotals[0].Steps != 1 {
		t.Fatalf("shared totals double-counted: %+v", s)
	}
	if child.ParentTaskID != "root" || child.RootTaskID != "root" || child.Depth != 1 {
		t.Fatalf("lineage = %+v", child)
	}
	if _, err := l.Reserve(context.Background(), Request{TaskID: "child", StepID: "s2", Fingerprint: "f2", Estimate: Limits{Steps: 2, Tokens: 1, WallClock: 1, SpendMicros: 1}}); !errors.Is(err, ErrPaused) {
		t.Fatalf("root ceiling reserve = %v", err)
	}
}

func rUsage(r *Reservation) *Usage { u := r.Estimate; return &u }

func TestTodo_AGENT_050_ChildValidatesIdentityAndPersistsLineage(t *testing.T) {
	rec := &recordingPersister{}
	l, err := NewWithPersistence(budgetPolicy(), func() time.Time { return time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC) }, rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.OpenTask(TaskSpec{ID: "root", TenantID: "tenant", UserID: "user", Limit: budgetPolicy().TaskDefault}); err != nil {
		t.Fatal(err)
	}
	if err := l.OpenChildTask(TaskSpec{ID: "child", TenantID: "other", Limit: budgetPolicy().TaskDefault}, "root"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("identity mismatch = %v", err)
	}
	if err := l.OpenChildTask(TaskSpec{ID: "child", Limit: budgetPolicy().TaskDefault}, "root"); err != nil {
		t.Fatal(err)
	}
	opened := rec.seen[len(rec.seen)-1]
	if opened.Task.Spec.ParentTaskID != "root" || opened.Task.Spec.RootTaskID != "root" || opened.Task.Spec.Depth != 1 {
		t.Fatalf("persisted lineage = %+v", opened.Task.Spec)
	}
	var state RestoredState
	for _, tr := range rec.seen {
		if tr.Kind == TransitionOpenTask {
			state.Tasks = append(state.Tasks, tr.Task)
		}
	}
	restored, err := NewWithClock(budgetPolicy(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.Restore(state); err != nil {
		t.Fatal(err)
	}
	var child TaskSnapshot
	for _, task := range restored.Snapshot().Tasks {
		if task.ID == "child" {
			child = task
		}
	}
	if child.ParentTaskID != "root" || child.RootTaskID != "root" || child.Depth != 1 {
		t.Fatalf("restored lineage = %+v", child)
	}
}

func TestTodo_AGENT_050_GrandchildCannotBypassIntermediateCeiling(t *testing.T) {
	policy := budgetPolicy()
	policy.TaskDefault = Limits{Steps: 4, Tokens: 400, WallClock: time.Hour, SpendMicros: 400}
	l, err := NewWithClock(policy, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.OpenTask(TaskSpec{ID: "root", TenantID: "tenant", UserID: "user", Limit: policy.TaskDefault}); err != nil {
		t.Fatal(err)
	}
	if err := l.OpenChildTask(TaskSpec{ID: "parent", Limit: Limits{Steps: 2, Tokens: 200, WallClock: time.Hour, SpendMicros: 200}}, "root"); err != nil {
		t.Fatal(err)
	}
	if err := l.OpenChildTask(TaskSpec{ID: "grandchild", Limit: Limits{Steps: 2, Tokens: 200, WallClock: time.Hour, SpendMicros: 200}}, "parent"); err != nil {
		t.Fatal(err)
	}
	first, err := l.Reserve(context.Background(), Request{TaskID: "grandchild", StepID: "one", Fingerprint: "f1", Estimate: Limits{Steps: 1, Tokens: 1, WallClock: time.Second, SpendMicros: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Settle(first.Estimate); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Reserve(context.Background(), Request{TaskID: "grandchild", StepID: "two", Fingerprint: "f2", Estimate: Limits{Steps: 2, Tokens: 1, WallClock: time.Second, SpendMicros: 1}}); !errors.Is(err, ErrPaused) {
		t.Fatalf("grandchild bypassed parent ceiling: %v", err)
	}
	var parent, root TaskSnapshot
	for _, task := range l.Snapshot().Tasks {
		if task.ID == "parent" {
			parent = task
		}
		if task.ID == "root" {
			root = task
		}
	}
	if parent.Used.Steps != 1 || root.Used.Steps != 1 {
		t.Fatalf("ancestor usage = %+v / %+v", parent.Used, root.Used)
	}
}

func TestTodo_AGENT_050_RestoreRejectsBrokenLineageAtomically(t *testing.T) {
	policy := budgetPolicy()
	l, err := NewWithClock(policy, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	limit := policy.TaskDefault
	root := DurableTask{Spec: TaskSpec{ID: "root", TenantID: "tenant", UserID: "user", Limit: limit}, Revision: 1, Attempts: map[string]int{}, Failures: map[string]int{}}
	child := root
	child.Spec = TaskSpec{ID: "child", TenantID: "tenant", UserID: "user", Limit: limit, ParentTaskID: "root", RootTaskID: "wrong", Depth: 1}
	if err := l.Restore(RestoredState{Tasks: []DurableTask{root, child}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("broken root lineage = %v", err)
	}
	if len(l.Snapshot().Tasks) != 0 {
		t.Fatal("invalid restore partially applied")
	}
	child.Spec.RootTaskID = "root"
	child.Spec.Depth = 2
	if err := l.Restore(RestoredState{Tasks: []DurableTask{root, child}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("broken depth lineage = %v", err)
	}
	if len(l.Snapshot().Tasks) != 0 {
		t.Fatal("invalid depth restore partially applied")
	}
}

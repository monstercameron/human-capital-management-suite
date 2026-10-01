package project

import (
	"errors"
	"testing"
	"time"
)

func TestTodo_PM_050(t *testing.T) {
	checklist, err := NewChecklist("task-1")
	if err != nil {
		t.Fatal(err)
	}
	checklist, err = checklist.AddEntry(checklist.Revision, "one", "Collect approval", "owner-1")
	if err != nil {
		t.Fatal(err)
	}
	checklist, err = checklist.AddEntry(checklist.Revision, "two", "Attach evidence", "owner-2")
	if err != nil {
		t.Fatal(err)
	}
	checklist, err = checklist.ReorderEntries(checklist.Revision, []string{"two", "one"})
	if err != nil || checklist.Entries[0].ID != "two" || checklist.Entries[0].Position != 1 {
		t.Fatalf("ordered revisioned entries = %+v err=%v", checklist.Entries, err)
	}
	completed, err := checklist.CompleteEntry(checklist.Revision, "two", ChecklistCompletion{EvidenceID: "evidence-2", ActorID: "owner-2", Note: "uploaded receipt", CompletedAt: time.Unix(100, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if completed.TaskID != "task-1" || completed.ParentSemantics != ChecklistDoesNotChangeParent || completed.Entries[0].Completion == nil || completed.Entries[0].Completion.EvidenceID != "evidence-2" {
		t.Fatalf("completion evidence was not retained: %+v", completed)
	}
}

func TestTodo_PM_050_Integration(t *testing.T) {
	checklist, _ := NewChecklist("parent-task")
	beforeParentStatus := "IN_PROGRESS"
	var err error
	checklist, err = checklist.AddEntry(checklist.Revision, "entry", "Do the child work", "owner")
	if err != nil {
		t.Fatal(err)
	}
	checklist, err = checklist.CompleteEntry(checklist.Revision, "entry", ChecklistCompletion{EvidenceID: "proof", ActorID: "owner", Note: "done", CompletedAt: time.Unix(100, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if beforeParentStatus != "IN_PROGRESS" || checklist.TaskID != "parent-task" {
		t.Fatal("checklist completion changed or implied a parent-task status")
	}
}

func TestTodo_PM_050_Security(t *testing.T) {
	checklist, _ := NewChecklist("task")
	checklist, _ = checklist.AddEntry(checklist.Revision, "entry", "Evidence required", "owner")
	_, err := checklist.CompleteEntry(checklist.Revision, "entry", ChecklistCompletion{ActorID: "owner", Note: "missing evidence", CompletedAt: time.Unix(1, 0).UTC()})
	if !errors.Is(err, ErrInvalidChecklist) {
		t.Fatalf("missing evidence accepted: %v", err)
	}
	_, err = checklist.CompleteEntry(checklist.Revision, "entry", ChecklistCompletion{EvidenceID: "proof", Note: "missing actor", CompletedAt: time.Unix(1, 0).UTC()})
	if !errors.Is(err, ErrInvalidChecklist) {
		t.Fatalf("missing actor accepted: %v", err)
	}
}

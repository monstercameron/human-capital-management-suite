package agentrun

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
)

func TestTodo_AGENTDOC_004(t *testing.T) {
	refs := []agentdocref.Reference{{DocumentID: "doc-12345678-1234-4123-8123-123456789abc", VersionMode: agentdocref.ModeLatestPublished, SectionAnchor: "leave", Label: "Leave policy"}}
	ctx, err := WithDocumentReferences(context.Background(), refs)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlan([]PlanStep{{ID: "read", Type: StepRead, SkillID: "skill.read", SkillVersion: 1, ExpectedOutput: "facts", Tier: TierRead}})
	if err != nil {
		t.Fatal(err)
	}
	withoutDocuments := plan.Digest
	store := NewMemoryStore()
	runtime, err := NewRuntime(store)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	task, err := runtime.CreateTask(ctx, CreateRequest{ID: "task-doc", TenantID: "tenant-a", UserID: "user-a", Goal: "Summarize the policy", Plan: plan, Now: now, ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if task.Plan.Digest == withoutDocuments || len(task.Plan.DocumentReferences) != 1 || task.Plan.DocumentReferences[0] != refs[0] {
		t.Fatalf("created task documents/digest = %+v / %s", task.Plan.DocumentReferences, task.Plan.Digest)
	}
	omissions := []agentdocref.Omission{{Label: "Leave policy", Reason: agentdocref.NotPublished}}
	if err := runtime.RecordDocumentOmissions(context.Background(), task.ID, refs, omissions); err != nil {
		t.Fatal(err)
	}
	stored, err := runtime.GetTask(context.Background(), task.ID)
	if err != nil || stored.Version != task.Version || len(stored.Plan.DocumentOmissions) != 1 || stored.Plan.DocumentOmissions[0] != omissions[0] {
		t.Fatalf("stored omissions = %+v, %v", stored.Plan.DocumentOmissions, err)
	}
	confirmed, err := runtime.ConfirmPlan(context.Background(), task.ID, task.UserID, task.Version, now.Add(time.Minute))
	if err != nil || len(confirmed.Plan.DocumentOmissions) != 1 {
		t.Fatalf("state transition lost omissions: %+v, %v", confirmed.Plan.DocumentOmissions, err)
	}
	confirmed.Plan.DocumentReferences[0].Label = "mutated"
	again, _ := runtime.GetTask(context.Background(), task.ID)
	if again.Plan.DocumentReferences[0].Label != "Leave policy" {
		t.Fatal("task projection aliased document references")
	}
	replacement, err := NewPlan([]PlanStep{{ID: "read", Type: StepRead, SkillID: "skill.read", SkillVersion: 1, ExpectedOutput: "fresh facts", Tier: TierRead}})
	if err != nil {
		t.Fatal(err)
	}
	replanned, err := runtime.Replan(context.Background(), task.ID, confirmed.Version, replacement, now.Add(2*time.Minute))
	if err != nil || len(replanned.Plan.DocumentReferences) != 1 || len(replanned.Plan.DocumentOmissions) != 1 {
		t.Fatalf("replan lost documents = %+v, %v", replanned.Plan, err)
	}
}

func TestTodo_AGENTDOC_004_Security_RuntimeValidation(t *testing.T) {
	refs := make([]agentdocref.Reference, agentdocref.MaxRequestReferences+1)
	for i := range refs {
		refs[i] = agentdocref.Reference{DocumentID: string(rune('a' + i)), VersionMode: agentdocref.ModeLatestPublished, Label: string(rune('A' + i))}
	}
	if _, err := WithDocumentReferences(context.Background(), refs); !errors.Is(err, ErrDocumentReferenceInvalid) {
		t.Fatalf("six references = %v", err)
	}
	store := NewMemoryStore()
	runtime, _ := NewRuntime(store)
	if err := runtime.RecordDocumentOmissions(context.Background(), "missing", nil, []agentdocref.Omission{{Label: "secret", Reason: agentdocref.NotFound}}); !errors.Is(err, ErrDocumentReferenceInvalid) {
		t.Fatalf("unbound omission = %v", err)
	}
}

package workitem

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// smb003AssignedTemplate builds a procedure that exercises the SMB-003 GREEN
// clause: assigned owners, due offsets, a governed HCM step that must not
// complete through the checklist alone, and a read step awaiting an
// authorized document link.
func smb003AssignedTemplate(now time.Time) ChecklistTemplate {
	return ChecklistTemplate{
		TemplateID: uuid.New(),
		TenantID:   uuid.New(),
		Title:      "Engineer onboarding",
		RoleRefs:   []string{"people.manager"},
		Items: []TemplateItem{
			{Kind: ItemKindDo, Title: "Provision laptop", Required: true, AssigneeRole: "it.help", DueInDays: 3},
			{Kind: ItemKindRead, Title: "Read the handbook", Required: true, AssigneeRole: "subject", DueInDays: 7, DocVersionID: "handbook-v12"},
			{Kind: ItemKindSign, Title: "Sign the IP agreement", Required: true, AssigneeRole: "subject", GovernedBy: "hr.signing/v1"},
			{Kind: ItemKindConfirm, Title: "Confirm desk assignment", Required: false, AssigneeRole: "people.manager"},
		},
		CreatedAt: now,
	}
}

// TestTodo_SMB_003_AssignedDueGoverned proves instantiation carries
// ownership, due dates and governance: assignees resolve from the caller's
// mapping, due offsets become concrete dates, and a governed item refuses a
// direct completion until its owning workflow cites its own completed item.
func TestTodo_SMB_003_AssignedDueGoverned(t *testing.T) {
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	template := smb003AssignedTemplate(now)
	if err := template.Validate(); err != nil {
		t.Fatalf("template validate: %v", err)
	}

	list, err := template.Instantiate(ChecklistInput{
		ChecklistID: uuid.New(),
		Title:       "Ada onboarding",
		CreatedAt:   now,
		Assignees:   map[int]string{1: "it.ada", 2: "ada", 3: "ada", 4: "bea.manager"},
	})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	wantAssignee := []string{"it.ada", "ada", "ada", "bea.manager"}
	for i, item := range list.Items {
		if item.Assignee != wantAssignee[i] {
			t.Fatalf("item %d assignee = %q, want %q", i, item.Assignee, wantAssignee[i])
		}
	}
	if list.Items[0].DueAt == nil || !list.Items[0].DueAt.Equal(now.AddDate(0, 0, 3)) {
		t.Fatalf("item 0 due = %v, want now+3d", list.Items[0].DueAt)
	}
	if list.Items[3].DueAt != nil {
		t.Fatalf("item 3 due = %v, want none", list.Items[3].DueAt)
	}
	if list.Items[1].DocVersionID != "handbook-v12" {
		t.Fatalf("item 1 doc = %q, want handbook-v12", list.Items[1].DocVersionID)
	}
	if err := list.Validate(); err != nil {
		t.Fatalf("checklist validate: %v", err)
	}

	governed := list.Items[2].ItemID
	if err := list.CompleteItem(governed, "ada", now); err == nil {
		t.Fatal("direct completion of a governed item = success, want refusal")
	}
	if err := list.CompleteGovernedItem(governed, "ada", now, ""); err == nil {
		t.Fatal("governed completion without owning-workflow evidence = success, want refusal")
	}
	if err := list.CompleteGovernedItem(governed, "ada", now, uuid.NewString()); err != nil {
		t.Fatalf("governed completion with evidence: %v", err)
	}
	if list.Items[2].State != StepStateDone || list.Items[2].DelegatedRef == "" {
		t.Fatalf("governed item = %+v, want done with delegated ref", list.Items[2])
	}
	plain := list.Items[0].ItemID
	if err := list.CompleteGovernedItem(plain, "it.ada", now, uuid.NewString()); err == nil {
		t.Fatal("governed completion of a plain item = success, want refusal")
	}
	if err := list.Validate(); err != nil {
		t.Fatalf("worked checklist: %v", err)
	}
	if err := list.ReopenItem(governed, now); err != nil {
		t.Fatalf("reopen governed: %v", err)
	}
	if list.Items[2].DelegatedRef != "" {
		t.Fatalf("reopened governed item keeps delegated ref %q", list.Items[2].DelegatedRef)
	}

	negative := template
	negative.Items[0].DueInDays = -1
	if err := negative.Validate(); err == nil {
		t.Fatal("negative due offset = valid, want refusal")
	}
}

// TestTodo_SMB_003_PersistedChecklists proves the store round-trips validated
// templates and checklists, refuses invalid or cross-tenant writes, and never
// hands out a live reference the caller can mutate behind its back.
func TestTodo_SMB_003_PersistedChecklists(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	template := smb003AssignedTemplate(now)
	store := &ChecklistStore{}

	if err := store.SaveTemplate(ctx, template); err != nil {
		t.Fatalf("save template: %v", err)
	}
	if err := store.SaveTemplate(ctx, template); err == nil {
		t.Fatal("duplicate template save = success, want refusal")
	}
	bad := template
	bad.TemplateID = uuid.Nil
	if err := store.SaveTemplate(ctx, bad); err == nil {
		t.Fatal("invalid template save = success, want refusal")
	}
	loaded, err := store.LoadTemplate(ctx, template.TenantID, template.TemplateID)
	if err != nil {
		t.Fatalf("load template: %v", err)
	}
	if len(loaded.Items) != 4 || loaded.Items[2].GovernedBy != "hr.signing/v1" {
		t.Fatalf("loaded template = %+v, want 4 governed-carrying items", loaded)
	}
	if _, err := store.LoadTemplate(ctx, uuid.New(), template.TemplateID); err == nil {
		t.Fatal("cross-tenant template load = success, want refusal")
	}

	list, err := template.Instantiate(ChecklistInput{ChecklistID: uuid.New(), Title: "Ada onboarding", CreatedAt: now})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	if err := store.Save(ctx, list); err != nil {
		t.Fatalf("save checklist: %v", err)
	}
	if err := store.Save(ctx, list); err == nil {
		t.Fatal("duplicate checklist save = success, want refusal")
	}
	broken := list
	broken.Items = append([]ChecklistItem(nil), list.Items...)
	broken.Items[0].State = StepStateDone
	if _, err := store.Update(ctx, broken); err == nil {
		t.Fatal("forged completion update = success, want refusal")
	}
	if err := list.CompleteItem(list.Items[0].ItemID, "it.ada", now); err != nil {
		t.Fatalf("complete: %v", err)
	}
	updated, err := store.Update(ctx, list)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Items[0].State != StepStateDone || updated.Items[0].CompletedBy != "it.ada" {
		t.Fatalf("updated item = %+v, want done by it.ada", updated.Items[0])
	}
	updated.Items[0].CompletedBy = "mallory"
	reloaded, err := store.Load(ctx, list.TenantID, list.ChecklistID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Items[0].CompletedBy != "it.ada" {
		t.Fatal("caller mutated stored state through a returned value")
	}
	if _, err := store.Load(ctx, uuid.New(), list.ChecklistID); err == nil {
		t.Fatal("cross-tenant checklist load = success, want refusal")
	}
}

// TestTodo_SMB_003_ServedByOwner proves the employee/team serving surface:
// assignee queues, completion by owner, and document links that stay
// invisible to anyone outside their authorized viewer set.
func TestTodo_SMB_003_ServedByOwner(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	template := smb003AssignedTemplate(now)
	store := &ChecklistStore{}

	ada, err := template.Instantiate(ChecklistInput{
		ChecklistID: uuid.New(), Title: "Ada onboarding", CreatedAt: now,
		Assignees: map[int]string{1: "it.ada", 2: "ada", 3: "ada", 4: "bea.manager"},
	})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	if err := store.Save(ctx, ada); err != nil {
		t.Fatalf("save: %v", err)
	}
	link := AuthorizedDocumentLink{
		TenantID:            ada.TenantID,
		DocumentVersionID:   "handbook-v12",
		DocumentDigest:      "sha256:handbook",
		AuthorizedViewerIDs: []string{"ada", "bea.manager"},
	}
	if _, err := store.LinkDocument(ctx, ada.TenantID, ada.ChecklistID, ada.Items[1].ItemID, "mallory", link); err == nil {
		t.Fatal("document link by an unauthorized viewer = success, want refusal")
	}
	linked, err := store.LinkDocument(ctx, ada.TenantID, ada.ChecklistID, ada.Items[1].ItemID, "ada", link)
	if err != nil {
		t.Fatalf("link document: %v", err)
	}
	if len(linked.Items[1].Documents) != 1 {
		t.Fatalf("linked item documents = %d, want 1", len(linked.Items[1].Documents))
	}
	if err := linked.CompleteItem(linked.Items[1].ItemID, "ada", now); err != nil {
		t.Fatalf("complete read step: %v", err)
	}
	if _, err := store.Update(ctx, linked); err != nil {
		t.Fatalf("update: %v", err)
	}

	queue := store.ListByAssignee(ctx, ada.TenantID, "ada")
	if len(queue) != 1 || len(queue[0].Items) != 2 {
		t.Fatalf("ada queue = %d lists, want 1 list with 2 items", len(queue))
	}
	for _, item := range queue[0].Items {
		if item.Assignee != "ada" {
			t.Fatalf("queue item assignee = %q, want ada", item.Assignee)
		}
	}
	progress, err := store.ProgressByOwner(ctx, ada.TenantID, ada.ChecklistID)
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if progress["ada"].Done != 1 || progress["ada"].Total != 2 {
		t.Fatalf("ada progress = %+v, want done 1 of 2", progress["ada"])
	}
	if progress["it.ada"].Done != 0 || progress["it.ada"].Total != 1 {
		t.Fatalf("it.ada progress = %+v, want done 0 of 1", progress["it.ada"])
	}

	adaView := store.View(ctx, ada.TenantID, ada.ChecklistID, "ada")
	if len(adaView.Items[1].Documents) != 1 {
		t.Fatal("authorized viewer sees no document link")
	}
	malloryView := store.View(ctx, ada.TenantID, ada.ChecklistID, "mallory")
	for _, item := range malloryView.Items {
		if len(item.Documents) != 0 {
			t.Fatal("unauthorized viewer sees a private document link")
		}
	}
	if _, err := store.ProgressByOwner(ctx, uuid.New(), ada.ChecklistID); err == nil {
		t.Fatal("cross-tenant progress = success, want refusal")
	}
}

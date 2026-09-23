package workitem

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func smb003Template(now time.Time) ChecklistTemplate {
	return ChecklistTemplate{
		TemplateID: uuid.New(),
		TenantID:   uuid.New(),
		Title:      "Engineer onboarding",
		RoleRefs:   []string{"people.manager"},
		Items: []TemplateItem{
			{Kind: ItemKindDo, Title: "Provision laptop", Required: true},
			{Kind: ItemKindRead, Title: "Read the handbook", Required: true},
			{Kind: ItemKindSign, Title: "Sign the IP agreement", Required: true},
			{Kind: ItemKindUpload, Title: "Upload right-to-work", Required: true},
			{Kind: ItemKindConfirm, Title: "Confirm desk assignment", Required: false},
		},
		CreatedAt: now,
	}
}

func smb003Instantiate(t *testing.T, template ChecklistTemplate, title string, now time.Time) Checklist {
	t.Helper()
	list, err := template.Instantiate(ChecklistInput{ChecklistID: uuid.New(), Title: title, CreatedAt: now})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	return list
}

// TestTodo_SMB_003 proves the repeatable-procedure kernel: a reusable
// template with ordered items of every kind instantiates into a fresh
// checklist with all items open; completing, skipping (non-required only)
// and reopening items behave; and malformed templates and checklists are
// refused before anything is stored or served.
func TestTodo_SMB_003(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	template := smb003Template(now)
	if err := template.Validate(); err != nil {
		t.Fatalf("template validate: %v", err)
	}

	list := smb003Instantiate(t, template, "Ada onboarding", now)
	if len(list.Items) != 5 {
		t.Fatalf("items = %d, want 5", len(list.Items))
	}
	for i, item := range list.Items {
		if item.Position != i+1 {
			t.Fatalf("item %d position = %d, want %d", i, item.Position, i+1)
		}
		if item.State != StepStateOpen {
			t.Fatalf("fresh item %d state = %q, want open", i, item.State)
		}
		if item.ChecklistID != list.ChecklistID {
			t.Fatal("fresh item does not reference its checklist")
		}
	}

	first := list.Items[0].ItemID
	second := list.Items[1].ItemID
	optional := list.Items[4].ItemID
	if err := list.CompleteItem(first, "ada", now); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if list.Items[0].State != StepStateDone || list.Items[0].CompletedBy != "ada" {
		t.Fatalf("completed item = %+v, want done by ada", list.Items[0])
	}
	if err := list.SkipItem(second, "ada", now); err == nil {
		t.Fatal("skipping a required item = success, want refusal")
	}
	if err := list.SkipItem(optional, "ada", now); err != nil {
		t.Fatalf("skip optional: %v", err)
	}
	if list.Items[4].State != StepStateSkipped {
		t.Fatalf("skipped item = %+v, want skipped", list.Items[4])
	}
	if err := list.CompleteItem(first, "ada", now); err == nil {
		t.Fatal("completing a done item = success, want refusal")
	}
	if err := list.ReopenItem(first, now); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if list.Items[0].State != StepStateOpen || list.Items[0].CompletedBy != "" || list.Items[0].CompletedAt != nil {
		t.Fatalf("reopened item = %+v, want open with no completion", list.Items[0])
	}
	if err := list.Validate(); err != nil {
		t.Fatalf("checklist validate: %v", err)
	}

	badKind := template
	badKind.Items = []TemplateItem{{Kind: "dance", Title: "Party"}}
	if err := badKind.Validate(); err == nil {
		t.Fatal("unknown item kind = valid, want refusal")
	}
	dupRole := template
	dupRole.RoleRefs = []string{"people.manager", "people.manager"}
	if err := dupRole.Validate(); err == nil {
		t.Fatal("duplicate role ref = valid, want refusal")
	}
	blankTitle := template
	blankTitle.Title = "  "
	if err := blankTitle.Validate(); err == nil {
		t.Fatal("blank template title = valid, want refusal")
	}
	broken := list
	broken.Items[2].Position = 1
	if err := broken.Validate(); err == nil {
		t.Fatal("duplicate position = valid, want refusal")
	}
	if err := list.CompleteItem(uuid.New(), "ada", now); err == nil {
		t.Fatal("completing an unknown item = success, want refusal")
	}
	if err := list.CompleteItem(optional, "", now); err == nil {
		t.Fatal("completing without an actor = success, want refusal")
	}
}

// TestTodo_SMB_003_Integration proves template/instance separation across
// instantiations: unique ids, independent progress, and a fully worked
// checklist that stays valid and shows completion by owner.
func TestTodo_SMB_003_Integration(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	template := smb003Template(now)
	list := smb003Instantiate(t, template, "Ada onboarding", now)
	other := smb003Instantiate(t, template, "Bo onboarding", now)

	seenIDs := map[uuid.UUID]bool{}
	for _, item := range list.Items {
		seenIDs[item.ItemID] = true
	}
	for _, item := range other.Items {
		if seenIDs[item.ItemID] {
			t.Fatal("two instantiations share an item id")
		}
	}
	for i := range list.Items {
		if list.Items[i].Required {
			if err := list.CompleteItem(list.Items[i].ItemID, "ada", now); err != nil {
				t.Fatalf("complete required %d: %v", i, err)
			}
		}
	}
	if err := list.SkipItem(list.Items[4].ItemID, "ada", now); err != nil {
		t.Fatalf("skip optional: %v", err)
	}
	if err := list.Validate(); err != nil {
		t.Fatalf("completed checklist: %v", err)
	}
	owned := false
	for _, item := range list.Items {
		if item.State == StepStateDone {
			if item.CompletedBy == "" || item.CompletedAt == nil {
				t.Fatalf("done item %+v shows no owner", item)
			}
			owned = true
		}
	}
	if !owned {
		t.Fatal("completed checklist shows no owner")
	}
	if len(other.Items) != 5 || other.Items[0].State != StepStateOpen {
		t.Fatalf("second instantiation disturbed: %+v", other.Items[0])
	}
}

// TestTodo_SMB_003_Security proves hand-edited state never validates:
// foreign checklist ids, forged completion, and skipped-required items are
// all refused, so a tampered checklist cannot present as complete.
func TestTodo_SMB_003_Security(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	template := smb003Template(now)
	list := smb003Instantiate(t, template, "Ada onboarding", now)
	other := smb003Instantiate(t, template, "Bo onboarding", now)

	attacker := other
	attacker.Items[0].ChecklistID = list.ChecklistID
	if err := attacker.Validate(); err == nil {
		t.Fatal("foreign-checklist item = valid, want refusal")
	}
	attacker = other
	attacker.Items[1].State = StepStateDone
	if err := attacker.Validate(); err == nil {
		t.Fatal("forged completion without actor = valid, want refusal")
	}
	attacker = other
	attacker.Items[0].State = StepStateSkipped
	if err := attacker.Validate(); err == nil {
		t.Fatal("skipped required item = valid, want refusal")
	}
}

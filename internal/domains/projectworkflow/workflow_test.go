package projectworkflow

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func workflowFixture() Config {
	min, max := 2, 80
	return Config{
		TaskTypes: []TaskType{{ID: "task", Name: "Task", FieldIDs: []string{"owner", "kind", "summary"}, InitialStatus: "todo", RequiredFields: []string{"owner"}}},
		Statuses: []Status{
			{ID: "todo", Name: "To do", Category: CategoryNotStarted, AllowedNextStatusIDs: []string{"doing"}},
			{ID: "doing", Name: "In progress", Category: CategoryActive, AllowedNextStatusIDs: []string{"done"}},
			{ID: "done", Name: "Done", Category: CategoryDone},
		},
		Transitions: []Transition{{From: "todo", To: "doing"}, {From: "doing", To: "done", RequiredFields: []string{"owner"}}},
		Fields: []Field{
			{ID: "owner", Name: "Owner", Type: FieldPerson, Classification: "INTERNAL"},
			{ID: "kind", Name: "Kind", Type: FieldEnum, Classification: "INTERNAL", Validation: FieldValidation{Options: []string{"request", "follow-up"}}},
			{ID: "summary", Name: "Summary", Type: FieldText, Classification: "INTERNAL", Validation: FieldValidation{MinLength: &min, MaxLength: &max}},
		},
		Columns: []Column{{ID: "queue", Name: "Queue", StatusIDs: []string{"todo"}}, {ID: "work", Name: "Work", StatusIDs: []string{"doing"}}, {ID: "complete", Name: "Complete", StatusIDs: []string{"done"}}},
	}
}

func TestTodo_PM_012(t *testing.T) {
	c := workflowFixture()
	if got := Validate(c); len(got) != 0 {
		t.Fatalf("valid config errors = %#v", got)
	}

	c.Statuses = append(c.Statuses, Status{ID: "orphan", Name: "Orphan", Category: CategoryBlocked})
	c.Transitions = append(c.Transitions, Transition{From: "todo", To: "missing"})
	c.Columns[0].StatusIDs = append(c.Columns[0].StatusIDs, "todo")
	c.Fields = append(c.Fields, c.Fields[0])
	got := Validate(c)
	wantCodes := map[string]bool{"UNREACHABLE_STATUS": false, "INVALID_TRANSITION_TARGET": false, "DUPLICATE_COLUMN_STATUS": false, "DUPLICATE_FIELD_ID": false}
	for _, e := range got {
		if _, ok := wantCodes[e.Code]; ok {
			wantCodes[e.Code] = true
		}
	}
	for code, found := range wantCodes {
		if !found {
			t.Errorf("missing %s in errors: %#v", code, got)
		}
	}
	if !reflect.DeepEqual(got, Validate(c)) {
		t.Fatal("validation errors are not deterministic")
	}
}

func TestTodo_PM_012_Property(t *testing.T) {
	c := workflowFixture()
	published, err := Publish(c, 7)
	if err != nil {
		t.Fatal(err)
	}
	if published.Version() != 7 || len(published.Digest()) != 64 {
		t.Fatalf("published metadata invalid: version=%d digest=%q", published.Version(), published.Digest())
	}

	copy := published.Snapshot()
	copy.Columns[0].StatusIDs[0] = "tampered"
	copy.Statuses[0].AllowedNextStatusIDs[0] = "tampered"
	copy.TaskTypes[0].FieldIDs[0] = "tampered"
	copy.TaskTypes[0].RequiredFields[0] = "tampered"
	copy.Fields[1].Validation.Options[0] = "tampered"
	copy.Fields[2].Validation.MinLength = ptrInt(99)
	copy.Fields[0].Default = json.RawMessage(`"changed"`)
	second := published.Snapshot()
	if second.Columns[0].StatusIDs[0] != "todo" || second.Statuses[0].AllowedNextStatusIDs[0] != "doing" || second.TaskTypes[0].FieldIDs[0] != "owner" || second.TaskTypes[0].RequiredFields[0] != "owner" || second.Fields[1].Validation.Options[0] != "request" || *second.Fields[2].Validation.MinLength != 2 || len(second.Fields[0].Default) != 0 {
		t.Fatal("snapshot mutation changed the published configuration")
	}
	secondPublished, err := Publish(workflowFixture(), 8)
	if err != nil {
		t.Fatal(err)
	}
	if published.Digest() != secondPublished.Digest() {
		t.Fatalf("same config digest differs: %s != %s", published.Digest(), secondPublished.Digest())
	}
	if _, err := Publish(c, 0); !errors.Is(err, ErrInvalidVersion) {
		t.Fatalf("zero version error = %v", err)
	}
}

func TestTodo_PM_012_Golden(t *testing.T) {
	p, err := Publish(workflowFixture(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if p.Digest() != "180298daded9d5843a0925e9923101cc6605908f5bd8ba8da78f35f29db022fd" {
		t.Fatalf("canonical config digest changed: %s", p.Digest())
	}
}

func TestTodo_PM_013(t *testing.T) {
	c := workflowFixture()
	if _, err := Publish(c, 1); err != nil {
		t.Fatalf("publish valid configuration: %v", err)
	}
	c.TaskTypes[0].InitialStatus = "retired"
	c.Statuses[0].Retired = true
	if _, err := Publish(c, 2); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid draft published: %v", err)
	}
	if _, err := Publish(workflowFixture(), 0); !errors.Is(err, ErrInvalidVersion) {
		t.Fatalf("invalid version published: %v", err)
	}
}

func TestTodo_PM_013_Golden(t *testing.T) {
	first, err := Publish(workflowFixture(), 11)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Publish(workflowFixture(), 12)
	if err != nil {
		t.Fatal(err)
	}
	if first.Version() == second.Version() || first.Digest() != second.Digest() {
		t.Fatalf("content pin not stable across version number: first=(%d,%s) second=(%d,%s)", first.Version(), first.Digest(), second.Version(), second.Digest())
	}
}

func TestTodo_PM_009(t *testing.T) {
	c := workflowFixture()
	c.Transitions = []Transition{{From: "todo", To: "done"}}
	c.Statuses[0].AllowedNextStatusIDs = []string{"done"}
	if got := Validate(c); !hasCode(got, "UNREACHABLE_STATUS") {
		t.Fatalf("disconnected workflow accepted: %#v", got)
	}
	c = workflowFixture()
	c.Transitions[1].RequiredFields = []string{"retired"}
	c.Fields = append(c.Fields, Field{ID: "retired", Name: "Retired", Type: FieldText, Classification: "INTERNAL", Retired: true})
	if got := Validate(c); !hasCode(got, "INVALID_REQUIRED_FIELD") {
		t.Fatalf("required retired field accepted: %#v", got)
	}
}

func TestTodo_PM_009_Property(t *testing.T) {
	c := workflowFixture()
	c.Transitions = append(c.Transitions, Transition{From: "todo", To: "done"})
	c.Statuses[0].AllowedNextStatusIDs = append(c.Statuses[0].AllowedNextStatusIDs, "done")
	if got := Validate(c); hasCode(got, "UNREACHABLE_STATUS") {
		t.Fatalf("adding a legal edge made statuses unreachable: %#v", got)
	}
}

func TestTodo_PM_009_Golden(t *testing.T) {
	c := workflowFixture()
	c.Transitions[1].To = "todo"
	c.Statuses[1].AllowedNextStatusIDs = []string{"todo"}
	if got := Validate(c); !hasCode(got, "UNREACHABLE_STATUS") {
		t.Fatalf("transition graph did not preserve reachability finding: %#v", got)
	}
}

func TestTypedFieldDefinitionsRejectInvalidBoundsAndOptions(t *testing.T) {
	c := workflowFixture()
	c.Fields[2].Validation.MinLength = ptrInt(90)
	c.Fields[2].Validation.MaxLength = ptrInt(2)
	c.Fields[1].Validation.Options = append(c.Fields[1].Validation.Options, "request")
	got := Validate(c)
	if !hasCode(got, "INVALID_FIELD_VALIDATION") || !hasCode(got, "DUPLICATE_ENUM_OPTION") {
		t.Fatalf("bad typed field definitions were accepted: %#v", got)
	}
	if !strings.Contains(got.Error(), "validation") {
		t.Fatalf("structured error message missing useful path: %v", got)
	}
}

func TestValidateTaskCreationChecksPinnedWorkflowAndTypedRequiredFields(t *testing.T) {
	c := workflowFixture()
	values := map[string]json.RawMessage{"owner": json.RawMessage(`"person-1"`), "summary": json.RawMessage(`"Prepare packet"`), "kind": json.RawMessage(`"request"`)}
	if err := ValidateTaskCreation(c, 8, 8, "task", "todo", values); err != nil {
		t.Fatalf("valid task creation: %v", err)
	}
	if err := ValidateTaskCreation(c, 7, 8, "task", "todo", values); !errors.Is(err, ErrTaskCreationRejected) {
		t.Fatalf("stale workflow version error=%v", err)
	}
	bad := map[string]json.RawMessage{"owner": json.RawMessage(`""`), "summary": json.RawMessage(`17`), "unknown": json.RawMessage(`true`)}
	err := ValidateTaskCreation(c, 8, 8, "task", "doing", bad)
	var details TaskCreationErrors
	if !errors.Is(err, ErrTaskCreationRejected) || !errors.As(err, &details) {
		t.Fatalf("creation rejection is not structured: %T %v", err, err)
	}
	for _, code := range []string{"INVALID_INITIAL_STATUS", "INVALID_FIELD_VALUE", "UNKNOWN_FIELD", "REQUIRED_FIELD_MISSING"} {
		found := false
		for _, detail := range details {
			if detail.Code == code {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %s in creation errors: %+v", code, details)
		}
	}
}

func TestStatusAndFieldRequirementsFollowProtoConfig(t *testing.T) {
	c := workflowFixture()
	c.Fields[1].Required = true
	c.Statuses[1].RequiredFieldIDs = []string{"summary"}
	c.Statuses[2].RequiredFieldIDs = []string{"summary"}
	c.TaskTypes[0].FieldIDs = []string{"owner", "kind", "summary"}
	if got := Validate(c); len(got) != 0 {
		t.Fatalf("valid proto-shaped config errors=%+v", got)
	}
	p, err := Publish(c, 5)
	if err != nil {
		t.Fatal(err)
	}
	createErr := ValidateTaskCreation(c, 5, 5, "task", "todo", map[string]json.RawMessage{"owner": json.RawMessage(`"p1"`), "summary": json.RawMessage(`"ready"`)})
	if !errors.Is(createErr, ErrTaskCreationRejected) {
		t.Fatalf("required field on type was not enforced: %v", createErr)
	}
	moveErr := ValidateTransition(p, TransitionInput{ExpectedConfigVersion: 5, TaskTypeID: "task", FromStatusID: "todo", ToStatusID: "doing", CurrentFields: map[string]json.RawMessage{"owner": json.RawMessage(`"p1"`), "kind": json.RawMessage(`"request"`)}})
	if !errors.Is(moveErr, ErrTransitionRejected) || !hasTransitionCode(moveErr, "REQUIRED_FIELD_MISSING") {
		t.Fatalf("required destination status field was not enforced: %v", moveErr)
	}
	moveErr = ValidateTransition(p, TransitionInput{ExpectedConfigVersion: 5, TaskTypeID: "task", FromStatusID: "todo", ToStatusID: "done", CurrentFields: map[string]json.RawMessage{"owner": json.RawMessage(`"p1"`), "kind": json.RawMessage(`"request"`), "summary": json.RawMessage(`"ready"`)}})
	if !errors.Is(moveErr, ErrTransitionRejected) || !hasTransitionCode(moveErr, "TRANSITION_NOT_ALLOWED") {
		t.Fatalf("undeclared allowed-next edge succeeded: %v", moveErr)
	}

	c.TaskTypes[0].FieldIDs = []string{"owner"}
	if got := Validate(c); !hasCode(got, "REQUIRED_FIELD_NOT_ALLOWED") {
		t.Fatalf("required status field outside task type field IDs accepted: %+v", got)
	}
}

func hasCode(errs ValidationErrors, code string) bool {
	for _, e := range errs {
		if e.Code == code {
			return true
		}
	}
	return false
}
func ptrInt(v int) *int { return &v }

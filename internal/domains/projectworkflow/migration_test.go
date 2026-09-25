package projectworkflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestTodo_PM_014(t *testing.T) {
	current := workflowFixture()
	pending := workflowFixture()
	pending.Statuses[1].Category = CategoryBlocked
	pending.Fields[1].Validation.Options = []string{"request"}
	pending.Fields = append(pending.Fields, Field{ID: "priority", Name: "Priority", Type: FieldEnum, Classification: "INTERNAL", Default: json.RawMessage(`"normal"`), Validation: FieldValidation{Options: []string{"normal", "urgent"}}})
	pending.TaskTypes[0].FieldIDs = append(pending.TaskTypes[0].FieldIDs, "priority")
	pending.TaskTypes[0].RequiredFields = append(pending.TaskTypes[0].RequiredFields, "priority")
	tasks := []TaskSnapshot{
		{ID: "task-2", TypeID: "task", StatusID: "doing", Fields: map[string]json.RawMessage{"owner": json.RawMessage(`"person-2"`), "kind": json.RawMessage(`"request"`)}},
		{ID: "task-1", TypeID: "task", StatusID: "todo", Fields: map[string]json.RawMessage{"owner": json.RawMessage(`"person-1"`), "kind": json.RawMessage(`"request"`)}},
	}
	preview, err := PreviewMigration(MigrationRequest{Current: current, Pending: pending, Tasks: tasks})
	if err != nil {
		t.Fatalf("valid migration preview: %v (issues=%+v invalid=%+v)", err, preview.Issues, preview.InvalidValues)
	}
	if !preview.Safe || preview.TaskCount != 2 || preview.AffectedTaskCount != 2 || len(preview.AffectedTaskIDs) != 2 || preview.AffectedTaskIDs[0] != "task-1" || preview.AffectedTaskIDs[1] != "task-2" {
		t.Fatalf("impact report incorrect: %+v", preview)
	}
	if len(preview.StatusChanges) != 1 || preview.StatusChanges[0].StatusID != "doing" || preview.StatusChanges[0].NewCategory != CategoryBlocked {
		t.Fatalf("category change missing: %+v", preview.StatusChanges)
	}
	if len(preview.FieldChanges) == 0 {
		t.Fatal("field definition changes missing")
	}
	if len(preview.TaskMappings) != 2 || len(preview.TaskMappings[0].DefaultsApplied) != 1 || preview.TaskMappings[0].DefaultsApplied[0] != "priority" {
		t.Fatalf("default target mapping missing: %+v", preview.TaskMappings)
	}
}

func TestTodo_PM_014_Property(t *testing.T) {
	current := workflowFixture()
	pending := workflowFixture()
	pending.Statuses = pending.Statuses[1:]
	pending.Columns = []Column{{ID: "work", Name: "Work", StatusIDs: []string{"doing"}}, {ID: "complete", Name: "Complete", StatusIDs: []string{"done"}}}
	pending.TaskTypes[0].InitialStatus = "doing"
	pending.Transitions = []Transition{{From: "doing", To: "done"}, {From: "done", To: "doing"}}
	pending.Statuses[1].AllowedNextStatusIDs = []string{"doing"}
	task := TaskSnapshot{ID: "t1", TypeID: "task", StatusID: "todo", Fields: map[string]json.RawMessage{"owner": json.RawMessage(`"p1"`)}}
	_, err := PreviewMigration(MigrationRequest{Current: current, Pending: pending, Tasks: []TaskSnapshot{task}})
	if !errors.Is(err, ErrUnsafeMigration) {
		t.Fatalf("removed status without mapping error=%v", err)
	}
	preview, err := PreviewMigration(MigrationRequest{Current: current, Pending: pending, Tasks: []TaskSnapshot{task}, StatusMappings: map[string]string{"todo": "doing"}})
	if err != nil {
		t.Fatalf("explicit destination mapping rejected: %v, issues=%+v", err, preview.Issues)
	}
	if len(preview.TaskMappings) != 1 || preview.TaskMappings[0].TargetStatusID != "doing" {
		t.Fatalf("task destination not reported: %+v", preview.TaskMappings)
	}
}

func TestTodo_PM_014_Golden(t *testing.T) {
	current := workflowFixture()
	pending := workflowFixture()
	pending.Fields[0].Type = FieldText
	pending.Fields[0].Validation = FieldValidation{MinLength: intPtr(2)}
	task := TaskSnapshot{ID: "t1", TypeID: "task", StatusID: "todo", Fields: map[string]json.RawMessage{"owner": json.RawMessage(`"person-1"`)}}
	preview, err := PreviewMigration(MigrationRequest{Current: current, Pending: pending, Tasks: []TaskSnapshot{task}})
	if !errors.Is(err, ErrUnsafeMigration) || preview.Safe {
		t.Fatalf("retyped field without mapping was not rejected: preview=%+v err=%v", preview, err)
	}
	if !containsMigrationIssue(preview.Issues, "FIELD_MAPPING_REQUIRED") {
		t.Fatalf("missing retyped-field issue: %+v", preview.Issues)
	}
}

func TestTodo_PM_014_InvalidValue(t *testing.T) {
	current := workflowFixture()
	pending := workflowFixture()
	pending.Fields[1].Validation.Options = []string{"request"}
	task := TaskSnapshot{ID: "t1", TypeID: "task", StatusID: "todo", Fields: map[string]json.RawMessage{"owner": json.RawMessage(`"person-1"`), "kind": json.RawMessage(`"follow-up"`)}}
	preview, err := PreviewMigration(MigrationRequest{Current: current, Pending: pending, Tasks: []TaskSnapshot{task}})
	if !errors.Is(err, ErrUnsafeMigration) || !containsMigrationIssue(preview.Issues, "INVALID_PENDING_VALUE") || len(preview.InvalidValues) != 1 || preview.InvalidValues[0].TaskID != "t1" {
		t.Fatalf("value incompatible with the pending enum was not reported: preview=%+v err=%v", preview, err)
	}
}

func TestTodo_PM_015(t *testing.T) {
	current := workflowFixture()
	pending := workflowFixture()
	pending.Statuses[1].Category = CategoryBlocked
	tasks := make([]TaskSnapshot, MaxAffectedTasksPerPublish+1)
	for i := range tasks {
		tasks[i] = TaskSnapshot{ID: fmt.Sprintf("task-%04d", i), TypeID: "task", StatusID: "doing", Fields: map[string]json.RawMessage{"owner": json.RawMessage(`"person"`)}}
	}
	preview, err := PreviewMigration(MigrationRequest{Current: current, Pending: pending, Tasks: tasks})
	if !errors.Is(err, ErrMigrationLimitExceeded) || preview.Safe || preview.TaskCount != MaxAffectedTasksPerPublish+1 || preview.AffectedTaskCount != MaxAffectedTasksPerPublish+1 {
		t.Fatalf("over-limit migration was accepted: count=%d safe=%t err=%v", preview.TaskCount, preview.Safe, err)
	}
}

func TestTodo_PM_015_Fault(t *testing.T) {
	current := workflowFixture()
	pending := workflowFixture()
	pending.TaskTypes[0].RequiredFields = append(pending.TaskTypes[0].RequiredFields, "new-field")
	pending.Fields = append(pending.Fields, Field{ID: "new-field", Name: "New field", Type: FieldText, Classification: "INTERNAL"})
	pending.TaskTypes[0].FieldIDs = append(pending.TaskTypes[0].FieldIDs, "new-field")
	task := TaskSnapshot{ID: "t1", TypeID: "task", StatusID: "todo", Fields: map[string]json.RawMessage{"owner": json.RawMessage(`"person"`)}}
	preview, err := PreviewMigration(MigrationRequest{Current: current, Pending: pending, Tasks: []TaskSnapshot{task}})
	if !errors.Is(err, ErrUnsafeMigration) || !containsMigrationIssue(preview.Issues, "NEW_REQUIRED_FIELD_UNSATISFIED") {
		t.Fatalf("required field without value/default was accepted: %+v, %v", preview, err)
	}
}

func TestTodo_PM_015_Recovery(t *testing.T) {
	current := workflowFixture()
	pending := workflowFixture()
	pending.TaskTypes[0].RequiredFields = append(pending.TaskTypes[0].RequiredFields, "priority")
	pending.Fields = append(pending.Fields, Field{ID: "priority", Name: "Priority", Type: FieldEnum, Classification: "INTERNAL", Default: json.RawMessage(`"normal"`), Validation: FieldValidation{Options: []string{"normal"}}})
	pending.TaskTypes[0].FieldIDs = append(pending.TaskTypes[0].FieldIDs, "priority")
	task := TaskSnapshot{ID: "t1", TypeID: "task", StatusID: "todo", Fields: map[string]json.RawMessage{"owner": json.RawMessage(`"person"`)}}
	preview, err := PreviewMigration(MigrationRequest{Current: current, Pending: pending, Tasks: []TaskSnapshot{task}})
	if err != nil || !preview.Safe || len(preview.TaskMappings) != 1 || preview.TaskMappings[0].DefaultsApplied[0] != "priority" {
		t.Fatalf("valid default did not repair the pending schema: %+v, %v", preview, err)
	}
}

func TestValidateTransitionUsesPublishedPinAndMergedLaneEdits(t *testing.T) {
	p, err := Publish(workflowFixture(), 4)
	if err != nil {
		t.Fatal(err)
	}
	input := TransitionInput{ExpectedConfigVersion: 4, TaskTypeID: "task", FromStatusID: "todo", ToStatusID: "doing", CurrentFields: map[string]json.RawMessage{"owner": json.RawMessage(`"person-1"`)}, FieldEdits: map[string]json.RawMessage{"summary": json.RawMessage(`"ready"`)}}
	if got := ValidateTransition(p, input); len(got) != 0 {
		t.Fatalf("valid transition errors=%+v", got)
	}
	input.ExpectedConfigVersion = 3
	input.ToStatusID = "done"
	input.CurrentFields = map[string]json.RawMessage{}
	input.FieldEdits = map[string]json.RawMessage{"owner": json.RawMessage(`""`)}
	got := ValidateTransition(p, input)
	if !errors.Is(got, ErrTransitionRejected) || !hasTransitionCode(got, "CONFIG_VERSION_CONFLICT") || !hasTransitionCode(got, "TRANSITION_NOT_ALLOWED") || !hasTransitionCode(got, "REQUIRED_FIELD_MISSING") {
		t.Fatalf("invalid transition evidence incomplete: %+v", got)
	}
}

func TestValidateTransitionRejectsWrongTypedEdit(t *testing.T) {
	p, err := Publish(workflowFixture(), 1)
	if err != nil {
		t.Fatal(err)
	}
	input := TransitionInput{ExpectedConfigVersion: 1, TaskTypeID: "task", FromStatusID: "todo", ToStatusID: "doing", FieldEdits: map[string]json.RawMessage{"summary": json.RawMessage(`9`)}}
	got := ValidateTransition(p, input)
	if !hasTransitionCode(got, "INVALID_FIELD_VALUE") {
		t.Fatalf("numeric edit accepted for text field: %+v", got)
	}
}

func containsMigrationIssue(issues MigrationIssues, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
func hasTransitionCode(errs TransitionErrors, code string) bool {
	for _, issue := range errs {
		if issue.Code == code {
			return true
		}
	}
	return false
}
func intPtr(value int) *int { return &value }

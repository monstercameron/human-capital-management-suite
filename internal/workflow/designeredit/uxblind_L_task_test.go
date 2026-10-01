package designeredit_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designeredit"
)

func TestTodo_UXBLIND_033(t *testing.T) {
	service, _, draft := refinementService(t)
	taskID := ""
	for _, node := range draft.Nodes {
		if node.StepType == string(workflow.StepTask) {
			taskID = node.ID
			break
		}
	}
	if taskID == "" {
		t.Fatal("promotion draft has no task step")
	}
	change, err := service.UpdateNodeParameters(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.UpdateNodeParametersRequest{
		DraftID: draft.DraftID, ExpectedRevision: draft.Revision, NodeID: taskID,
		Values: map[string]string{
			"task_assignee":        "role:manager",
			"task_instructions":    "Review the promotion evidence.",
			"task_due_period_days": "5",
		},
	})
	if err != nil {
		t.Fatalf("update task settings: %v", err)
	}
	node := draftNode(t, change.Draft, taskID)
	for id, want := range map[string]string{"task_assignee": "role:manager", "task_instructions": "Review the promotion evidence.", "task_due_period_days": "5"} {
		if got := parameterValue(node, id); got != want {
			t.Fatalf("task parameter %s = %q, want %q", id, got, want)
		}
	}
	_, err = service.UpdateNodeParameters(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.UpdateNodeParametersRequest{
		DraftID: draft.DraftID, ExpectedRevision: change.Draft.Revision, NodeID: taskID,
		Values: map[string]string{"task_assignee": ""},
	})
	if !errors.Is(err, designeredit.ErrInvalid) {
		t.Fatalf("empty assignee error = %v, want ErrInvalid", err)
	}
}

func TestTodo_UXBLIND_033_Browser(t *testing.T) {
	_, _, draft := refinementService(t)
	for _, node := range draft.Nodes {
		if node.StepType != string(workflow.StepTask) {
			continue
		}
		fields := map[string]designeredit.ParameterView{}
		for _, parameter := range node.Parameters {
			fields[parameter.ID] = parameter
		}
		for _, id := range []string{"task_assignee", "task_instructions", "task_due_period_days"} {
			if _, ok := fields[id]; !ok {
				t.Fatalf("task %s has no %s setting", node.ID, id)
			}
		}
		if !fields["task_assignee"].Required {
			t.Fatal("task assignee is not required")
		}
		return
	}
	t.Fatal("promotion draft has no task step")
}

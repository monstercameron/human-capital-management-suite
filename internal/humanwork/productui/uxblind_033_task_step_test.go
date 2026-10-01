package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_033(t *testing.T) {
	i18n := I18nProps{Locale: ResolveProductLocale("en-US")}
	task := WorkflowDraftNode{
		ID: "task-1", StepType: "TASK", Label: "Review request",
		Parameters: []WorkflowNodeParameter{
			{ID: "task_assignee", Kind: "TEXT", Value: "role:manager", Required: true, Maximum: 160},
			{ID: "task_instructions", Kind: "TEXT", Value: "Review the request", Maximum: 2000},
			{ID: "task_due_period_days", Kind: "INTEGER", Value: "5", Minimum: 1, Maximum: 365},
		},
		Outcomes: []WorkflowDraftOutcome{{RouteKey: "SUCCEEDED", TargetNodeIDs: []string{"end"}}},
	}
	markup, err := ui.RenderToString(ui.CreateElement(WorkflowStepInspector, WorkflowStepInspectorProps{
		I18nProps: i18n, Draft: WorkflowDraftView{Nodes: []WorkflowDraftNode{task}}, Node: task,
		OnUpdateNode: func(WorkflowNodeParameterChange) {},
	}))
	if err != nil {
		t.Fatalf("render Task inspector: %v", err)
	}
	for _, want := range []string{
		`Assignee (role or relationship)`, `name="task_assignee"`, `required`, `value="role:manager"`,
		`Instructions`, `name="task_instructions"`, `Review the request`,
		`Due period (days)`, `name="task_due_period_days"`, `type="number"`, `min="1"`, `max="365"`, `value="5"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("Task inspector missing %q:\n%s", want, markup)
		}
	}

	if workflowParameterChangeAllowed(task.Parameters[0], "", false) {
		t.Fatal("a required parameter on another step became clearable")
	}
	if !workflowParameterChangeAllowed(task.Parameters[0], "", true) {
		t.Fatal("clearing the Task assignee must reach the draft so the missing-assignee validation can appear")
	}
	if workflowParameterChangeAllowed(task.Parameters[0], "role:manager", true) {
		t.Fatal("an unchanged assignee should not submit a draft update")
	}

	problems := workflowPathProblems(buildWorkflowPath(WorkflowDraftView{
		StartNodeID: task.ID,
		Nodes: []WorkflowDraftNode{{
			ID: task.ID, StepType: task.StepType,
			Parameters: []WorkflowNodeParameter{{ID: "task_assignee", Required: true}},
			Outcomes:   []WorkflowDraftOutcome{{RouteKey: "SUCCEEDED", TargetNodeIDs: []string{"end"}}},
		}, {ID: "end", StepType: "END"}},
	}, ""), nil)
	missingAssignee := 0
	for _, problem := range problems {
		if problem.Key == "workflow_editor.problem_task_assignee" {
			missingAssignee++
		}
	}
	if missingAssignee != 1 {
		t.Fatalf("missing Task assignee problems = %+v, want one required-assignee issue", problems)
	}
}

func TestTodo_UXBLIND_033_Browser(t *testing.T) {
	i18n := I18nProps{Locale: ResolveProductLocale("en-US")}
	task := WorkflowDraftNode{ID: "task-1", StepType: "TASK", Label: "Review request", Parameters: []WorkflowNodeParameter{
		{ID: "task_assignee", Kind: "TEXT", Required: true, Maximum: 160},
		{ID: "task_instructions", Kind: "TEXT", Maximum: 2000},
		{ID: "task_due_period_days", Kind: "INTEGER", Minimum: 1, Maximum: 365},
	}}
	taskMarkup, err := ui.RenderToString(ui.CreateElement(WorkflowStepInspector, WorkflowStepInspectorProps{
		I18nProps: i18n, Node: task, OnUpdateNode: func(WorkflowNodeParameterChange) {},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(taskMarkup, `name="target_node"`) {
		t.Fatal("Task settings must not replace the existing outcome routing controls")
	}
	for _, id := range []string{"task_assignee", "task_instructions", "task_due_period_days"} {
		if !strings.Contains(taskMarkup, `name="`+id+`"`) {
			t.Errorf("Task settings do not expose %s", id)
		}
	}

	approval := WorkflowDraftNode{ID: "approval-1", StepType: "APPROVAL", Parameters: []WorkflowNodeParameter{{ID: "signal_timeout_seconds", Kind: "INTEGER", Value: "3600", Minimum: 0, Maximum: 31536000}}}
	approvalMarkup, err := ui.RenderToString(ui.CreateElement(WorkflowStepInspector, WorkflowStepInspectorProps{
		I18nProps: i18n, Node: approval, OnUpdateNode: func(WorkflowNodeParameterChange) {},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(approvalMarkup, `class="workflow-inspector-note"`) && strings.Contains(approvalMarkup, "Say who owns this work") {
		t.Fatal("Task-specific instructions leaked into Approval settings")
	}
	if !strings.Contains(approvalMarkup, `name="signal_timeout_seconds"`) {
		t.Fatal("Approval's existing settings were changed")
	}
}

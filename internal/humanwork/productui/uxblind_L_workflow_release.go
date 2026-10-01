package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// workflowReleasePanel makes the governed release path visible while an
// author is editing. The server remains the authority for tests, approvals,
// and activation; this panel only projects the next stage from the draft.
func workflowReleasePanel(i18n I18nProps, draft WorkflowDraftView, problems []workflowPathProblem, onCreate func(WorkflowDraftCreateRequest)) ui.Node {
	unsaved := strings.TrimSpace(draft.DraftID) == ""
	testState, testAction := "workflow_release.state_not_started", "workflow_release.action_save"
	if !unsaved {
		testAction = "workflow_release.action_test"
		if len(problems) == 0 {
			testState = "workflow_release.state_ready"
		} else {
			testState = "workflow_release.state_blocked"
			testAction = "workflow_editor.problems_title"
		}
	}
	steps := []struct{ key, label, state, action string }{
		{key: "test", label: i18n.Text("workflow_release.test"), state: i18n.Text(testState), action: i18n.Text(testAction)},
		{key: "approval", label: i18n.Text("workflow_release.approval"), state: i18n.Text("workflow_release.state_waiting"), action: i18n.Text("workflow_release.action_approval")},
		{key: "activation", label: i18n.Text("workflow_release.activation"), state: i18n.Text("workflow_release.state_waiting"), action: i18n.Text("workflow_release.action_activate")},
	}
	rows := make([]ui.Node, 0, len(steps))
	for _, step := range steps {
		rows = append(rows, html.Li(html.Props{Key: step.key, Class: "workflow-release-step", Data: map[string]string{"state": strings.ToLower(step.state)}},
			html.Strong(html.Props{Class: "workflow-release-step-label"}, ui.Text(step.label)),
			html.Span(html.Props{Class: "status-chip workflow-release-state"}, ui.Text(step.state)),
			html.Span(html.Props{Class: "workflow-release-action"}, ui.Text(step.action)),
		))
	}
	next := testAction
	if !unsaved && len(problems) == 0 {
		next = "workflow_release.action_test"
	}
	return html.Section(html.Props{Class: "workflow-release", Aria: map[string]string{"labelledby": "workflow-release-title"}},
		html.Div(html.Props{Class: "workflow-release-heading"},
			html.H3(html.Props{ID: "workflow-release-title"}, ui.Text(i18n.Text("workflow_release.title"))),
			html.P(html.Props{Class: "muted"}, ui.Text(i18n.Text("workflow_release.description"))),
		),
		html.Ol(html.Props{Class: "workflow-release-steps"}, rows...),
		html.P(html.Props{Class: "workflow-release-next", Raw: map[string]any{"role": "status"}}, ui.Text(i18n.Text("workflow_release.next", map[string]string{"action": i18n.Text(next)}))),
	)
}

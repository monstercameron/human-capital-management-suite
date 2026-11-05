package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// workflowReleaseActions are the release steps the host authorized for this
// viewer and draft. A nil action means the host cannot perform it, and the
// panel explains that instead of presenting a dead next step (UXBLIND-034):
// the server remains the authority for tests, approvals, and activation.
type workflowReleaseActions struct {
	save, test, submit, publish func()
}

type workflowReleaseStepProps struct {
	I18nProps
	StepKey string
	Label   string
	State   string
	Action  string
	// Guidance names real work the author can do in the editor (fixing the
	// draft's problems) rather than a host action. It renders as text.
	Guidance string
	OnAction func()
}

// workflowReleaseStep is one release row: its label and state always, and
// either the authorized control or an honest explanation of why the action
// is not available. The click handler is unconditional so hook order is
// stable; the nil check runs at press time.
func workflowReleaseStep(props workflowReleaseStepProps) ui.Node {
	click := ui.UseEvent(func(ui.MouseEvent) {
		if props.OnAction != nil {
			props.OnAction()
		}
	})
	var action ui.Node
	switch {
	case props.OnAction != nil:
		action = html.Button(html.Props{
			Class: "button primary compact workflow-release-action", Type: "button",
			Data: map[string]string{"release-step": props.StepKey}, OnClick: click,
		}, ui.Text(props.Action))
	case props.Guidance != "":
		action = html.Span(html.Props{Class: "workflow-release-action"}, ui.Text(props.Guidance))
	default:
		action = html.Span(html.Props{Class: "workflow-release-action workflow-release-unavailable"},
			ui.Text(props.I18nProps.Text("workflow_release.unavailable", map[string]string{"action": props.Action})))
	}
	return html.Li(html.Props{Key: props.StepKey, Class: "workflow-release-step", Data: map[string]string{"state": strings.ToLower(props.State)}},
		html.Strong(html.Props{Class: "workflow-release-step-label"}, ui.Text(props.Label)),
		html.Span(html.Props{Class: "status-chip workflow-release-state"}, ui.Text(props.State)),
		action,
	)
}

// workflowReleasePanel makes the governed release path visible while an
// author is editing. Steps and states always render; each step's action is a
// real control only when the host authorized it, and the next line names the
// next authorized action or explains why none is available.
func workflowReleasePanel(i18n I18nProps, draft WorkflowDraftView, problems []workflowPathProblem, release workflowReleaseActions) ui.Node {
	unsaved := strings.TrimSpace(draft.DraftID) == ""
	testState, testActionKey := "workflow_release.state_not_started", "workflow_release.action_save"
	var testCallback func()
	guidance := ""
	switch {
	case unsaved:
		testCallback = release.save
	case len(problems) > 0:
		testState, guidance = "workflow_release.state_blocked", i18n.Text("workflow_editor.problems_title")
	default:
		testState, testActionKey, testCallback = "workflow_release.state_ready", "workflow_release.action_test", release.test
	}
	next := nextReleaseText(i18n, unsaved, len(problems) > 0, testActionKey, testCallback)
	steps := []workflowReleaseStepProps{
		{I18nProps: i18n, StepKey: "test", Label: i18n.Text("workflow_release.test"), State: i18n.Text(testState), Action: i18n.Text(testActionKey), Guidance: guidance, OnAction: testCallback},
		{I18nProps: i18n, StepKey: "approval", Label: i18n.Text("workflow_release.approval"), State: i18n.Text("workflow_release.state_waiting"), Action: i18n.Text("workflow_release.action_approval"), OnAction: release.submit},
		{I18nProps: i18n, StepKey: "activation", Label: i18n.Text("workflow_release.activation"), State: i18n.Text("workflow_release.state_waiting"), Action: i18n.Text("workflow_release.action_activate"), OnAction: release.publish},
	}
	rows := make([]ui.Node, 0, len(steps))
	for _, step := range steps {
		rows = append(rows, ui.CreateElement(workflowReleaseStep, step))
	}
	return html.Section(html.Props{Class: "workflow-release", Aria: map[string]string{"labelledby": "workflow-release-title"}},
		html.Div(html.Props{Class: "workflow-release-heading"},
			html.H3(html.Props{ID: "workflow-release-title"}, ui.Text(i18n.Text("workflow_release.title"))),
			html.P(html.Props{Class: "muted"}, ui.Text(i18n.Text("workflow_release.description"))),
		),
		html.Ol(html.Props{Class: "workflow-release-steps"}, rows...),
		html.P(html.Props{Class: "workflow-release-next", Raw: map[string]any{"role": "status"}}, ui.Text(next)),
	)
}

// nextReleaseText names the next authorized release action, keeps the
// problems guidance while the draft is blocked, and otherwise explains why
// the expected next action is not available rather than naming a dead step.
func nextReleaseText(i18n I18nProps, unsaved, blocked bool, testActionKey string, testCallback func()) string {
	switch {
	case unsaved && testCallback != nil:
		return i18n.Text("workflow_release.next", map[string]string{"action": i18n.Text(testActionKey)})
	case unsaved:
		return i18n.Text("workflow_release.unavailable", map[string]string{"action": i18n.Text(testActionKey)})
	case blocked:
		return i18n.Text("workflow_release.next", map[string]string{"action": i18n.Text("workflow_editor.problems_title")})
	case testCallback != nil:
		return i18n.Text("workflow_release.next", map[string]string{"action": i18n.Text(testActionKey)})
	default:
		return i18n.Text("workflow_release.unavailable", map[string]string{"action": i18n.Text(testActionKey)})
	}
}

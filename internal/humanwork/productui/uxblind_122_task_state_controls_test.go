package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func renderTaskStateControls(t *testing.T, locale string, task AgentTask) string {
	t.Helper()
	view := ApplyLocale(NewView(PageAgents, "tenant-1", "principal-1", "scope-1"), ResolveProductLocale(locale))
	markup, err := ui.RenderToString(tagAgentTaskView(view, view.Locale, task))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func TestTodo_UXBLIND_122_TaskControlsFollowTerminalState(t *testing.T) {
	cases := []struct {
		name       string
		state      AgentTaskState
		wantAction bool
	}{
		{name: "running", state: AgentTaskRunning, wantAction: true},
		{name: "awaiting approval", state: AgentTaskAwaitingApproval, wantAction: true},
		{name: "awaiting input", state: AgentTaskAwaitingInput, wantAction: true},
		{name: "awaiting plan confirmation", state: AgentTaskAwaitingPlanConfirmation, wantAction: true},
		{name: "paused", state: AgentTaskPaused, wantAction: true},
		{name: "completed", state: AgentTaskCompleted},
		{name: "failed", state: AgentTaskFailed},
		{name: "cancelled", state: AgentTaskCancelled},
		{name: "expired", state: AgentTaskExpired},
		{name: "unknown", state: AgentTaskState("future-state")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			markup := renderTaskStateControls(t, "en-US", AgentTask{ID: "task-1", Version: 1, Title: "Review", Goal: "Review the request", State: tc.state, Actions: AgentTaskActionPolicy{ConfirmPlan: true, Pause: true, Resume: true, Cancel: true}})
			got := strings.Contains(markup, `class="agents-task-actions"`)
			if got != tc.wantAction {
				t.Fatalf("action group present = %v, want %v: %s", got, tc.wantAction, markup)
			}
			if !tc.wantAction && strings.Contains(markup, `data-task-action=`) {
				t.Fatalf("terminal or unknown task exposed controls: %s", markup)
			}
			if tc.name == "unknown" && !strings.Contains(markup, ResolveProductLocale("en-US").Text("agents.state.unknown")) {
				t.Fatalf("unknown task did not render the unavailable status: %s", markup)
			}
		})
	}
}

func TestTodo_UXBLIND_122_TaskDetailsHideEmptySections(t *testing.T) {
	markup := renderTaskStateControls(t, "en-US", AgentTask{ID: "task-empty", Title: "Review", Goal: "Review the request", State: AgentTaskCompleted})
	for _, class := range []string{"agents-task-live", "agents-task-detail", "agents-plan-diff"} {
		if strings.Contains(markup, `class="`+class+`"`) {
			t.Fatalf("empty %s section rendered: %s", class, markup)
		}
	}
	if strings.Contains(markup, "Live step") || strings.Contains(markup, "Checkpoints") || strings.Contains(markup, "Artifacts") || strings.Contains(markup, "Pending approvals") || strings.Contains(markup, "Submitted intents") || strings.Contains(markup, "Plan revision") {
		t.Fatalf("empty detail headings rendered: %s", markup)
	}

	populated := AgentTask{
		ID: "task-full", Version: 1, Title: "Review", Goal: "Review the request", State: AgentTaskRunning, Actions: AgentTaskActionPolicy{Pause: true, Cancel: true},
		LiveStep: "Read the request", BudgetUsed: "2", BudgetLimit: "10", PlanDiff: "+ keep scope",
		Checkpoints:      []AgentCheckpoint{{Label: "Checked", At: "09:00"}},
		Artifacts:        []AgentArtifact{{Name: "Report", Kind: "document", Href: "/docs/report"}},
		Approvals:        []AgentApproval{{ID: "approval-1", Summary: "Approve report"}},
		SubmittedIntents: []AgentIntentStatus{{Name: "people.review/v1", Status: "draft"}},
	}
	markup = renderTaskStateControls(t, "en-US", populated)
	for _, want := range []string{"Live step", "Budget used: 2 of 10", "Checkpoints", "Artifacts", "Pending approvals", "Submitted intents", "Plan revision", "+ keep scope"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("populated detail missing %q: %s", want, markup)
		}
	}
}

func TestTodo_UXBLIND_122_TaskControlStatusLocalizedAndLive(t *testing.T) {
	keys := []string{"agents.control_working", "agents.control_done", "agents.control_conflict", "agents.control_denied", "agents.control_disabled", "agents.control_failed"}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup := renderTaskStateControls(t, locale, AgentTask{ID: "task-status", Title: "Review", Goal: "Review", State: AgentTaskCompleted})
		if !strings.Contains(markup, `id="agents-task-control-status"`) || !strings.Contains(markup, `role="status"`) || !strings.Contains(markup, `aria-live="polite"`) {
			t.Fatalf("%s task control status is not an accessible live region: %s", locale, markup)
		}
		for _, key := range keys {
			text := ResolveProductLocale(locale).Text(key)
			if text == "" || text == key || strings.Contains(text, "⟦") || !strings.Contains(markup, `data-msg-`+strings.TrimPrefix(key, "agents.control_")+`="`+text+`"`) {
				t.Fatalf("%s missing localized status %s=%q: %s", locale, key, text, markup)
			}
		}
	}
}

func TestTodo_UXBLIND_122_TaskStateControlsLocalizedAndAccessible(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		label := ResolveProductLocale(locale).Text("agents.task_view")
		for _, state := range []AgentTaskState{AgentTaskCompleted, AgentTaskFailed, AgentTaskCancelled, AgentTaskExpired} {
			markup := renderTaskStateControls(t, locale, AgentTask{ID: "task-1", Title: "Review", Goal: "Review the request", State: state})
			if strings.Contains(markup, "⟦") || strings.Contains(markup, "agents.state.") || strings.Contains(markup, `class="agents-task-actions"`) || strings.Contains(markup, `data-task-action=`) {
				t.Fatalf("%s %s terminal task has unresolved copy or controls: %s", locale, state, markup)
			}
			if !strings.Contains(markup, `aria-label="`+label+`"`) || !strings.Contains(markup, `aria-labelledby="agents-task-title"`) {
				t.Fatalf("%s task view lost accessibility labels: %s", locale, markup)
			}
			if !strings.Contains(markup, ResolveProductLocale(locale).Text("agents.state."+string(state))) {
				t.Fatalf("%s terminal state label missing for %s: %s", locale, state, markup)
			}
		}
	}
	markup := renderTaskStateControls(t, "ar", AgentTask{ID: "task-1", Version: 1, Title: "Review", Goal: "Review the request", State: AgentTaskPaused, Actions: AgentTaskActionPolicy{Resume: true}})
	if !strings.Contains(markup, `aria-label="`+ResolveProductLocale("ar").Text("agents.resume")+`"`) || !strings.Contains(markup, `data-task-action="resume"`) {
		t.Fatalf("paused task did not expose the existing resume control: %s", markup)
	}
}

func TestTodo_UXBLIND_122_TaskPlanUsesReadableLocalizedLabels(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup := renderTaskStateControls(t, locale, AgentTask{
			ID: "task-plan", Title: "Review", Goal: "Review the request", State: AgentTaskRunning,
			Steps: []AgentTaskStep{
				{Name: "agent.read_own_worker_state", State: "RUNNING", Tier: "T0", Detail: "Inspect the current state"},
				{Name: "agent.summarize_request", State: "AWAITING_APPROVAL", Tier: "T1", Detail: "Prepare a private summary"},
			},
		})
		for _, want := range []string{
			"Read own worker state", "Summarize request",
			ResolveProductLocale(locale).Text("agents.step_state.running"),
			ResolveProductLocale(locale).Text("agents.step_state.awaiting_approval"),
		} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s plan missing readable localized label %q: %s", locale, want, markup)
			}
		}
		// AGENTUX-017: the step name already says what happened, so the tier
		// caption is not repeated under it.
		for _, raw := range []string{"agent.read_own_worker_state", "agent.summarize_request", ">T0<", ">T1<", ">RUNNING<", ">AWAITING_APPROVAL<", ">" + ResolveProductLocale(locale).Text("agents.tier.read") + "<", ">" + ResolveProductLocale(locale).Text("agents.tier.private_draft") + "<"} {
			if strings.Contains(markup, raw) {
				t.Fatalf("%s plan leaked internal value %q: %s", locale, raw, markup)
			}
		}
	}
}

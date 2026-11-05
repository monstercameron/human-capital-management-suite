package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// agent2017Render renders the Agents route the way the served page does: the
// server's owner-scoped snapshot, then the address selector, then the surface.
func agent2017Render(t *testing.T, snapshot AgentSnapshot, taskID, locale string) string {
	t.Helper()
	view := NewView(PageAgents, "tenant-1", "principal-1", "")
	view = ApplyAgentsAvailability(view, AgentsAvailabilityProjection{Enabled: true, Snapshot: snapshot})
	view = ApplyLocale(ApplyRequest(view, PageRequest{Page: PageAgents, AgentTaskID: taskID}), ResolveProductLocale(locale))
	markup, err := ui.RenderToString(BuildAgentsSurface(view))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func agent2017Fixture() AgentSnapshot {
	snapshot := agentsFixture()
	snapshot.SelectedTask = nil
	return snapshot
}

// TestTodo_AGENT2_017_DeepLinkOpensDetail proves a direct deep link opens the
// owner's task detail: the address alone selects the task, and the detail
// carries the goal, the plan, the approvals and the one-click controls.
func TestTodo_AGENT2_017_DeepLinkOpensDetail(t *testing.T) {
	snapshot := agent2017Fixture()
	snapshot.Tasks = []AgentTask{*agentsFixture().SelectedTask, snapshot.Tasks[1]}
	markup := agent2017Render(t, snapshot, "task-approval", "en-US")
	for _, want := range []string{
		`data-task-view="task-approval"`, "agents-task-detail-pane",
		"Review eligible team members and prepare drafts for your approval.",
		"What the agent did", "Live step", "Budget used: 12 of 40",
		"Population verified", "Promotion draft report",
		`href="/workspace/app/docs?document=draft-report"`,
		"sha256:abc123", "Calibration document", "derived-tainted",
		"Plan revision 3", "people.promote_worker/v1",
		`data-task-action="pause"`, `data-task-action="cancel"`,
		`data-task-id="task-approval"`, `data-task-version="1"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("deep link to task-approval is missing %q", want)
		}
	}
}

// TestTodo_AGENT2_017_DeepLinkUnknownRevealsNothing proves an unknown or denied
// id opens no detail and leaks no task data: the list stays, the detail pane
// is gone, and nothing of the named task is rendered.
func TestTodo_AGENT2_017_DeepLinkUnknownRevealsNothing(t *testing.T) {
	for _, taskID := range []string{"task-missing", "task-theirs", "", "  ", "null"} {
		markup := agent2017Render(t, agent2017Fixture(), taskID, "en-US")
		for _, forbidden := range []string{`data-task-view=`, "agents-task-detail-pane", `data-has-task-detail="true"`, "What the agent did", "sha256:abc123"} {
			if strings.Contains(markup, forbidden) {
				t.Errorf("task %q rendered %q", taskID, forbidden)
			}
		}
		if !strings.Contains(markup, "Prepare promotion drafts") || !strings.Contains(markup, "Collect missing evidence") {
			t.Errorf("task %q did not fall back to the task list", taskID)
		}
	}
}

// TestTodo_AGENT2_017_ExtendBudgetControl proves a task paused at its budget
// ceiling offers extend-budget as a one-click control bound to the task,
// next to resume.
func TestTodo_AGENT2_017_ExtendBudgetControl(t *testing.T) {
	snapshot := agent2017Fixture()
	selected := *agentsFixture().SelectedTask
	selected.State = AgentTaskPaused
	selected.Actions = AgentTaskActionPolicy{Resume: true, ExtendBudget: true}
	snapshot.Tasks = []AgentTask{selected}
	markup := agent2017Render(t, snapshot, selected.ID, "en-US")
	for _, want := range []string{
		`data-task-action="extend-budget"`, `data-task-action="resume"`,
		`data-task-id="task-approval"`, `data-task-version="1"`, `type="button"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("paused task is missing %q: %s", want, markup)
		}
	}
	running := agent2017Render(t, agent2017Fixture(), "task-approval", "en-US")
	if strings.Contains(running, `data-task-action="extend-budget"`) {
		t.Errorf("a task that is not paused at its ceiling offered extend-budget")
	}
}

// TestTodo_AGENT2_017_PlanDiffConfirm proves a plan revision shows the diff to
// confirm: what changed, in words, before the control that starts the plan.
func TestTodo_AGENT2_017_PlanDiffConfirm(t *testing.T) {
	snapshot := agent2017Fixture()
	selected := *agentsFixture().SelectedTask
	selected.State = AgentTaskAwaitingPlanConfirmation
	selected.Actions = AgentTaskActionPolicy{ConfirmPlan: true, Cancel: true}
	selected.PlanChanges = []AgentPlanChange{
		{Change: AgentPlanChangeAdded, Step: AgentTaskStep{Name: "agent.read_handbook", Tier: "T0"}},
		{Change: AgentPlanChangeRemoved, Step: AgentTaskStep{Name: "skill.lookup", Tier: "T0"}},
	}
	snapshot.Tasks = []AgentTask{selected}
	markup := agent2017Render(t, snapshot, selected.ID, "en-US")
	for _, want := range []string{
		"Plan revision 3", "What changed in this plan",
		"Added: Read handbook", "Removed: skill.lookup",
		"- read all workers", "+ read direct reports only",
		`data-task-action="confirm-plan"`, `data-plan-change="added"`, `data-plan-change="removed"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("revision view is missing %q", want)
		}
	}
	if changes, confirm := strings.Index(markup, "What changed in this plan"), strings.Index(markup, `data-task-action="confirm-plan"`); changes < 0 || confirm < 0 || changes > confirm {
		t.Errorf("the changes are not shown before the control that confirms them")
	}
}

// TestTodo_AGENT2_017_IntentStatuses proves every owner status code for a
// submitted intent renders as a sentence, never as the code.
func TestTodo_AGENT2_017_IntentStatuses(t *testing.T) {
	snapshot := agent2017Fixture()
	selected := *agentsFixture().SelectedTask
	selected.SubmittedIntents = []AgentIntentStatus{
		{Name: "intent.draft", Status: AgentIntentDraft},
		{Name: "intent.awaiting", Status: AgentIntentAwaitingApproval},
		{Name: "intent.executing", Status: AgentIntentExecuting},
		{Name: "intent.observed", Status: AgentIntentObserved},
		{Name: "intent.repair", Status: AgentIntentNeedsRepair},
		{Name: "intent.failed", Status: AgentIntentFailed},
	}
	snapshot.Tasks = []AgentTask{selected}
	markup := agent2017Render(t, snapshot, selected.ID, "en-US")
	for _, want := range []string{
		"Draft, not sent", "Waiting for your approval", "Being carried out",
		"Done and checked", "Needs review before it continues", "Could not be completed",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("submitted intents do not say %q", want)
		}
	}
	for _, code := range []string{">awaiting_approval<", ">needs_repair<", `data-intent-status=""`} {
		if strings.Contains(markup, code) {
			t.Errorf("submitted intents show the code %q", code)
		}
	}
}

// TestTodo_AGENT2_017_StepStateAndTier proves each plan step carries its state
// as a sentence and its tier on the step, without internal tier captions.
func TestTodo_AGENT2_017_StepStateAndTier(t *testing.T) {
	snapshot := agent2017Fixture()
	selected := *agentsFixture().SelectedTask
	snapshot.Tasks = []AgentTask{selected, snapshot.Tasks[1]}
	markup := agent2017Render(t, snapshot, selected.ID, "en-US")
	for _, want := range []string{`data-step-state="observed"`, `data-step-tier="T0"`, `data-step-tier="T2"`, "Owner-scoped read", "No intent submitted"} {
		if !strings.Contains(markup, want) {
			t.Errorf("plan steps are missing %q", want)
		}
	}
	if strings.Contains(markup, ResolveProductLocale("en-US").Text("agents.tier.communicate")) {
		t.Fatalf("task steps exposed internal tier captions: %s", markup)
	}
}

// TestTodo_AGENT2_017_DetailIsolation proves the open detail shows only its own
// task: another owned task's approval digest never renders inside it.
func TestTodo_AGENT2_017_DetailIsolation(t *testing.T) {
	snapshot := agent2017Fixture()
	selected := *agentsFixture().SelectedTask
	other := selected
	other.ID, other.Title = "task-other", "Another owned task"
	other.Approvals = []AgentApproval{{ID: "other-approval", Digest: "sha256:other-only", Summary: "other.intent", Sources: []string{"request"}}}
	snapshot.Tasks = []AgentTask{selected, other}
	markup := agent2017Render(t, snapshot, selected.ID, "en-US")
	if !strings.Contains(markup, "sha256:abc123") {
		t.Fatalf("the open detail lost its own approval digest")
	}
	if strings.Contains(markup, "sha256:other-only") {
		t.Fatalf("the open detail shows another task's approval digest")
	}
	if !strings.Contains(markup, "Another owned task") {
		t.Fatalf("the list lost the other owned task")
	}
}

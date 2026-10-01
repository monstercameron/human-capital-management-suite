package productui

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type agentSnapshotFixture struct {
	snapshot AgentSnapshot
	err      error
}

func (f agentSnapshotFixture) Snapshot(context.Context, AgentSnapshotRequest) (AgentSnapshot, error) {
	return f.snapshot, f.err
}

func agentsFixture() AgentSnapshot {
	return AgentSnapshot{
		Availability: AgentsAvailable,
		Agents:       []AgentSummary{{ID: "coach", Name: "People Coach", Description: "Helps prepare governed people work.", Status: "Ready", Skills: []string{"policy lookup", "drafting"}}},
		Threads:      []AgentThread{{ID: "thread-1", AgentID: "coach", Title: "Promotion preparation", Posts: []AgentPost{{ID: "post-1", Body: "I can prepare a reviewable draft for your team.", Skills: []string{"policy lookup", "drafting"}, ForUser: true}}}},
		Tasks: []AgentTask{
			{ID: "task-approval", Title: "Prepare promotion drafts", Goal: "Review eligible team members", State: AgentTaskAwaitingApproval},
			{ID: "task-input", Title: "Collect missing evidence", Goal: "Ask for the missing document", State: AgentTaskAwaitingInput},
		},
		SelectedTask: &AgentTask{
			ID: "task-approval", Version: 1, AgentID: "coach", Title: "Prepare promotion drafts", Goal: "Review eligible team members and prepare drafts for your approval.", State: AgentTaskAwaitingApproval,
			Actions:  AgentTaskActionPolicy{Pause: true, Cancel: true},
			LiveStep: "Validate the authorized team population", BudgetUsed: "12", BudgetLimit: "40", PlanRevision: "3", PlanDiff: "- read all workers\n+ read direct reports only",
			Steps:            []AgentTaskStep{{Name: "Read authorized team", State: "observed", Tier: "T0", Detail: "Owner-scoped read"}, {Name: "Prepare drafts", State: "awaiting approval", Tier: "T2", Detail: "No intent submitted"}},
			Checkpoints:      []AgentCheckpoint{{Label: "Population verified", At: "09:14"}},
			Artifacts:        []AgentArtifact{{Name: "Promotion draft report", Kind: "Report", Href: "/workspace/app/docs?document=draft-report"}},
			Approvals:        []AgentApproval{{ID: "approval-1", Digest: "sha256:abc123", Sources: []string{"Calibration document"}, Taint: "derived-tainted", Summary: "Approve two promotion drafts"}},
			SubmittedIntents: []AgentIntentStatus{{Name: "people.promote_worker/v1", Status: "draft"}},
		},
	}
}

func pausedAgentsFixture() AgentSnapshot {
	snapshot := agentsFixture()
	selected := *snapshot.SelectedTask
	selected.State = AgentTaskPaused
	selected.Actions = AgentTaskActionPolicy{Resume: true}
	snapshot.SelectedTask = &selected
	return snapshot
}

func TestTodo_AGENT2_016(t *testing.T) {
	view := ApplyLocale(NewView(PageAgents, "tenant-1", "principal-1", "scope-1"), ResolveProductLocale("en-US"))
	markup, err := ui.RenderToString(BuildAgentsPage(view, agentSnapshotFixture{snapshot: agentsFixture()}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Your agents", "People Coach", "Acting for you", "Skills used", "Tasks", "Awaiting approval", "Awaiting input", "Quick answer", "Start long task", "agents-composer-input", "task=task-approval",
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("agents page missing %q: %s", want, markup)
		}
	}
	if got := strings.Count(markup, "<h1"); got != 1 || strings.Contains(markup, "<main") {
		t.Fatalf("agents page must have one h1 and no nested main, h1=%d: %s", got, markup)
	}
	unavailable, err := ui.RenderToString(BuildAgentsPage(view, nil))
	if err != nil || !strings.Contains(unavailable, "Agents are not available yet") || strings.Contains(unavailable, "People Coach") {
		t.Fatalf("unavailable agent client did not produce a truthful empty state: %s", unavailable)
	}
	navigation := navigationFor(view.Locale, nil)
	found := false
	for _, item := range navigation {
		if item.Page != PageChat {
			continue
		}
		for _, child := range item.Children {
			if child.Page == PageAgents && child.LabelKey == "page.agents.label" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("Chat navigation does not expose the admitted Agents child page")
	}
}

func TestTodo_AGENT2_016_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := ApplyLocale(NewView(PageAgents, "tenant-1", "principal-1", "scope-1"), ResolveProductLocale(locale))
		markup, err := ui.RenderToString(BuildAgentsPage(view, agentSnapshotFixture{snapshot: agentsFixture()}))
		if err != nil {
			t.Fatalf("render %s: %v", locale, err)
		}
		if strings.Contains(markup, "⟦") || !strings.Contains(markup, "data-task-view=\"task-approval\"") {
			t.Fatalf("%s agents page has unresolved copy or no task view: %s", locale, markup)
		}
	}
}

func TestTodo_AGENT2_016_Accessibility(t *testing.T) {
	view := ApplyLocale(NewView(PageAgents, "tenant-1", "principal-1", "scope-1"), ResolveProductLocale("en-US"))
	markup, err := ui.RenderToString(BuildAgentsPage(view, agentSnapshotFixture{snapshot: agentsFixture()}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="agents-page"`, `aria-labelledby="agents-page-title"`, `aria-label="Agent conversations"`, `aria-label="Start work with an agent"`, `aria-describedby="agents-composer-help"`, `type="button"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("accessibility contract missing %q: %s", want, markup)
		}
	}
}

func TestTodo_AGENT2_016_I18n(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := ApplyLocale(NewView(PageAgents, "tenant-1", "principal-1", "scope-1"), ResolveProductLocale(locale))
		markup, err := ui.RenderToString(BuildAgentsPage(view, agentSnapshotFixture{snapshot: agentsFixture()}))
		if err != nil {
			t.Fatalf("render %s: %v", locale, err)
		}
		if strings.Contains(markup, "⟦") || strings.Contains(markup, "page.agents.") || strings.Contains(markup, "agents.state.") {
			t.Fatalf("%s agents page has unresolved catalog keys: %s", locale, markup)
		}
	}
}

func TestTodo_AGENT2_017_SelectedTaskBeforeHistory(t *testing.T) {
	view := ApplyLocale(NewView(PageAgents, "tenant-1", "principal-1", "scope-1"), ResolveProductLocale("en-US"))
	snapshot := agentsFixture()
	snapshot.SelectedTask.AnswerText = "Live answer appears here."
	markup, err := ui.RenderToString(BuildAgentsPage(view, agentSnapshotFixture{snapshot: snapshot}))
	if err != nil {
		t.Fatal(err)
	}
	answer, composer, history := strings.Index(markup, "Live answer appears here."), strings.Index(markup, `id="agents-composer-input"`), strings.Index(markup, `class="agents-tasks"`)
	if strings.Count(markup, `id="agents-composer-input"`) != 1 {
		t.Fatal("agent composer must appear exactly once")
	}
	if answer < 0 || composer < 0 || history < 0 || answer >= composer || composer >= history {
		t.Fatalf("selected answer, composer, history order=%d,%d,%d", answer, composer, history)
	}
}

func TestTodo_AGENT2_017(t *testing.T) {
	view := ApplyLocale(NewView(PageAgents, "tenant-1", "principal-1", "scope-1"), ResolveProductLocale("en-US"))
	markup, err := ui.RenderToString(BuildAgentsPage(view, agentSnapshotFixture{snapshot: agentsFixture()}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Confirmed plan", "Owner-scoped read", ResolveProductLocale("en-US").Text("agents.tier.communicate"), "Live step", "Budget used: 12 of 40", "Population verified", "Promotion draft report", "sha256:abc123", "Calibration document", "derived-tainted", "Plan revision 3", "Pause", "Cancel", "Extend budget", "people.promote_worker/v1", "draft",
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("task view missing %q: %s", want, markup)
		}
	}
	pausedView := ApplyLocale(NewView(PageAgents, "tenant-1", "principal-1", "scope-1"), ResolveProductLocale("en-US"))
	pausedMarkup, err := ui.RenderToString(BuildAgentsPage(pausedView, agentSnapshotFixture{snapshot: pausedAgentsFixture()}))
	if err != nil || !strings.Contains(pausedMarkup, `data-task-action="resume"`) {
		t.Fatalf("paused task did not expose resume: %s", pausedMarkup)
	}
}

func TestTodo_AGENT2_017_Browser(t *testing.T) {
	view := ApplyLocale(NewView(PageAgents, "tenant-1", "principal-1", "scope-1"), ResolveProductLocale("en-US"))
	markup, err := ui.RenderToString(BuildAgentsPage(view, agentSnapshotFixture{snapshot: agentsFixture()}))
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"pause", "cancel", "extend-budget"} {
		if !strings.Contains(markup, `data-task-action="`+action+`"`) {
			t.Fatalf("task action %q is not rendered as a one-click control: %s", action, markup)
		}
	}
	pausedMarkup, err := ui.RenderToString(BuildAgentsPage(view, agentSnapshotFixture{snapshot: pausedAgentsFixture()}))
	if err != nil || !strings.Contains(pausedMarkup, `data-task-action="resume"`) {
		t.Fatalf("paused task did not expose resume: %s", pausedMarkup)
	}
	if !strings.Contains(markup, `data-task-id="task-approval"`) {
		t.Fatalf("task actions are not bound to the selected task: %s", markup)
	}
}

func TestTodo_AGENT2_017_ControlsFailClosedWithoutAuthority(t *testing.T) {
	fixture := agentsFixture()
	fixture.SelectedTask.Version = 0
	fixture.SelectedTask.Actions = AgentTaskActionPolicy{Pause: true, Cancel: true, Resume: true, ConfirmPlan: true}
	view := ApplyLocale(NewView(PageAgents, "tenant-1", "principal-1", "scope-1"), ResolveProductLocale("en-US"))
	markup, err := ui.RenderToString(BuildAgentsPage(view, agentSnapshotFixture{snapshot: fixture}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, `data-task-action=`) {
		t.Fatalf("zero-version task exposed controls: %s", markup)
	}
}

func TestTodo_AGENT2_017_Accessibility(t *testing.T) {
	view := ApplyLocale(NewView(PageAgents, "tenant-1", "principal-1", "scope-1"), ResolveProductLocale("en-US"))
	markup, err := ui.RenderToString(BuildAgentsPage(view, agentSnapshotFixture{snapshot: agentsFixture()}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`aria-label="Task details"`, `aria-labelledby="agents-task-title"`, `<ol`, `<pre`, `data-detail="agents.approvals"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("task accessibility contract missing %q: %s", want, markup)
		}
	}
}

func TestTodo_AGENT2_017_Security(t *testing.T) {
	view := ApplyLocale(NewView(PageAgents, "tenant-1", "principal-1", "scope-1"), ResolveProductLocale("en-US"))
	markup, err := ui.RenderToString(BuildAgentsPage(view, agentSnapshotFixture{snapshot: agentsFixture()}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "other-tenant") || strings.Contains(markup, "salary") {
		t.Fatalf("task view leaked an out-of-scope field: %s", markup)
	}
	if !strings.Contains(markup, "derived-tainted") || !strings.Contains(markup, "Calibration document") {
		t.Fatalf("approval provenance and taint labels are not visible: %s", markup)
	}
}

// TestTodo_UXBLIND_122_ComposerStatus pins the composer's status region: the
// browser client writes into it and reads its localized messages from data
// attributes, so each locale must render a real translation of every message.
func TestTodo_UXBLIND_122_ComposerStatus(t *testing.T) {
	want := map[string]string{
		"en-US": "The task could not be started. Try again.",
		"de-DE": "Die Aufgabe konnte nicht gestartet werden. Versuchen Sie es erneut.",
		"ar":    "تعذر بدء المهمة. حاول مرة أخرى.",
	}
	seen := map[string]string{}
	for locale, failed := range want {
		view := ApplyLocale(NewView(PageAgents, "tenant-1", "principal-1", "scope-1"), ResolveProductLocale(locale))
		markup, err := ui.RenderToString(BuildAgentsPage(view, agentSnapshotFixture{snapshot: agentsFixture()}))
		if err != nil {
			t.Fatal(err)
		}
		for _, attr := range []string{"data-msg-working", "data-msg-done", "data-msg-empty", "data-msg-disabled", "data-msg-denied", "data-msg-failed"} {
			if !strings.Contains(markup, attr+`="`) {
				t.Fatalf("%s composer status lacks %s: %s", locale, attr, markup)
			}
		}
		if !strings.Contains(markup, `id="agents-composer-status"`) || !strings.Contains(markup, `role="status"`) || !strings.Contains(markup, `data-msg-failed="`+failed+`"`) {
			t.Fatalf("%s composer status = %s", locale, markup)
		}
		seen[locale] = failed
	}
	if seen["en-US"] == seen["de-DE"] || seen["en-US"] == seen["ar"] {
		t.Fatal("a locale fell back to English copy")
	}
}

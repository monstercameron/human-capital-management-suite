package productui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func renderAgentUXPage5(t *testing.T, localeID string, snapshot AgentSnapshot, admin bool) string {
	t.Helper()
	locale := ResolveProductLocale(localeID)
	view := ApplyLocale(NewView(PageAgents, "tenant", "viewer", ""), locale)
	view.AgentsProjection = &AgentsAvailabilityProjection{Enabled: true, ViewerIsAdmin: admin, Snapshot: snapshot}
	markup, err := ui.RenderToString(renderAgentsPage(view, locale, snapshot))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func TestAgentUXPage5_ComposerHierarchyAndCopy(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		markup := renderAgentUXPage5(t, localeID, AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, DocumentHubAvailable: true}, true)
		for _, want := range []string{
			locale.Text("agents.page_title"), locale.Text("agents.composer_placeholder"), agentUXR7Text(locale, "other_agents"),
			locale.Text("agents.documents_label"), locale.Text("agents.documents_access_help"), locale.Text("agents.chat_history_note"),
			locale.Text("agents.nav.setup"), locale.Text("agents.nav.operations"), `rows="3"`, `data-agent-document-count="true" hidden`,
		} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s Agents composer missing %q: %s", localeID, want, markup)
			}
		}
		if help, actions := strings.Index(markup, locale.Text("agents.actions_help")), strings.Index(markup, `data-agent-action="quick-answer"`); help < 0 || actions < 0 || help > actions {
			t.Fatalf("%s action explanation is not before the controls: %s", localeID, markup)
		}
	}
	withAgent := renderAgentUXPage5(t, "ar", AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, Agents: []AgentSummary{{ID: "policy", Name: "Policy Helper", Description: "Answers policy questions."}}}, false)
	for _, want := range []string{`dir="auto">Policy Helper`, `lang="en"><bdi>Answers policy questions.`} {
		if !strings.Contains(withAgent, want) {
			t.Fatalf("administrator-authored fallback lacks direction/language %q: %s", want, withAgent)
		}
	}
	if markup := renderAgentUXPage5(t, "en-US", AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true}, true); !strings.Contains(markup, ">Ask</span>") || !strings.Contains(markup, ">Setup</a>") || !strings.Contains(markup, ">Operations</a>") {
		t.Fatalf("administrator title row is missing the shared page navigation: %s", markup)
	}
	css := agentsStylesheet() + agentUXR7Stylesheet()
	for _, want := range []string{".agents-composer textarea{", "min-height:96px", "padding-inline:12px", "outline:2px solid var(--hcm-color-focus,var(--accent))", ".agents-page-header{", "width:100%"} {
		if !strings.Contains(css, want) {
			t.Fatalf("composer/page template CSS missing %q", want)
		}
	}
}

func TestAgentUXPage5_TaskRowsAreTruthfulAndFullWidth(t *testing.T) {
	failed := AgentTask{ID: "failed", Title: "Read the leave policy", State: AgentTaskFailed, Retryable: true, FailureReason: "The request timed out.", AnsweringAgentDisplayName: "Policy Helper", AnsweringAgentID: "policy-helper"}
	locale := ResolveProductLocale("en-US")
	view := ApplyLocale(NewView(PageAgents, "tenant", "viewer", ""), locale)
	markup, err := ui.RenderToString(RenderAgentTasksRegion(view, locale, AgentSnapshot{Tasks: []AgentTask{failed}, StartAvailable: true}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Policy Helper", "The agent stopped before it could answer. Nothing was changed.", "It took too long.", `class="agents-task-failure-reason"`, `data-agent-retry="true"`, `aria-expanded="false"`, `aria-controls="agents-task-detail-failed"`, `class="agents-task-chevron"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("failed row missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "Answered by Policy Helper") {
		t.Fatalf("failed row claimed an answer: %s", markup)
	}
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		localized := ResolveProductLocale(localeID)
		localizedView := ApplyLocale(NewView(PageAgents, "tenant", "viewer", ""), localized)
		localizedMarkup, renderErr := ui.RenderToString(RenderAgentTasksRegion(localizedView, localized, AgentSnapshot{Tasks: []AgentTask{failed}, StartAvailable: true}))
		for _, want := range []string{
			localized.Text("agents.agent_failed", map[string]string{"agent": "Policy Helper"}),
			localized.Text("agents.task_failed_reassurance"), localized.Text("agents.failure_timeout"), localized.Text("agents.ask_again"),
		} {
			if renderErr != nil || !strings.Contains(localizedMarkup, want) {
				t.Fatalf("%s failed task row missing %q: %v %s", localeID, want, renderErr, localizedMarkup)
			}
		}
	}
	nonRetryable := failed
	nonRetryable.ID, nonRetryable.Retryable = "terminal", false
	markup, err = ui.RenderToString(RenderAgentTasksRegion(view, locale, AgentSnapshot{Tasks: []AgentTask{nonRetryable}, StartAvailable: true}))
	if err != nil || !strings.Contains(markup, `data-agent-ask-again="true"`) {
		t.Fatalf("terminal failure did not offer a safe new request: %v %s", err, markup)
	}
	if got := agentTaskResultPreview(locale, AgentTask{ResultPreview: "Final answer."}); got != "Final answer." {
		t.Fatalf("result preview did not preserve the projection-owned answer: %q", got)
	}
	if got := agentTaskResultPreview(locale, AgentTask{ResultPreview: "Draft reply to your request: Final answer."}); got != "Draft reply to your request: Final answer." {
		t.Fatalf("client changed projection-owned preview copy: %q", got)
	}
	failureDetail, err := ui.RenderToString(RenderAgentTaskDetail(view, locale, failed, true))
	if err != nil || !strings.Contains(failureDetail, "Ask in Chat instead") {
		t.Fatalf("mentionable failed agent has no Chat recovery: %v %s", err, failureDetail)
	}
	css := agentsStylesheet() + agentUXR7Stylesheet()
	for _, want := range []string{".agents-task-row{", "justify-self:stretch", "grid-template-columns:auto minmax(0,1fr) auto auto", "text-align:start", "repeat(3,minmax(0,1fr))", "background:var(--surface-subtle,var(--canvas))"} {
		if !strings.Contains(css, want) {
			t.Fatalf("task row CSS missing %q", want)
		}
	}
}

func TestAgentUXPage5_RelativeTimePluralAndDigits(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		locale    string
		yesterday string
		fiveHours string
	}{
		{"en-US", "yesterday", "5 hours ago"},
		{"de-DE", "gestern", "vor 5 Stunden"},
		{"ar", "أمس", "قبل ٥ ساعات"},
	} {
		locale := ResolveProductLocale(tc.locale)
		if got := locale.FormatRelativeTime(now.Add(-25*time.Hour), now); got != tc.yesterday {
			t.Fatalf("%s yesterday = %q, want %q", tc.locale, got, tc.yesterday)
		}
		if got := locale.FormatRelativeTime(now.Add(-5*time.Hour), now); got != tc.fiveHours {
			t.Fatalf("%s five hours = %q, want %q", tc.locale, got, tc.fiveHours)
		}
	}
	locale := ResolveProductLocale("ar")
	markup := renderAgentUXPage5(t, "ar", AgentSnapshot{Availability: AgentsAvailable, Tasks: []AgentTask{{ID: "done", State: AgentTaskCompleted}}}, false)
	if want := `aria-label="` + locale.Text("agents.task_count") + `" class="agents-task-filter-count">١</span>`; !strings.Contains(markup, want) {
		t.Fatalf("Arabic tab count did not use Arabic-Indic digits: %s", markup)
	}
}

func TestAgentUXPage5_TaskDetailOpensAtItsRow(t *testing.T) {
	started := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	task := AgentTask{
		ID: "task-7", Title: "Summarize the leave policy", State: AgentTaskCompleted, AnswerText: "Employees may carry over 40 hours.",
		AnsweringAgentID: "policy-helper", AnsweringAgentDisplayName: "Policy Helper", CreatedAt: started, UpdatedAt: started.Add(30 * time.Second),
		DocumentUsageState: AgentDocumentUsageUsed,
		UsedDocuments:      []AgentTaskDocumentReference{{DocumentID: "leave", Label: "Leave policy"}},
	}
	markup := renderAgentUXPage5(t, "en-US", AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, Tasks: []AgentTask{task}, SelectedTask: &task}, false)
	for _, want := range []string{
		`class="agents-task-row is-expanded"`, `data-has-task-detail="true"`, `aria-expanded="true"`, `id="agents-task-detail-task-7"`, `id="agents-task-title" tabindex="-1"`,
		"Summarize the leave policy", "Employees may carry over 40 hours.", "Policy Helper", "Leave policy", "took under a minute", "Back to tasks",
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("inline detail missing %q: %s", want, markup)
		}
	}
	if detail, row := strings.Index(markup, `id="agents-task-detail-task-7"`), strings.Index(markup, `data-task-id="task-7"`); detail < row {
		t.Fatalf("task detail did not follow its row: %s", markup)
	}
	css := agentsStylesheet() + agentUXR7Stylesheet()
	for _, want := range []string{"@media (min-width:1024px)", "grid-template-columns:420px minmax(0,1fr)", "grid-column:2", "@media (max-width:400px)", "-webkit-line-clamp:3"} {
		if !strings.Contains(css, want) {
			t.Fatalf("responsive detail CSS missing %q", want)
		}
	}
}

func TestAgentUXPage5_UnavailableDirectoryDoesNotClaimEmpty(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		view := ApplyLocale(NewView(PageAgents, "tenant", "viewer", ""), locale)
		markup, err := ui.RenderToString(agentsUnavailablePage(view, locale))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{locale.Text("agents.unavailable_title"), locale.Text("agents.load_failed_detail"), locale.Text("agents.try_again"), `data-agent-page-retry="true"`} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s unavailable directory missing %q: %s", localeID, want, markup)
			}
		}
		if strings.Contains(markup, locale.Text("agents.no_agents")) {
			t.Fatalf("%s load failure claimed an empty catalog: %s", localeID, markup)
		}
	}
}

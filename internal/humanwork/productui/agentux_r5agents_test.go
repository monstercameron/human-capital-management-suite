package productui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestAgentUXR5Agents_TaskDetailResponsiveOrder(t *testing.T) {
	started := time.Date(2026, 10, 1, 9, 46, 0, 0, time.UTC)
	task := AgentTask{
		ID: "leave", Title: "How is sick leave accrued?", State: AgentTaskCompleted,
		AnswerText:                "Sick leave accrues at one hour for every 30 hours worked.",
		AnsweringAgentDisplayName: "General agent", CreatedAt: started, UpdatedAt: started.Add(30 * time.Second),
		DocumentUsageState: AgentDocumentUsageUsed, UsedDocuments: []AgentTaskDocumentReference{{DocumentID: "leave", Label: "Sick leave policy"}},
		Steps: []AgentTaskStep{{Name: "Read Sick leave policy", State: "completed"}, {Name: "Prepare answer", State: "completed"}},
	}
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		view := ApplyLocale(NewView(PageAgents, "tenant", "viewer", ""), locale)
		markup, err := ui.RenderToString(RenderAgentTasksRegion(view, locale, AgentSnapshot{StartAvailable: true, Tasks: []AgentTask{task}, SelectedTask: &task}))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`data-has-task-detail="true"`, `data-selected="true"`, `class="agents-task-detail-pane"`, task.Title, locale.Text("agents.answer"), locale.Text("agents.used_documents"), locale.Text("agents.ask_follow_up"), locale.Text("agents.copy_answer")} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s detail missing %q: %s", localeID, want, markup)
			}
		}
		if strings.Contains(markup, `>Request<`) || strings.Contains(markup, locale.Text("agents.your_question")) {
			t.Fatalf("%s detail retained the duplicate request section: %s", localeID, markup)
		}
		detailStart := strings.Index(markup, `class="agents-task-detail-pane"`)
		detail := markup
		if detailStart >= 0 {
			detail = markup[detailStart:]
		}
		if answer, documents, followUp := strings.Index(detail, locale.Text("agents.answer")), strings.Index(detail, locale.Text("agents.used_documents")), strings.Index(detail, locale.Text("agents.ask_follow_up")); answer < 0 || documents < answer || followUp < documents {
			t.Fatalf("%s answer/detail order is wrong: %s", localeID, detail)
		}
	}
	css := agentsStylesheet() + agentUXR7Stylesheet()
	for _, want := range []string{"@media (min-width:1024px)", "grid-template-columns:420px minmax(0,1fr)", "@media (max-width:1023px)", ".agents-tasks[data-has-task-detail=true] .agents-task-listing", "display:none"} {
		if !strings.Contains(css, want) {
			t.Fatalf("responsive task-detail contract missing %q", want)
		}
	}
}

func TestAgentUXR5Agents_FailureRecoveryAndUnknownLoad(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		view := ApplyLocale(NewView(PageAgents, "tenant", "viewer", ""), locale)
		failed := AgentTask{ID: "failed", Title: "Check my leave balance", State: AgentTaskFailed, FailureReason: "agent service unavailable", AnsweringAgentID: "policy", Documents: []AgentTaskDocumentReference{{DocumentID: "leave", Label: "Leave policy"}}}
		markup, err := ui.RenderToString(RenderAgentTasksRegion(view, locale, AgentSnapshot{Tasks: []AgentTask{failed}, SelectedTask: &failed}))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{locale.Text("agents.task_failed_reassurance"), locale.Text("agents.failure_service"), locale.Text("agents.ask_again"), `data-agent-ask-again="true"`, `data-agent-retry-prompt="Check my leave balance"`} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s failed recovery missing %q: %s", localeID, want, markup)
			}
		}
	}
	locale := ResolveProductLocale("en-US")
	view := ApplyLocale(NewView(PageAgents, "tenant", "viewer", ""), locale)
	markup, err := ui.RenderToString(RenderAgentTasksRegion(view, locale, AgentSnapshot{TasksLoadFailed: true}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, locale.Text("agents.tasks_load_failed")) || !strings.Contains(markup, `data-agent-tasks-retry="true"`) || strings.Contains(markup, locale.Text("agents.no_tasks")) {
		t.Fatalf("load failure does not render as one actionable in-place state: %s", markup)
	}
	if got := localizedAgentFailureReason(locale, "unrecognized backend failure"); got != locale.Text("agents.failure_unknown") {
		t.Fatalf("unknown failure = %q", got)
	}
}

func TestAgentUXR5Agents_DocumentPickerAndShellTokens(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		markup, err := ui.RenderToString(AgentRequestDocumentResult(locale, AgentRequestDocumentSuggestion{DocumentID: "policy", Title: "Paid time off", Owner: "You", Updated: "Oct 1"}, 0))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(markup, locale.Text("agents.document_folder_none")) || !strings.Contains(markup, "Paid time off") || !strings.Contains(markup, "You · "+locale.Text("agents.document_updated", map[string]string{"date": "Oct 1"})) {
			t.Fatalf("%s picker filler or title contract failed: %s", localeID, markup)
		}
	}
	css := Stylesheet()
	for _, want := range []string{".agents-document-picker-panel input:focus-visible", "box-shadow:none", "display:grid!important", ".footer", "margin-inline:var(--hcm-space-2)"} {
		if !strings.Contains(css, want) {
			t.Fatalf("shell/picker stylesheet missing %q", want)
		}
	}
	agentCSS := agentsStylesheet()
	if strings.Contains(agentCSS, ".agents-layout{align-items:flex-start;display:grid;gap:20px;max-inline-size:880px") || strings.Contains(agentCSS, ".agents-page-subtitle{color:var(--muted);max-inline-size:720px") || !strings.Contains(agentCSS, ".agents-agent-choice input") {
		t.Fatalf("Agents page retained a private max width or detached radio control: %s", agentCSS)
	}
}

func TestAgentUXR5Agents_ResultPreviewIsProjectionOwned(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	answer := "Agent connection works. Use no company data."
	if got := agentTaskResultPreview(locale, AgentTask{ResultPreview: answer}); got != answer {
		t.Fatalf("client changed result preview: %q", got)
	}
}

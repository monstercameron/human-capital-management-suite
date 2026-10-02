package productui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func agentUXPage3Task() AgentTask {
	return AgentTask{
		ID: "task-8", Version: 3, Title: "Summarize the benefits update", Goal: "Summarize the benefits update", State: AgentTaskFailed,
		CreatedAt: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 9, 30, 12, 5, 0, 0, time.UTC),
		FailureReason:     "The document service was temporarily unavailable. Try again.",
		Retryable:         true,
		Documents:         []AgentTaskDocumentReference{{DocumentID: "benefits policy", Label: "Benefits policy", SectionAnchor: "eligibility"}},
		DocumentOmissions: []AgentTaskDocumentOmission{{Label: "Private notes", Reason: "unreadable"}, {Reason: "unreadable"}},
	}
}

func TestTodo_AGENTUX_008(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	view := ApplyLocale(NewView(PageAgents, "tenant", "owner", ""), locale)
	snapshot := AgentSnapshot{Availability: AgentsAvailable, Tasks: []AgentTask{agentUXPage3Task()}, TasksLoadFailed: false}
	markup, err := ui.RenderToString(RenderAgentTasksRegion(view, locale, snapshot))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`datetime="2026-09-30T12:05:00Z"`, `title="Sep 30, 12:05 PM"`, locale.Text("agents.task_failed_reassurance"), "Benefits policy", `document=benefits+policy#eligibility`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("task projection missing %q: %s", want, markup)
		}
	}
	if got := agentTaskRelativeTime(locale, time.Date(2026, 9, 30, 12, 5, 0, 0, time.UTC), time.Date(2026, 9, 30, 12, 10, 0, 0, time.UTC)); got != "5 min ago" {
		t.Fatalf("relative time = %q", got)
	}
}

func TestTodo_AGENTUX_008_Browser(t *testing.T) {
	for _, localeID := range []string{"de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		view := ApplyLocale(NewView(PageAgents, "tenant", "owner", ""), locale)
		task := agentUXPage3Task()
		markup, err := ui.RenderToString(RenderAgentTaskDetail(view, locale, task, true))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{agentUXR7Text(locale, "attached"), locale.Text("agents.ask_again"), `class="agents-task-meta muted"`, `data-tone="failed"`} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s detail missing %q: %s", localeID, want, markup)
			}
		}
	}
	css := agentsStylesheet() + agentUXR7Stylesheet()
	for _, want := range []string{"@media (max-width:390px)", "@media (max-width:480px)", ".agents-request-documents", "min-width:0", "width:100%"} {
		if !strings.Contains(css, want) {
			t.Fatalf("responsive Agents CSS missing %q", want)
		}
	}
}

func TestTodo_AGENTDOC_006(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	markup, err := ui.RenderToString(AgentRequestDocumentPicker(locale))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Documents to read (optional)", "Add document", "Search documents", "Up to 5. The agent only reads documents you can already read.", `data-agent-document-count="true" hidden`, `role="combobox"`, `data-agent-document-chips="true"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("request picker missing %q: %s", want, markup)
		}
	}
	view := ApplyLocale(NewView(PageAgents, "tenant", "owner", ""), locale)
	detail, err := ui.RenderToString(RenderAgentTaskDetail(view, locale, agentUXPage3Task(), true))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Attached:", "Not used: Private notes (you cannot read it)", "1 document was not used", `#eligibility`} {
		if !strings.Contains(detail, want) {
			t.Fatalf("document task detail missing %q: %s", want, detail)
		}
	}
}

func TestTodo_AGENTDOC_006_Browser(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		markup, err := ui.RenderToString(AgentRequestDocumentPicker(locale))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{locale.Text("agents.add_document"), locale.Text("agents.documents_label"), locale.Text("agents.documents_access_help")} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s picker missing %q", localeID, want)
			}
		}
	}
	view := ApplyLocale(NewView(PageAgents, "tenant", "owner", ""), ResolveProductLocale("en-US"))
	withoutHub, err := ui.RenderToString(renderAgentsPage(view, view.Locale, AgentSnapshot{Availability: AgentsAvailable}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(withoutHub, ">Add document<") || !strings.Contains(withoutHub, `data-agent-request-document-mount="true"`) {
		t.Fatalf("unconfigured hub exposed a dead control: %s", withoutHub)
	}
}

package productui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestAgentUXPage7_SharedAdministratorNavigation(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		view := ApplyLocale(NewView(PageAgents, "tenant", "viewer", ""), locale)
		markup, err := ui.RenderToString(AgentPageNavigation(view, AgentPageAsk))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{locale.Text("agents.nav.ask"), locale.Text("agents.nav.setup"), locale.Text("agents.nav.operations"), `aria-current="page"`} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s navigation missing %q: %s", localeID, want, markup)
			}
		}
		if strings.Contains(markup, `href="/workspace/app/chat/agents`) {
			t.Fatalf("%s current Ask destination remained clickable: %s", localeID, markup)
		}
	}
}

func TestAgentUXPage7_TaskLayoutAndMobileTabs(t *testing.T) {
	css := agentsStylesheet() + agentUXR7Stylesheet()
	for selector, wants := range map[string][]string{
		".agents-layout":           {"width:100%"},
		".agents-page-subtitle":    {"width:100%"},
		".agents-task-row":         {"width:100%", "justify-self:stretch", "border-block-end"},
		".agents-task-link":        {"width:100%", "justify-content:stretch"},
		".agents-task-row-meta":    {"justify-content:space-between"},
		".agents-task-request":     {"word-break:normal"},
		".agents-task-filter":      {"white-space:nowrap"},
		".agents-document-results": {"width:100%"},
	} {
		declarations := declarationsFor(css, selector)
		for _, want := range wants {
			if !strings.Contains(declarations, want) {
				t.Errorf("%s missing %q: %s", selector, want, declarations)
			}
		}
	}
	for _, want := range []string{".agents-composer", ".agents-tasks", ".agents-task-view", "@media (min-width:600px)", "min-inline-size:min(240px,100%)", "grid-template-columns:420px minmax(0,1fr)", "@media (min-width:1024px)", "flex-direction:column"} {
		if !strings.Contains(css, want) {
			t.Errorf("responsive task contract missing %q", want)
		}
	}
}

func TestAgentUXPage7_FailedRowsUseTwoLinesAndActionableRetry(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	view := ApplyLocale(NewView(PageAgents, "tenant", "viewer", ""), locale)
	retryable := AgentTask{ID: "retry", Title: "Review policy", State: AgentTaskFailed, Retryable: true, FailureReason: "request timed out"}
	markup, err := ui.RenderToString(RenderAgentTasksRegion(view, locale, AgentSnapshot{StartAvailable: true, Tasks: []AgentTask{retryable}}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="agents-task-preview"`, `class="agents-task-failure-reason"`, locale.Text("agents.task_failed_reassurance"), locale.Text("agents.failure_timeout"), `data-agent-ask-again="true"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("retryable failure missing %q: %s", want, markup)
		}
	}
	retryable.FailureReason = "A step could not complete because the old backend said so."
	markup, err = ui.RenderToString(RenderAgentTasksRegion(view, locale, AgentSnapshot{Tasks: []AgentTask{retryable}}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, retryable.FailureReason) || strings.Contains(markup, `disabled`) {
		t.Fatalf("row leaked an unlocalized reason or disabled retry: %s", markup)
	}
	if !strings.Contains(markup, locale.Text("agents.ask_again")) {
		t.Fatalf("row lacks a next step when retry is unavailable: %s", markup)
	}
}

func TestAgentUXPage7_BuiltInAgentNameIsLocalized(t *testing.T) {
	for _, localeID := range []string{"de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		view := ApplyLocale(NewView(PageAgents, "tenant", "viewer", ""), locale)
		task := AgentTask{ID: "general", Title: "Answer", State: AgentTaskCompleted, AnsweringAgentID: "general-agent", AnsweringAgentDisplayName: "General agent"}
		markup, err := ui.RenderToString(RenderAgentTasksRegion(view, locale, AgentSnapshot{Tasks: []AgentTask{task}}))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, locale.Text("agents.general_agent")) || strings.Contains(markup, ">General agent<") {
			t.Fatalf("%s built-in agent name was not localized: %s", localeID, markup)
		}
	}
}

func TestAgentUXPage7_DocumentsRemainInsideRowsAndPickerIsInformative(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		result, err := ui.RenderToString(AgentRequestDocumentResult(locale, AgentRequestDocumentSuggestion{
			DocumentID: "doc-policy", Title: "Sick leave policy", Owner: "Ana Flores", Folder: "People", Updated: "Sep 25", Snippet: "Paid sick leave is available.",
		}, 2))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Sick leave policy", "Ana Flores · People · " + locale.Text("agents.document_updated", map[string]string{"date": "Sep 25"}), "Paid sick leave is available.", `role="option"`} {
			if !strings.Contains(result, want) {
				t.Fatalf("%s result missing %q: %s", localeID, want, result)
			}
		}
		chip, err := ui.RenderToString(AgentRequestDocumentChip(locale, "doc-policy", "Sick leave policy", "leave"))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`<svg aria-hidden="true" class="agents-document-icon"`, `href="/workspace/app/docs?document=doc-policy#leave"`, `aria-label="` + locale.Text("agents.document_remove_named", map[string]string{"title": "Sick leave policy"}) + `"`, ">×<"} {
			if !strings.Contains(chip, want) {
				t.Fatalf("%s chip missing %q: %s", localeID, want, chip)
			}
		}
	}
	locale := ResolveProductLocale("en-US")
	view := ApplyLocale(NewView(PageAgents, "tenant", "viewer", ""), locale)
	task := AgentTask{ID: "answered", Title: "Leave", State: AgentTaskCompleted, DocumentUsageState: AgentDocumentUsageUsed, UsedDocuments: []AgentTaskDocumentReference{{DocumentID: "doc-policy", Label: "Sick leave policy"}}}
	markup, err := ui.RenderToString(RenderAgentTasksRegion(view, locale, AgentSnapshot{Tasks: []AgentTask{task}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Sources:") || !strings.Contains(markup, `class="agents-document-reference"`) {
		t.Fatalf("answered row did not list its document as a plain read link: %s", markup)
	}
}

func TestAgentUXPage7_ViewerZoneUsesDocumentsDateStyle(t *testing.T) {
	locale := ResolveProductLocale("en-US").WithTimeZone("America/New_York")
	at := time.Date(2026, 10, 1, 0, 25, 0, 0, time.UTC)
	if got, want := agentTaskDateTimeLabel(locale, at, at), "Sep 30, 8:25 PM"; got != want {
		t.Fatalf("task date = %q, want %q", got, want)
	}
	if got := agentTaskDateTimeLabel(locale, at, at); strings.Contains(got, "UTC") || strings.Contains(got, "2026-10") {
		t.Fatalf("task date leaked transport formatting: %q", got)
	}
}

func TestAgentUXPage7_ComposerDirectionAndConciseGuidance(t *testing.T) {
	locale := ResolveProductLocale("ar")
	markup, err := ui.RenderToString(tagAgentComposer(locale, AgentSnapshot{StartAvailable: true, DocumentHubAvailable: true, Agents: []AgentSummary{{ID: "policy", Name: "Policy Helper", Description: "Answers policy questions."}}}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, `id="agents-composer-help"`) || strings.Contains(markup, locale.Text("agents.composer_help")) {
		t.Fatalf("available composer retained redundant helper: %s", markup)
	}
	for _, want := range []string{`dir="rtl"`, `dir="auto"`, `data-msg-done="` + locale.Text("agents.document_done") + `"`, locale.Text("agents.documents_empty")} {
		if !strings.Contains(markup, want) {
			t.Fatalf("Arabic composer missing %q: %s", want, markup)
		}
	}
}

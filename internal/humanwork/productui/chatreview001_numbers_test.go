package productui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATREVIEW-001: numbers composed into user-facing sentences use the
// reader's digits (locale.FormatNumber), as the run history, durations and
// document dates already do. The persona progress fraction and the task
// step number formatted with the default digits instead, so Arabic readers
// saw Western digits inside Arabic sentences.
func TestChatReview001_ProgressFractionUsesLocaleDigits(t *testing.T) {
	projection := PersonaChatProjection{
		Availability: PersonaChatAvailable, ViewerID: "user-1", InvokerID: "user-1",
		Progress: &PersonaProgress{InvocationID: "invocation-1", InvokerID: "user-1", AgentName: "Comp Analyst", Activity: "reading", CurrentStep: 2, TotalSteps: 5, Visible: true},
	}
	view := NewView(PageChat, "tenant-1", "user-1", "scope-1")
	view.Locale = ResolveProductLocale("ar")
	markup, err := ui.RenderToString(RenderPersonaChat(view, projection, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "(٢/٥)") {
		t.Fatalf("arabic progress fraction not localized: %s", markup)
	}
	view.Locale = ResolveProductLocale("en-US")
	markup, err = ui.RenderToString(RenderPersonaChat(view, projection, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "(2/5)") {
		t.Fatalf("english progress fraction changed: %s", markup)
	}
}

func TestChatReview001_StepNumberUsesLocaleDigits(t *testing.T) {
	task := AgentTask{ID: "task-1", Title: "Prepare scenario", Goal: "Draft it", Steps: []AgentTaskStep{{Name: "Read bands", State: "completed", Tier: "T0"}}}
	ar := ResolveProductLocale("ar")
	markup, err := ui.RenderToString(RenderAgentTaskSummary(ar, task, "/workspace/app/chat/agents?task=task-1"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, ">١<") {
		t.Fatalf("arabic step number not localized: %s", markup)
	}
	en := ResolveProductLocale("en-US")
	markup, err = ui.RenderToString(RenderAgentTaskSummary(en, task, "/workspace/app/chat/agents?task=task-1"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, ">1<") {
		t.Fatalf("english step number changed: %s", markup)
	}
}

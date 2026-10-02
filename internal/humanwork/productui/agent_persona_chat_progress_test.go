package productui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func personaChatTestProjection() PersonaChatProjection {
	task := AgentTask{ID: "task-1", AgentID: "comp-analyst", Title: "Prepare compensation scenario", Goal: "Draft a scenario for my review", State: AgentTaskAwaitingApproval, LiveStep: "Waiting for your approval", Steps: []AgentTaskStep{{Name: "Read bands", State: "completed", Tier: "T0"}, {Name: "Prepare draft", State: "waiting", Tier: "T3"}}}
	return PersonaChatProjection{
		Availability: PersonaChatAvailable, ViewerID: "user-1", InvokerID: "user-1", Task: &task, TaskRevision: "rev-2",
		Reply:    &PersonaThreadReply{ThreadID: "thread-1", ReplyToPost: "post-1", ReplyID: "reply-1", AgentName: "Comp Analyst", Body: "I found the relevant bands."},
		Progress: &PersonaProgress{InvocationID: "invocation-1", InvokerID: "user-1", AgentName: "Comp Analyst", Activity: "reading 3 sources", CurrentStep: 2, TotalSteps: 5, Visible: true},
	}
}

func TestTodo_AGENTP_020(t *testing.T) {
	view := NewView(PageChat, "tenant-1", "user-1", "scope-1")
	markup, err := ui.RenderToString(RenderPersonaChat(view, personaChatTestProjection(), time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"persona-thread-reply", "agent-reply-to=\"post-1\"", "Comp Analyst", "persona-chat-progress", "reading 3 sources", `agent-task-card="task-1"`, `agent-task-revision="rev-2"`, "Awaiting approval"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("persona chat missing %q: %s", want, markup)
		}
	}
	if strings.Count(markup, `agent-task-card="task-1"`) != 1 {
		t.Fatalf("task hand-off posted more than once: %s", markup)
	}
}

func TestTodo_AGENTP_020_Browser(t *testing.T) {
	view := NewView(PageChat, "tenant-1", "user-1", "scope-1")
	projection := personaChatTestProjection()
	projection.ResultReady = true
	markup, err := ui.RenderToString(RenderPersonaChat(view, projection, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "persona-chat-progress") {
		t.Fatalf("progress spinner remained after result: %s", markup)
	}
	if !strings.Contains(markup, `agent-task-card="task-1"`) || !strings.Contains(markup, "agents-task-link") || !strings.Contains(markup, "task=task-1") {
		t.Fatalf("long task did not hand off to task view: %s", markup)
	}
}

func TestTodo_AGENTP_020_Accessibility(t *testing.T) {
	view := NewView(PageChat, "tenant-1", "user-1", "scope-1")
	projection := personaChatTestProjection()
	projection.Failure = &PersonaChatFailure{InvocationID: "invocation-1", InvokerID: "user-1", Heading: "Provider unavailable", Message: "Try again later.", RetryLabel: "Retry", Retryable: true}
	markup, err := ui.RenderToString(RenderPersonaChat(view, projection, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`role="status"`, `aria-live="polite"`, `aria-atomic="true"`, `reduced-motion="respect"`, `role="alert"`, `aria-live="assertive"`, `data-agent-action="retry"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("accessibility contract missing %q: %s", want, markup)
		}
	}
}

func TestTodo_AGENTP_020_I18n(t *testing.T) {
	projection := personaChatTestProjection()
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := NewView(PageChat, "tenant-1", "user-1", "scope-1")
		view.Locale = ResolveProductLocale(locale)
		markup, err := ui.RenderToString(RenderPersonaChat(view, projection, time.Now()))
		if err != nil {
			t.Fatalf("%s: %v", locale, err)
		}
		if strings.Contains(markup, "agents.") || strings.Contains(markup, "�") {
			t.Fatalf("%s rendered missing or malformed catalog text: %s", locale, markup)
		}
		if !strings.Contains(markup, view.Locale.Text("agents.live_step")) || !strings.Contains(markup, view.Locale.Text("agents.threads")) {
			t.Fatalf("%s did not render localized persona copy: %s", locale, markup)
		}
	}
}

func TestTodo_AGENTP_020_Fault(t *testing.T) {
	view := NewView(PageChat, "tenant-1", "user-1", "scope-1")
	projection := personaChatTestProjection()
	projection.Progress = nil
	projection.Task = nil
	projection.Failure = &PersonaChatFailure{InvocationID: "invocation-1", InvokerID: "user-1", Heading: "Provider unavailable", Message: "The provider failed.", RetryLabel: "Retry", Retryable: true}
	markup, err := ui.RenderToString(RenderPersonaChat(view, projection, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `data-agent-failure="typed"`) || !strings.Contains(markup, "The provider failed.") || !strings.Contains(markup, "Retry") {
		t.Fatalf("provider failure did not become typed retry state: %s", markup)
	}
	projection.Availability = PersonaChatUnavailable
	unavailable, err := ui.RenderToString(RenderPersonaChat(view, projection, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unavailable, "agents-page") && !strings.Contains(unavailable, "Agents are not available") {
		if !strings.Contains(unavailable, "Agents") {
			t.Fatalf("unavailable state missing: %s", unavailable)
		}
	}
	projection = personaChatTestProjection()
	projection.ViewerID = "member-2"
	member, err := ui.RenderToString(RenderPersonaChat(NewView(PageChat, "tenant-1", "member-2", "scope-1"), projection, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(member, "reading 3 sources") || strings.Contains(member, "persona-task-handoff") {
		t.Fatalf("member saw invoker-only progress or task: %s", member)
	}
	if !strings.Contains(member, "I found the relevant bands.") {
		t.Fatalf("thread reply was not preserved for the conversation audience: %s", member)
	}
}

func TestAgentUXChat3_LocalizedAnswerStates(t *testing.T) {
	keys := []string{
		"chat.agent.finding_answer", "chat.agent.still_working", "chat.agent.saved_conversation",
		"chat.agent.subtitle", "chat.agent.failed_now", "chat.agent.thread_continue",
		"chat.agent.legacy_private", "chat.agent.feedback_owner_named", "chat.agent.undo",
		"chat.agent.you_asked_in", "chat.agent.view_in",
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		resolved := ResolveProductLocale(locale)
		for _, key := range keys {
			text := resolved.Text(key, map[string]string{"name": "Policy Helper", "conversation": "#general"})
			if strings.TrimSpace(text) == "" || text == key || strings.Contains(text, "{name}") || strings.Contains(text, "{conversation}") {
				t.Errorf("%s catalog did not resolve %s: %q", locale, key, text)
			}
		}
	}
}

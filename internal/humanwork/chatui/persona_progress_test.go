package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func renderPersonaProgressTest(t *testing.T, model Model, projection PersonaProgressProjection) string {
	t.Helper()
	// This test proves delegation and private placement. The shared task
	// renderer's markup and locales are exercised in productui itself.
	model.RenderPersonaTask = func(task PersonaTaskCardProps) ui.Node {
		return html.Article(html.Props{Data: map[string]string{"agent-task-card": task.ID, "agent-task-revision": task.Revision}}, html.A(html.Props{Href: task.OpenTaskHref, Text: task.Title}), html.Span(html.Props{Text: personaProgressText(model, "chat.persona.awaiting_approval", "Awaiting approval")}))
	}
	markup, err := ui.RenderToString(RenderPersonaProgress(model, projection))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func personaProgressFixture() PersonaProgressProjection {
	return PersonaProgressProjection{
		ViewerID: "user-1", InvokerID: "user-1", AgentName: "Comp Analyst",
		Progress: &PersonaProgressProps{InvocationID: "inv-1", InvokerID: "user-1", AgentName: "Comp Analyst", Activity: "reading 3 sources", CurrentStep: 2, TotalSteps: 5, Visible: true},
		Task:     &PersonaTaskCardProps{ID: "task-1", Title: "Prepare compensation scenario", Goal: "Draft a scenario for my review", State: "awaiting_approval", Revision: "rev-2", OpenTaskHref: "/chat/agents?task=task-1", AwaitingApproval: true},
	}
}

func TestTodo_AGENTP_020(t *testing.T) {
	markup := renderPersonaProgressTest(t, Model{Locale: "en-US"}, personaProgressFixture())
	for _, want := range []string{"persona-progress-status", "Finding an answer in your policy documents…", `agent-task-card="task-1"`, `agent-task-revision="rev-2"`, `href="/chat/agents?task=task-1"`, "Awaiting approval"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("persona progress missing %q: %s", want, markup)
		}
	}
	if strings.Count(markup, `agent-task-card="task-1"`) != 1 {
		t.Fatalf("task card was duplicated: %s", markup)
	}
}

func TestTodo_AGENTP_020_Accessibility(t *testing.T) {
	p := personaProgressFixture()
	markup := renderPersonaProgressTest(t, Model{Locale: "en-US"}, p)
	for _, want := range []string{`role="status"`, `aria-live="polite"`, `aria-atomic="true"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("accessibility/failure contract missing %q: %s", want, markup)
		}
	}
	if !strings.Contains(PersonaProgressStyles, "prefers-reduced-motion:reduce") || !strings.Contains(PersonaProgressStyles, "animation:none") {
		t.Fatal("reduced-motion rule missing")
	}
}

func TestTodo_AGENTP_020_Fault(t *testing.T) {
	p := personaProgressFixture()
	p.Failure = &PersonaProgressFailure{InvocationID: "inv-1", InvokerID: "user-1", Code: "MODEL_UNAVAILABLE", Message: "Provider failed", Retryable: true}
	markup := renderPersonaProgressTest(t, Model{Locale: "en-US"}, p)
	for _, want := range []string{`role="status"`, `aria-live="polite"`, `data-agent-action="retry"`, "Comp Analyst could not answer because the service had a problem."} {
		if !strings.Contains(markup, want) {
			t.Fatalf("failure missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "persona-progress-status") {
		t.Fatalf("failure retained working status: %s", markup)
	}
	if strings.Contains(markup, "MODEL_UNAVAILABLE") || strings.Contains(markup, "Provider failed") {
		t.Fatalf("failure leaked an internal code or unsanitized message: %s", markup)
	}
	// CHATBUG-054: the person who asked may ask again whatever the server said
	// about the run; nobody else is offered anything.
	p.Failure.Retryable = false
	if markup := renderPersonaProgressTest(t, Model{Locale: "en-US"}, p); !strings.Contains(markup, `data-agent-action="retry"`) || !strings.Contains(markup, ">Ask again<") {
		t.Fatalf("a failed card lost its Ask again: %s", markup)
	}
	other := p
	other.ViewerID = "member-2"
	if markup := renderPersonaProgressTest(t, Model{Locale: "en-US"}, other); strings.Contains(markup, `data-agent-action`) {
		t.Fatalf("somebody who did not ask is offered an action: %s", markup)
	}
}

func TestTodo_AGENTP_020_I18n(t *testing.T) {
	for _, tc := range []struct{ locale, want string }{{"en-US", "Awaiting approval"}, {"de-DE", "Wartet auf Genehmigung"}, {"ar", "بانتظار الموافقة"}} {
		markup := renderPersonaProgressTest(t, Model{Locale: tc.locale}, personaProgressFixture())
		if !strings.Contains(markup, tc.want) || strings.Contains(markup, "chat.persona.") {
			t.Fatalf("%s localization missing: %s", tc.locale, markup)
		}
	}
}

func TestTodo_AGENTP_020_InvokerOnlyAndResultClearsProgress(t *testing.T) {
	p := personaProgressFixture()
	member := renderPersonaProgressTest(t, Model{Locale: "en-US"}, PersonaProgressProjection{ViewerID: "member-2", InvokerID: "user-1", Progress: p.Progress, Task: p.Task})
	if strings.Contains(member, "reading 3 sources") || strings.Contains(member, "persona-task-card") {
		t.Fatalf("non-invoker saw private progress or task: %s", member)
	}
	p.Progress.ResultReady = true
	result := renderPersonaProgressTest(t, Model{Locale: "en-US"}, p)
	if strings.Contains(result, "persona-progress-status") {
		t.Fatalf("progress remained after result: %s", result)
	}
}

func TestTodo_AGENTP_020_OnlyReportsSuppliedStepCounts(t *testing.T) {
	p := personaProgressFixture()
	p.Progress.CurrentStep, p.Progress.TotalSteps = 0, 0
	markup := renderPersonaProgressTest(t, Model{Locale: "en-US"}, p)
	if strings.Contains(markup, "(1/1)") || !strings.Contains(markup, "Finding an answer in your policy documents…") || strings.Contains(markup, "agent-reply-counter") {
		t.Fatalf("missing steps were fabricated: %s", markup)
	}
}

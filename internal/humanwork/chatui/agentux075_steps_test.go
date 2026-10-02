package chatui

import (
	"strings"
	"testing"
)

// AGENTUX-075 part 2: the working line follows the typed steps of the run, in
// the viewer's language, with a name isolated for direction, and shows the
// elapsed time and Stop only after ten seconds.
func TestTodo_AGENTUX_075_Steps(t *testing.T) {
	title := "2026 holiday guide"
	for _, tc := range []struct {
		locale string
		lines  map[string]string
	}{
		{"en-US", map[string]string{"reading_question": "Reading the question…", "searching": "Searching documents…", "reading_document": "Reading ⁨" + title + "⁩…", "writing": "Writing the answer…"}},
		{"de-DE", map[string]string{"reading_question": "Die Frage wird gelesen…", "searching": "Dokumente werden durchsucht…", "reading_document": "⁨" + title + "⁩ wird gelesen…", "writing": "Die Antwort wird geschrieben…"}},
		{"ar", map[string]string{"reading_question": "جارٍ قراءة السؤال…", "searching": "جارٍ البحث في المستندات…", "reading_document": "جارٍ قراءة ⁨" + title + "⁩…", "writing": "جارٍ كتابة الإجابة…"}},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			model := Model{Locale: tc.locale, Callbacks: Callbacks{CancelPersonaInvocation: func(string) {}}}
			for kind, want := range tc.lines {
				subject := ""
				if kind == "reading_document" {
					subject = title
				}
				render := func(seconds int) string {
					projection := PersonaProgressProjection{ViewerID: "alice", InvokerID: "alice", AgentName: "Assistant",
						Progress: &PersonaProgressProps{InvocationID: "run-1", InvokerID: "alice", AgentName: "Assistant", StepKind: kind, StepSubject: subject, Visible: true, ElapsedSeconds: seconds}}
					return renderAgentUXChat3Node(t, RenderPersonaProgress(model, projection), 390)
				}
				early := render(9)
				if !strings.Contains(early, want) || !strings.Contains(early, "agent-working-dots") {
					t.Errorf("%s %s: line %q missing at 9s: %s", tc.locale, kind, want, early)
				}
				if strings.Contains(early, "agent-reply-counter") || strings.Contains(early, "agent-progress-cancel") {
					t.Errorf("%s %s: elapsed time or Stop shown before ten seconds: %s", tc.locale, kind, early)
				}
				late := render(10)
				if !strings.Contains(late, "agent-reply-counter") || !strings.Contains(late, "agent-progress-cancel") {
					t.Errorf("%s %s: elapsed time or Stop missing at ten seconds: %s", tc.locale, kind, late)
				}
				if strings.Contains(late, "chat.agent.") || strings.Contains(late, "reading_document") {
					t.Errorf("%s %s: a key or kind reached the page: %s", tc.locale, kind, late)
				}
			}
		})
	}
	// A kind nobody named, or a server that sends none, never prints the word.
	model := Model{Locale: "en-US"}
	for _, progress := range []PersonaProgressProps{
		{InvocationID: "run-1", InvokerID: "alice", StepKind: "ZXQ_17", Visible: true},
		{InvocationID: "run-1", InvokerID: "alice", Activity: "ZXQ-17", Visible: true},
	} {
		projection := PersonaProgressProjection{ViewerID: "alice", InvokerID: "alice", AgentName: "Assistant", Progress: &progress}
		markup := renderAgentUXChat3Node(t, RenderPersonaProgress(model, projection), 390)
		if strings.Contains(markup, "ZXQ") || !strings.Contains(markup, "Finding an answer in your policy documents…") {
			t.Errorf("an unknown step was printed or the generic line lost: %s", markup)
		}
	}
	// Stop is the asker's alone.
	projection := PersonaProgressProjection{ViewerID: "bob", InvokerID: "alice", AgentName: "Assistant",
		Progress: &PersonaProgressProps{InvocationID: "run-1", InvokerID: "alice", StepKind: "writing", Visible: true, ElapsedSeconds: 30}}
	if markup := renderAgentUXChat3Node(t, RenderPersonaProgress(Model{Locale: "en-US", Callbacks: Callbacks{CancelPersonaInvocation: func(string) {}}}, projection), 390); strings.Contains(markup, "agent-progress-cancel") || strings.Contains(markup, "Writing") {
		t.Errorf("someone other than the asker sees the run or its Stop control: %s", markup)
	}
}

// The marks that isolate a name inside a line for direction.
var agentUX075FSI, agentUX075PDI = string(rune(0x2068)), string(rune(0x2069))

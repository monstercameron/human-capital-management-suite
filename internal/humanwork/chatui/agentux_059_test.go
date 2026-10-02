package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// A rating the server did not save is not shown as given: the buttons are back
// at what the server holds and the card says, beside them, that the rating was
// not saved. Rating again clears the sentence.
func TestTodo_AGENTUX_059(t *testing.T) {
	for _, tc := range []struct {
		name            string
		held, attempted string
	}{
		{"first rating refused", "", AgentFeedbackHelpful},
		{"a change refused", AgentFeedbackHelpful, AgentFeedbackNotRight},
		{"a removal refused", AgentFeedbackNotRight, ""},
	} {
		m := chat4Fixture("en-US", "answered", false)
		m.PersonaActivityReady = true
		if tc.held != "" {
			m.AgentFeedbackSaved = map[string]string{"run": tc.held}
		}
		// The click showed the attempted rating; the server then refused it.
		local := localUI{}
		if tc.attempted != "" {
			local.agentFeedback = map[string]string{"run": tc.attempted}
		}
		m.AgentFeedbackRestored = map[string]string{"run": tc.held}
		helpful, notRight := chatbug066Buttons(t, m, local)
		if (chatPolishAttr(helpful, "aria-pressed") == "true") != (tc.held == AgentFeedbackHelpful) || (chatPolishAttr(notRight, "aria-pressed") == "true") != (tc.held == AgentFeedbackNotRight) {
			t.Fatalf("%s: the buttons do not show what the server holds (%q)", tc.name, tc.held)
		}
		group := helpful.Parent
		notes := chatPolishNodesIn(group, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "agent-feedback-unsaved") })
		if len(notes) != 1 || chatbug030Text(notes[0]) != "Your rating was not saved. Try again." || chatPolishAttr(notes[0], "role") != "alert" {
			t.Fatalf("%s: the card does not say beside the buttons that the rating was not saved", tc.name)
		}
		// The person rates again: the client clears the mark before the request.
		m.AgentFeedbackRestored = nil
		helpful, _ = chatbug066Buttons(t, m, localUI{agentFeedback: map[string]string{"run": AgentFeedbackHelpful}})
		if chatPolishAttr(helpful, "aria-pressed") != "true" {
			t.Fatalf("%s: rating again does not show the new choice", tc.name)
		}
		if again := chatPolishNodesIn(helpful.Parent, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "agent-feedback-unsaved") }); len(again) != 0 {
			t.Fatalf("%s: the sentence stays after the person rated again", tc.name)
		}
	}
	for locale, want := range map[string]string{"de-DE": "Ihre Bewertung wurde nicht gespeichert. Versuchen Sie es erneut.", "ar": "لم يتم حفظ تقييمك. حاول مرة أخرى."} {
		m := chat4Fixture(locale, "answered", false)
		m.PersonaActivityReady = true
		m.AgentFeedbackRestored = map[string]string{"run": ""}
		helpful, _ := chatbug066Buttons(t, m, localUI{})
		if !strings.Contains(chatbug030Text(helpful.Parent), want) {
			t.Fatalf("%s: the sentence is not translated", locale)
		}
	}
	// A working run offers Stop once it has run five seconds, and names the run.
	working := chat4Fixture("en-US", "working15", false)
	stop := chatPolishNodes(t, chatbug047Rows(t, working), func(n *xhtml.Node) bool { return chatPolishHasClass(n, "agent-progress-cancel") })
	if len(stop) != 1 || chatPolishAttr(stop[0], "data-action") != "agent-invocation-cancel" || chatPolishAttr(stop[0], "data-id") != "run" {
		t.Fatal("a working run has no Stop that names it")
	}
}

package chatui

import (
	"strings"
	"testing"
)

// A rating that could not be saved is put back to the rating that stands: the
// optimistic choice the click showed is overridden, and the notice is written
// in the viewer's language.
func TestTodo_CHATBUG_006_RatingRestored(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		model := chat4Fixture(locale, "answered", true)
		optimistic := localUI{agentFeedback: map[string]string{"run": "not-right"}}
		if markup := renderNode(t, renderAgentFeedback(model, optimistic, "answer")); !strings.Contains(markup, `aria-pressed="true"`) || !strings.Contains(markup, "agent-feedback-sent") {
			t.Fatalf("%s: optimistic rating not shown: %s", locale, markup)
		}
		model.AgentFeedbackRestored = map[string]string{"run": ""}
		if markup := renderNode(t, renderAgentFeedback(model, optimistic, "answer")); strings.Contains(markup, `aria-pressed="true"`) || strings.Contains(markup, "agent-feedback-sent") {
			t.Fatalf("%s: failed rating still shown as saved: %s", locale, markup)
		}
		model.AgentFeedbackRestored = map[string]string{"run": AgentFeedbackHelpful}
		markup := renderNode(t, renderAgentFeedback(model, optimistic, "answer"))
		if strings.Count(markup, `aria-pressed="true"`) != 1 || strings.Contains(markup, "agent-feedback-sent") {
			t.Fatalf("%s: previous helpful rating not restored: %s", locale, markup)
		}
		notice := AgentRatingNotSavedText(locale)
		if notice == "" || strings.Contains(notice, "chat.agent") || (locale != "en-US" && notice == AgentRatingNotSavedText("en-US")) {
			t.Fatalf("%s: notice %q", locale, notice)
		}
	}
	if AgentRatingNotSavedText("en-US") != "Your rating was not saved. Try again." {
		t.Fatalf("notice text: %q", AgentRatingNotSavedText("en-US"))
	}
}

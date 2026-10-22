package application

import (
	"context"
	"testing"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// The production outcome chooser reads the question's topic without a model
// call, and every pick it can make is inside the closed set the reactor
// enforces anyway (AGENTUX-052).
func TestAgentUX052_OutcomeChooserTable(t *testing.T) {
	allowed := chatcore.AgentQuestionReactionEmoji()
	chooser := personaOutcomeEmojiTable{}
	for question, want := range map[string]string{
		"When is my birthday party?":            "🎂",
		"When is payday?":                       "💸",
		"Show me all the holidays":              "🏖️",
		"Thanks for the help":                   "🙏",
		"Congratulations on the launch":         "🎉",
		"When is the deadline?":                 "📅",
		"An idea for the offsite":               "💡",
		"How do I fix this error?":              "🔧",
		"What does the travel policy say?":      "📝",
		"Book my travel to Berlin":              "✈️",
		" coercion attempt: 💣 outside the set ": "",
		"": "",
	} {
		got, ok := chooser.ChooseOutcomeEmoji(context.Background(), question, allowed)
		if got != want || ok != (want != "") {
			t.Errorf("%q: chose %q ok=%v, want %q", question, got, ok, want)
		}
		if got != "" && !chatcore.ValidAgentQuestionReactionEmoji(got) {
			t.Errorf("%q: %q is outside the closed set", question, got)
		}
	}
	// A pick outside the allowed list is declined, not forced.
	if got, ok := chooser.ChooseOutcomeEmoji(context.Background(), "When is payday?", []string{"✅"}); ok || got != "" {
		t.Errorf("a pick outside the allowed list was returned: %q ok=%v", got, ok)
	}
}

package chat

import (
	"strings"
	"testing"
)

// TestTodo_AGENTCOST_006_LimitCardNamesConversationAndZone: a run stopped by the
// owner's daily limit says which agent, in which conversation, and when the day
// ends in the zone the gate counts days in (UTC), in each language; a card that
// cannot name the conversation says it for the agent, still with the zone, and
// is not retryable.
func TestTodo_AGENTCOST_006_LimitCardNamesConversationAndZone(t *testing.T) {
	for _, tc := range []struct{ locale, want string }{
		{"en-US", "Policy Helper reached today's limit for #general. It resets at 00:00 UTC."},
		{"de-DE", "Policy Helper hat das heutige Limit für #general erreicht. Es wird um 00:00 Uhr UTC zurückgesetzt."},
		{"ar", "بلغ Policy Helper حد اليوم في #general. يُعاد تعيينه عند 00:00 بالتوقيت العالمي (UTC)."},
	} {
		got := AgentAnswerDailyLimit(tc.locale, "Policy Helper", "#general")
		if got.Sentence != tc.want || got.Retryable || got.Class != "daily_limit" || strings.TrimSpace(got.NextStep) == "" {
			t.Fatalf("%s: %+v", tc.locale, got)
		}
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		general := AgentAnswerDailyLimit(locale, "Policy Helper", "  ")
		if general.Class != "daily_limit" || general.Retryable || !strings.Contains(general.Sentence, "UTC") || strings.Contains(general.Sentence, "{") {
			t.Fatalf("%s general sentence: %+v", locale, general)
		}
	}
}

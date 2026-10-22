package chatui

import (
	"strings"
	"testing"
)

// TestTodo_AGENTCOST_006_LimitCardNamesTheConversation: the card for a run the
// owner's daily limit stopped says which conversation it is, in the name the
// conversation list shows, and when the day ends in UTC. In the person's own
// conversation with the agent the conversation is not named twice.
func TestTodo_AGENTCOST_006_LimitCardNamesTheConversation(t *testing.T) {
	for _, tc := range []struct {
		locale string
		direct bool
		want   string
		not    string
	}{
		{"en-US", false, "Policy Helper reached today's limit for #general. It resets at 00:00 UTC.", ""},
		{"de-DE", false, "Policy Helper hat das heutige Limit für #general erreicht. Es wird um 00:00 Uhr UTC zurückgesetzt.", ""},
		{"ar", false, "بلغ Policy Helper حد اليوم في #general.", ""},
		{"en-US", true, "Policy Helper reached today's limit. It resets at 00:00 UTC.", "for #"},
	} {
		m := chat4Fixture(tc.locale, "failed", tc.direct)
		m.PersonaInvocations[0].Projection.Failure = &PersonaProgressFailure{InvocationID: "run", InvokerID: "alice", Code: "DAILY_LIMIT_REACHED"}
		text := strings.Join(strings.Fields(chatbug030Text(chatbug054Card(t, m))), " ")
		if !strings.Contains(text, tc.want) || tc.not != "" && strings.Contains(text, tc.not) {
			t.Fatalf("%s direct=%v: card reads %q, want %q", tc.locale, tc.direct, text, tc.want)
		}
	}
}

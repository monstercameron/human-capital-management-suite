package chatui

import (
	"strings"
	"testing"
	"time"
)

// CHATUX-026, settling. The same answer in the same conversation drew two
// ways on two loads of one build: "Shared with #general" with its rating buttons
// when the agent activity was read first, and "Only visible to you" with no
// rating when the private answer arrived first. The card now says nothing about
// who can see it, and offers nothing that depends on it, until the activity that
// names its run has been read; then it is the same card whatever came first.
func TestTodo_CHATUX_026_Settling(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		build := func(answer, activity bool) Model {
			m := chat4Fixture(locale, "answered", false)
			m.Callbacks.ShareAgentAnswer = func(string) {}
			m.Callbacks.RemoveSharedAgentAnswer = func(string) {}
			m.Callbacks.OpenThread = func(string) {}
			m.Callbacks.OpenMenu = func(string) {}
			m.Conversations[0].MemberCount = 18
			cards := m.EphemeralMessages
			invocations, ready := m.PersonaInvocations, true
			m.EphemeralMessages, m.PersonaInvocations, m.PersonaActivityReady = nil, nil, false
			if answer {
				m.EphemeralMessages = cards
			}
			if activity {
				m.PersonaInvocations, m.PersonaActivityReady = invocations, ready
				// The first read of the activity also says the answer was shared.
				m.AgentShare = map[string]AgentShareState{"run": {Status: AgentShareShared, PostID: "copy"}}
			}
			return m
		}
		card := func(m Model) string { return chatbug047Rows(t, m) }
		mark := agentReplyFallback(locale, "chat.agent.only_visible", "Only visible to you")

		// The private answer is first: the card is neutral, not the wrong card.
		first := card(build(true, false))
		if !strings.Contains(first, `data-agent-answer-visibility="settling"`) {
			t.Fatalf("%s: the early card is not the settling card: %s", locale, first)
		}
		for _, wrong := range []string{mark, `data-visibility=`, `class="agent-feedback"`, `data-action="agent-share`, `agent-reply-why`, "Shared with", "Geteilt mit"} {
			if strings.Contains(first, wrong) {
				t.Fatalf("%s: the early card carries %q before the activity was read: %s", locale, wrong, first)
			}
		}
		if !strings.Contains(first, "agent-follow-up") {
			t.Fatalf("%s: the early card lost its follow-up", locale)
		}

		// Once the activity was read: the shared card with its rating. (The order in
		// which the page's own reads arrive is TestTodo_CHATUX_026_ArrivalOrder in
		// tools/uxqual/cmd/journeywasm.)
		answerFirst := card(build(true, true))
		if strings.Contains(answerFirst, mark) || !strings.Contains(answerFirst, `data-visibility="shared"`) || !strings.Contains(answerFirst, `class="agent-feedback"`) || strings.Contains(answerFirst, `data-agent-answer-visibility="settling"`) {
			t.Fatalf("%s: the settled card is not the shared card with its rating: %s", locale, answerFirst)
		}

		// An activity that names no run for the answer ends the wait: private, as stored.
		settledPrivate := build(true, false)
		settledPrivate.PersonaActivityReady = true
		if page := card(settledPrivate); !strings.Contains(page, mark) || strings.Contains(page, `data-agent-answer-visibility="settling"`) {
			t.Fatalf("%s: an answer the activity never claimed stays unsettled: %s", locale, page)
		}
		// Expired answers are not drawn in any order.
		expired := build(true, true)
		expired.EphemeralMessages[0].ExpiresAt = time.Now().Add(-time.Hour)
		if page := card(expired); strings.Contains(page, `data-agent-reply-state="answered-private"`) {
			t.Fatalf("%s: an expired answer is drawn", locale)
		}
	}
}

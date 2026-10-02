package chatui

import (
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"
)

// In a person's own conversation with an agent the answer is shown once, as the
// agent's message. There is no "Only visible to you" card, no link to the
// conversation the reader is already in, and a private envelope for the same
// answer (as an earlier build delivered beside the stored message) adds nothing.
func TestTodo_AGENTUX_038_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chat4Fixture(locale, "answered", true)
		// The same answer delivered as a private envelope too.
		m.EphemeralMessages = []EphemeralMessage{{ID: "envelope", ThreadID: "question", Body: "Carry over up to 40 hours.", OnlyVisibleToYou: true, CreatedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour)}}
		markup := chatPolishMarkup(t, Build(m), 1440, "light")
		list := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "message-list") })
		if len(list) != 1 {
			t.Fatalf("%s: %d message lists", locale, len(list))
		}
		text := chatbug030Text(list[0])
		if got := strings.Count(text, "Carry over up to 40 hours."); got != 1 {
			t.Fatalf("%s: the answer is shown %d times, want once: %q", locale, got, text)
		}
		private := agentReplyFallback(locale, "chat.agent.only_visible", "Only visible to you")
		if strings.Contains(text, private) {
			t.Fatalf("%s: the agent's own conversation marks an answer %q", locale, private)
		}
		if cards := chatPolishNodesIn(list[0], func(n *xhtml.Node) bool { return chatPolishHasClass(n, "chat-ephemeral") }); len(cards) != 0 {
			t.Fatalf("%s: %d private cards in the agent's own conversation", locale, len(cards))
		}
		// No control leads to the conversation the reader is in.
		self := ChannelReferenceURL(m.SelectedID)
		if links := chatPolishNodesIn(list[0], func(n *xhtml.Node) bool { return n.Data == "a" && chatPolishAttr(n, "href") == self }); len(links) != 0 {
			t.Fatalf("%s: %d links lead to the conversation the reader is already in", locale, len(links))
		}
		saved := agentReplyFallback(locale, "chat.agent.saved_conversation", "Saved in your conversation with")
		if strings.Contains(text, saved) {
			t.Fatalf("%s: the answer says %q inside that very conversation", locale, saved)
		}
		// The one answer is the agent's message, with its badge.
		answers := chatPolishNodesIn(list[0], func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-message-id") == "answer" })
		if len(answers) != 1 || chatPolishHasClass(answers[0], "own") || len(chatPolishNodesIn(answers[0], func(n *xhtml.Node) bool { return chatPolishHasClass(n, "agent-badge") })) == 0 {
			t.Fatalf("%s: the answer is not the agent's one message", locale)
		}
	}

	// A question the agent is still answering shows its state once, as the
	// agent's message, with no card either.
	working := chat4Fixture("en-US", "working2", true)
	rows := personaReplyRowsForPost(working, localUI{}, "question", time.Now())
	if len(rows) != 1 {
		t.Fatalf("%d rows under a question the agent is answering", len(rows))
	}
	if page := chatPolishMarkup(t, rows[0], 1440, "light"); strings.Contains(page, "Only visible to you") || strings.Count(page, `data-agent-reply-state=`) != 1 {
		t.Fatalf("the working state in the agent's own conversation: %s", page)
	}
}

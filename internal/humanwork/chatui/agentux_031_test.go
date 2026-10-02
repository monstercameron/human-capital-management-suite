package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// agentUX031Answer is the stored answer's row in the agent's own conversation.
func agentUX031Answer(t *testing.T, m Model, id string) *xhtml.Node {
	t.Helper()
	markup := chatPolishMarkup(t, Build(m), 1440, "light")
	rows := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-message-id") == id })
	if len(rows) != 1 {
		t.Fatalf("%d rows for message %s", len(rows), id)
	}
	return rows[0]
}

// The stored copy of a private answer, in the asker's conversation with the
// agent, is the agent's message: its name, badge and icon, never the asker's
// own bubble. What a person reads holds no address, and exactly one control
// leads back to the question. A copy stored the old way, as the asker's own
// message ending in a raw address, is drawn the same.
func TestTodo_AGENTUX_031_Browser(t *testing.T) {
	current := chat4Fixture("en-US", "answered", true)

	// The same answer as an earlier build stored it: authored by the asker, with
	// the address written out at the end, attributed to the agent by its receipt.
	earlier := chat4Fixture("en-US", "answered", true)
	earlier.Messages[1].AuthorID, earlier.Messages[1].Author, earlier.Messages[1].PersonaActor = "alice", "Alice", nil
	earlier.Messages[1].Body = "Carry over up to 40 hours.\n\nSources\n- [Paid time off policy](/workspace/app/docs?document=pto)\n\nOpen the source conversation: /chat/share/signed"
	earlier.PersonaPostActors = map[string]PersonaPostActor{"answer": {Display: "Policy Helper", Actor: PersonaActor{PersonaID: "policy-helper", AgentID: "policy-helper", Trusted: true}}}

	for name, m := range map[string]Model{"stored as the agent": current, "stored the old way": earlier} {
		row := agentUX031Answer(t, m, "answer")
		text := chatbug030Text(row)
		if !strings.Contains(text, "Policy Helper") || !strings.Contains(text, "Carry over up to 40 hours.") {
			t.Fatalf("%s: the answer reads %q", name, text)
		}
		if strings.Contains(text, "Alice") && name == "stored the old way" {
			t.Fatalf("%s: the answer is still shown as the asker's: %q", name, text)
		}
		if chatPolishHasClass(row, "own") {
			t.Fatalf("%s: the answer is drawn as the asker's own message", name)
		}
		if badges := chatPolishNodesIn(row, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "agent-badge") }); len(badges) == 0 {
			t.Fatalf("%s: the answer has no Agent badge", name)
		}
		if icons := chatPolishNodesIn(row, func(n *xhtml.Node) bool {
			return chatPolishHasClass(n, "agent-dm-avatar") || chatPolishHasClass(n, "agent-icon")
		}); len(icons) == 0 {
			t.Fatalf("%s: the answer has no agent icon", name)
		}
		// No address in what a person reads.
		for _, path := range []string{"/chat/share/", "Open the source conversation", "chat-agent-question", "/workspace/app/docs"} {
			if strings.Contains(text, path) {
				t.Fatalf("%s: the answer shows %q: %q", name, path, text)
			}
		}
		// Exactly one control leads back to the question.
		back := chatPolishNodesIn(row, func(n *xhtml.Node) bool { return n.Data == "a" && chatPolishAttr(n, "href") == "/chat/share/signed" })
		if len(back) != 1 {
			t.Fatalf("%s: %d controls lead back to the question, want one", name, len(back))
		}
		// And no "Only visible to you" card: the conversation is the person's own.
		if strings.Contains(text, "Only visible to you") || len(chatPolishNodesIn(row, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "chat-ephemeral") })) != 0 {
			t.Fatalf("%s: the stored answer is drawn as a private card: %q", name, text)
		}
	}
}

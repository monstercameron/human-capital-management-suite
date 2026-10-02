package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// TestTodo_AGENTUX_057_Browser draws the composer at the three moments of the
// keyboard sequence, in a channel and in a direct conversation with the agent:
// "@pol" with the list open, the question typed after the agent was picked, and
// the same after a second Enter. The key decisions themselves are in
// TestTodo_AGENTUX_057; this checks what the page offers at each moment: the
// list is open (and named by the field) only while there is something to pick,
// and once the question is in the box the list is gone and Send is enabled, so
// Enter has nothing in its way.
func TestTodo_AGENTUX_057_Browser(t *testing.T) {
	const question = "how many PTO hours carry over?"
	sendEnabled := func(markup string) bool {
		send := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "button" && chatPolishHasClass(n, "send-button") })
		if len(send) != 1 {
			t.Fatalf("the composer has %d send buttons: %s", len(send), markup)
		}
		return chatPolishAttr(send[0], "aria-disabled") != "true"
	}
	field := func(markup string) *xhtml.Node {
		found := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "textarea" && chatPolishAttr(n, "id") == "chat-composer" })
		if len(found) != 1 {
			t.Fatalf("the composer has %d fields", len(found))
		}
		return found[0]
	}
	for _, direct := range []bool{false, true} {
		where := map[bool]string{false: "channel", true: "direct conversation"}[direct]

		// The question is typed after the agent was picked: the agent is a chip
		// beside the field, no list is open, and Send is on.
		m := chat4Fixture("en-US", "sent", direct)
		m.Draft = question
		typed := renderNode(t, composer(m, handlers{composerAgentName: "Policy Helper"}))
		if !strings.Contains(typed, "composer-agent-token") || !strings.Contains(typed, `data-chat-value="`+question+`"`) {
			t.Errorf("%s: the picked agent or the question is not in the composer: %s", where, typed)
		}
		if field(typed).Data != "textarea" || chatPolishAttr(field(typed), "aria-expanded") != "false" || strings.Contains(typed, `id="chat-composer-mentions"`) {
			t.Errorf("%s: a list is open over the typed question: %s", where, typed)
		}
		if !sendEnabled(typed) {
			t.Errorf("%s: Send is off with the question typed", where)
		}
	}

	// In the channel, "@pol" with the list open: the field names the highlighted
	// row, and the row is the agent.
	m := chat4Fixture("en-US", "sent", false)
	m.Draft = "@pol"
	open := mentionState{Target: "chat-composer", Query: "pol", Start: 0, End: 4, Open: true}
	listed := renderNode(t, composer(m, handlers{mentionView: open}))
	f := field(listed)
	if chatPolishAttr(f, "aria-expanded") != "true" || chatPolishAttr(f, "aria-controls") != "chat-composer-mentions" || chatPolishAttr(f, "aria-activedescendant") != "chat-composer-mention-1" {
		t.Fatalf("the field does not point at the open list: %v", f.Attr)
	}
	options := chatPolishNodes(t, listed, func(n *xhtml.Node) bool { return chatPolishAttr(n, "role") == "option" })
	if len(options) == 0 || chatPolishAttr(options[0], "aria-selected") != "true" || !strings.Contains(chatPolishAttr(options[0], "aria-label"), "Policy Helper") {
		t.Fatalf("the highlighted row is not Policy Helper: %s", listed)
	}
}

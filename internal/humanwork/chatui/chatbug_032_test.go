package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func TestTodo_CHATBUG_032(t *testing.T) {
	// RED: the narrow-container rule hid the separator in front of the member
	// count, so the type and the count ran together at phone width.
	if strings.Contains(Stylesheet, ".topic-count>.topic-sep{display:none}") {
		t.Fatal("the member-count separator is still hidden at narrow widths")
	}
	// The card is a link, and a link is inline-block under the workspace's
	// generic anchor rule; it must stay a grid so its three parts stack.
	if got := chatbugCascadeValue(ChatMsgListStyles, ".chat-workspace a.chat-embed", "display"); got != "grid" {
		t.Fatalf("a.chat-embed display = %q, want grid", got)
	}
	for _, selector := range []string{".chat-embed-label", ".chat-embed-source", ".chat-embed-byline"} {
		if got := chatbugCascadeValue(ChatMsgListStyles, selector, "display"); got != "block" {
			t.Fatalf("%s display = %q, want block", selector, got)
		}
	}
}

func TestTodo_CHATBUG_032_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chat4Fixture(locale, "sent", false)
		card := docPreviewCard(m, DocPreview{ID: "d1", State: "ready", Readable: true, Title: "2026 holiday guide", Owner: "Walt Brennan", UpdatedAt: "Oct 1", Snippet: "Offices are closed."})
		markup := chatPolishMarkup(t, card, 390, "light")
		anchors := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "a" && chatPolishHasClass(n, "chat-embed") })
		if len(anchors) != 1 {
			t.Fatalf("%s: %d cards", locale, len(anchors))
		}
		parts := map[string]string{}
		for _, class := range []string{"chat-embed-label", "chat-embed-source", "chat-embed-byline"} {
			found := chatPolishNodesIn(anchors[0], func(n *xhtml.Node) bool { return chatPolishHasClass(n, class) })
			if len(found) != 1 || found[0].Parent != anchors[0] {
				t.Fatalf("%s: %s is not one direct part of the card", locale, class)
			}
			parts[class] = chatbug030Text(found[0])
		}
		if parts["chat-embed-label"] != m.t(KeyDocEmbedTitle) || parts["chat-embed-source"] != "2026 holiday guide" || parts["chat-embed-byline"] != "Walt Brennan · Oct 1" {
			t.Fatalf("%s: parts %q", locale, parts)
		}
		// Whatever flattens the card (copy, a reader) still sees three words groups
		// with whitespace between them, not "Linked document2026 holiday guideWalt".
		flat := chatbug030Text(anchors[0])
		if strings.Contains(flat, parts["chat-embed-label"]+parts["chat-embed-source"]) || strings.Contains(flat, parts["chat-embed-source"]+parts["chat-embed-byline"]) {
			t.Fatalf("%s: the card's text runs its parts together: %q", locale, flat)
		}
	}
	// The conversation header: type, member count and agent count, separated.
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, width := range []int{1440, 800, 390, 320} {
			m := chat4Fixture(locale, "sent", false)
			if len(chatConversationAgents(m)) == 0 {
				t.Fatal("fixture has no agent in the conversation")
			}
			markup := chatPolishMarkup(t, timeline(m, handlers{}), width, "light")
			topics := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "p" && chatPolishHasClass(n, "conversation-topic") })
			if len(topics) != 1 {
				t.Fatalf("%s/%d: %d topic lines", locale, width, len(topics))
			}
			if strings.Count(chatbug030Text(topics[0]), " · ") != 2 {
				t.Fatalf("%s/%d: topic line %q does not separate type, member count and agent count", locale, width, chatbug030Text(topics[0]))
			}
			seps := chatPolishNodesIn(topics[0], func(n *xhtml.Node) bool { return chatPolishHasClass(n, "topic-sep") })
			if len(seps) != 2 {
				t.Fatalf("%s/%d: %d separators", locale, width, len(seps))
			}
		}
	}
	// No narrow-width rule may hide a separator.
	for _, css := range []string{Stylesheet, AgentUXChat4Styles, AgentUXChat5Styles, ChatMsgListStyles} {
		if strings.Contains(css, ".topic-sep{display:none}") {
			t.Fatal("a rule hides the topic separator")
		}
	}
}

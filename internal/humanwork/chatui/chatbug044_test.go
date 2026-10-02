package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func chatbug044Elements(n *xhtml.Node) []*xhtml.Node {
	var out []*xhtml.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode {
			out = append(out, c)
		}
	}
	return out
}

func chatbug044Model() Model {
	m := agentUXMentionModel(PersonaLookupReady)
	m.ResolvedPersonaMentions = append(m.ResolvedPersonaMentions,
		ResolvedPersonaMention{Reference: ChatReference{Kind: "AGENT_MENTION", TenantID: "tenant", ID: "assistant", Display: "Assistant", ConversationID: "room"}, Handle: "assistant", Purpose: "Answers everyday questions about the workspace, its people and how work gets done here, in plain language that is long enough to need a third line"},
		ResolvedPersonaMention{Reference: ChatReference{Kind: "AGENT_MENTION", TenantID: "tenant", ID: "benefits-guide", Display: "Benefits Guide", ConversationID: "room"}, Handle: "benefits-guide", Purpose: "Explains benefits"},
	)
	return m
}

func TestTodo_CHATBUG_044(t *testing.T) {
	markup := MentionMenuMarkupForTest(t, chatbug044Model(), false)

	menus := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "id") == "chat-composer-mentions" })
	if len(menus) != 1 || !chatPolishHasClass(menus[0], "mention-menu-split") {
		t.Fatalf("%d split mention menus, want one", len(menus))
	}
	// The menu is a list above a footer: the hint is not inside what scrolls.
	parts := chatbug044Elements(menus[0])
	if len(parts) != 2 || !chatPolishHasClass(parts[0], "mention-list") || !chatPolishHasClass(parts[1], "mention-hint") {
		t.Fatalf("the menu is not a list followed by a hint line: %d parts", len(parts))
	}
	for _, option := range chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "role") == "option" }) {
		inList := false
		for p := option.Parent; p != nil; p = p.Parent {
			inList = inList || chatPolishHasClass(p, "mention-list")
			if chatPolishHasClass(p, "mention-hint") {
				t.Fatal("an option is inside the hint line")
			}
		}
		if !inList {
			t.Fatal("an option is outside the scrolling list")
		}
	}

	// Each agent is one row: the option (icon, name and handle, Agent badge, the
	// description) and then the information button, both children of one grid.
	rows := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "mention-agent-row") })
	if len(rows) != 3 {
		t.Fatalf("%d agent rows, want 3", len(rows))
	}
	for _, row := range rows {
		grids := chatbug044Elements(row)
		if len(grids) != 1 || !chatPolishHasClass(grids[0], "mention-agent-option") {
			t.Fatal("an agent row is not one mention-agent-option grid")
		}
		cells := chatbug044Elements(grids[0])
		if len(cells) != 2 || !chatPolishHasClass(cells[0], "persona") || !chatPolishHasClass(cells[1], "mention-agent-info") {
			t.Fatal("the grid is not the option followed by the information button")
		}
		parts := chatbug044Elements(cells[0])
		var classes []string
		for _, part := range parts {
			classes = append(classes, chatPolishAttr(part, "class"))
		}
		joined := strings.Join(classes, "|")
		if len(parts) < 4 || !strings.Contains(joined, "mention-agent-identity") || !strings.Contains(joined, "agent-badge") || !strings.Contains(joined, "mention-purpose") {
			t.Fatalf("the option does not hold icon, identity, badge and description: %s", joined)
		}
		if strings.Index(joined, "mention-agent-identity") > strings.Index(joined, "agent-badge") || strings.Index(joined, "agent-badge") > strings.Index(joined, "mention-purpose") {
			t.Fatalf("the option's parts are not in the order identity, badge, description: %s", joined)
		}
		identity := chatbug044Elements(parts[1])
		if len(identity) != 2 || !chatPolishHasClass(identity[0], "mention-name") || !chatPolishHasClass(identity[1], "mention-handle") {
			t.Fatal("the identity is not the name and then the handle")
		}
	}

	// The styles: a grid with named areas, the information button centred on the
	// row, the hint a footer that takes its own space, the list the scroller, and
	// the description limited to two lines.
	css := ChatBug044Styles
	for _, want := range []string{
		`.mention-menu .mention-agent-option>.mention-option.persona{display:grid;grid-template-columns:auto minmax(0,1fr) auto;grid-template-areas:"icon identity badge" "icon purpose purpose" "icon attribution attribution"`,
		`.mention-menu .mention-agent-option{display:grid;grid-template-columns:minmax(0,1fr) 44px;align-items:center`,
		`.mention-agent-info{align-self:center`,
		`.mention-menu.mention-menu-split{overflow:hidden`,
		`.mention-menu-split>.mention-list{display:flex;flex-direction:column;flex:1 1 auto;min-block-size:0;overflow-y:auto`,
		`.mention-menu.mention-menu-split>.mention-hint{position:static`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("the mention menu styles do not hold %q", want)
		}
	}
	if !strings.Contains(PersonaProfileStyles, "-webkit-line-clamp:2") {
		t.Error("the description is not limited to two lines")
	}
	if !strings.Contains(Stylesheet, css) || strings.Index(Stylesheet, css) < strings.LastIndex(Stylesheet, ".mention-hint{position:sticky") {
		t.Error("the new menu styles are missing from the page stylesheet or come before the sticky hint they replace")
	}
	if strings.Contains(css, "position:sticky") {
		t.Error("the hint line overlays the list")
	}

	// Details open: the profile card is inside the list too, under its row.
	details := MentionMenuMarkupForTest(t, chatbug044Model(), true)
	cards := chatPolishNodes(t, details, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "mention-agent-preview") })
	if len(cards) != 1 {
		t.Fatalf("%d profile cards with details open, want 1", len(cards))
	}
	for p := cards[0].Parent; p != nil; p = p.Parent {
		if chatPolishHasClass(p, "mention-hint") {
			t.Fatal("the profile card is inside the hint line")
		}
	}
}

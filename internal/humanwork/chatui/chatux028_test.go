package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// chatux028Row is the buttons of the card's action row, by what they do, with
// the "…" button named "menu".
func chatux028Row(card *xhtml.Node) []string {
	var row []string
	for _, actions := range chatux003Find(card, "agent-reply-actions") {
		for _, n := range chatPolishNodesIn(actions, func(n *xhtml.Node) bool { return n.Data == "button" }) {
			row = append(row, chatPolishAttr(n, "data-action"))
		}
	}
	return row
}

// A shared answer's card keeps three actions in its row and the "…" button; the
// way to the shared copy and the way to take it back are items of that menu.
func TestTodo_CHATUX_028(t *testing.T) {
	shared := func(m *Model) {
		m.AgentShare = map[string]AgentShareState{"run": {Status: AgentShareShared, PostID: "copy"}}
		m.MenuID = ""
	}
	card, _ := chatux026Card(t, "en-US", localUI{}, shared)
	row := chatux028Row(card)
	if strings.Join(row, ",") != "agent-feedback,agent-feedback,agent-follow-up,menu" {
		t.Fatalf("the shared card's row holds %v, want Helpful, Not right, Ask a follow-up and the menu", row)
	}
	if len(chatux003Find(card, "agent-share-view")) != 0 || len(chatux003Find(card, "agent-share-remove")) != 0 {
		t.Fatal("the share controls are still in the card while the menu is closed")
	}
	// The status mark stays at the top: "Shared with #general".
	if note := chatux003Find(card, "agent-reply-private"); len(note) != 1 || !strings.Contains(chatbug030Text(note[0]), "Shared with") {
		t.Fatalf("the header does not say the answer is shared: %v", note)
	}

	// Open, the menu holds both, and the saved copy's link; pressing one closes it.
	open, _ := chatux026Card(t, "en-US", localUI{}, func(m *Model) {
		shared(m)
		m.MenuID = "agent-card:answer"
	})
	menus := chatux003Find(open, "agent-reply-menu")
	if len(menus) != 1 || chatPolishAttr(menus[0], "role") != "menu" {
		t.Fatalf("%d menus on the open card", len(menus))
	}
	var items []string
	for _, item := range chatPolishNodesIn(menus[0], func(n *xhtml.Node) bool { return chatPolishAttr(n, "role") == "menuitem" }) {
		items = append(items, strings.TrimSpace(chatbug030Text(item)))
	}
	if len(items) != 3 || items[0] != "View shared answer" || items[1] != "Remove shared answer" || !strings.HasPrefix(items[2], "Saved in your conversation with") {
		t.Fatalf("the menu holds %q", items)
	}
	if got := chatux028Row(open); strings.Join(got, ",") != "agent-feedback,agent-feedback,agent-follow-up,menu" {
		t.Fatalf("with the menu open the row still holds %v", got)
	}

	// A shared answer whose copy cannot be named offers no removal, and the
	// "…" button is still there for the way to the copy.
	noCopy, _ := chatux026Card(t, "en-US", localUI{}, func(m *Model) {
		m.AgentShare = map[string]AgentShareState{"run": {Status: AgentShareShared}}
		m.MenuID = ""
		m.PersonaInvocations[0].Projection.PrivateReplyHref = ""
	})
	if row := chatux028Row(noCopy); strings.Join(row, ",") != "agent-feedback,agent-feedback,agent-follow-up,menu" {
		t.Fatalf("a shared answer with no known copy has the row %v", row)
	}

	// An answer that is not shared shows no way to a shared copy anywhere.
	private, _ := chatux026Card(t, "en-US", localUI{}, func(m *Model) { m.MenuID = "agent-card:answer" })
	if len(chatux003Find(private, "agent-share-view")) != 0 || len(chatux003Find(private, "agent-share-remove")) != 0 {
		t.Fatal("a private answer offers to view or remove a shared copy")
	}

	// A removal that is under way or failed is said in the row, where it is seen with the menu closed.
	removing, _ := chatux026Card(t, "en-US", localUI{}, func(m *Model) {
		m.AgentShare = map[string]AgentShareState{"run": {Status: AgentShareRemoving, PostID: "copy"}}
		m.MenuID = ""
	})
	if note := chatux003Find(removing, "agent-reply-share-note"); len(note) != 1 || !strings.Contains(chatbug030Text(note[0]), "Removing…") {
		t.Fatalf("a removal under way is not said in the row: %v", note)
	}

	// Using an item closes the menu.
	var opened []string
	model := chat4Fixture("en-US", "answered", false)
	model.Callbacks.OpenMenu = func(id string) { opened = append(opened, id) }
	chatux028CloseMenu(model)
	chatux028CloseMenu(Model{})
	if len(opened) != 1 || opened[0] != "" {
		t.Fatalf("closing the menu called OpenMenu with %q", opened)
	}
}

// Three languages, and the phone: the icon buttons keep their names, and the
// stylesheet keeps the row to one line at that width.
func TestTodo_CHATUX_028_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		{
			model := func(m *Model) {
				m.AgentShare = map[string]AgentShareState{"run": {Status: AgentShareShared, PostID: "copy"}}
				m.MenuID = ""
			}
			card, text := chatux026Card(t, locale, localUI{}, model)
			row := chatux028Row(card)
			if len(row) != 4 || row[3] != "menu" {
				t.Fatalf("%s: the row holds %v, want three actions and the menu", locale, row)
			}
			// Every control of the row has a name and a tooltip for when its words are hidden.
			for _, n := range chatPolishNodesIn(card, func(n *xhtml.Node) bool {
				return n.Data == "button" && chatPolishHasClass(n, "agent-feedback-button") && chatPolishAttr(n, "data-action") != "agent-follow-up"
			}) {
				if chatPolishAttr(n, "aria-label") == "" || chatPolishAttr(n, "title") != chatPolishAttr(n, "aria-label") || chatPolishAttr(n, "aria-pressed") == "" {
					t.Fatalf("%s: the rating button %q has no name, no tooltip or no state", locale, chatbug030Text(n))
				}
			}
			if strings.Contains(text, "agentux") || strings.Contains(text, "chatux") || strings.Contains(text, "⟦") {
				t.Fatalf("%s: a key is on the card: %q", locale, text)
			}
			// "Shared with #general" is the status mark in the header.
			if note := chatux003Find(card, "agent-reply-private"); len(note) != 1 || chatPolishAttr(note[0], "data-visibility") != "shared" {
				t.Fatalf("%s: the header does not carry the shared status mark: %v", locale, note)
			}
			// The menu's items are named in the reader's language.
			open, _ := chatux026Card(t, locale, localUI{}, func(m *Model) {
				model(m)
				m.MenuID = "agent-card:answer"
			})
			names := map[string]string{"en-US": "View shared answer", "de-DE": "Geteilte Antwort ansehen", "ar": ""}
			if view := chatux003Find(open, "agent-share-view"); len(view) != 1 || chatPolishAttr(view[0], "role") != "menuitem" || chatbug030Text(view[0]) == "" || (names[locale] != "" && !strings.Contains(chatbug030Text(view[0]), names[locale])) {
				t.Fatalf("%s: View shared answer is not a named menu item: %v", locale, view)
			}
		}
	}
	for _, want := range []string{
		"@media(max-width:480px){.agent-reply-actions .agent-feedback{flex-wrap:nowrap}",
		".agent-reply-actions .agent-feedback .agent-feedback-button>span,.agent-reply-actions .agent-reply-share>span{position:absolute;width:1px;height:1px",
		".agent-reply-actions .agent-follow-up{flex:1 1 0;min-width:0",
		".agent-reply-actions .agent-reply-share-note{flex:1 0 100%}",
		"@media(pointer:coarse) and (max-width:480px){.agent-reply-actions .agent-feedback .agent-feedback-button",
	} {
		if !strings.Contains(Stylesheet, want) {
			t.Fatalf("the stylesheet lacks the phone rule %q", want)
		}
	}
}

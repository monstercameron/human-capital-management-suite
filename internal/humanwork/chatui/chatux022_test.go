package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// chatux022Rows renders a message's menu and returns its rows in order: the
// visible label of each row, "---" for a divider, and "(bar)" after a row the
// stylesheet hides while the hover bar carries the same action.
func chatux022Rows(t *testing.T, m Model, msg Message) []string {
	t.Helper()
	markup := renderNode(t, spanOf(chatMessageMenuItems(m, msg)))
	var rows []string
	for _, n := range chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
		return chatPolishAttr(n, "role") == "menuitem" || chatPolishAttr(n, "role") == "separator"
	}) {
		if chatPolishAttr(n, "role") == "separator" {
			rows = append(rows, "---")
			continue
		}
		label := strings.TrimSpace(chatbug030Text(n))
		if chatPolishHasClass(n, "menu-bar-twin") || chatPolishHasClass(n, "chatsave-action") {
			label += " (bar)"
		}
		rows = append(rows, label)
	}
	return rows
}

func chatux022Model() Model {
	m := Model{
		State: StateReady, Locale: "en-US", SelectedID: "room", CurrentUser: "walt", CurrentTenantID: "tenant",
		Conversations: []Conversation{{ID: "room", Name: "general", Kind: PublicChannel, Joined: true}},
	}
	m.Callbacks = Callbacks{OpenThread: func(string) {}, CopyLink: func(string) {}, CopyContents: func(string) {}, OpenShare: func(string) {},
		BeginEdit: func(string) {}, Pin: func(string) {}, DeleteMessage: func(string, uint64) {}}
	return m
}

// TestTodo_CHATUX_022 holds the message menu to what the hover bar does not
// carry, in the order the todo gives.
func TestTodo_CHATUX_022(t *testing.T) {
	own := Message{ID: "m1", AuthorID: "walt", Author: "Walt Brennan", Body: "Text", Revision: 1}
	other := Message{ID: "m2", AuthorID: "loretta", Author: "Loretta Haynes", Body: "Text", Revision: 1}
	join := func(rows []string) string { return strings.Join(rows, " | ") }

	t.Run("own message", func(t *testing.T) {
		m := chatux022Model()
		want := "Reply in thread | Copy link | Copy text | Share to channel | Pin message (bar) | Save for later (bar) | Edit message | --- | Delete message"
		if got := join(chatux022Rows(t, m, own)); got != want {
			t.Fatalf("menu rows:\n got %s\nwant %s", got, want)
		}
		marked := ""
		m.Callbacks.MarkUnreadFrom = func(id string) { marked = id }
		m.Messages = []Message{own}
		if got := join(chatux022Rows(t, m, own)); !strings.HasPrefix(got, "Reply in thread | Mark unread from here | Copy link | ") {
			t.Fatalf("Mark unread from here is not the second row: %s", got)
		}
		m.act("mark-unread", "m1")
		if marked != "m1" {
			t.Fatalf("Mark unread from here asked for %q, want m1", marked)
		}
		marked = ""
		m.act("mark-unread", "not-on-the-page")
		if marked != "" {
			t.Fatal("a message that is not in the conversation was marked unread")
		}
	})

	t.Run("someone else's message", func(t *testing.T) {
		m := chatux022Model()
		got := join(chatux022Rows(t, m, other))
		if !strings.HasPrefix(got, "Reply in thread | Copy link | Copy text | Share to channel | Pin message (bar) | Save for later (bar) | --- | ") || strings.Contains(got, "Edit message") || strings.Contains(got, "Delete message") {
			t.Fatalf("menu rows of someone else's message: %s", got)
		}
	})

	t.Run("Message language has its own divider", func(t *testing.T) {
		m, german, _, _ := chatlangModel("en-US")
		m.CurrentUser = "hans"
		m.Callbacks = chatux022Model().Callbacks
		got := join(chatux022Rows(t, m, german))
		if !strings.Contains(got, " | Edit message | --- | Message language… | --- | Delete message") {
			t.Fatalf("Message language is not between dividers, after Edit and before Delete: %s", got)
		}
	})

	t.Run("no thread, no Reply in thread", func(t *testing.T) {
		m := chatux022Model()
		m.Conversations[0].Agent = true
		if got := join(chatux022Rows(t, m, own)); strings.Contains(got, "Reply in thread") {
			t.Fatalf("a conversation with an agent has no threads, and the menu offers one: %s", got)
		}
		m = chatux022Model()
		m.MenuID = "thread:m1"
		got := join(chatux022Rows(t, m, own))
		if strings.Contains(got, "Reply in thread") || !strings.Contains(got, "| Pin message |") {
			t.Fatalf("the menu of a reply in the thread pane must not offer a thread and must show Pin, which its bar lacks: %s", got)
		}
	})

	t.Run("Pin and Save are shown only where there is no bar for them", func(t *testing.T) {
		const twins = ".message-menu :is(.menu-bar-twin,.menu-item.chatsave-action)"
		if !strings.HasPrefix(ChatUX022Styles, "\n"+twins+"{display:none}\n") {
			t.Fatalf("Pin and Save are not hidden in the menu by default: %s", ChatUX022Styles)
		}
		for _, context := range []string{"@media(max-width:767px),(pointer:coarse){" + twins + "{display:flex}}", "@container chatmain (max-width:560px){" + twins + "{display:flex}}"} {
			if !strings.Contains(Stylesheet, context) {
				t.Errorf("the stylesheet does not show Pin and Save in the menu under %s", context[:strings.Index(context, "{")])
			}
		}
		// The same narrow column is where the bar gives them up.
		if !strings.Contains(Stylesheet, `@container chatmain (max-width:560px){
.chat-workspace .message-list .message-actions :is(.quick-react,.chatsave-action,[data-action="pin"],[data-action="unpin"]){display:none}`) {
			t.Error("the bar does not give up Pin and Save in the column where the menu takes them")
		}
	})
}

// TestTodo_CHATUX_022_Browser renders the menu in the three languages with a
// catalog that answers every key with a bracketed key.
func TestTodo_CHATUX_022_Browser(t *testing.T) {
	own := Message{ID: "m1", AuthorID: "walt", Author: "Walt Brennan", Body: "Text", Revision: 1}
	for locale, want := range map[string][2]string{
		"en-US": {"Copy text", "Mark unread from here"},
		"de-DE": {"Text kopieren", "Ab hier als ungelesen markieren"},
		"ar":    {"نسخ النص", "تحديد كغير مقروء من هنا"},
	} {
		m := chatux022Model()
		m.Locale = locale
		m.Text = func(key string) string { return "⟦" + key + "⟧" }
		m.Callbacks.MarkUnreadFrom = func(string) {}
		rows := strings.Join(chatux022Rows(t, m, own), " | ")
		if strings.Contains(rows, "⟦") {
			t.Errorf("%s: the menu prints a copy key: %s", locale, rows)
		}
		if !strings.Contains(rows, "| "+want[1]+" |") || !strings.Contains(rows, "| "+want[0]+" |") {
			t.Errorf("%s: want the rows %q and %q: %s", locale, want[1], want[0], rows)
		}
	}
}

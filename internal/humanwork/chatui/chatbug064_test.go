package chatui

import (
	"os"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func chatbug064Model() (Model, Message) {
	msg := Message{ID: "m1", AuthorID: "ben", Author: "Ben", Body: "plan"}
	m := Model{State: StateReady, Locale: "en-US", CurrentUser: "walt", CurrentTenantID: "t", SelectedID: "room", MenuID: "m1",
		Conversations: []Conversation{{ID: "room", Name: "general", Kind: PublicChannel, Joined: true}}, Messages: []Message{msg},
		Callbacks: Callbacks{ReactWith: func(string, string) {}, OpenPicker: func(string) {}, OpenThread: func(string) {}, CopyLink: func(string) {}, Pin: func(string) {}, OpenMenu: func(string) {}}}
	return m, msg
}

// TestTodo_CHATBUG_064 reads the sheet a phone opens for a message: the
// quick reactions and Add reaction come first, then Reply in thread and Save,
// then Copy link and the rest. The reaction row is not drawn above 768 px, where
// the hover bar carries it.
func TestTodo_CHATBUG_064(t *testing.T) {
	m, msg := chatbug064Model()
	items := chatMessageMenuItems(m, msg)
	root, err := xhtml.Parse(strings.NewReader(renderNode(t, spanOf(items))))
	if err != nil {
		t.Fatal(err)
	}
	row := chatPolishNodesIn(root, func(n *xhtml.Node) bool { return strings.Contains(chatPolishAttr(n, "class"), "menu-reaction-row") })
	if len(row) != 1 {
		t.Fatalf("%d reaction rows", len(row))
	}
	var emoji []string
	for _, button := range chatPolishNodesIn(row[0], func(n *xhtml.Node) bool { return n.Data == "button" }) {
		switch chatPolishAttr(button, "data-action") {
		case "react-with":
			emoji = append(emoji, chatPolishAttr(button, "data-emoji"))
		case "react-pick":
			emoji = append(emoji, "+")
		}
		if chatPolishAttr(button, "data-id") != "m1" || chatPolishAttr(button, "aria-label") == "" {
			t.Errorf("a reaction button names no message or no action: %v", button.Attr)
		}
	}
	want := append(append([]string{}, chatEmojiQuickReactions()...), "+")
	if strings.Join(emoji, " ") != strings.Join(want, " ") {
		t.Fatalf("the row holds %v, want the quick reactions and Add reaction %v", emoji, want)
	}
	// The row is the first thing in the menu; Reply in thread and Save are in it.
	first := items[0]
	if markup := renderNode(t, first); !strings.Contains(markup, "menu-reaction-row") {
		t.Fatalf("the menu starts with %s", markup)
	}
	markup := renderNode(t, spanOf(items))
	for _, want := range []string{`data-action="reply"`, `data-saved-action="save"`, `data-action="copy-link"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("the sheet lacks %s", want)
		}
	}
	// Where nothing can be reacted to there is no row, and a menu that asks
	// about deleting is only the question.
	none := m
	none.Callbacks.ReactWith, none.Callbacks.OpenPicker = nil, nil
	if strings.Contains(renderNode(t, spanOf(chatMessageMenuItems(none, msg))), "menu-reaction-row") {
		t.Error("a row of reactions is offered where there is nothing to react with")
	}
	asking := m
	asking.deleteAsk = "m1"
	if strings.Contains(renderNode(t, spanOf(chatMessageMenuItems(asking, Message{ID: "m1", AuthorID: "walt", Body: "x"}))), "menu-reaction-row") {
		t.Error("the delete question carries a row of reactions")
	}
}

// TestTodo_CHATBUG_064_Browser holds what the styles do with the sheet below
// 768 px, and the two other findings of the entry: the search icon takes focus
// into the search box, and the drawer is flush with the screen edge.
func TestTodo_CHATBUG_064_Browser(t *testing.T) {
	sheet := Stylesheet
	start := strings.Index(sheet, chatbug064Styles)
	if start < 0 {
		t.Fatal("the sheet's styles are not in the stylesheet")
	}
	if !strings.HasPrefix(chatbug064Styles, `.chat-workspace .message-menu .menu-reaction-row{display:none}`) {
		t.Fatal("the reaction row must be hidden above the phone breakpoint")
	}
	phone := chatbug064Styles[strings.Index(chatbug064Styles, "@media(max-width:767px){"):]
	for _, want := range []string{
		`.chat-workspace .message-menu{position:fixed!important;top:auto!important;bottom:0!important;left:0!important;right:0!important;width:100%!important`,
		`.menu-reaction-row{display:flex;order:-3`,
		`[data-action="reply"]{order:-2}`,
		`.menu-item.chatsave-action{order:-1}`,
		`.chat-workspace .message-menu .menu-item{min-height:48px`,
	} {
		if !strings.Contains(phone, want) {
			t.Errorf("the phone sheet lacks %q", want)
		}
	}
	// Reaction row, then Reply, then Save, ahead of every row that keeps its order of 0.
	if strings.Index(phone, "order:-3") > strings.Index(phone, "order:-2") || strings.Index(phone, "order:-2") > strings.Index(phone, "order:-1") {
		t.Error("the sheet's rows are out of order")
	}
	if !strings.Contains(sheet, `.chat-workspace[data-sidebar-open="true"] .chat-rail{left:calc(-1 * var(--chat-edge-start,0px))}`) {
		t.Error("the drawer is not pulled out to the screen edge")
	}
	for file, want := range map[string][]string{
		"mobile_rail_focus_js.go": {"mobileRailFocusSearch = false", `"--chat-edge-start"`},
		"search_box_focus_js.go":  {"mobileRailFocusSearch = true"},
		"chatux010_js.go":         {"mobileRailFocusSearch = true"},
	} {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range want {
			if !strings.Contains(string(source), text) {
				t.Errorf("%s no longer contains %q: the search icon and shortcut must put focus in the search box", file, text)
			}
		}
	}
}

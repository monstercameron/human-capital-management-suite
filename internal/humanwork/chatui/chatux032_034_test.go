package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func chatux032ThreadModel() Model {
	m := chatux022Model()
	parent := Message{ID: "p1", AuthorID: "walt", Author: "Walt Brennan", Body: "Parent", Revision: 1, Replies: 1}
	m.Messages = []Message{parent}
	m.ShowThread, m.ThreadParentID, m.ThreadParent = true, "p1", &parent
	m.ThreadMessages = []Message{{ID: "r1", AuthorID: "walt", Author: "Walt Brennan", Body: "A reply", Revision: 1}}
	m.Callbacks.ReplyInThread = func(string, string) {}
	m.Callbacks.SendMessage = func(string, string) {}
	return m
}

// TestTodo_CHATUX_032 pins the thread's reply composer: one field, one tool row
// that holds the paperclip-less tools, "Also send to #channel" and Reply, the
// formatting row closed whatever the viewer chose for the channel composer, and
// one type size for the cards a thread draws.
func TestTodo_CHATUX_032_Thread(t *testing.T) {
	m := chatux032ThreadModel()
	m.ComposerFormatRow = composerFormatShown
	markup := renderNode(t, threadPane(m, handlers{}))

	form := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "thread-composer") })
	if len(form) != 1 {
		t.Fatalf("%d thread composers", len(form))
	}
	if got := chatPolishAttr(form[0], "data-format-row"); got != composerFormatHidden {
		t.Errorf("the thread's formatting row is %q when the channel composer's choice is shown; want it closed", got)
	}
	toggle := chatPolishNodesIn(form[0], func(n *xhtml.Node) bool { return chatPolishHasClass(n, "composer-format-toggle") })
	if len(toggle) != 1 || chatPolishAttr(toggle[0], "aria-pressed") != "false" {
		t.Errorf("the Aa toggle of a closed thread row is not unpressed: %v", toggle)
	}

	// The choice to also send to the channel is on the tool row, with Reply, and
	// is not a row of its own.
	toolbar := chatPolishNodesIn(form[0], func(n *xhtml.Node) bool { return chatPolishHasClass(n, "composer-toolbar") })
	if len(toolbar) != 1 {
		t.Fatalf("%d tool rows in the thread composer", len(toolbar))
	}
	also := chatPolishNodesIn(toolbar[0], func(n *xhtml.Node) bool { return chatPolishHasClass(n, "thread-also") })
	reply := chatPolishNodesIn(toolbar[0], func(n *xhtml.Node) bool { return chatPolishHasClass(n, "send-button") })
	if len(also) != 1 || len(reply) != 1 {
		t.Fatalf("the tool row holds %d Also-send choices and %d Reply buttons", len(also), len(reply))
	}
	if all := chatPolishNodesIn(form[0], func(n *xhtml.Node) bool { return chatPolishHasClass(n, "thread-also") }); len(all) != 1 {
		t.Errorf("Also send appears %d times in the composer", len(all))
	}
	if !strings.Contains(markup, "Also send to #general") {
		t.Errorf("the choice is not worded for the channel: %s", markup)
	}

	// Opening it is the session's choice, and only the thread's.
	local := localStore{box: &localUI{}}
	chatux032ThreadFormatChoose(local)
	if chatux032ThreadFormatState(local.get()) != composerFormatShown || local.get().formatRow != "" {
		t.Errorf("opening the thread's row: thread %q, channel %q", local.get().threadFormatRow, local.get().formatRow)
	}
	opened := renderNode(t, threadPane(m, handlers{local: local.get()}))
	if !strings.Contains(opened, `data-format-row="shown"`) {
		t.Errorf("the opened thread row is not shown: %s", opened)
	}
	chatux032ThreadFormatChoose(local)
	if chatux032ThreadFormatState(local.get()) != composerFormatHidden {
		t.Errorf("the second press did not close it")
	}

	// One size for the text of a card, in a thread and in the conversation.
	for _, want := range []string{".chat-embed-body{font-size:.875rem", ".chat-workspace .thread-composer .composer-toolbar{flex-wrap:wrap"} {
		if !strings.Contains(Stylesheet, want) {
			t.Errorf("the stylesheet lacks %q", want)
		}
	}
}

// TestTodo_CHATBUG_089_Card: a document card ends its text at a sentence or a
// word with one ellipsis and offers Show more, which holds the whole text.
func TestTodo_CHATBUG_089_Card(t *testing.T) {
	long := "Full-time employees accrue paid time off (PTO) every pay period. The accrual rate rises with tenure: 15 days a year in years one to three, 20 days a year in years four to seven, and 25 days after that..."
	short, full, clamped := docSnippetParts(long)
	if !clamped || short != "Full-time employees accrue paid time off (PTO) every pay period…" {
		t.Errorf("the closed text is %q (clamped %v)", short, clamped)
	}
	if !strings.HasSuffix(full, "after that…") || strings.Contains(full, "...") {
		t.Errorf("the whole text is %q", full)
	}
	words := strings.Repeat("alpha beta gamma ", 20)
	short, _, clamped = docSnippetParts(words)
	if !clamped || !strings.HasSuffix(short, "…") || strings.Contains(strings.TrimSuffix(short, "…"), "…") || strings.HasSuffix(strings.TrimSuffix(short, "…"), "alph") {
		t.Errorf("a text with no sentence ends at a word: %q", short)
	}
	if got, _, clamped := docSnippetParts("Offices are closed."); clamped || got != "Offices are closed." {
		t.Errorf("a short snippet changed to %q (clamped %v)", got, clamped)
	}
	if got, _, clamped := docSnippetParts("Offices are closed on listed days..."); clamped || got != "Offices are closed on listed days…" {
		t.Errorf("a service cut was not made one ellipsis: %q", got)
	}

	m := Model{Locale: "en-US", EmbedOrigin: "https://hcm.example", DocPreviews: map[string]DocPreview{
		"d1": {ID: "d1", State: "ready", Readable: true, Title: "2026 holiday guide", Owner: "Walt Brennan", UpdatedAt: "Oct 1", Snippet: long}}}
	card := renderNode(t, spanOf(docPreviewEmbedsFor(m, "doc:d1", false)))
	for _, want := range []string{"2026 holiday guide", "Show more", "Show less", "chat-embed-more", "Full-time employees accrue paid time off (PTO) every pay period…"} {
		if !strings.Contains(card, want) {
			t.Errorf("the card lacks %q: %s", want, card)
		}
	}
	// The thread names the document on its card.
	pane := renderNode(t, threadPane(func() Model {
		tm := chatux032ThreadModel()
		tm.DocPreviews, tm.EmbedOrigin = m.DocPreviews, m.EmbedOrigin
		tm.ThreadMessages[0].Body = "See doc:d1"
		return tm
	}(), handlers{}))
	if !strings.Contains(pane, `class="chat-embed-source"`) || !strings.Contains(pane, "2026 holiday guide") {
		t.Errorf("the thread's card does not name the document: %s", pane)
	}
}

// TestTodo_CHATUX_034 pins the small details of the composer menus and the
// message toolbar.
func TestTodo_CHATUX_034(t *testing.T) {
	// The emoji preview is the name; the shortcode is not printed. The skin-tone
	// control has its name beside it.
	withEmojiHost(t, emojiModel("en-US"), func(local *localUI) {
		markup := emojiMarkup(t, emojiModel("en-US"), emojiOpenState("chat-composer", false, false, false), "emoji")
		preview := emojiNodes(t, markup, func(n *xhtml.Node) bool { return emojiHasClass(n, "emoji-pop-preview") })
		if len(preview) != 1 {
			t.Fatalf("%d previews", len(preview))
		}
		if text := nodeText(preview[0]); strings.Contains(text, ":") || text == "" {
			t.Errorf("the emoji preview is %q; want a name with no :shortcode:", nodeText(preview[0]))
		}
		if len(emojiNodes(t, markup, func(n *xhtml.Node) bool { return emojiHasClass(n, "emoji-pop-code") })) != 0 {
			t.Errorf("a shortcode line is drawn")
		}
		if !strings.Contains(markup, `class="emoji-pop-tone-label"`) || !strings.Contains(markup, ">Skin tone<") {
			t.Errorf("the skin-tone control is not labelled: %s", markup)
		}
	})

	// A "/" row is one line; its hint shows on the highlighted row only. The
	// footer reads as words.
	m := chat4Fixture("en-US", "sent", false)
	menu := renderNode(t, composerCommandMenuView(m, composerCommandMenu{Target: "chat-composer", Open: true}, "chat-composer", 1))
	if !strings.Contains(menu, "Enter or Tab to choose") || strings.Contains(menu, "Tab details") {
		t.Errorf("the command footer is not plain words: %s", menu)
	}
	for _, want := range []string{
		`.chat-workspace .command-option .command-args{display:none}`,
		`.chat-workspace .command-option:is(.active,[aria-selected="true"]) .command-args{display:block}`,
		`.chat-workspace .composer-add-item{min-block-size:56px`,
		`.chatux002-layer .section-create-trigger>.chat-icon{display:none}`,
		`.chat-workspace .message-actions .chatsave-action[aria-pressed="true"]{color:var(--hcm-color-text)}`,
		`.chat-workspace .emoji-pop .emoji-pop-input:is(:focus,:focus-visible){outline:0`,
	} {
		if !strings.Contains(Stylesheet, want) {
			t.Errorf("the stylesheet lacks %q", want)
		}
	}
	if got := mentionHintText("en-US", false); got != "↑↓ to move · Enter to insert · Tab for details · Esc to close" {
		t.Errorf("the people list's footer is %q", got)
	}

	// The Add menu's attachment row is one line; its limits are the tooltip's.
	am := composerToolsModel("en-US", false)
	am.Chatattach001 = &Chatattach001Composer{Choose: func() {}}
	add := composerToolsMarkup(t, am, localUI{})
	rows := chatPolishNodes(t, add, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "composer-add-item") })
	if len(rows) < 3 {
		t.Fatalf("%d Add menu rows", len(rows))
	}
	for _, row := range rows {
		note := chatPolishNodesIn(row, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "composer-add-note") })
		if len(note) != 1 || len([]rune(nodeText(note[0]))) > 50 {
			t.Errorf("an Add menu description is not one short line: %q", nodeText(note[0]))
		}
	}
	if !strings.Contains(add, "up to 20 MB each") || strings.Contains(nodeText(rows[0]), "20 MB") {
		t.Errorf("the attachment limits are not moved to the tooltip: %s", nodeText(rows[0]))
	}

	// "Message language…" has a language icon; a saved message's bookmark is
	// filled, not coloured; Create a channel and New section differ.
	for _, name := range []string{"language", "hash", "section-new"} {
		if _, ok := iconPaths[name]; !ok {
			t.Errorf("icon %q is not drawn", name)
		}
	}
	if iconPaths["hash"] == iconPaths["section-new"] || iconPaths["plus"] == iconPaths["section-new"] {
		t.Errorf("the add menu's icons are not distinct")
	}
	lm, german, _, _ := chatlangModel("en-US")
	lm.CurrentUser = "hans"
	lang := renderNode(t, spanOf(chatlangMenuItems(lm, german)))
	if !strings.Contains(lang, "icon-language") || strings.Contains(lang, "icon-info") {
		t.Errorf("Message language… is not drawn with the language icon: %s", lang)
	}
}

func nodeText(n *xhtml.Node) string {
	var out strings.Builder
	var walk func(*xhtml.Node)
	walk = func(c *xhtml.Node) {
		if c.Type == xhtml.TextNode {
			out.WriteString(c.Data)
		}
		for ch := c.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return strings.TrimSpace(out.String())
}

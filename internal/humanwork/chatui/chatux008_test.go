package chatui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	xhtml "golang.org/x/net/html"
)

// chatux008Model is an open thread in #random with two replies.
func chatux008Model(followed bool) Model {
	room := Conversation{ID: "random", Name: "random", Kind: PublicChannel, Joined: true}
	return Model{State: StateReady, Locale: "en-US", SelectedID: room.ID, CurrentUser: "jake", ShowThread: true, ThreadParentID: "root", ThreadFollowed: followed,
		Text:          func(key string) string { return englishCopy[key] },
		Conversations: []Conversation{room},
		Messages:      []Message{{ID: "root", AuthorID: "walt", Author: "Walt Brennan", Body: "Who has the Q3 numbers?", Replies: 2}},
		ThreadMessages: []Message{
			{ID: "r1", AuthorID: "jake", Author: "Jake Sullivan", Body: "I do"},
			{ID: "r2", AuthorID: "walt", Author: "Walt Brennan", Body: "Thanks"},
		},
		Callbacks: Callbacks{SetThreadFollow: func(bool) {}, CloseThread: func() {}, ReplyInThread: func(string, string) {}},
	}
}

func chatux008Pane(t *testing.T, m Model) string {
	t.Helper()
	return renderNode(t, threadPane(m, handlers{}))
}

func TestTodo_CHATUX_008(t *testing.T) {
	m := chatux008Model(false)
	off := chatux008Pane(t, m)

	// The header reads "Thread" with the channel as a link under it.
	heading := off[strings.Index(off, `class="side-heading chat-panel-head thread-heading"`):strings.Index(off, `class="thread-scroll"`)]
	chatux005Before(t, heading, "<h2>Thread</h2>", `class="thread-channel-link"`, `data-action="follow"`, `data-action="close-thread"`)
	if strings.Contains(off, "Thread · ") {
		t.Errorf("the header still reads Thread · name: %s", heading)
	}
	for _, want := range []string{`href="/workspace/app/chat#channel=random"`, `data-action="thread-channel"`, "⁨#random⁩</a>"} {
		if !strings.Contains(heading, want) {
			t.Errorf("the channel link misses %q: %s", want, heading)
		}
	}

	// The bell is the Follow action named for what it does: pressed when on, the
	// same accessible name in both states, a tooltip saying what it will do.
	if !strings.Contains(heading, `aria-label="Notify me about replies"`) || !strings.Contains(heading, `aria-pressed="false"`) || !strings.Contains(heading, `icon-bell`) ||
		!strings.Contains(heading, `title="Get a notification for each new reply in this thread"`) {
		t.Errorf("bell (off): %s", heading)
	}
	on := chatux008Pane(t, chatux008Model(true))
	onHeading := on[strings.Index(on, `class="side-heading chat-panel-head thread-heading"`):strings.Index(on, `class="thread-scroll"`)]
	if !strings.Contains(onHeading, `aria-label="Notify me about replies"`) || !strings.Contains(onHeading, `aria-pressed="true"`) || !strings.Contains(onHeading, `title="Following: you get a notification for each reply. Select to stop."`) {
		t.Errorf("bell (on): %s", onHeading)
	}
	for _, gone := range []string{">Follow<", ">Following<"} {
		if strings.Contains(off, gone) || strings.Contains(on, gone) {
			t.Errorf("a bare %q button is still in the header", gone)
		}
	}
	unwired := m
	unwired.Callbacks.SetThreadFollow = nil
	if got := chatux008Pane(t, unwired); !strings.Contains(got[strings.Index(got, "thread-notify"):], "disabled") {
		t.Error("the bell is enabled with nothing behind it")
	}

	// The reply box says where the reply goes, in a channel and in a direct message.
	if !strings.Contains(off, `placeholder="Reply in thread"`) || strings.Contains(off, "Reply to everyone") {
		t.Errorf("reply box: %s", off[strings.Index(off, "<textarea"):][:200])
	}
	direct := m
	direct.Conversations = []Conversation{{ID: "random", Name: "Walt Brennan", Kind: DirectMessage}}
	if got := chatux008Pane(t, direct); !strings.Contains(got, `placeholder="Reply in thread"`) || !strings.Contains(got, "⁨Walt Brennan⁩</a>") {
		t.Errorf("direct message pane: %s", got[:600])
	}

	// The service has no also-send-to-channel option on a thread reply, so the pane
	// offers none: no checkbox that does nothing. If the request ever gains one,
	// this fails and the checkbox "Also send to #channel" is to be added.
	for _, field := range reflect.VisibleFields(reflect.TypeOf(chat.SendPostRequest{})) {
		name := strings.ToLower(field.Name)
		if strings.Contains(name, "broadcast") || strings.Contains(name, "alsosend") || strings.Contains(name, "alsopost") || strings.Contains(name, "tochannel") {
			t.Fatalf("SendPostRequest now has %s: add the checkbox \"Also send to #channel\" to the thread reply box", field.Name)
		}
	}
	if strings.Contains(off, `type="checkbox"`) || strings.Contains(off, "Also send") {
		t.Errorf("the reply box offers a checkbox the service cannot honour")
	}

	// The parent message is set apart from the replies, with the count as the divider.
	chatux005Before(t, off, `class="thread-root"`, `class="thread-count"`, "2 replies", `class="thread-replies"`)
	if !strings.Contains(off, `class="thread-count" role="separator"`) {
		t.Errorf("the reply count is not a separator")
	}
	if got := chatux008Pane(t, func() Model { v := m; v.ThreadMessages = nil; return v }()); strings.Contains(got, `class="thread-count"`) || !strings.Contains(got, "thread-empty") {
		t.Errorf("a thread with no replies draws a divider with no count")
	}

	// The three languages, with no key or marker in any of them.
	for locale, want := range map[string][4]string{
		"en-US": {"Notify me about replies", "Reply in thread", "Get a notification", "Following:"},
		"de-DE": {"Über Antworten benachrichtigen", "Im Thread antworten", "Bei jeder neuen Antwort", "Folge ich:"},
		"ar":    {"أبلغني بالردود", "الرد في السلسلة", "احصل على إشعار", "تتابع:"},
	} {
		model := chatux008Model(false)
		model.Locale = locale
		markup := chatux008Pane(t, model) + chatux008Pane(t, func() Model { v := chatux008Model(true); v.Locale = locale; return v }())
		for _, text := range want {
			if !strings.Contains(markup, text) {
				t.Errorf("%s: missing %q", locale, text)
			}
		}
		if strings.Contains(markup, "⟦") || strings.Contains(markup, "chat.ux008") {
			t.Errorf("%s: a copy key reaches the page", locale)
		}
	}
	if chatux008Click(ui.MouseEvent{}, m) {
		t.Error("a click with no thread channel link was taken as one")
	}
}

func TestTodo_CHATUX_008_Accessibility(t *testing.T) {
	for _, followed := range []bool{false, true} {
		markup := chatux008Pane(t, chatux008Model(followed))
		root, err := xhtml.Parse(strings.NewReader(markup))
		if err != nil {
			t.Fatal(err)
		}
		var h2, bell, link, textarea, labelFor *xhtml.Node
		ids := map[string]bool{}
		walkChat5HTML(root, func(n *xhtml.Node) {
			if n.Type != xhtml.ElementNode {
				return
			}
			if id := chat5Attr(n, "id"); id != "" {
				if ids[id] {
					t.Errorf("duplicate id %q", id)
				}
				ids[id] = true
			}
			switch {
			case n.Data == "h2":
				h2 = n
			case n.Data == "button" && strings.Contains(chat5Attr(n, "class"), "thread-notify"):
				bell = n
			case n.Data == "a" && strings.Contains(chat5Attr(n, "class"), "thread-channel-link"):
				link = n
			case n.Data == "textarea" && chat5Attr(n, "id") == "thread-composer":
				textarea = n
			case n.Data == "label" && chat5Attr(n, "for") == "thread-composer":
				labelFor = n
			}
		})
		if h2 == nil || chat5NodeText(h2) != "Thread" {
			t.Errorf("followed=%v: the pane's heading is %v", followed, h2)
		}
		if bell == nil || chat5Attr(bell, "type") != "button" || chat5Attr(bell, "aria-label") != "Notify me about replies" || chat5Attr(bell, "aria-pressed") != map[bool]string{true: "true", false: "false"}[followed] || chat5Attr(bell, "title") == "" {
			t.Errorf("followed=%v: the bell is not a named toggle button with a tooltip: %v", followed, bell)
		}
		// The glyph is hidden from assistive technology; the name is the label.
		if bell != nil {
			hidden := false
			walkChat5HTML(bell, func(n *xhtml.Node) {
				hidden = hidden || (n.Type == xhtml.ElementNode && n.Data == "svg" && chat5Attr(n, "aria-hidden") == "true")
			})
			if !hidden {
				t.Errorf("followed=%v: the bell's glyph is announced", followed)
			}
		}
		if link == nil || strings.TrimSpace(chat5NodeText(link)) == "" || chat5Attr(link, "href") == "" {
			t.Errorf("followed=%v: the channel link has no text or no address", followed)
		}
		if textarea == nil || labelFor == nil || chat5Attr(textarea, "placeholder") != "Reply in thread" {
			t.Errorf("followed=%v: the reply box is not a labelled field that says where it goes", followed)
		}
		// The divider is a separator that carries its text.
		walkChat5HTML(root, func(n *xhtml.Node) {
			if n.Type == xhtml.ElementNode && strings.Contains(chat5Attr(n, "class"), "thread-count") && (chat5Attr(n, "role") != "separator" || strings.TrimSpace(chat5NodeText(n)) != "2 replies") {
				t.Errorf("followed=%v: the divider is %q with role %q", followed, chat5NodeText(n), chat5Attr(n, "role"))
			}
		})
	}
}

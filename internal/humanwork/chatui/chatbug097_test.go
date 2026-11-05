package chatui

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestTodo_CHATBUG_097: a thread opens with the whole parent at the top and
// the replies under it; the thread composer's tools fit its width; no element
// id appears twice with a thread open; date chips and hover toolbars never
// overlap the header or each other; on a phone the thread takes the screen
// with a back control.
func TestTodo_CHATBUG_097(t *testing.T) {
	threadModel := func() (Model, Message) {
		m, german, _, _ := chatlangModel("en-US")
		m.SelectedID, m.CurrentUser = "room", "eve"
		m.Conversations = []Conversation{{ID: "room", Name: "General", Kind: PublicChannel, Joined: true, MemberCount: 18}}
		parent := german
		parent.TimeLabel = "09:41"
		parent.Attachments = []Attachment{{ID: "a1", Name: "plan.png", ContentType: "image/png", Width: 320, Height: 240, Bytes: 9000}}
		parent.Chips = []ReactionChip{{Emoji: "tada", Count: 2, Mine: true}}
		m.Messages = []Message{parent}
		m.ShowThread, m.ThreadParentID = true, parent.ID
		parent.Replies = 6
		m.ThreadMessages = []Message{{ID: "r1", AuthorID: "mona", Author: "Mona", Body: "On it.", TimeLabel: "09:52"}}
		m.Callbacks = walkCallbacks()
		return m, parent
	}
	threadLocal := localUI{chatlang: chatlangLocal{}.toggleOriginal("de")}

	t.Run("ThreadOpensAtParent", func(t *testing.T) {
		// The scroller carries the thread's anchor, and the client holds a
		// new thread at its top: the parent whole (author, time, text,
		// attachments, reactions), the replies under it.
		m, parent := threadModel()
		pane := renderNode(t, threadPane(m, handlers{local: threadLocal}))
		if !strings.Contains(pane, `data-chat-thread-anchor="room:de"`) {
			t.Errorf("the thread scroller lost its anchor: %s", pane)
		}
		source, err := os.ReadFile("fieldsync_js.go")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`"thread-scroll"`, `list.Set("scrollTop", 0)`, `"__chatNearBottom", false`} {
			if !strings.Contains(string(source), want) {
				t.Errorf("the client no longer holds a new thread at its top (%q)", want)
			}
		}
		root := pane[strings.Index(pane, `class="thread-root"`):]
		for _, want := range []string{"Hans", "09:41", chatlangEnglish, "attachment-image", "reaction-count"} {
			if !strings.Contains(root, want) {
				t.Errorf("the open thread's parent misses %q: %s", want, root)
			}
		}
		if strings.Index(pane, "thread-replies") < strings.Index(pane, "thread-root") {
			t.Error("the replies are not drawn under the parent")
		}
		_ = parent
	})

	t.Run("ThreadComposerToolsFit", func(t *testing.T) {
		// The thread composer's tools wrap inside the narrow pane instead
		// of sliding under Reply; the bar itself already wraps there.
		for _, want := range []string{
			`.chat-workspace .thread-composer .composer-tools{flex-wrap:wrap!important}`,
			`.chat-workspace .thread-composer .composer-toolbar{flex-wrap:wrap`,
		} {
			if !strings.Contains(Stylesheet, want) {
				t.Errorf("the stylesheet misses %q", want)
			}
		}
		m, _ := threadModel()
		pane := renderNode(t, threadPane(m, handlers{}))
		toolbar := pane[strings.Index(pane, "thread-toolbar"):]
		if !strings.Contains(pane, "composer-tools") || !strings.Contains(toolbar, "send-button") {
			t.Errorf("the thread toolbar lost its tools or its Reply: %s", toolbar)
		}
	})

	t.Run("NoDuplicateIds", func(t *testing.T) {
		// The parent is drawn twice with a thread open: in the timeline and
		// in the pane. The pane's copy namespaces its ids, so every id in
		// the document is still unique and every toggle controls its own.
		m, parent := threadModel()
		h := handlers{local: threadLocal}
		joined := renderNode(t, message(m, h, parent, false)) + "\n" + renderNode(t, threadPane(m, h))
		ids := map[string]int{}
		// Element ids only: data-id and aria references are message keys by
		// design and repeat across the two copies.
		for _, match := range regexp.MustCompile(`\sid="([^"]+)"`).FindAllStringSubmatch(joined, -1) {
			ids[match[1]]++
		}
		for id, n := range ids {
			if n > 1 {
				t.Errorf("id %q appears %d times with a thread open", id, n)
			}
		}
		if !strings.Contains(joined, `id="thread-chatlang-original-de"`) {
			t.Errorf("the pane's original is not namespaced: %s", joined)
		}
		for _, controls := range regexp.MustCompile(`aria-controls="([^"]*chatlang-original[^"]*)"`).FindAllStringSubmatch(joined, -1) {
			if ids[controls[1]] != 1 {
				t.Errorf("a toggle controls %q, which is not exactly once in the document", controls[1])
			}
		}
	})

	t.Run("DividerToolbarOverlap", func(t *testing.T) {
		// A message right under a date or unread divider keeps its hover bar
		// inside its own row, like the list's first message, in plain and
		// virtualised lists; landings clear the header fade.
		for _, want := range []string{
			`.chat-workspace .message-list>.day-divider + .message .message-actions`,
			`.chat-workspace .message-list>.unread-divider + .message .message-actions`,
			`.chat-workspace .message-list .virtual-row>.day-divider + .message .message-actions`,
			`.message-list{scroll-padding-top:20px}`,
		} {
			if !strings.Contains(Stylesheet, want) {
				t.Errorf("the stylesheet misses %q", want)
			}
		}
	})

	t.Run("PhoneThread", func(t *testing.T) {
		// The reply count is a pressable button that opens the thread; on a
		// phone the open thread is the whole screen with its back control.
		m, parent := threadModel()
		row := renderNode(t, message(m, handlers{}, parent, false))
		at := strings.Index(row, "message-stats")
		button := ""
		if at >= 0 {
			button = row[strings.LastIndex(row[:at], "<button"):]
			button = button[:strings.Index(button, ">")+1]
		}
		if !strings.Contains(button, `data-action="stats"`) || strings.Contains(button, "disabled") {
			t.Errorf("the reply count is not a pressable thread button: %s", button)
		}
		for _, want := range []string{
			`.chat-side.thread-pane{position:fixed;inset:0;width:100%;max-width:none;z-index:1300`,
			`.thread-back-label{display:inline}`,
			`data-action="close-thread"`,
		} {
			if !strings.Contains(Stylesheet+renderNode(t, threadPane(m, handlers{})), want) {
				t.Errorf("the phone thread misses %q", want)
			}
		}
		pane := renderNode(t, threadPane(m, handlers{}))
		closeAt := strings.Index(pane, `data-action="close-thread"`)
		closeButton := pane[strings.LastIndex(pane[:closeAt], "<button"):]
		closeButton = closeButton[:strings.Index(closeButton, ">")+1]
		if !strings.Contains(closeButton, `aria-label="`) {
			t.Errorf("the thread back control has no accessible name: %s", closeButton)
		}
	})
}

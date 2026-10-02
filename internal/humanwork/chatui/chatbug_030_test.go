package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func TestTodo_CHATBUG_030(t *testing.T) {
	// A menu stays open only for its own layer kind.
	for kind, want := range map[string]bool{"": false, "menu": false, "saved": true, "search": true, "reaction": true, "emoji": true, "todo": true, "poll": true, "details": true} {
		if got := messageMenuClosesFor(kind); got != want {
			t.Fatalf("messageMenuClosesFor(%q) = %v, want %v", kind, got, want)
		}
	}
	// A remembered row with no element, or that is neither hovered nor focused,
	// no longer owns an action bar; one that is held keeps it; none is not stale.
	for _, tc := range []struct {
		row          string
		exists, held bool
		want         bool
	}{{"", false, false, false}, {"a", false, false, true}, {"a", true, false, true}, {"a", true, true, false}, {"a", false, true, true}} {
		if got := chatRowActionsStale(tc.row, tc.exists, tc.held); got != tc.want {
			t.Fatalf("chatRowActionsStale(%+v) = %v, want %v", tc, got, tc.want)
		}
	}
	// RED: only .thread-message hid its bar, so the thread's parent message drew a
	// card with one bookmark button over the pane until something was hovered.
	for _, selector := range []string{".thread-message .message-actions", ".thread-root .message-actions"} {
		if got := chatbugCascadeValue(ChatPolishStyles, selector, "visibility"); got != "hidden" {
			t.Fatalf("%s visibility = %q, want hidden", selector, got)
		}
	}
	for _, selector := range []string{".thread-root:hover .message-actions", ".thread-root:focus-within .message-actions", ".thread-root:has(.message-menu) .message-actions"} {
		if got := chatbugCascadeValue(ChatPolishStyles, selector, "visibility"); got != "visible" {
			t.Fatalf("%s visibility = %q, want visible", selector, got)
		}
	}
}

func chatbug030Destructive(t *testing.T, m Model, msg Message) (labels []string, deletes, removes int) {
	t.Helper()
	_, entries := chatbugMenuMarkup(t, m, msg)
	for _, n := range entries {
		if !chatPolishHasClass(n, "danger") {
			continue
		}
		text := chatbug030Text(n)
		labels = append(labels, text)
		if chatPolishAttr(n, "data-action") == "delete" {
			deletes++
		}
		if strings.Contains(chatPolishAttr(n, "data-chatremove-open"), "action=remove") {
			removes++
		}
	}
	return labels, deletes, removes
}

func chatbug030Text(n *xhtml.Node) string {
	var out strings.Builder
	var walk func(*xhtml.Node)
	walk = func(c *xhtml.Node) {
		if c.Type == xhtml.TextNode {
			out.WriteString(c.Data)
		}
		for k := c.FirstChild; k != nil; k = k.NextSibling {
			walk(k)
		}
	}
	walk(n)
	return strings.TrimSpace(out.String())
}

func TestTodo_CHATBUG_030_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chat4Fixture(locale, "sent", false)
		m.Callbacks.DeleteMessage = func(string, uint64) {}
		own := Message{ID: "own", AuthorID: m.CurrentUser, Body: "mine"}
		other := Message{ID: "other", AuthorID: "bob", Author: "Bob", Body: "theirs"}
		everyone := chatremoveText(locale, "remove_everyone")
		if everyone == "" || everyone == chatremoveText(locale, "remove") || everyone == m.t(KeyDelete) {
			t.Fatalf("%s: the removal command %q is not distinct from Remove message / Delete message", locale, everyone)
		}
		for _, admin := range []bool{false, true} {
			m.IsTenantAdmin = admin
			// Your own message: one destructive command, Delete message, however
			// much removal permission you hold.
			labels, deletes, removes := chatbug030Destructive(t, m, own)
			if len(labels) != 1 || deletes != 1 || removes != 0 || labels[0] != m.t(KeyDelete) {
				t.Fatalf("%s admin=%v: own message destructive entries %q (delete=%d remove=%d)", locale, admin, labels, deletes, removes)
			}
			// Somebody else's: Report, and Remove for everyone only with permission.
			labels, deletes, removes = chatbug030Destructive(t, m, other)
			if deletes != 0 {
				t.Fatalf("%s admin=%v: Delete message offered on someone else's message", locale, admin)
			}
			if admin {
				if removes != 1 || labels[len(labels)-1] != everyone {
					t.Fatalf("%s: admin entries %q, want the last to be %q", locale, labels, everyone)
				}
			} else if removes != 0 {
				t.Fatalf("%s: Remove offered without the permission: %q", locale, labels)
			}
			for _, label := range labels {
				if label == chatremoveText(locale, "remove") {
					t.Fatalf("%s admin=%v: the ambiguous command %q is still offered", locale, admin, label)
				}
			}
		}
		// The channel owner holds the permission too, on someone else's message only.
		m.IsTenantAdmin = false
		m.Conversations[0].OwnerID = m.CurrentUser
		if _, deletes, removes := chatbug030Destructive(t, m, other); deletes != 0 || removes != 1 {
			t.Fatalf("%s: channel owner delete=%d remove=%d on someone else's message", locale, deletes, removes)
		}
		if _, deletes, removes := chatbug030Destructive(t, m, own); deletes != 1 || removes != 0 {
			t.Fatalf("%s: channel owner delete=%d remove=%d on their own message", locale, deletes, removes)
		}
	}
	// The thread pane's parent message: its bar is in the tree (for hover and
	// focus) but the stylesheet keeps it out of sight until then.
	m := chat4Fixture("en-US", "sent", false)
	m.Callbacks.OpenMenu = func(string) {}
	m.ShowThread, m.ThreadParentID = true, "question"
	thread := chatPolishMarkup(t, threadPane(m, handlers{}), 1440, "light")
	bars := chatPolishNodes(t, thread, func(n *xhtml.Node) bool {
		return n.Data == "div" && chatPolishHasClass(n, "message-actions") && chatPolishAncestor(n, "thread-root")
	})
	if len(bars) != 1 {
		t.Fatalf("%d bars on the thread's parent message", len(bars))
	}
	if got := chatbugCascadeValue(ChatPolishStyles, ".thread-root .message-actions", "visibility"); got != "hidden" {
		t.Fatalf("the parent message's bar is %q at rest", got)
	}
}

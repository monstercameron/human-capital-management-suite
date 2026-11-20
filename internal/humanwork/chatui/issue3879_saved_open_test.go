package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// Issue 3879: a saved message's way into its conversation is a named button,
// not a focusable list item, and no control sits inside another.
func TestIssue3879_SavedOpenIsANamedButton(t *testing.T) {
	want := map[string]string{"en-US": "Open in the conversation", "de-DE": "In der Unterhaltung öffnen", "ar": "فتح في المحادثة"}
	for locale, label := range want {
		view := chatsave002View("all", chatsave002Rows())
		view.Locale = locale
		markup := renderNode(t, RenderSavedMessages(view))
		items := chatsave002Find(t, markup, func(n *xhtml.Node) bool {
			return n.Data == "li" && chatsave002Class(n, "chatsave-item") && chatPolishAttr(n, "data-saved-item") == "true"
		})
		if len(items) != 4 {
			t.Fatalf("%s: want 4 items, got %d", locale, len(items))
		}
		for _, item := range items {
			if chatPolishAttr(item, "tabindex") != "" || chatPolishAttr(item, "title") != "" || chatPolishAttr(item, "role") != "" {
				t.Errorf("%s: the list item must not be the focusable control: %v", locale, item.Attr)
			}
		}
		opens := chatsave002Find(t, markup, func(n *xhtml.Node) bool {
			return n.Data == "button" && chatPolishAttr(n, "data-saved-action") == "open"
		})
		if len(opens) != 3 {
			t.Fatalf("%s: want one Open button per readable item, got %d: %s", locale, len(opens), markup)
		}
		for _, open := range opens {
			if chatPolishAttr(open, "aria-label") != label || chatPolishAttr(open, "type") != "button" || chatPolishAttr(open, "data-saved-post") == "" {
				t.Errorf("%s: Open button is not named or typed: %v", locale, open.Attr)
			}
			for p := open.Parent; p != nil; p = p.Parent {
				if p.Type == xhtml.ElementNode && (p.Data == "button" || p.Data == "a") {
					t.Errorf("%s: Open button is nested inside <%s>", locale, p.Data)
				}
			}
		}
		if strings.Count(markup, `class="chatsave-actions"`) < 1 {
			t.Errorf("%s: the note, reminder, done and remove controls are gone", locale)
		}
	}
}

// Issue 3878: the empty-state link names the tab it selects, so the browser
// client can move focus there when the link is removed by that selection.
func TestIssue3878_EmptyStateLinkNamesItsTab(t *testing.T) {
	for locale, label := range map[string]string{"en-US": "Show To do", "de-DE": "Zu erledigen anzeigen", "ar": "عرض للتنفيذ"} {
		view := chatsave002View("done", nil)
		view.Locale = locale
		view.TodoCount, view.DoneCount, view.AllCount = 1, 0, 1
		markup := renderNode(t, RenderSavedMessages(view))
		links := chatsave002Find(t, markup, func(n *xhtml.Node) bool { return n.Data == "button" && chatsave002Class(n, "chatsave-empty-link") })
		if len(links) != 1 || chatPolishAttr(links[0], "data-saved-tab") != "todo" || !strings.Contains(markup, ">"+label+"<") {
			t.Fatalf("%s: empty Done tab must offer %q selecting todo: %s", locale, label, markup)
		}
	}
}

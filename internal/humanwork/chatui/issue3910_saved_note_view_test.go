package chatui

import (
	"testing"

	xhtml "golang.org/x/net/html"
)

// Issue 3910: whichever Saved filter is showing, an item that is being written
// on keeps its note editor, so the browser client can carry the typed text over.
func TestIssue3910_NoteEditorIsRenderedOnEveryFilterThatShowsTheItem(t *testing.T) {
	rows := chatsave002Rows()
	for _, tab := range []string{"todo", "all"} {
		view := chatsave002View(tab, rows)
		view.EditingNote = SavedItemKey("tenant", "general", "p1")
		markup := renderNode(t, RenderSavedMessages(view))
		fields := chatsave002Find(t, markup, func(n *xhtml.Node) bool {
			return n.Data == "input" && chatPolishAttr(n, "id") == "chatsave-note-field-p1"
		})
		if len(fields) != 1 {
			t.Fatalf("%s: want the note editor for the item being written on, got %d: %s", tab, len(fields), markup)
		}
		if chatPolishAttr(fields[0], "data-chat-value") != "ask Ben on Friday" {
			t.Errorf("%s: the editor must carry the saved note so an unchanged field is not mistaken for a draft: %v", tab, fields[0].Attr)
		}
	}
}

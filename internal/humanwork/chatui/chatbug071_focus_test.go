package chatui

import (
	"strings"
	"testing"
)

// TestTodo_CHATBUG_071_AddPeopleFocus: the Add people dialog opens with the
// cursor in "Search by name", so typing goes into it. The page's dialog focus
// finds the dialog by class, so the dialog must be one the selector names, and
// the field must be the one it focuses.
func TestTodo_CHATBUG_071_AddPeopleFocus(t *testing.T) {
	m := chatux002Model("en-US")
	m.ShowAddMembers = true
	m.Callbacks.AddMembers = func([]string) {}
	m.Callbacks.CloseAddMembers = func() {}
	markup := renderNode(t, addMembersDialog(m, handlers{}))
	at := strings.Index(markup, `id="new-chat-member-search"`)
	if at < 0 {
		t.Fatalf("no search field in the dialog: %s", markup)
	}
	tag := markup[strings.LastIndex(markup[:at], "<") : at+strings.Index(markup[at:], ">")+1]
	if !strings.Contains(tag, "autofocus") || !strings.Contains(tag, `placeholder="Search by name"`) {
		t.Fatalf("the search field does not take the cursor: %s", tag)
	}
	if !strings.Contains(markup, "add-members-dialog") || !strings.Contains(chatFocusDialogSelector, ".add-members-dialog") {
		t.Fatal("the page's dialog focus does not know the Add people dialog")
	}
	// The create dialog keeps its own first field.
	m.ShowAddMembers = false
	create := renderNode(t, memberPicker(m, handlers{}, GroupChat))
	if strings.Contains(create, "autofocus") {
		t.Fatalf("the create dialog's people field steals the cursor from the name: %s", create)
	}
}

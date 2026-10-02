package chatui_test

import (
	"strings"
	"testing"

	gwchtml "github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATBUG_078_Browser draws the Saved panel with a deleted message and
// checks the page the person sees: the To do heading counts what can still be
// done, the deleted message sits under All with a control that takes it off,
// and while the Moderation page shows, exactly one sidebar row is drawn as
// selected.
func TestTodo_CHATBUG_078_Browser(t *testing.T) {
	rows := []chatui.SavedMessageRow{
		{TenantID: "t", ConversationID: "general", PostID: "p1", AuthorID: "ben", Author: "Ben Whitaker", Channel: "general", InChannel: true, Sequence: 4, Availability: "readable", Body: "keep this"},
		{TenantID: "t", ConversationID: "general", PostID: "p2", Availability: "deleted"},
	}
	render := func(tab string, rows []chatui.SavedMessageRow) string {
		view := chatui.SavedMessagesView{Locale: "en-US", Tab: tab, Rows: rows, Model: chatui.Model{SelectedID: "general", CurrentUser: "walt", CurrentTenantID: "t"}}
		markup, err := ui.RenderToString(gwchtml.Div(gwchtml.Props{}, chatui.RenderSavedMessages(view)))
		if err != nil {
			t.Fatal(err)
		}
		return markup
	}
	all := render("all", rows)
	if !strings.Contains(all, "1 to do") {
		t.Fatalf("the heading must count only what can still be done:\n%s", all)
	}
	if !strings.Contains(all, "This message was deleted.") || !strings.Contains(all, `data-saved-action="remove"`) {
		t.Fatalf("a deleted message stays under All with a remove control:\n%s", all)
	}
	if todo := render("todo", rows[:1]); strings.Contains(todo, "This message was deleted.") {
		t.Fatal("To do lists a deleted message")
	}

	sheet := chatui.ScopedStylesheet()
	for _, want := range []string{
		`:has([data-chatremove-overlay="page"]) .chat-row.selected`,
		`:has([data-chatremove-overlay="page"]) .chatmod005-row`,
		`.chatsave-sidebar-row[aria-expanded=true]{background:transparent;color:var(--accent);font-weight:600}`,
	} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("the stylesheet has no rule %q: with the Moderation page open the conversation row must stop looking selected and the Moderation row must look selected", want)
		}
	}
}

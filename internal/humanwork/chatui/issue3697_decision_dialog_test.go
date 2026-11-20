package chatui

import (
	stdhtml "html"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func issue3697Item(state, kind string, restorable bool) chat.ModerationItem {
	at := time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)
	return chat.ModerationItem{ID: "filter:7", Kind: kind, State: state, ConversationID: "general", ConversationName: "general", PostID: "p5", AuthorID: "jake", At: at,
		Reason: "spam", Message: chat.Post{ID: "p5", Body: "the words", CreatedAt: at}, CanRemove: true, CanRestore: restorable}
}

// TestIssue3697_DismissAndRestoreAskFirst: Dismiss and Restore of an item that
// has a message open the decision dialog (where an optional note can be
// written, and Cancel leaves the item alone) instead of deciding on the press.
func TestIssue3697_DismissAndRestoreAskFirst(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		open := stdhtml.UnescapeString(chatremoveMarkup(t, ModerationPage(ModerationPageModel{Locale: locale, State: StateReady, Items: []chat.ModerationItem{issue3697Item("OPEN", "filter", false)}, OpenCount: 1})))
		if strings.Contains(open, `data-chatremove-act="dismiss"`) || !strings.Contains(open, "action=dismiss") || !strings.Contains(open, "case=filter%3A7") {
			t.Errorf("%s: Dismiss still decides on the press: %s", locale, open)
		}
		resolved := issue3697Item("CLOSED", "removal", true)
		resolved.Message.Deleted = true
		done := stdhtml.UnescapeString(chatremoveMarkup(t, ModerationPage(ModerationPageModel{Locale: locale, State: StateReady, Tab: "resolved", Items: []chat.ModerationItem{resolved}})))
		if strings.Contains(done, `data-chatremove-act="restore"`) || !strings.Contains(done, "action=restore") || !strings.Contains(done, "case=filter%3A7") {
			t.Errorf("%s: Restore still decides on the press: %s", locale, done)
		}
	}
}

// TestIssue3697_ItemWithoutMessageKeepsDirectDismiss: an item no message could
// be matched to has no author to record the decision against and no message to
// show in a dialog, so its Dismiss stays the one press it was.
func TestIssue3697_ItemWithoutMessageKeepsDirectDismiss(t *testing.T) {
	item := issue3697Item("OPEN", "filter", false)
	item.PostID, item.Message = "", chat.Post{}
	markup := chatremoveMarkup(t, ModerationPage(ModerationPageModel{Locale: "en-US", State: StateReady, Items: []chat.ModerationItem{item}, OpenCount: 1}))
	if !strings.Contains(markup, `data-chatremove-act="dismiss"`) {
		t.Fatalf("an item with no message lost its Dismiss: %s", markup)
	}
}

// TestIssue3697_DismissDialogOffersAnOptionalNote: the dialog names the author,
// shows the message, has a note that is not required, a button that records the
// decision, and Cancel, in all three languages.
func TestIssue3697_DismissDialogOffersAnOptionalNote(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		text := func(key string) string { return chatremoveText(locale, key) }
		markup := stdhtml.UnescapeString(chatremoveMarkup(t, ModerationDialog(ModerationDialogModel{
			Model: Model{Locale: locale}, Action: "dismiss", CaseID: "filter:7",
			Selection: chat.RemovalSelection{ConversationID: "general", PostIDs: []string{"p5"}},
			Target:    ModerationTargetView{AuthorID: "jake", AuthorName: "Jake Sullivan", Body: "the words"},
		})))
		for _, want := range []string{
			strings.ReplaceAll(text("dismiss_title"), "{name}", "Jake Sullivan"),
			strings.ReplaceAll(text("dismiss_help"), "{name}", "Jake Sullivan"),
			`data-chatremove="resolve"`, `data-case-id="filter:7"`, `name="action"`, `value="dismiss"`, "the words", text("cancel"), `name="note"`,
		} {
			if want == "" || !strings.Contains(markup, want) {
				t.Errorf("%s: the dismiss dialog misses %q: %s", locale, want, markup)
			}
		}
		if strings.Contains(markup, `name="reason"`) || strings.Contains(markup, " required") {
			t.Errorf("%s: a dismissal asks for a reason or a required note: %s", locale, markup)
		}
		if text("dismiss_title") == "" || text("dismiss_help") == "" || text("dismiss_note") == "" {
			t.Errorf("%s: the dismiss copy is missing", locale)
		}
	}
}

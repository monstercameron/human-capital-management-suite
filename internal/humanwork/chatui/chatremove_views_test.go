package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func chatremoveMarkup(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func TestTodo_CHATMOD_004_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		msg := Message{ID: "post", Body: chat.RemovedByAdministrator, Replies: 2, Attachments: []Attachment{{Name: "secret attachment"}}, Chips: []ReactionChip{{Emoji: "SECRET"}}, Reactions: 1}
		markup := chatremoveMarkup(t, message(Model{Locale: locale, Callbacks: Callbacks{OpenThread: func(string) {}}}, handlers{}, msg, false))
		if !strings.Contains(markup, chatremoveText(locale, "removed")) || strings.Contains(markup, "secret attachment") || strings.Contains(markup, "SECRET") || !strings.Contains(markup, `data-action="thread"`) {
			t.Fatalf("tombstone=%s", markup)
		}
		projection := ProjectModerationMessage(locale, msg)
		authorDeleted := chatremoveMarkup(t, ModerationTombstone(Model{Locale: locale}, msg, false))
		if !strings.Contains(authorDeleted, chatremoveText(locale, "deleted")) || strings.Contains(authorDeleted, "secret attachment") {
			t.Fatal("author tombstone", authorDeleted)
		}
		restoredNotice := chatremoveMarkup(t, ModerationRestoredNotice(locale))
		if !strings.Contains(restoredNotice, chatremoveText(locale, "restored")) || !strings.Contains(restoredNotice, `role="status"`) {
			t.Fatal("restored notice", restoredNotice)
		}
		if len(projection.Attachments) != 0 || len(projection.Chips) != 0 || projection.Body != chatremoveText(locale, "removed") {
			t.Fatalf("projection=%+v", projection)
		}
		saved := ProjectModerationSavedMessage(locale, msg)
		if saved.Body != chatremoveText(locale, "saved_removed") || len(saved.Attachments) != 0 {
			t.Fatalf("saved=%+v", saved)
		}
		markup = chatremoveMarkup(t, threadPane(Model{Locale: locale, ThreadParentID: msg.ID, ThreadParent: &msg, ThreadMessages: []Message{msg}}, handlers{}))
		if !strings.Contains(markup, chatremoveText(locale, "removed")) || strings.Contains(markup, "secret attachment") || strings.Contains(markup, "SECRET") {
			t.Fatalf("thread=%s", markup)
		}
		// The dialog names the author and shows the message; one button removes it;
		// removing several is a separate form that shows the count first.
		preview := chat.RemovalPreview{Count: 3, Confirmation: "digest"}
		target := ModerationTargetView{AuthorID: "alex-id", HomeTenantID: "tenant", AuthorName: "Alex Rivera", Body: "the message text"}
		markup = chatremoveMarkup(t, ModerationDialog(ModerationDialogModel{Model: Model{Locale: locale}, Selection: chat.RemovalSelection{ConversationID: "room", PostIDs: []string{"post"}}, Target: target, Preview: &preview, Action: "remove"}))
		for _, want := range []string{`role="dialog"`, `data-confirmed-count="3"`, `data-confirmation="digest"`, `data-chatremove="quick"`, "Alex Rivera", "the message text", chatremoveText(locale, "confirm"), chatremoveText(locale, "remove"), chatremoveText(locale, "several"), chatremoveText(locale, "harassment"), chatremoveText(locale, "spam"), `type="radio"`} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s: missing %q: %s", locale, want, markup)
			}
		}
		if strings.Contains(markup, "⟦") || strings.Contains(markup, "{name}") {
			t.Fatalf("%s: copy key or placeholder printed: %s", locale, markup)
		}
	}
}

func TestTodo_CHATMOD_005_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := ModerationPageModel{Locale: locale, State: StateReady, Items: []chat.ModerationItem{
			{ID: "report:internal", Kind: "report", State: "OPEN", PostID: "post", ReporterID: "worker-internal", AuthorID: "author-internal", Message: chat.Post{ID: "post", Body: "<script>untrusted</script>"}, Reason: "spam", CanRemove: true},
			{ID: "report:appeal", Kind: "appeal", State: "OPEN", PostID: "gone", ReporterID: "author-internal", AuthorID: "author-internal", Message: chat.Post{ID: "gone", Body: "restorable", Deleted: true}, Reason: "appeal", CanRestore: true},
		}, Names: map[string]string{"worker-internal": "Dana", "author-internal": "Alex"}}
		markup := chatremoveMarkup(t, ModerationPage(m))
		for _, want := range []string{chatremoveText(locale, "moderation"), chatremoveText(locale, "dismiss"), chatremoveText(locale, "message_author"), chatremoveText(locale, "restore"), "Dana", "Alex", "&lt;script&gt;"} {
			if !strings.Contains(markup, want) {
				t.Fatalf("missing %q", want)
			}
		}
		if strings.Contains(markup, "<script>untrusted") || strings.Contains(markup, ">worker-internal<") {
			t.Fatal("untrusted markup or identifier rendered")
		}
		markup = chatremoveMarkup(t, ModerationAuthorNotice(locale, chat.ModerationNotice{Reason: "spam", Outcome: "remove", CanAppeal: true}))
		if !strings.Contains(markup, chatremoveText(locale, "appeal")) {
			t.Fatal("appeal missing")
		}
		for _, state := range []LoadState{StateLoading, StateError, StateEmpty} {
			markup = chatremoveMarkup(t, ModerationPage(ModerationPageModel{Locale: locale, State: state}))
			key := "nothing"
			if state == StateLoading {
				key = "loading"
			}
			if state == StateError {
				key = "error"
			}
			if !strings.Contains(markup, chatremoveText(locale, key)) {
				t.Fatal(key)
			}
		}
	}
}

func TestTodo_CHATMOD_005_Accessibility(t *testing.T) {
	markup := chatremoveMarkup(t, ModerationDialog(ModerationDialogModel{Model: Model{Locale: "ar"}, Report: true, Selection: chat.RemovalSelection{ConversationID: "room", PostIDs: []string{"post"}}}))
	for _, want := range []string{`dir="rtl"`, `aria-labelledby="chatremove-title"`, `for="chatremove-reason-0"`, `for="chatremove-note"`, `name="note"`, `role="dialog"`, `<legend>`} {
		if !strings.Contains(markup, want) {
			t.Fatal(want)
		}
	}
	for _, token := range []string{"min-block-size:44px", ":focus-visible", "prefers-reduced-motion", "max-inline-size:100%", "--hcm-"} {
		if !strings.Contains(ChatremoveStyles, token) {
			t.Fatal(token)
		}
	}
	entries := ModerationMenuEntries("de-DE", "room", "post", false)
	if len(entries) != 1 {
		t.Fatal("non-holder remove entry")
	}
	entries = ModerationMenuEntries("ar", "room", "post", true)
	if len(entries) != 2 {
		t.Fatal("holder remove missing")
	}
	markup = chatremoveMarkup(t, entries[1])
	if !strings.Contains(markup, chatremoveText("ar", "remove_everyone")) || !strings.Contains(markup, `role="menuitem"`) {
		t.Fatal(markup)
	}
}

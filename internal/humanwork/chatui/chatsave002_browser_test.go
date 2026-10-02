package chatui_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	gwchtml "github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_CHATSAVE_002_Browser draws the Saved panel with the product's own
// catalog, the one that answers an unknown key with a marker, in the three
// product languages and at the desktop and the phone width, in every state the
// panel has: To do, Done, All, empty, loading, failed, a reminder set, a
// reminder menu, a note set, a note being written, the Undo line, a refused
// change. No copy key is ever printed, and the markup never carries a style
// element or a style attribute.
func TestTodo_CHATSAVE_002_Browser(t *testing.T) {
	now := time.Date(2026, time.October, 1, 8, 30, 0, 0, time.UTC)
	const doc = "doc-47892b80-d600-4401-8244-e2fa2a31caa7"
	want := map[string]struct{ todo, tomorrow, empty, direction string }{
		"en-US": {"To do", "Tomorrow 9:00 AM", "Hover a message and press the bookmark to keep it here.", "ltr"},
		"de-DE": {"Zu erledigen", "Morgen 09:00", "Fahren Sie über eine Nachricht und wählen Sie das Lesezeichen, um sie hier zu behalten.", "ltr"},
		"ar":    {"للتنفيذ", "غدًا ٠٩:٠٠", "مرّر المؤشر فوق رسالة واضغط على العلامة المرجعية لإبقائها هنا.", "rtl"},
	}
	for locale, words := range want {
		ctx := productui.ResolveProductLocale(locale)
		model := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), Text: func(key string) string { return ctx.Text(key) }, SelectedID: "general", CurrentUser: "walt", CurrentTenantID: "t",
			Conversations: []chatui.Conversation{{ID: "general", Name: "general", Kind: chatui.PublicChannel, Joined: true}},
			Members:       []chatui.Member{{ID: "ben", HomeTenantID: "t", Name: "Ben Whitaker"}}}
		rows := []chatui.SavedMessageRow{
			{TenantID: "t", ConversationID: "general", PostID: "p1", AuthorID: "ben", Author: "Ben Whitaker", Channel: "general", InChannel: true, SentAt: now.Add(-time.Hour), Sequence: 4, Availability: "readable", Attachments: 1,
				Body:       "@Ben Whitaker the guide is doc:" + doc + "\nsecond line",
				References: []chatui.ChatReference{{Kind: "PERSON_MENTION", TenantID: "t", ID: "ben", Display: "Ben Whitaker"}},
				DueAt:      time.Date(2026, time.October, 2, 9, 0, 0, 0, time.UTC), Note: "ask on Friday"},
			{TenantID: "t", ConversationID: "general", PostID: "p2", AuthorID: "ben", Author: "Ben Whitaker", Channel: "general", InChannel: true, SentAt: now.Add(-48 * time.Hour), Sequence: 2, Availability: "readable", Body: "Late reminder", DueAt: now.Add(-3 * time.Hour)},
			{TenantID: "t", ConversationID: "general", PostID: "p3", AuthorID: "ben", Author: "Ben Whitaker", Channel: "general", InChannel: true, SentAt: now.Add(-72 * time.Hour), Sequence: 1, Availability: "readable", Body: "Finished", Done: true, DoneAt: now.Add(-time.Hour)},
			{TenantID: "t", ConversationID: "gone", PostID: "p4", Availability: "no_access"},
		}
		base := chatui.SavedMessagesView{Locale: ctx.Resolved, Rows: rows, Model: model, Now: now, TodoCount: 2, DoneCount: 1, AllCount: 4,
			DocPreviews: map[string]chatui.DocPreview{doc: {ID: doc, Title: "Open enrollment guide", Readable: true, State: "ready"}}}
		key := chatui.SavedItemKey("t", "general", "p1")
		states := map[string]chatui.SavedMessagesView{}
		for name, mutate := range map[string]func(*chatui.SavedMessagesView){
			"todo":          func(v *chatui.SavedMessagesView) { v.Tab = "todo" },
			"done":          func(v *chatui.SavedMessagesView) { v.Tab, v.Rows = "done", rows[2:3] },
			"all":           func(v *chatui.SavedMessagesView) { v.Tab = "all" },
			"empty-todo":    func(v *chatui.SavedMessagesView) { v.Tab, v.Rows, v.AllCount = "todo", nil, 0 },
			"empty-done":    func(v *chatui.SavedMessagesView) { v.Tab, v.Rows = "done", nil },
			"loading":       func(v *chatui.SavedMessagesView) { v.Rows, v.Loading = nil, true },
			"failed":        func(v *chatui.SavedMessagesView) { v.Rows, v.Error = nil, "unavailable" },
			"limit":         func(v *chatui.SavedMessagesView) { v.Error = "saved_limit" },
			"reminder-menu": func(v *chatui.SavedMessagesView) { v.ReminderMenu = key },
			"date-picker":   func(v *chatui.SavedMessagesView) { v.ReminderMenu, v.PickingDate = key, true },
			"note-editing":  func(v *chatui.SavedMessagesView) { v.EditingNote = key },
			"undo-done":     func(v *chatui.SavedMessagesView) { v.Undo = "done" },
			"undo-remove":   func(v *chatui.SavedMessagesView) { v.Undo = "remove" },
			"refused":       func(v *chatui.SavedMessagesView) { v.ActionError, v.Action = "unavailable", "done" },
			"search":        func(v *chatui.SavedMessagesView) { v.AllCount, v.Query = 12, "guide" },
			"expanded":      func(v *chatui.SavedMessagesView) { v.Expanded = map[string]bool{"p1": true} },
		} {
			view := base
			mutate(&view)
			states[name] = view
		}
		sheet := chatui.ScopedStylesheet()
		for _, width := range []int{1280, 390} {
			for name, view := range states {
				node := gwchtml.Div(gwchtml.Props{Data: map[string]string{"viewport": fmt.Sprint(width)}}, chatui.RenderSavedMessages(view))
				markup, err := ui.RenderToString(node)
				if err != nil {
					t.Fatalf("%s %s %d: %v", locale, name, width, err)
				}
				for _, bad := range []string{"⟦", "<style", ` style="`, "<details", "<summary", "<textarea"} {
					if strings.Contains(markup, bad) {
						t.Errorf("%s %s %d: the panel prints %q: %s", locale, name, width, bad, regexp.MustCompile(`.{0,30}`+regexp.QuoteMeta(bad)+`.{0,40}`).FindString(markup))
					}
				}
				if !strings.Contains(markup, `dir="`+words.direction+`"`) {
					t.Errorf("%s %s %d: the panel is not %s", locale, name, width, words.direction)
				}
				if visible := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(markup, " "); strings.Contains(visible, doc) || strings.Contains(visible, "47892b80") {
					t.Errorf("%s %s %d: a document identifier is printed", locale, name, width)
				}
			}
		}
		text := func(name string) string {
			markup, _ := ui.RenderToString(chatui.RenderSavedMessages(states[name]))
			return regexp.MustCompile(`<[^>]*>`).ReplaceAllString(markup, " ")
		}
		if got := text("todo"); !strings.Contains(got, words.todo) || !strings.Contains(got, words.tomorrow) || !strings.Contains(got, "Open enrollment guide") {
			t.Errorf("%s: the To do list does not read in its language: %s", locale, got)
		}
		if got := text("empty-todo"); !strings.Contains(got, words.empty) {
			t.Errorf("%s: the empty list does not say how to save a message: %s", locale, got)
		}
		if got := text("all"); !strings.Contains(got, chatui.SavedMessagesCopy(locale).NoAccess) {
			t.Errorf("%s: an item the reader lost access to is not told so", locale)
		}
		for _, rule := range []string{".chatsave-actions{position:static", "inset:0", "grid-column:2"} {
			if !strings.Contains(sheet, rule) {
				t.Errorf("%s: the phone layout lacks %q", locale, rule)
			}
		}
		// The whole page, with its Saved row and the panel's own mount, in the catalog.
		page, err := ui.RenderToString(chatui.Build(model))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(page, "⟦") {
			t.Errorf("%s: the page prints a copy key: %s", locale, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(page))
		}
		if !strings.Contains(page, `id="chatsave-list"`) || !strings.Contains(page, `data-saved-action="toggle"`) {
			t.Errorf("%s: the Saved row or its panel is missing", locale)
		}
	}
}

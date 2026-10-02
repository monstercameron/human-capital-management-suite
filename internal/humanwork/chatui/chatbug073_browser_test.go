package chatui_test

import (
	"html"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_CHATBUG_073_Browser renders the page with the product's own catalog,
// the one that answers a missing key with a bracketed key, in the three
// languages: the open edit box names its keys, and an edited message says so
// after its text.
func TestTodo_CHATBUG_073_Browser(t *testing.T) {
	at := time.Date(2026, 10, 2, 12, 55, 0, 0, time.UTC)
	for locale, want := range map[string]struct{ hint, edited string }{
		"en-US": {"Enter to save, Shift+Enter for a new line, Esc to cancel", "(edited)"},
		"de-DE": {"Enter speichert, Umschalt+Enter fügt eine Zeile ein, Esc bricht ab", "(bearbeitet)"},
		"ar":    {"Enter للحفظ، Shift+Enter لسطر جديد، Esc للإلغاء", "(معدّلة)"},
	} {
		ctx := productui.ResolveProductLocale(locale)
		build := func(editing string) string {
			m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: "general", CurrentUser: "walt", CurrentTenantID: "t",
				Text:          func(key string) string { return ctx.Text(key) },
				Conversations: []chatui.Conversation{{ID: "general", Name: "general", Kind: chatui.PublicChannel, Joined: true}},
				Messages: []chatui.Message{
					{ID: "m1", AuthorID: "walt", Author: "Walt Brennan", SentAt: at, Body: "First message", Revision: 1},
					{ID: "m2", AuthorID: "walt", Author: "Walt Brennan", SentAt: at.Add(time.Minute), Body: "Second message, corrected", Revision: 2, Edited: true},
				},
				EditingID: editing,
			}
			m.Callbacks.BeginEdit = func(string) {}
			m.Callbacks.CancelEdit = func() {}
			m.Callbacks.EditMessage = func(string, string, uint64) {}
			page, err := ui.RenderToString(chatui.Build(m))
			if err != nil {
				t.Fatal(err)
			}
			return html.UnescapeString(page)
		}
		for _, editing := range []string{"", "m2"} {
			page := build(editing)
			if strings.Contains(page, "⟦") {
				t.Errorf("%s: the page prints a copy key: %s", locale, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(page))
			}
			row := page[strings.Index(page, `data-message-id="m2"`):]
			if editing == "" {
				text, mark := strings.Index(row, "Second message, corrected"), strings.Index(row, ">"+want.edited+"<")
				if text < 0 || mark < text {
					t.Errorf("%s: %q does not follow the message text (text at %d, mark at %d)", locale, want.edited, text, mark)
				}
				continue
			}
			if !strings.Contains(row, `id="edit-m2"`) || !strings.Contains(row, `aria-describedby="edit-hint-m2"`) {
				t.Errorf("%s: the edit box is missing or is not described by its key hint", locale)
			}
			if !strings.Contains(row, ">"+want.hint+"<") {
				t.Errorf("%s: the key hint %q is not under the edit box", locale, want.hint)
			}
			if strings.Contains(row, want.edited) {
				t.Errorf("%s: the message open in the edit box still says %q", locale, want.edited)
			}
		}
	}
}

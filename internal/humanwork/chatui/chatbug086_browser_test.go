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

// TestTodo_CHATBUG_086_Browser renders the page with the product's own catalog
// in the three languages, with a draft that ends in a shortcode as a reload
// restores it: the colon list is closed (nobody has typed), its slot is in the
// composer and in the thread composer so that opening and closing it never
// moves the text area, and the text area is the one the press handler and the
// caret keeper look for.
func TestTodo_CHATBUG_086_Browser(t *testing.T) {
	at := time.Date(2026, 10, 2, 12, 55, 0, 0, time.UTC)
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		ctx := productui.ResolveProductLocale(locale)
		m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: "general", CurrentUser: "walt", CurrentTenantID: "t",
			Text:          func(key string) string { return ctx.Text(key) },
			Conversations: []chatui.Conversation{{ID: "general", Name: "general", Kind: chatui.PublicChannel, Joined: true}},
			Messages:      []chatui.Message{{ID: "m1", AuthorID: "walt", Author: "Walt Brennan", SentAt: at, Body: "Parent", Revision: 1, Replies: 1}},
			Draft:         "nice :thum",
			ShowThread:    true, ThreadParentID: "m1",
			ThreadDrafts: map[string]string{"m1": "also :fir"},
		}
		m.Callbacks.SendMessage = func(string, string) {}
		m.Callbacks.ReplyInThread = func(string, string) {}
		rendered, err := ui.RenderToString(chatui.Build(m))
		if err != nil {
			t.Fatal(err)
		}
		page := html.UnescapeString(rendered)
		if strings.Contains(page, "⟦") {
			t.Errorf("%s: the page prints a copy key: %s", locale, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(page))
		}
		if strings.Contains(page, "emoji-completion-menu") || strings.Contains(page, `role="listbox"`) {
			t.Errorf("%s: a restored draft that ends in a shortcode opened a suggestion list", locale)
		}
		if got := strings.Count(page, `<div class="emoji-completion-slot"></div>`); got != 2 {
			t.Errorf("%s: %d closed-list slots, want one in the composer and one in the thread composer", locale, got)
		}
		for _, field := range []string{`id="chat-composer"`, `id="thread-composer"`} {
			tag := regexp.MustCompile(`<textarea[^>]*` + field + `[^>]*>`).FindString(page)
			if tag == "" || !strings.Contains(tag, `class="composer-input"`) {
				t.Errorf("%s: the text area %s is not textarea.composer-input: %s", locale, field, tag)
			}
			if strings.Contains(tag, `aria-expanded="true"`) || strings.Contains(tag, "aria-activedescendant") {
				t.Errorf("%s: %s says a list is open: %s", locale, field, tag)
			}
		}
		if !strings.Contains(page, `data-chat-value="nice :thum"`) {
			t.Errorf("%s: the restored draft is not in the composer", locale)
		}
	}
}

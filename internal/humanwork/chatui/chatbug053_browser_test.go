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

// TestTodo_CHATBUG_053_Browser renders the page with the product's own catalog
// in the three languages: an address in a message is a link in the timeline
// and in the thread pane, a long one is shortened in the middle and keeps its
// whole text, a query parameter named like an old HTML entity stays as typed,
// and a share link of another server is named in the reader's language.
func TestTodo_CHATBUG_053_Browser(t *testing.T) {
	const (
		long    = "https://example.com/a/very/long/path/that/keeps/going/and/going/for/a/while?with=query&and=more"
		query   = "https://example.com/report?a=1&copy=2"
		foreign = "http://chat.other.example/workspace/app/chat#share=aXJvbmNsYWQtc2hhcmUtdG9rZW4tZm9yLXRoZS10ZXN0"
	)
	at := time.Date(2026, 10, 2, 12, 55, 0, 0, time.UTC)
	tags := regexp.MustCompile(`<[^>]*>`)
	for locale, shared := range map[string]string{"en-US": "a shared message", "de-DE": "eine geteilte Nachricht", "ar": "رسالة تمت مشاركتها"} {
		ctx := productui.ResolveProductLocale(locale)
		root := chatui.Message{ID: "m1", AuthorID: "walt", Author: "Walt Brennan", SentAt: at, Body: "First " + long + " then " + query + " end", Revision: 1, Replies: 1}
		m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: "general", CurrentUser: "walt", CurrentTenantID: "t",
			EmbedOrigin:   "http://localhost:8290",
			Text:          func(key string) string { return ctx.Text(key) },
			Conversations: []chatui.Conversation{{ID: "general", Name: "general", Kind: chatui.PublicChannel, Joined: true}},
			Messages:      []chatui.Message{root},
			ShowThread:    true, ThreadParentID: "m1", ThreadParent: &root,
			ThreadMessages: []chatui.Message{{ID: "r1", AuthorID: "loretta", Author: "Loretta Haynes", SentAt: at.Add(time.Minute), Body: "did you see " + foreign, Revision: 1}},
		}
		rendered, err := ui.RenderToString(chatui.Build(m))
		if err != nil {
			t.Fatal(err)
		}
		page := html.UnescapeString(rendered)
		if strings.Contains(page, "⟦") {
			t.Errorf("%s: the page prints a copy key: %s", locale, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(page))
		}
		// Once in the timeline and once as the thread's parent.
		if got := strings.Count(page, `class="chat-link" data-link="shortened" href="`+long+`" title="`+long+`"`); got != 2 {
			t.Errorf("%s: the long address is a shortened link %d times, want 2 (timeline and thread)", locale, got)
		}
		if got := strings.Count(page, `<a class="chat-link" href="`+query+`">`+query+`</a>`); got != 2 {
			t.Errorf("%s: the address with &copy= is an exact link %d times, want 2", locale, got)
		}
		if strings.Contains(page, "©") {
			t.Errorf("%s: a query parameter was drawn as a copyright sign", locale)
		}
		if !strings.Contains(page, ">"+shared+" (chat.other.example)</a>") || !strings.Contains(page, `data-share-link="foreign"`) {
			t.Errorf("%s: the share link of another server does not read %q with its site", locale, shared)
		}
		row := page[strings.Index(page, `data-message-id="m1"`):]
		row = row[:strings.Index(row, "</article>")]
		if text := tags.ReplaceAllString(row, "\x00"); !strings.Contains(strings.ReplaceAll(text, "\x00", ""), "First "+long+" then "+query+" end") {
			t.Errorf("%s: the text of the message is not the text that was typed: %q", locale, strings.ReplaceAll(text, "\x00", ""))
		}
	}
	for _, rule := range []string{".chat-workspace a.chat-link{", ".chat-workspace a.chat-link .chat-link-cut{"} {
		if !strings.Contains(chatui.Stylesheet, rule) {
			t.Errorf("the served stylesheet has no rule %s", rule)
		}
	}
}

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

// TestTodo_CHATBUG_083_Browser renders a conversation with three images in the
// three languages. The viewer is built in the page from what each thumbnail
// carries, so the thumbnails are held to it: the file's name, who sent it and
// when, its own size (what "twice its size" is measured from), and their order
// in the conversation, which is the order the arrow keys follow. The served
// stylesheet carries the viewer's caption, tools and step buttons.
func TestTodo_CHATBUG_083_Browser(t *testing.T) {
	at := time.Date(2026, 3, 9, 14, 5, 0, 0, time.Local)
	image := func(id, name string, w, h int) chatui.Attachment {
		return chatui.Attachment{ID: id, Name: name, ContentType: "image/png", Width: w, Height: h, Bytes: 4096}
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		ctx := productui.ResolveProductLocale(locale)
		m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: "dm", CurrentUser: "walt", CurrentTenantID: "t",
			Text:          func(key string) string { return ctx.Text(key) },
			Conversations: []chatui.Conversation{{ID: "dm", Name: "Loretta Haynes", Kind: chatui.DirectMessage}},
			Messages: []chatui.Message{
				{ID: "m1", AuthorID: "loretta", Author: "Loretta Haynes", SentAt: at, TimeLabel: "2:05 PM", Revision: 1, Attachments: []chatui.Attachment{image("a1", "first.png", 160, 160), image("a2", "second.png", 2400, 1600)}},
				{ID: "m2", AuthorID: "walt", Author: "Walt Brennan", SentAt: at.Add(time.Minute), TimeLabel: "2:06 PM", Revision: 1, Attachments: []chatui.Attachment{image("a3", "third.png", 320, 200)}},
			},
		}
		rendered, err := ui.RenderToString(chatui.Build(m))
		if err != nil {
			t.Fatal(err)
		}
		page := html.UnescapeString(rendered)
		if strings.Contains(page, "⟦") {
			t.Errorf("%s: the page prints a copy key: %s", locale, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(page))
		}
		buttons := regexp.MustCompile(`<button[^>]*class="attachment-image-open"[^>]*>`).FindAllString(page, -1)
		if len(buttons) != 3 {
			t.Fatalf("%s: %d image thumbnails, want 3", locale, len(buttons))
		}
		for i, want := range []struct{ name, author, width, height string }{
			{"first.png", "Loretta Haynes", "160", "160"}, {"second.png", "Loretta Haynes", "2400", "1600"}, {"third.png", "Walt Brennan", "320", "200"},
		} {
			for _, attr := range []string{`data-action="view-image"`, `data-media-name="` + want.name + `"`, `data-media-author="` + want.author + `"`, `data-media-width="` + want.width + `"`, `data-media-height="` + want.height + `"`} {
				if !strings.Contains(buttons[i], attr) {
					t.Errorf("%s: thumbnail %d lacks %s: %s", locale, i+1, attr, buttons[i])
				}
			}
			sent := regexp.MustCompile(`data-media-sent="([^"]*)"`).FindStringSubmatch(buttons[i])
			if sent == nil || !regexp.MustCompile(`\p{Nd}`).MatchString(sent[1]) || !strings.Contains(sent[1], ",") {
				t.Errorf("%s: thumbnail %d does not say when it was sent, with the day: %v", locale, i+1, sent)
			}
		}
	}
	sheet := chatui.ScopedStylesheet()
	for _, rule := range []string{".chat-image-viewer-caption{", ".chat-image-viewer-tools{", ".chat-image-viewer-step{", ".chat-image-viewer-media:not(.chat-image-viewer-zoomed) img{width:var(--chat-viewer-w,auto)"} {
		if !strings.Contains(sheet, rule) {
			t.Errorf("the served stylesheet has no rule %s", rule)
		}
	}
}

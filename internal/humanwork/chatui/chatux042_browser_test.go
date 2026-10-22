package chatui_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_CHATUX_042_Browser: with the product's own catalog in each language,
// a message's hover bar holds react, reply, save and more and nothing else, its
// menu does not repeat them where the bar is drawn and always offers Pin, and an
// announcement is the standalone variant of the agent answer with an author line
// that carries the time.
func TestTodo_CHATUX_042_Browser(t *testing.T) {
	button := regexp.MustCompile(`<button[^>]*>`)
	for locale := range map[string]bool{"en-US": true, "de-DE": true, "ar": true} {
		page := lane3Page(t, locale, func(m *chatui.Model) {
			m.Messages = []chatui.Message{{ID: "m1", AuthorID: "jake", Author: "Jake Sullivan", Body: "Can you check the numbers?", TimeLabel: "9:30", SentAt: time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)}}
			m.MenuID = "m1"
			m.Callbacks.OpenMenu, m.Callbacks.OpenThread, m.Callbacks.Pin, m.Callbacks.OpenPicker = func(string) {}, func(string) {}, func(string) {}, func(string) {}
			m.Callbacks.CopyLink, m.Callbacks.OpenShare = func(string) {}, func(string) {}
		})
		lane3NoLeaks(t, locale+" message", page)
		bar := page[strings.Index(page, `class="message-actions"`):]
		bar = bar[:strings.Index(bar, `class="message-menu`)]
		var kinds []string
		for _, b := range button.FindAllString(bar, -1) {
			switch {
			case strings.Contains(b, `data-action="react-with"`):
				kinds = append(kinds, "emoji")
			case strings.Contains(b, `data-action="react-pick"`):
				kinds = append(kinds, "react")
			case strings.Contains(b, `data-action="reply"`):
				kinds = append(kinds, "reply")
			case strings.Contains(b, `data-saved-action="save"`):
				kinds = append(kinds, "save")
			case strings.Contains(b, `data-action="menu"`):
				kinds = append(kinds, "more")
			default:
				kinds = append(kinds, "other:"+b)
			}
		}
		if strings.Join(kinds, ",") != "emoji,emoji,emoji,react,reply,save,more" {
			t.Errorf("%s: the bar holds %v, want three emoji, react, reply, save, more", locale, kinds)
		}
		menu := page[strings.Index(page, `class="message-menu`):]
		if !strings.Contains(menu, `data-action="pin"`) || strings.Contains(menu, "menu-bar-twin") {
			t.Errorf("%s: Pin is not always offered by the menu", locale)
		}
		reply := regexp.MustCompile(`<button[^>]*data-action="reply"[^>]*>`).FindString(menu)
		if reply == "" || !strings.Contains(reply, "menu-phone-only") {
			t.Errorf("%s: the menu's Reply is not limited to where there is no bar: %q", locale, reply)
		}

		ctx := productui.ResolveProductLocale(locale)
		m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), Text: func(key string) string { return ctx.Text(key) }}
		markup, err := ui.RenderToString(chatui.RenderAgentAnnouncementMessage(m, chatui.AgentAnnouncementMessage{AgentName: "Policy Helper", Text: "Open enrollment starts Monday.", PostedAt: time.Date(2026, 10, 1, 9, 5, 0, 0, time.UTC),
			Sources: []chatui.AgentAnnouncementSource{{Title: "Open enrollment guide", Href: "/workspace/app/docs?document=d1"}}}))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"agent-answer agent-answer-standalone", "agent-reply-identity", "<time", "agent-reply-sources", "Open enrollment guide"} {
			if !strings.Contains(markup, want) {
				t.Errorf("%s: the announcement misses %q", locale, want)
			}
		}
		lane3NoLeaks(t, locale+" announcement", markup)
	}
}

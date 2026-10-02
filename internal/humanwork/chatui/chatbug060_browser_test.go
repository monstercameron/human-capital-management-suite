package chatui_test

import (
	"html"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_CHATBUG_060_Browser draws the page in each direction with the
// product's own catalog and checks what the right-to-left findings depend on:
// the workspace states its direction, the shortcut badge is written
// left-to-right so its physical rule places it, Send is the mirrored glyph, and
// a message in English inside the Arabic page takes its own direction inside a
// block that starts where its author line does. The rules themselves are in the
// shipped stylesheet.
func TestTodo_CHATBUG_060_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "ar"} {
		t.Run(locale, func(t *testing.T) {
			ctx := productui.ResolveProductLocale(locale)
			m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: "room", CurrentUser: "walt", CurrentTenantID: "t",
				Text:          func(key string) string { return ctx.Text(key) },
				Conversations: []chatui.Conversation{{ID: "room", Name: "general", Kind: chatui.PublicChannel, Joined: true}},
				Messages:      []chatui.Message{{ID: "m1", AuthorID: "ben", Author: "Ben", Body: "Plan for the quarter", Revision: 1}},
			}
			m.Callbacks.SendMessage = func(string, string) {}
			rendered, err := ui.RenderToString(chatui.Build(m))
			if err != nil {
				t.Fatal(err)
			}
			page := html.UnescapeString(rendered)
			if strings.Contains(page, "⟦") {
				t.Fatal("a copy key is printed")
			}
			wantDir := "ltr"
			if locale == "ar" {
				wantDir = "rtl"
			}
			workspace := page[strings.Index(page, `class="chat-workspace"`):]
			if open := workspace[:strings.Index(workspace, ">")]; !strings.Contains(open, `dir="`+wantDir+`"`) {
				t.Errorf("the workspace does not state its direction %s: %s", wantDir, open)
			}
			if badge := page[strings.Index(page, "chat-search-shortcut"):]; !strings.Contains(badge[:strings.Index(badge, ">")], `dir="ltr"`) {
				t.Errorf("the shortcut badge is not written left-to-right: %s", badge[:80])
			}
			if !strings.Contains(page, "icon-send") {
				t.Error("Send has no glyph the right-to-left rule can mirror")
			}
			body := page[strings.Index(page, "Plan for the quarter")-200 : strings.Index(page, "Plan for the quarter")]
			if !strings.Contains(body, `dir="auto"`) {
				t.Errorf("the message body does not take its own direction: %s", body)
			}
		})
	}
	for _, rule := range []string{
		`.chat-workspace[dir="rtl"] .rail-search .chat-search-shortcut{inset-inline-end:auto;left:22px;right:auto}`,
		`.rail-search:has(.chat-search-shortcut) .chat-search{padding-inline-end:64px}`,
		`.chat-workspace[dir="rtl"] .icon-send,.chat-workspace[dir="rtl"] .icon-reply,.chat-workspace[dir="rtl"] .icon-chevron-right{transform:scaleX(-1)}`,
		`.message-body,.agent-reply-answer{width:100%;max-width:72ch;box-sizing:border-box;margin-inline:0;text-align:match-parent}`,
	} {
		if !strings.Contains(chatui.Stylesheet, rule) {
			t.Errorf("the stylesheet lacks %s", rule)
		}
	}
}

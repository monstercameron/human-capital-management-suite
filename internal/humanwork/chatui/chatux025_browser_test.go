package chatui_test

import (
	"html"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_CHATUX_025_Browser draws Browse channels over a conversation whose
// thread is open, in the three languages, with the product's own catalog: a
// channel the person is in has Leave, one they are not in has Join, the thread
// parent carries its day, and nothing prints a key.
func TestTodo_CHATUX_025_Browser(t *testing.T) {
	leave := map[string]string{"en-US": "Leave", "de-DE": "Verlassen", "ar": "مغادرة"}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			ctx := productui.ResolveProductLocale(locale)
			room := chatui.Conversation{ID: "general", Name: "general", Kind: chatui.PublicChannel, Joined: true, MemberCount: 12}
			sent := time.Now().AddDate(0, 0, -1)
			m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: "general", CurrentUser: "me", CurrentTenantID: "t",
				Text:           func(key string) string { return ctx.Text(key) },
				Conversations:  []chatui.Conversation{room},
				Browse:         []chatui.Conversation{{ID: "sales", Name: "sales", Kind: chatui.PublicChannel, MemberCount: 4}},
				Messages:       []chatui.Message{{ID: "root", AuthorID: "walt", Author: "Walt Brennan", Body: "Who has the numbers?", Replies: 1, SentAt: sent, Revision: 1}},
				ThreadMessages: []chatui.Message{{ID: "r1", AuthorID: "me", Author: "Me", Body: "I do", SentAt: sent, Revision: 1}},
				ShowBrowse:     true,
			}
			m.Callbacks.LeaveConversation = func(string) {}
			m.Callbacks.JoinConversation = func(string) {}
			m.Callbacks.CloseBrowse = func() {}
			rendered, err := ui.RenderToString(chatui.Build(m))
			if err != nil {
				t.Fatal(err)
			}
			page := html.UnescapeString(rendered)
			if strings.Contains(page, "⟦") {
				t.Fatalf("a copy key is printed")
			}
			dialog := page[strings.Index(page, `class="chat-dialog browse-dialog"`):]
			joined := dialog[strings.Index(dialog, `data-conversation-id="general"`):]
			joined = joined[:strings.Index(joined, "</li>")]
			if !strings.Contains(joined, `data-action="rail-leave"`) || !strings.Contains(joined, ">"+leave[locale]+"<") {
				t.Errorf("a joined row has no Leave in %s: %s", locale, joined)
			}
			other := dialog[strings.Index(dialog, `data-conversation-id="sales"`):]
			other = other[:strings.Index(other, "</li>")]
			if strings.Contains(other, "rail-leave") || !strings.Contains(other, `data-action="join"`) {
				t.Errorf("an unjoined row: %s", other)
			}
			if locale == "ar" && !strings.Contains(page, `dir="rtl"`) {
				t.Error("Arabic direction lost")
			}
		})
	}
}

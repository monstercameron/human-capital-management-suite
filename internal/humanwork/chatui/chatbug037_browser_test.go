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

// TestTodo_CHATBUG_037_Browser renders the page with the product's own catalog,
// the one that answers a missing key with a bracketed key, in the three
// languages: the sent message names its link, prints no token, and its card
// counts attachments with an icon.
func TestTodo_CHATBUG_037_Browser(t *testing.T) {
	const origin = "http://localhost:8290"
	const token = "aXJvbnJpZGdlLWRlbW8AODZjZmNjYWltNWM2Mi01MTBhLTg3NjItYjRkMzIzNjgwMDNiAGJjNWZmYTZkLTcwNmUt"
	address := origin + "/workspace/app/chat#share=" + token
	for locale, want := range map[string]struct{ link, one, many string }{
		"en-US": {"a message in #design", "1 attachment", "3 attachments"},
		"de-DE": {"eine Nachricht in #design", "1 Anhang", "3 Anhänge"},
		"ar":    {"رسالة في #design", "مرفق واحد", "3 مرفقات"},
	} {
		ctx := productui.ResolveProductLocale(locale)
		build := func(count int) string {
			m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: "general", EmbedOrigin: origin, CurrentUser: "cam", CurrentTenantID: "t",
				Text: func(key string) string { return ctx.Text(key) },
				Conversations: []chatui.Conversation{
					{ID: "general", Name: "general", Kind: chatui.PublicChannel, Joined: true},
					{ID: "design", Name: "design", Kind: chatui.PublicChannel, Joined: true},
				},
				Messages: []chatui.Message{{ID: "m1", AuthorID: "cam", Author: "Cam", SentAt: time.Date(2026, 10, 1, 19, 9, 0, 0, time.UTC), Body: "did y'all see this " + address}},
				Embeds:   map[string]chatui.LinkEmbed{token: {Token: token, State: "ready", SourceRoom: "design", SourcePost: "p1", Channel: "design", Author: "Eddie Ramirez", TimeLabel: "11:59 AM", Body: "Confirmed with legal, we are fine to proceed.", AttachmentCount: count}},
			}
			page, err := ui.RenderToString(chatui.Build(m))
			if err != nil {
				t.Fatal(err)
			}
			return html.UnescapeString(page)
		}
		for count, label := range map[int]string{1: want.one, 3: want.many} {
			page := build(count)
			if strings.Contains(page, "⟦") {
				t.Errorf("%s: the page prints a copy key: %s", locale, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(page))
			}
			if !strings.Contains(page, ">"+want.link+"</a>") {
				t.Errorf("%s: the message does not carry the short link %q", locale, want.link)
			}
			// The token is not in any text the reader sees (it stays in the link's
			// own address and in attributes).
			if text := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(page, " "); strings.Contains(text, token[:20]) {
				t.Errorf("%s: the raw token is still printed: %s", locale, regexp.MustCompile(`.{40}`+regexp.QuoteMeta(token[:20])+`.{20}`).FindString(text))
			}
			if !strings.Contains(page, `aria-label="`+label+`"`) || !strings.Contains(page, "chat-embed-attachments") {
				t.Errorf("%s: the card does not name its %d attachment(s) %q", locale, count, label)
			}
			if strings.Contains(page, "Attachments: ") {
				t.Errorf("%s: the bare label is back", locale)
			}
		}
	}
}

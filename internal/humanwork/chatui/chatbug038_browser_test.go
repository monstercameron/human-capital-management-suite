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

// TestTodo_CHATBUG_038_Browser renders the page with the product's own catalog
// in the three languages. The preview under the composer belongs to the draft:
// it is drawn while the draft holds the link, and it is gone when the draft is
// empty (sent or cleared) and when the link was taken out of the draft, even
// while the page still holds the resolved preview for the sent message above.
func TestTodo_CHATBUG_038_Browser(t *testing.T) {
	const origin = "http://localhost:8290"
	const token = "aXJvbnJpZGdlLWRlbW8AODZjZmNjYWltNWM2Mi01MTBhLTg3NjItYjRkMzIzNjgwMDNiAGJjNWZmYTZkLTcwNmUt"
	address := origin + "/workspace/app/chat#share=" + token
	composerOf := regexp.MustCompile(`(?s)<form[^>]*class="chat-composer".*?</form>`)
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		ctx := productui.ResolveProductLocale(locale)
		build := func(draft string) (page, composer string) {
			m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: "general", EmbedOrigin: origin, CurrentUser: "cam", CurrentTenantID: "t",
				Text: func(key string) string { return ctx.Text(key) },
				Conversations: []chatui.Conversation{
					{ID: "general", Name: "general", Kind: chatui.PublicChannel, Joined: true},
					{ID: "design", Name: "design", Kind: chatui.PublicChannel, Joined: true},
				},
				Messages: []chatui.Message{{ID: "m1", AuthorID: "cam", Author: "Cam", SentAt: time.Date(2026, 10, 1, 19, 9, 0, 0, time.UTC), Body: "did y'all see this " + address}},
				Embeds:   map[string]chatui.LinkEmbed{token: {Token: token, State: "ready", SourceRoom: "design", SourcePost: "p1", Channel: "design", Author: "Eddie Ramirez", TimeLabel: "11:59 AM", Body: "Confirmed with legal."}},
				Draft:    draft,
			}
			m.Callbacks.SendMessage = func(string, string) {}
			rendered, err := ui.RenderToString(chatui.Build(m))
			if err != nil {
				t.Fatal(err)
			}
			page = html.UnescapeString(rendered)
			return page, composerOf.FindString(page)
		}
		for name, tc := range map[string]struct {
			draft string
			want  bool
		}{
			"the draft holds the link":            {"look at " + address, true},
			"the draft was sent or cleared":       {"", false},
			"the link was taken out of the draft": {"look at this", false},
		} {
			page, composer := build(tc.draft)
			if composer == "" {
				t.Fatalf("%s, %s: the page has no composer", locale, name)
			}
			if strings.Contains(page, "⟦") {
				t.Errorf("%s, %s: the page prints a copy key: %s", locale, name, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(page))
			}
			if got := strings.Contains(composer, `data-embed-key="`+token+`"`); got != tc.want {
				t.Errorf("%s, %s: preview under the composer = %v, want %v", locale, name, got, tc.want)
			}
			// The sent message keeps its own card in every case.
			if strings.Count(page, `data-embed-key="`+token+`"`) < 1 {
				t.Errorf("%s, %s: the sent message lost its card", locale, name)
			}
		}
	}
}

package chatui_test

import (
	"html"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestTodo_CHATBUG_039_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			ctx := productui.ResolveProductLocale(locale)
			if value := ctx.Text("chatbug039.missing"); !strings.Contains(value, "⟦") {
				t.Fatalf("fixture must use the real missing-key catalog: %q", value)
			}
			surfaces := chatui.Chatbug039SurfacesForTest(t, chatui.Model{
				Locale: ctx.Resolved, Direction: string(ctx.Direction),
				Text: func(key string) string { return ctx.Text(key) },
			})
			for name, page := range surfaces {
				t.Run(name, func(t *testing.T) {
					if strings.TrimSpace(page) == "" {
						t.Fatal("surface did not render")
					}
					// Check all SSR output, including hidden disclosure contents,
					// title, aria-label and placeholder attributes.
					if page = html.UnescapeString(page); strings.ContainsAny(page, "⟦⟧") {
						start := strings.IndexAny(page, "⟦⟧")
						t.Fatalf("raw catalog key: %s", page[max(0, start-60):min(len(page), start+160)])
					}
				})
			}
			words := map[string][4]string{
				"en-US": {"Choose an emoji", "Poll", "Search Chat", "Manage filters"},
				"de-DE": {"Emoji auswählen", "Umfrage", "Chat durchsuchen", "Filter verwalten"},
				"ar":    {"اختر رمزًا تعبيريًا", "استطلاع", "البحث في الدردشة", "أدر المرشحات"},
			}[locale]
			for i, name := range []string{"emoji picker", "composer with add and command menus", "sidebar with preferences and Channels menu", "details with Manage channel open"} {
				if !strings.Contains(html.UnescapeString(surfaces[name]), words[i]) {
					t.Errorf("%s: localized copy %q is missing", name, words[i])
				}
			}
			for name, marker := range map[string]string{
				"conversation, messages and header":          "message-body",
				"agent answer card":                          "chat-ephemeral",
				"composer with add and command menus":        "command-menu",
				"mention menu":                               "mention-menu",
				"sidebar with preferences and Channels menu": "chatux002-layer",
				"details with Manage channel open":           "chat-details-manage",
				"thread pane":                                "thread-composer",
				"Saved panel":                                "chatsave",
				"search results":                             "chatsearch",
				"filters settings with editor":               "chatfilter-panel",
				"moderation dialog":                          "chatremove",
				"emoji picker":                               "emoji-pop-grid",
			} {
				if !strings.Contains(surfaces[name], marker) {
					t.Errorf("%s fixture does not contain %q", name, marker)
				}
			}
		})
	}
}

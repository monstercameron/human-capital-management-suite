package chatui_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// The product catalog answers a key it does not hold with a ⟦key⟧ marker. The
// picker takes its words from its own table when the catalog has none, so no
// marker, copy key or English fallback reaches a person in any language.
func TestTodo_CHATEMOJI_002_Browser_RealCatalog(t *testing.T) {
	for _, tc := range []struct {
		locale, frequent, group, search string
	}{
		{"en-US", "Frequently used", "Smileys & Emotion", "Search emoji"},
		{"de-DE", "Häufig verwendet", "Smileys & Emotionen", "Emoji suchen"},
		{"ar", "الأكثر استخدامًا", "الوجوه والمشاعر", "البحث عن رمز تعبيري"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			ctx := productui.ResolveProductLocale(tc.locale)
			m := chatui.Model{State: chatui.StateReady, SelectedID: "room", Locale: ctx.Resolved, Direction: string(ctx.Direction), CurrentUser: "ari", CurrentTenantID: "t",
				Text:          func(key string) string { return ctx.Text(key) },
				Conversations: []chatui.Conversation{{ID: "room", Name: "People"}}}
			for _, state := range []string{"browse", "search", "tone"} {
				markup := html.UnescapeString(chatui.EmojiPickerMarkupForTest(t, m, state))
				if strings.Contains(markup, "⟦") || strings.Contains(markup, "chat.emoji") {
					t.Fatalf("%s/%s: the picker prints a copy key: %s", tc.locale, state, regexp.MustCompile(`.{30}(⟦[^⟧]*⟧|chat\.emoji[a-z._-]*)`).FindString(markup))
				}
				for _, want := range []string{tc.search} {
					if !strings.Contains(markup, want) {
						t.Errorf("%s/%s: the picker lacks %q", tc.locale, state, want)
					}
				}
				if state == "browse" {
					for _, want := range []string{tc.frequent, tc.group} {
						if !strings.Contains(markup, `aria-label="`+want+`"`) {
							t.Errorf("%s: no tab named %q", tc.locale, want)
						}
					}
				}
			}
		})
	}
}

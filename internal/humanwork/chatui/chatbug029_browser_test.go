package chatui_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_CHATBUG_029_Browser renders the open picker with the product's own
// catalog and the shipped emoji data in the three languages: it has a search
// field, a tab for each of Unicode's categories named in the reader's language,
// "Frequently used" in place of an empty "Recent", and more than the eight
// emoji the old picker had.
func TestTodo_CHATBUG_029_Browser(t *testing.T) {
	for _, tc := range []struct {
		locale, search, frequent string
		categories               []string
	}{
		{"en-US", "Search emoji", "Frequently used", []string{"Smileys & Emotion", "People & Body", "Animals & Nature", "Food & Drink", "Travel & Places", "Activities", "Objects", "Symbols"}},
		{"de-DE", "Emoji suchen", "Häufig verwendet", []string{"Smileys & Emotionen"}},
		{"ar", "البحث عن رمز تعبيري", "الأكثر استخدامًا", []string{"الوجوه والمشاعر"}},
	} {
		ctx := productui.ResolveProductLocale(tc.locale)
		m := chatui.Model{State: chatui.StateReady, SelectedID: "room", Locale: ctx.Resolved, Direction: string(ctx.Direction), CurrentUser: "ari", CurrentTenantID: "t",
			Text:          func(key string) string { return ctx.Text(key) },
			Conversations: []chatui.Conversation{{ID: "room", Name: "People"}}}
		markup := html.UnescapeString(chatui.EmojiPickerMarkupForTest(t, m, "browse"))
		if strings.Contains(markup, "⟦") {
			t.Errorf("%s: the picker prints a copy key: %s", tc.locale, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(markup))
		}
		if !strings.Contains(markup, tc.search) {
			t.Errorf("%s: no search field named %q", tc.locale, tc.search)
		}
		for _, name := range append([]string{tc.frequent}, tc.categories...) {
			if !strings.Contains(markup, `aria-label="`+name+`"`) {
				t.Errorf("%s: no category tab named %q", tc.locale, name)
			}
		}
		// Flags has a tab only where the platform draws flags (CHATBUG-043).
		if got := strings.Count(markup, `class="emoji-pop-tab`); got < 9 {
			t.Errorf("%s: %d category tabs, want Frequently used and Unicode's categories", tc.locale, got)
		}
		for _, old := range []string{">Recent<", ">Faces<"} {
			if strings.Contains(markup, old) {
				t.Errorf("%s: the old picker's heading %s is drawn", tc.locale, old)
			}
		}
		// Only the rows near the viewport are in the page.
		if got := strings.Count(markup, "data-emoji="); got <= 8 {
			t.Errorf("%s: the open picker draws %d emoji", tc.locale, got)
		}
	}
}

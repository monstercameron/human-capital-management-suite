package chatui

import (
	"regexp"
	"strings"
	"testing"
)

// TestTodo_CHATBUG_041_Browser draws the sidebar's Saved row: no number while the
// count is not known (the model holds none), the count of items still to do once
// it is, and the same number every time it is drawn for the same list.
func TestTodo_CHATBUG_041_Browser(t *testing.T) {
	count := regexp.MustCompile(`(?s)<span class="chat-count chatsave-count"([^>]*)>([^<]*)</span>`)
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		unknown := renderNode(t, chatsaveSidebar(Model{Locale: locale, CurrentUser: "person", CurrentTenantID: "tenant"}))
		found := count.FindStringSubmatch(unknown)
		if found == nil || !strings.Contains(found[1], "hidden") || strings.TrimSpace(found[2]) != "" {
			t.Errorf("%s: the Saved row shows a number before the count is known: %s", locale, unknown)
		}
		known := Model{Locale: locale, CurrentUser: "person", CurrentTenantID: "tenant", SavedOpenCount: 4}
		first, second := renderNode(t, chatsaveSidebar(known)), renderNode(t, chatsaveSidebar(known))
		shown := count.FindStringSubmatch(first)
		if shown == nil || strings.Contains(shown[1], "hidden") || strings.TrimSpace(shown[2]) != chatCount(locale, 4) || first != second {
			t.Errorf("%s: the Saved row does not show the count of items to do, steadily: %s", locale, first)
		}
		if strings.Contains(first, "⟦") || strings.Contains(unknown, "⟦") {
			t.Errorf("%s: the row prints a copy key", locale)
		}
	}
}

package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestTodo_CHATMOD_005_SearchView: the Moderation page keeps its search box
// while a search is showing, with the words in it, also when nothing matched,
// and says that nothing matched instead of "Nothing to review". Without the
// box a search with no result left no way back but closing the page.
func TestTodo_CHATMOD_005_SearchView(t *testing.T) {
	for locale, want := range map[string]struct{ none, nothing string }{
		"en-US": {"No items match your search.", "Nothing to review."},
		"de-DE": {"Keine Einträge passen zur Suche.", "Nichts zu überprüfen."},
		"ar":    {"لا توجد عناصر تطابق بحثك.", "لا شيء للمراجعة."},
	} {
		found := chatremoveMarkup(t, ModerationPage(ModerationPageModel{Locale: locale, State: StateReady, Tab: "open", Query: "client names", Items: []chat.ModerationItem{
			{ID: "report:1", Kind: "report", State: "CLOSED", PostID: "post", Message: chat.Post{ID: "post", Body: "client names in the channel"}, Reason: "sensitive_information"},
		}}))
		if !strings.Contains(found, `id="chatremove-search"`) || !strings.Contains(found, `data-chat-value="client names"`) || !strings.Contains(found, `data-chatremove="filter"`) {
			t.Errorf("%s: a search that found items lost its box or its words: %s", locale, found)
		}

		none := chatremoveMarkup(t, ModerationPage(ModerationPageModel{Locale: locale, State: StateReady, Tab: "open", Query: "no such words"}))
		if !strings.Contains(none, `id="chatremove-search"`) || !strings.Contains(none, `data-chat-value="no such words"`) {
			t.Errorf("%s: a search that found nothing has no box to change or clear it: %s", locale, none)
		}
		if !strings.Contains(none, want.none) || strings.Contains(none, want.nothing) {
			t.Errorf("%s: a search that found nothing does not say %q: %s", locale, want.none, none)
		}

		// With no search and nothing to review there is nothing to search.
		empty := chatremoveMarkup(t, ModerationPage(ModerationPageModel{Locale: locale, State: StateReady, Tab: "open"}))
		if strings.Contains(empty, `id="chatremove-search"`) || !strings.Contains(empty, want.nothing) {
			t.Errorf("%s: an empty queue shows a search box, or lost its empty state: %s", locale, empty)
		}
	}
}

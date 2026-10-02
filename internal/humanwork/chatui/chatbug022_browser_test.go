package chatui_test

import (
	"html"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func chatbug022Render(t *testing.T, locale string, view chatui.ChatSearchView) string {
	t.Helper()
	markup, err := ui.RenderToString(chatui.RenderChatSearch(locale, view))
	if err != nil {
		t.Fatal(err)
	}
	return html.UnescapeString(markup)
}

// TestTodo_CHATBUG_022_Browser: a search that is waiting says so at once, and a
// search that failed says so once, in plain words, with Try again beside it,
// in every language, and never with a copy key showing.
func TestTodo_CHATBUG_022_Browser(t *testing.T) {
	words := map[string]struct{ searching, failed, retry string }{
		"en-US": {"Searching…", "Search is not available right now.", "Try again"},
		"de-DE": {"Aktuelle Inhalte werden durchsucht…", "Die Suche ist gerade nicht verfügbar.", "Erneut versuchen"},
		"ar":    {"جارٍ البحث في المحتوى الحالي…", "البحث غير متاح حالياً.", "حاول مرة أخرى"},
	}
	for locale, want := range words {
		waiting := chatbug022Render(t, locale, chatui.ChatSearchView{Query: "holiday", Loading: true})
		lane3NoLeaks(t, locale+" searching", waiting)
		if !strings.Contains(waiting, want.searching) || !strings.Contains(waiting, `aria-busy="true"`) {
			t.Errorf("%s: the waiting search shows no progress line: %s", locale, waiting)
		}

		failed := chatbug022Render(t, locale, chatui.ChatSearchView{Query: "holiday", Error: "error"})
		lane3NoLeaks(t, locale+" failed search", failed)
		if strings.Count(failed, want.failed) != 1 {
			t.Errorf("%s: the failure is not said exactly once: %d times", locale, strings.Count(failed, want.failed))
		}
		if !strings.Contains(failed, `data-chatsearch-action="retry"`) || !strings.Contains(failed, ">"+want.retry+"<") {
			t.Errorf("%s: the failure has no %q control: %s", locale, want.retry, failed)
		}
		if strings.Contains(failed, want.searching) {
			t.Errorf("%s: the progress line stays after the failure", locale)
		}
	}
}

// TestTodo_CHATBUG_022: the failure of a later search leaves the answer the box
// already held on screen and still offers the retry; a waiting search disables
// the retry so one press is one request.
func TestTodo_CHATBUG_022(t *testing.T) {
	answer := chatsearch.Response{Groups: []chatsearch.Group{{Kind: chatsearch.Message, Count: 1, Rows: []chatsearch.Row{{Kind: chatsearch.Message, Text: "Holiday schedule", Target: chatsearch.Target{ConversationID: "room", MessageID: "m1", Sequence: 3}}}}}}
	failed := chatbug022Render(t, "en-US", chatui.ChatSearchView{Query: "holiday", Error: "error", Response: answer})
	if strings.Count(failed, "Search is not available right now.") != 1 || !strings.Contains(failed, `data-chatsearch-action="retry"`) || !strings.Contains(failed, `<mark class="search-hit">Holiday</mark> schedule`) {
		t.Errorf("a failure after an answer lost the line, the retry or the answer: %s", failed)
	}

	waiting := chatbug022Render(t, "en-US", chatui.ChatSearchView{Query: "holiday", Loading: true, Response: answer})
	for _, control := range strings.Split(waiting, "<button")[1:] {
		if strings.Contains(control, `data-chatsearch-action="retry"`) && !strings.Contains(control[:strings.Index(control, ">")], "disabled") {
			t.Errorf("the retry is live while a search is waiting: %s", control)
		}
	}
}

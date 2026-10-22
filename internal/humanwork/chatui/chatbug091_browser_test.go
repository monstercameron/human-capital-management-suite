package chatui_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATBUG_091_Browser draws Saved the way the review found it (To do
// empty, Done holding two) in each language: the empty tab says what is true and
// links to the tab that has the items, the first-use sentence is kept for a
// Saved that never held anything, and the sidebar marks exactly one row as the
// one open.
func TestTodo_CHATBUG_091_Browser(t *testing.T) {
	type want struct{ truth, link, firstUse string }
	words := map[string]want{
		"en-US": {"Nothing left to do. Done holds 2.", "Show Done", "Hover a message and press the bookmark to keep it here."},
		"de-DE": {"Nichts mehr zu erledigen. Unter Erledigt: 2.", "Erledigt anzeigen", ""},
		"ar":    {"لا شيء متبقٍ للتنفيذ. في تم: ٢.", "عرض تم", ""},
	}
	for _, locale := range w7Locales {
		render := func(view chatui.SavedMessagesView) *xhtml.Node {
			t.Helper()
			markup, err := ui.RenderToString(chatui.RenderSavedMessages(view))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(markup, "⟦") {
				t.Fatalf("%s: Saved prints a copy key", locale)
			}
			return w7Parse(t, markup)
		}
		w := words[locale]
		firstUse := chatui.SavedMessagesCopy(locale).EmptyTodo

		// Saved opens on the first tab that holds items.
		if tab := chatui.ChatBug091FirstTab(0, 2, 3); tab != "done" {
			t.Errorf("%s: Saved with nothing to do and two done opens on %q, want done", locale, tab)
		}

		// To do is empty and Done holds two: the panel says so, and links to Done.
		page := render(chatui.SavedMessagesView{Locale: locale, Tab: "todo", TodoCount: 0, DoneCount: 2, AllCount: 3})
		text := w7Text(page)
		if !strings.Contains(text, w.truth) {
			t.Errorf("%s: an empty To do beside a Done of two does not say %q: %s", locale, w.truth, text)
		}
		if strings.Contains(text, firstUse) {
			t.Errorf("%s: an empty To do beside a Done of two prints the first-use sentence", locale)
		}
		link := w7One(t, locale+" link to the tab with items", page, func(n *xhtml.Node) bool { return w7Class(n, "chatsave-empty-link") })
		if w7Attr(link, "data-saved-tab") != "done" || w7Text(link) != w.link {
			t.Errorf("%s: the link to Done is %q for tab %q, want %q for done", locale, w7Text(link), w7Attr(link, "data-saved-tab"), w.link)
		}
		// The tab strip tells the same story as the sentence.
		for tab, count := range map[string]string{"todo": "0", "done": "2", "all": "3"} {
			button := w7One(t, locale+" tab "+tab, page, func(n *xhtml.Node) bool {
				return n.Data == "button" && w7Attr(n, "data-saved-tab") == tab && w7Attr(n, "role") == "tab"
			})
			if got := w7Text(button); !strings.Contains(got, s31Digits(locale, count)) {
				t.Errorf("%s: tab %s reads %q, want the count %s", locale, tab, got, s31Digits(locale, count))
			}
		}

		// Nothing ever saved: the first-use sentence alone, with no link.
		never := render(chatui.SavedMessagesView{Locale: locale, Tab: "todo"})
		if !strings.Contains(w7Text(never), firstUse) || len(w7All(never, func(n *xhtml.Node) bool { return w7Class(n, "chatsave-empty-link") })) != 0 {
			t.Errorf("%s: a Saved that never held anything does not print the first-use sentence alone", locale)
		}
		if locale == "en-US" && firstUse != w.firstUse {
			t.Errorf("the first-use sentence is %q, want %q", firstUse, w.firstUse)
		}
	}

	// In the sidebar one row is the open one: the conversation, not Saved too.
	for _, locale := range w7Locales {
		root := w7Parse(t, w7Page(t, locale, nil))
		rail := w7One(t, locale+" sidebar", root, func(n *xhtml.Node) bool { return w7Class(n, "chat-rail") })
		selected := w7All(rail, func(n *xhtml.Node) bool { return w7Class(n, "chat-row") && w7Class(n, "selected") })
		if len(selected) != 1 {
			t.Errorf("%s: %d sidebar rows are drawn selected, want exactly one", locale, len(selected))
		}
		saved := w7All(rail, func(n *xhtml.Node) bool { return w7Class(n, "chatsave-sidebar-row") })
		if len(saved) == 0 || w7Attr(saved[0], "aria-expanded") != "false" || w7Class(saved[0], "selected") {
			t.Errorf("%s: the Saved row is drawn open while its panel is closed", locale)
		}
	}
}

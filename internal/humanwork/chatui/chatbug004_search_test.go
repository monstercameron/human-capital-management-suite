package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// CHATBUG-004: the failure of search used to print a feature notice above the
// channel header, draw a second search dialog and leave both behind after the
// box was cleared. The one quiet line now lives in the results area.
func TestTodo_CHATBUG_004(t *testing.T) {
	if got := chatsearchText("en-US", "unavailable"); got != "Search is not available right now." {
		t.Fatalf("english unavailable line = %q", got)
	}
	seen := map[string]bool{}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		line := chatsearchText(locale, "unavailable")
		if line == "" || seen[line] || line == chatPolishUnavailable(locale) {
			t.Fatalf("%s unavailable line %q is missing, repeated or the generic feature notice", locale, line)
		}
		seen[line] = true
		markup := renderNode(t, RenderChatSearch(locale, ChatSearchView{Query: "holiday", Error: "unavailable"}))
		if !strings.Contains(markup, line) || strings.Contains(markup, chatPolishUnavailable(locale)) {
			t.Fatalf("%s failure markup %q", locale, markup)
		}
		if strings.Contains(markup, `data-chatsearch-action`) || strings.Contains(markup, "<input") {
			t.Fatalf("%s failure markup draws controls: %q", locale, markup)
		}
		hits := renderNode(t, RenderChatSearch(locale, ChatSearchView{Query: "holiday", Response: chatsearch.Response{Groups: []chatsearch.Group{{Kind: chatsearch.Message, Count: 1, Rows: []chatsearch.Row{{Kind: chatsearch.Message, ID: "m1", Text: "The Holiday schedule"}}}}}}))
		if !strings.Contains(hits, `<mark class="search-hit">Holiday</mark>`) {
			t.Fatalf("%s match not highlighted: %q", locale, hits)
		}
	}
}

func TestTodo_CHATBUG_004_Browser(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		count := func(markup string, match func(*xhtml.Node) bool) int { return len(chatPolishNodes(t, markup, match)) }
		searchBoxes := func(n *xhtml.Node) bool {
			return n.Data == "input" && chatPolishAttr(n, "id") == "chat-search" && chatPolishAttr(n, "type") == "search"
		}
		m.Search = "holiday"
		page := chatPolishMarkup(t, Build(m), width, theme)
		if n := count(page, searchBoxes); n != 1 {
			t.Fatalf("%d search boxes while searching", n)
		}
		if strings.Contains(page, `data-chat-layer="search"`) || strings.Contains(page, "chat-search-layer") || strings.Contains(page, "rail-search-trigger") {
			t.Fatal("a second search dialog is drawn")
		}
		if n := count(page, func(n *xhtml.Node) bool { return chatPolishAttr(n, "id") == "chatsearch-results" }); n != 1 {
			t.Fatalf("%d results mounts", n)
		}
		mounts := chatPolishNodes(t, page, func(n *xhtml.Node) bool { return chatPolishAttr(n, "id") == "chatsearch-results" })
		if !chatPolishAncestor(mounts[0], "chat-search-results") {
			t.Fatal("the results mount is outside the results area")
		}
		// The anchored dialog receives the one field while the rail shows its trigger.
		rail := chatPolishMarkup(t, rail(m, handlers{local: localUI{searchOpen: true}}), width, theme)
		if n := count(rail, searchBoxes); n != 0 {
			t.Fatalf("rail duplicates the dialog with %d fields", n)
		}
		layer := chatPolishMarkup(t, chatSearchLayer(m, handlers{local: localUI{searchOpen: true}}), width, theme)
		if n := count(layer, searchBoxes); n != 1 {
			t.Fatalf("dialog has %d fields", n)
		}

		// Clearing the box restores the conversation: no results area, no notice.
		m.Search = ""
		cleared := chatPolishMarkup(t, Build(m), width, theme)
		for _, gone := range []string{`id="chatsearch-results"`, `id="chat-search-results"`, chatsearchText(m.Locale, "unavailable")} {
			if strings.Contains(cleared, gone) {
				t.Fatalf("cleared search still draws %q", gone)
			}
		}
		if n := count(cleared, searchBoxes); n != 1 {
			t.Fatalf("%d search boxes after clearing", n)
		}
	})
}

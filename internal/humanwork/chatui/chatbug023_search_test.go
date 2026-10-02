package chatui

import (
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

func chatbug023Response(at time.Time) chatsearch.Response {
	return chatsearch.Response{Groups: []chatsearch.Group{{Kind: chatsearch.Message, Count: 2, Rows: []chatsearch.Row{
		{Kind: chatsearch.Message, ID: "m1", AuthorID: "alice", At: at, Text: "The holiday schedule is posted", Target: chatsearch.Target{ConversationID: "people", MessageID: "m1", Sequence: 4}},
		{Kind: chatsearch.Message, ID: "m2", AuthorID: "alice", At: at, Text: "Holiday party on Friday", Target: chatsearch.Target{ConversationID: "general", MessageID: "m2", Sequence: 9}},
	}}}}
}

func chatbug023Text(markup string) string {
	root, _ := xhtml.Parse(strings.NewReader(markup))
	var out strings.Builder
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.TextNode {
			out.WriteString(n.Data + "\n")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out.String()
}

// CHATBUG-023: a result says where it is (conversation, author, time) and the
// results state their count, instead of a bare "Open result" and the word
// "results".
func TestTodo_CHATBUG_023(t *testing.T) {
	at := time.Now().Add(-time.Hour)
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chat4Fixture(locale, "sent", false)
		m.Search = "holiday"
		view := ChatSearchView{Query: "holiday", Response: chatbug023Response(at)}
		m.ChatSearch = &view
		mount := renderNode(t, ChatSearchMount(m))
		open := chatsearchText(locale, "open")
		if strings.Contains(mount, ">"+open+"<") || strings.Contains(mount, ">"+open+" ·") {
			t.Fatalf("%s: a result is still labelled only %q: %s", locale, open, mount)
		}
		for _, want := range []string{"#people-ops", "#general", "Alice", chat5Clock(locale, at)} {
			if !strings.Contains(mount, want) {
				t.Fatalf("%s: a result does not say where it is, %q missing: %s", locale, want, mount)
			}
		}
		if label := searchCountLabel(m); !strings.Contains(label, "2") && !strings.Contains(label, "٢") {
			t.Fatalf("%s: the heading count is %q", locale, label)
		}
		if !strings.Contains(mount, `data-active="true"`) {
			t.Fatalf("%s: the results area is not marked active", locale)
		}
		view.Loading = true
		if searchCountLabel(m) != "" {
			t.Fatalf("%s: a count is stated while the search is still running", locale)
		}
		missing := renderNode(t, RenderChatSearch(locale, ChatSearchView{Query: "holiday", Response: chatsearch.Response{Unavailable: []chatsearch.Kind{chatsearch.Saved}}}))
		if strings.Contains(missing, "{kinds}") || !strings.Contains(missing, chatsearchKind(locale, chatsearch.Saved)) {
			t.Fatalf("%s: the unavailable source is not named: %s", locale, missing)
		}
	}
	if got := chatsearchKind("en-US", chatsearch.Pin); got != "Pinned messages" {
		t.Fatalf("pin heading %q", got)
	}
	one := ChatSearchView{Query: "x", Response: chatsearch.Response{Groups: []chatsearch.Group{{Kind: chatsearch.Message, Count: 1, Rows: []chatsearch.Row{{Kind: chatsearch.Message, ID: "m"}}}}}}
	m := chat4Fixture("en-US", "sent", false)
	m.Search, m.ChatSearch = "x", &one
	if searchCountLabel(m) != "1 result" {
		t.Fatalf("one result reads %q", searchCountLabel(m))
	}
}

func TestTodo_CHATBUG_023_Browser(t *testing.T) {
	at := time.Now().Add(-time.Hour)
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		count := func(markup string, match func(*xhtml.Node) bool) int { return len(chatPolishNodes(t, markup, match)) }
		boxes := func(n *xhtml.Node) bool {
			return n.Data == "input" && chatPolishAttr(n, "type") == "search" && chatPolishAttr(n, "id") == "chat-search"
		}
		view := ChatSearchView{Query: "holiday", Loading: true}
		m.Search, m.ChatSearch = "holiday", &view

		// While the search runs: one box, a progress line, no second dialog.
		page := chatPolishMarkup(t, Build(m), width, theme)
		if n := count(page, boxes); n != 1 {
			t.Fatalf("%d search boxes while searching", n)
		}
		if strings.Contains(page, "chat-search-layer") || strings.Contains(page, `data-chat-layer="search"`) {
			t.Fatal("the header icon's popover is drawn")
		}
		if !strings.Contains(chatbug023Text(page), chatsearchText(m.Locale, "loading")) {
			t.Fatal("no progress line while the search runs")
		}

		// Answered: the heading states the count, each result names its place.
		view = ChatSearchView{Query: "holiday", Response: chatbug023Response(at)}
		page = chatPolishMarkup(t, Build(m), width, theme)
		text := chatbug023Text(page)
		if !strings.Contains(page, `id="chatsearch-results"`) || !strings.Contains(text, "#people-ops") {
			t.Fatalf("results not drawn with their conversation: %s", text)
		}
		if heading := count(page, func(n *xhtml.Node) bool { return chatPolishAttr(n, "class") == "conversation-topic search-head-count" }); heading != 1 {
			t.Fatalf("%d count lines under the heading", heading)
		}
		if strings.Contains(page, `data-chatsearch-action="back"`) {
			t.Fatal("the return bar is drawn over the results")
		}

		// A result opened: the conversation shows with the query still in the
		// box, and one bar leads back to the results.
		m.SearchOpened = true
		opened := chatPolishMarkup(t, Build(m), width, theme)
		if strings.Contains(opened, `id="chatsearch-results"`) || strings.Contains(opened, `id="chat-search-results"`) {
			t.Fatal("results still replace the opened conversation")
		}
		if n := strings.Count(opened, `data-chatsearch-action="back"`); n != 1 {
			t.Fatalf("%d return bars over an opened conversation", n)
		}
		if n := count(opened, boxes); n != 1 || !strings.Contains(opened, `data-chat-value="holiday"`) {
			t.Fatalf("the query left the search box (%d boxes)", n)
		}
		if !strings.Contains(opened, `id="chat-composer"`) {
			t.Fatal("the composer is missing under the opened conversation")
		}
		// The bar sits above the messages, never under the composer.
		if strings.Index(opened, `data-chatsearch-action="back"`) > strings.Index(opened, `id="chat-composer"`) {
			t.Fatal("the return bar is under the composer")
		}

		// Cleared: the conversation as it was, with no results and no bar.
		m.Search, m.ChatSearch, m.SearchOpened = "", nil, false
		cleared := chatPolishMarkup(t, Build(m), width, theme)
		for _, gone := range []string{`id="chatsearch-results"`, `id="chat-search-results"`, `data-chatsearch-action="back"`} {
			if strings.Contains(cleared, gone) {
				t.Fatalf("a cleared search still draws %q", gone)
			}
		}
		if !strings.Contains(cleared, `id="chat-composer"`) {
			t.Fatal("the conversation did not come back")
		}
	})
}

package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	xhtml "golang.org/x/net/html"
)

func chatbug077Model() Model {
	return Model{State: StateReady, Locale: "en-US", CurrentUser: "walt", CurrentTenantID: "t", SelectedID: "general",
		Conversations: []Conversation{
			{ID: "general", Name: "general", Kind: PublicChannel, Joined: true},
			{ID: "policy", Name: "Policy Helper", Kind: DirectMessage, Agent: true, AgentID: "policy-helper", Joined: true},
			{ID: "loretta", Name: "Loretta Haynes", Kind: DirectMessage, Joined: true},
		},
		PeerIDs: map[string]string{"loretta": "loretta"},
		Members: []Member{{ID: "walt", HomeTenantID: "t", Name: "Walt Brennan"}, {ID: "loretta", HomeTenantID: "t", Name: "Loretta Haynes"}}}
}

func chatbug077Rows() []chatsearch.Group {
	at := time.Date(2026, time.October, 1, 11, 18, 0, 0, time.UTC)
	answer := chatsearch.Row{Kind: chatsearch.AgentAnswer, ID: "e1", Text: "Employees may carry over up to 40 hours of unused PTO. Carryover ends in March.", Private: true, At: at, Target: chatsearch.Target{ConversationID: "policy", MessageID: "answer", Sequence: 4}}
	return []chatsearch.Group{
		{Kind: chatsearch.AgentAnswer, Count: 1, Rows: []chatsearch.Row{answer}},
		{Kind: chatsearch.Message, Count: 3, Rows: []chatsearch.Row{
			{Kind: chatsearch.Message, ID: "answer", AuthorID: "policy-helper", Text: answer.Text, At: at, Target: chatsearch.Target{ConversationID: "policy", MessageID: "answer", Sequence: 4}},
			{Kind: chatsearch.Message, ID: "q", AuthorID: "walt", Text: "how many PTO hours carry over?", At: at, Target: chatsearch.Target{ConversationID: "general", MessageID: "q", Sequence: 2}},
			{Kind: chatsearch.Message, ID: "dm", AuthorID: "loretta", Text: "Ask HR what will carry over, see doc:doc-47892b80-d600-4401-8244-e2fa2a31caa7", At: at, Target: chatsearch.Target{ConversationID: "loretta", MessageID: "dm", Sequence: 9}},
		}},
		{Kind: chatsearch.Saved, Count: 2, Rows: []chatsearch.Row{
			{Kind: chatsearch.Saved, ID: "s1", AuthorID: "walt", Text: "\nhow many PTO hours carry over?", Private: true, At: at, Target: chatsearch.Target{ConversationID: "general", MessageID: "q", Sequence: 2}},
			{Kind: chatsearch.Saved, ID: "s2", AuthorID: "walt", Text: "\nsaved note about carry over only", Private: true, At: at, Target: chatsearch.Target{ConversationID: "general", MessageID: "s2only", Sequence: 3}},
		}},
	}
}

// TestTodo_CHATBUG_077 holds the logic of the search page: one count, said once
// and reading "No results" at zero, one tint per phrase with no word cut in
// two, and recent searches only while the field is empty.
func TestTodo_CHATBUG_077(t *testing.T) {
	// A phrase is one mark, and a word that holds two of the words is one mark.
	markup := renderNode(t, spanOf(highlightText("Employees may carry over up to Carryover", "carry over")))
	if got := strings.Count(markup, "<mark"); got != 2 || !strings.Contains(markup, `>carry over</mark>`) || !strings.Contains(markup, `>Carryover</mark>`) {
		t.Fatalf("the phrase must be one highlight and Carryover one word: %s", markup)
	}
	// A match inside a word marks the whole word.
	if markup := renderNode(t, spanOf(highlightText("a carryover plan", "carry"))); !strings.Contains(markup, `>carryover</mark>`) {
		t.Fatalf("a word is never split: %s", markup)
	}
	// Words that are apart stay apart.
	if markup := renderNode(t, spanOf(highlightText("carry the over", "carry over"))); strings.Count(markup, "<mark") != 2 {
		t.Fatalf("two matches with a word between are two marks: %s", markup)
	}

	// One agent answer is one result, not an answer and a message.
	groups, saved := chatsearchDedupe(chatbug077Rows())
	total := 0
	for _, g := range groups {
		total += len(g.Rows)
	}
	if total != 4 {
		t.Fatalf("answer, two messages and one saved note are four results, got %d: %+v", total, groups)
	}
	for _, g := range groups {
		for _, row := range g.Rows {
			if row.Kind == chatsearch.Message && row.Target.MessageID == "answer" {
				t.Fatal("the agent answer is listed again under Messages")
			}
			if row.Kind == chatsearch.Saved && row.Target.MessageID == "q" {
				t.Fatal("a saved message is listed twice")
			}
		}
	}
	if !saved[chatsearchMessageKey(chatsearch.Row{Kind: chatsearch.Message, Target: chatsearch.Target{ConversationID: "general", MessageID: "q"}})] {
		t.Fatal("the saved message lost its mark")
	}

	// The count is the deduplicated one, and it reads "No results" at zero.
	m := chatbug077Model()
	view := ChatSearchView{Query: "carry over", Response: chatsearch.Response{Groups: chatbug077Rows()}}
	m.Search, m.ChatSearch = "carry over", &view
	if got := searchCountLabel(m); got != "4 results" {
		t.Fatalf("count %q", got)
	}
	empty := ChatSearchView{Query: "zzz"}
	m.Search, m.ChatSearch = "zzz", &empty
	if got := searchCountLabel(m); got != "No results" {
		t.Fatalf("zero reads %q", got)
	}
	m.Locale = "de-DE"
	if got := searchCountLabel(m); got != "Keine Ergebnisse" {
		t.Fatalf("zero reads %q in German", got)
	}
	one := ChatSearchView{Query: "q", Response: chatsearch.Response{Groups: chatbug077Rows()[1:2]}}
	one.Response.Groups[0].Rows = one.Response.Groups[0].Rows[1:2]
	m.Locale, m.ChatSearch = "en-US", &one
	if got := searchCountLabel(m); got != "1 result" {
		t.Fatalf("one reads %q", got)
	}

	// The page prints the count once: the header has it, the list does not.
	m.Locale, m.Search, m.ChatSearch = "en-US", "carry over", &view
	m.Conversations[0].Joined = true
	page := renderNode(t, Build(m))
	if got := strings.Count(page, "4 results"); got != 1 {
		t.Fatalf("the count is printed %d times", got)
	}
	if strings.Contains(page, "chatsearch-count") || strings.Contains(page, "· 3") || strings.Contains(page, "Messages ·") {
		t.Fatalf("the list repeats the count: %s", page)
	}

	// Recent searches are offered for an empty box and never under results.
	recent := ChatSearchView{Query: "", Recent: []string{"budget"}}
	if markup := renderNode(t, RenderChatSearch("en-US", recent)); !strings.Contains(markup, "budget") || !strings.Contains(markup, `data-chatsearch-action="recent"`) {
		t.Fatalf("an empty box offers its recent searches: %s", markup)
	}
	recent.Query = "carry"
	if markup := renderNode(t, RenderChatSearch("en-US", recent)); strings.Contains(markup, `data-chatsearch-action="recent"`) || strings.Contains(markup, chatsearchText("en-US", "recent")) {
		t.Fatalf("recent searches are shown under a query: %s", markup)
	}
}

// TestTodo_CHATBUG_077_Browser reads the drawn page: every result starts with
// the conversation it is in, an agent's answer included, and a direct message
// is titled by the other person once.
func TestTodo_CHATBUG_077_Browser(t *testing.T) {
	for locale, in := range map[string]string{"en-US": "in ", "de-DE": "in ", "ar": "in "} {
		m := chatbug077Model()
		m.Locale = locale
		view := ChatSearchView{Query: "carry over", Response: chatsearch.Response{Groups: chatbug077Rows()}, Context: &m}
		root, err := xhtml.Parse(strings.NewReader(renderNode(t, RenderChatSearch(locale, view))))
		if err != nil {
			t.Fatal(err)
		}
		results := chatPolishNodesIn(root, func(n *xhtml.Node) bool {
			return strings.Contains(chatPolishAttr(n, "class"), "chatsearch-result") && n.Data == "div"
		})
		if len(results) != 4 {
			t.Fatalf("%s: %d results, want 4", locale, len(results))
		}
		for _, result := range results {
			first := result.FirstChild
			if first == nil || first.Data != "button" || chatPolishAttr(first, "data-chatsearch-action") != "open" {
				t.Fatalf("%s: a result does not start with the control that opens its conversation", locale)
			}
			line := chatbug077Visible(first)
			if line == "" || strings.Contains(line, "doc:") {
				t.Fatalf("%s: a result starts with %q", locale, line)
			}
		}
		if locale != "en-US" {
			continue
		}
		var lines []string
		for _, result := range results {
			lines = append(lines, chatbug077Visible(result.FirstChild))
		}
		for i, want := range []string{in + "Policy Helper", in + "#general", in + "Loretta Haynes", in + "#general"} {
			if lines[i] != want {
				t.Fatalf("result %d starts with %q, want %q (all: %q)", i, lines[i], want, lines)
			}
		}
		markup := renderNode(t, RenderChatSearch(locale, view))
		for _, bad := range []string{"doc:doc-", "⟦", "Loretta Haynes · Loretta Haynes", "Loretta Haynes - Loretta Haynes"} {
			if strings.Contains(markup, bad) {
				t.Fatalf("the page prints %q: %s", bad, markup)
			}
		}
		if got := strings.Count(markup, `class="chatsearch-saved"`); got != 2 {
			t.Fatalf("two saved messages carry a bookmark, got %d", got)
		}
	}
}

// chatbug077Visible is a node's text as it is read: the avatars, which are
// hidden from a reader, are left out.
func chatbug077Visible(n *xhtml.Node) string {
	var out strings.Builder
	var walk func(*xhtml.Node)
	walk = func(c *xhtml.Node) {
		if c.Type == xhtml.ElementNode && chatPolishAttr(c, "aria-hidden") == "true" {
			return
		}
		if c.Type == xhtml.TextNode {
			out.WriteString(c.Data)
		}
		for k := c.FirstChild; k != nil; k = k.NextSibling {
			walk(k)
		}
	}
	walk(n)
	return strings.TrimSpace(out.String())
}

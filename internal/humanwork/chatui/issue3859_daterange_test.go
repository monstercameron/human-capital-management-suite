package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	xhtml "golang.org/x/net/html"
)

func issue3859View() ChatSearchView {
	return ChatSearchView{
		Query: `Portland in:general kind:message lang:en after:2026-11-01 before:2026-12-01`,
		Response: chatsearch.Response{Groups: []chatsearch.Group{{Kind: chatsearch.Message, Count: 2, Rows: []chatsearch.Row{
			{Kind: chatsearch.Message, ID: "m1", Text: "Portland office opens"},
			{Kind: chatsearch.Message, ID: "m2", Text: "Portland parking"},
		}}}},
		Error:       "daterange",
		DraftBefore: "2026-11-01",
		DraftAfter:  "2026-11-10",
	}
}

// TestIssue3859_DateRangeErrorNamesTheFieldsAndDropsStaleResults: a reversed
// range says After must be earlier than Before next to the two date fields,
// marks them invalid, keeps the person's edited dates in the fields, hides the
// previous results and their count, and offers no generic retry.
func TestIssue3859_DateRangeErrorNamesTheFieldsAndDropsStaleResults(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := issue3859View()
		markup := renderNode(t, RenderChatSearch(locale, view))
		summary := chatsearchText(locale, "daterange")
		if summary == "" || summary == "daterange" || !strings.Contains(markup, summary) {
			t.Fatalf("%s: the summary %q is not shown: %s", locale, summary, markup)
		}
		for field, key := range map[string]string{"after": "daterangeAfter", "before": "daterangeBefore"} {
			input := issue3690Element(t, markup, "chatsearch-"+field)
			if v, _ := issue3690Attr(input, "aria-invalid"); v != "true" {
				t.Fatalf("%s: %s is not marked invalid", locale, field)
			}
			describedby, _ := issue3690Attr(input, "aria-describedby")
			if !strings.Contains(describedby, "chatsearch-"+field+"-error") {
				t.Fatalf("%s: %s is not described by its error (%q)", locale, field, describedby)
			}
			note := issue3690Element(t, markup, "chatsearch-"+field+"-error")
			if got := issue3859Text(note); got != chatsearchText(locale, key) || got == key {
				t.Fatalf("%s: %s error reads %q", locale, field, got)
			}
		}
		if v, _ := issue3690Attr(issue3690Element(t, markup, "chatsearch-after"), "value"); v != "2026-11-10" {
			t.Fatalf("%s: the edited After date was replaced by %q", locale, v)
		}
		if v, _ := issue3690Attr(issue3690Element(t, markup, "chatsearch-before"), "value"); v != "2026-11-01" {
			t.Fatalf("%s: the edited Before date was replaced by %q", locale, v)
		}
		if strings.Contains(markup, "Portland office opens") || strings.Contains(markup, `data-chatsearch-action="retry"`) {
			t.Fatalf("%s: stale results or a generic retry remain: %s", locale, markup)
		}
		if note := chatsearchText(locale, "draftnote"); note == "draftnote" || !strings.Contains(markup, note) {
			t.Fatalf("%s: applied versus edited filters are not told apart", locale)
		}
		if _, ok := chatSearchCount(&view); ok {
			t.Fatalf("%s: a stale result count is still stated", locale)
		}
		m := chat4Fixture(locale, "sent", false)
		m.Search, m.ChatSearch = view.Query, &view
		if got := searchCountLabel(m); got != "" {
			t.Fatalf("%s: heading still says %q", locale, got)
		}
	}
}

func issue3859Text(n *xhtml.Node) string {
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}

// TestIssue3837_AnyLanguageIsTheDefaultSelection: with no language in the query
// the "Written in" and "Content kind" selects mark their empty "any" option as
// selected, and no other option, so Apply adds no lang: or kind: filter.
func TestIssue3837_AnyLanguageIsTheDefaultSelection(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup := renderNode(t, RenderChatSearch(locale, ChatSearchView{Query: "open enrollment"}))
		for id, want := range map[string]string{"chatsearch-written": chattoneW10Text(locale, "search_any_language"), "chatsearch-kind": chatsearchText(locale, "all")} {
			sel := issue3690Element(t, markup, id)
			selected := []string{}
			for c := sel.FirstChild; c != nil; c = c.NextSibling {
				if c.Type != xhtml.ElementNode {
					continue
				}
				if _, ok := issue3690Attr(c, "selected"); ok {
					v, _ := issue3690Attr(c, "value")
					selected = append(selected, v+"|"+issue3859Text(c))
				}
			}
			if len(selected) != 1 || selected[0] != "|"+want {
				t.Fatalf("%s %s: selected options %v, want only the empty %q option", locale, id, selected, want)
			}
		}
	}
}

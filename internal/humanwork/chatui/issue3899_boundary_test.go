package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

func issue3899View(more bool) ChatSearchView {
	groups := issue3900Rows(4)
	v := ChatSearchView{Query: "png", Response: chatsearch.Response{Groups: groups}}
	if more {
		v.Response.NextCursor = "c2"
	}
	return v
}

// While the next page loads the results already read stay on screen; they used
// to be replaced by a loading frame, which reset the scroll and dropped focus.
func TestIssue3899_ResultsStayWhileNextPageLoads(t *testing.T) {
	m := chatbug077Model()
	view := issue3899View(true)
	view.LoadingMore = true
	got := renderNode(t, RenderChatSearch("en-US", view))
	if n := strings.Count(got, `class="chatsearch-open"`); n != 4 {
		t.Fatalf("the %d results read so far must stay while loading: %s", n, got)
	}
	if strings.Contains(got, `data-chatsearch-action="more"`) || !strings.Contains(got, `aria-busy="true"`) {
		t.Fatalf("the next-results control must be busy, not clickable: %s", got)
	}
	_ = m
}

// The first result a page added is preceded by a labelled, focusable seam that
// announces what came in, in each language; at the end it says nothing is left.
func TestIssue3899_BoundaryMarksFirstNewResultAndAnnounces(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := issue3899View(true)
		view.Fresh = []string{Issue3899RowKey(view.Response.Groups[0].Rows[2]), Issue3899RowKey(view.Response.Groups[0].Rows[3])}
		got := renderNode(t, RenderChatSearch(locale, view))
		if strings.Count(got, `id="`+Issue3899BoundaryID+`"`) != 1 || !strings.Contains(got, `tabIndex="-1"`) || !strings.Contains(got, `role="status"`) {
			t.Fatalf("%s: one focusable announced boundary expected: %s", locale, got)
		}
		at := strings.Index(got, Issue3899BoundaryID)
		if before := strings.Count(got[:at], `class="chatsearch-open"`); before != 2 {
			t.Fatalf("%s: the boundary sits after %d results, want 2", locale, before)
		}
		if !strings.Contains(got, issue3899Count(locale, 2)) || !strings.Contains(got, issue3899Count(locale, 4)) {
			t.Fatalf("%s: the announcement must carry 2 added and 4 shown: %s", locale, got)
		}
		if locale == "en-US" && !strings.Contains(got, "2 more results added. 4 shown.") {
			t.Fatalf("announcement reads wrong: %s", got)
		}
		view.Response.NextCursor = ""
		end := renderNode(t, RenderChatSearch(locale, view))
		if locale == "en-US" && !strings.Contains(end, "That is every result: 4 in all.") {
			t.Fatalf("the end of the list is not announced: %s", end)
		}
		if strings.Contains(end, `data-chatsearch-action="more"`) {
			t.Fatalf("the control is still offered at the end: %s", end)
		}
	}
	// A first page marks nothing.
	if got := renderNode(t, RenderChatSearch("en-US", issue3899View(true))); strings.Contains(got, Issue3899BoundaryID) {
		t.Fatalf("a first page has no boundary: %s", got)
	}
}

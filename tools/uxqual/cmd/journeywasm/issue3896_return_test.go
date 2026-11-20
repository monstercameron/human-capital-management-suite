package main

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func issue3896View(ids ...string) (chatui.ChatSearchView, []chatsearch.Row) {
	rows := []chatsearch.Row{}
	for _, id := range ids {
		rows = append(rows, chatsearch.Row{Kind: chatsearch.File, ID: id, Text: "a.png", Target: chatsearch.Target{ItemID: id}})
	}
	return chatui.ChatSearchView{Query: "png", Response: chatsearch.Response{Groups: []chatsearch.Group{{Kind: chatsearch.File, Count: len(rows), Rows: rows}}}}, rows
}

func issue3896Origin(row chatsearch.Row, scroll float64) chatsearchOrigin {
	target, kind, _ := strings.Cut(chatui.Issue3896RowToken(row), "\x00")
	return chatsearchOrigin{Target: target, Kind: kind, Scroll: scroll}
}

// The result that was opened gets focus back and the list returns to where it
// was scrolled; both used to be lost (focus on the page, list at the top).
func TestIssue3896_ReturnRestoresOriginResultAndScroll(t *testing.T) {
	view, rows := issue3896View("a", "b", "c")
	selector, scroll := chatsearchReturnPlan(issue3896Origin(rows[2], 963), view)
	if scroll != 963 || !strings.HasPrefix(selector, ".chatsearch-open[data-target=") || !strings.Contains(selector, `[data-kind="file"]`) {
		t.Fatalf("plan = %q at %v", selector, scroll)
	}
}

// A result that is no longer held falls back to the labelled summary at the top.
func TestIssue3896_ReturnFallsBackWhenResultIsGone(t *testing.T) {
	view, rows := issue3896View("a", "b", "c")
	gone := issue3896Origin(chatsearch.Row{Kind: chatsearch.File, ID: "zz", Text: "x", Target: chatsearch.Target{ItemID: "zz"}}, 500)
	selector, scroll := chatsearchReturnPlan(gone, view)
	if selector != "#"+chatui.Issue3857SummaryID || scroll != 0 {
		t.Fatalf("plan = %q at %v", selector, scroll)
	}
	if selector, _ = chatsearchReturnPlan(chatsearchOrigin{}, view); selector != "#"+chatui.Issue3857SummaryID {
		t.Fatalf("no origin must land on the summary, got %q", selector)
	}
	_ = rows
}

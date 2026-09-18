package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// UXLIVE-015's RED was measured on the running server: Insights printed
// "This summary covers promotion journeys you can view. Broader workforce
// reporting is not available yet." twice on one screen -- once as the
// action queue's description and once as the evidence card's scope note --
// because insightsPage passed the same copy key to both.
//
// The scope limitation belongs in the evidence card, which exists to
// describe the numbers. The action queue describes the queue.

func uxlive015Doc(t *testing.T, view View) string {
	t.Helper()
	doc, err := ui.RenderToString(insightsPage(view))
	if err != nil {
		t.Fatalf("render insights: %v", err)
	}
	return doc
}

// TestTodo_UXLIVE_015 is the primary red/green test: the scope limitation is
// stated once.
func TestTodo_UXLIVE_015(t *testing.T) {
	view := promoux012View(PageInsights)
	doc := uxlive015Doc(t, view)

	scope := view.Locale.Text("insights.attention_description")
	if got := strings.Count(doc, scope); got != 1 {
		t.Fatalf("scope limitation appears %d times, want 1:\n%s", got, doc)
	}

	queue := view.Locale.Text("insights.attention_queue_detail")
	if queue == "" || strings.HasPrefix(queue, "insights.") {
		t.Fatalf("no queue description copy is published: %q", queue)
	}
	if !strings.Contains(doc, queue) {
		t.Fatalf("the action queue does not describe the queue:\n%s", doc)
	}
}

// TestTodo_UXLIVE_015_Browser keeps the empty queue's own wording, which
// already described the queue rather than the scope.
func TestTodo_UXLIVE_015_Browser(t *testing.T) {
	view := promoux012View(PageInsights)
	view.Work = nil
	doc := uxlive015Doc(t, view)

	scope := view.Locale.Text("insights.attention_description")
	if got := strings.Count(doc, scope); got > 1 {
		t.Fatalf("scope limitation appears %d times on an empty queue, want at most 1:\n%s", got, doc)
	}
}

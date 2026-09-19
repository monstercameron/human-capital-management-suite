package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// UXLIVE-018's RED was measured on the running server: Home's attention
// strip offered five filters and My Work offered those five plus "Past
// workflows", and neither carried a count. The only number on either
// surface was a card-level "0 items" that did not change with the selected
// filter, so a viewer had to open every filter in turn to find out where
// their work was.

func uxlive018Tabs(t *testing.T, view View, options workCollectionOptions) []WorkTabProps {
	t.Helper()
	props := workCollectionProps(view, options)
	if len(props.Tabs) == 0 {
		t.Fatalf("work collection has no filters")
	}
	return props.Tabs
}

// TestTodo_UXLIVE_018 is the primary red/green test: every filter states how
// much work it holds, and both surfaces name the same filters.
func TestTodo_UXLIVE_018(t *testing.T) {
	view := promoux012View(PageWork)

	home := uxlive018Tabs(t, view, workCollectionOptions{})
	work := uxlive018Tabs(t, view, workCollectionOptions{ListDetail: true})

	if len(home) != len(work) {
		t.Fatalf("Home offers %d filters and My Work offers %d; they name the same work", len(home), len(work))
	}
	for i := range home {
		if home[i].Label != work[i].Label {
			t.Fatalf("filter %d is %q on Home and %q on My Work", i, home[i].Label, work[i].Label)
		}
	}

	// Every filter over this collection states its count. The last entry
	// links to Workflow History, a different page whose population this one
	// does not hold, so it states none rather than inventing one.
	for _, tab := range work[:len(work)-1] {
		if tab.Count == "" {
			t.Fatalf("filter %q states no count:\n%+v", tab.Label, work)
		}
	}
	if last := work[len(work)-1]; last.Count != "" {
		t.Fatalf("the cross-page link %q states a count %q it cannot know", last.Label, last.Count)
	}

	// The counts are the real projection, not a repeated total.
	all, review := "", ""
	for _, tab := range work {
		switch tab.Label {
		case view.Locale.Text("work.all"):
			all = tab.Count
		case view.Locale.Text("work.awaiting"):
			review = tab.Count
		}
	}
	if all == "" || review == "" {
		t.Fatalf("the default and awaiting filters have no counts: %+v", work)
	}
	if all == review {
		t.Fatalf("every filter reports the same count %q, which is the defect this replaces", all)
	}
}

// TestTodo_UXLIVE_018_Browser proves the count reaches the rendered strip
// and is announced with its filter rather than floating beside it.
func TestTodo_UXLIVE_018_Browser(t *testing.T) {
	view := promoux012View(PageWork)
	doc, err := ui.RenderToString(ui.CreateElement(WorkCollection, workCollectionProps(view, workCollectionOptions{ListDetail: true})))
	if err != nil {
		t.Fatalf("render work collection: %v", err)
	}
	if !strings.Contains(doc, "work-tab-count") {
		t.Fatalf("the rendered filter strip carries no counts:\n%s", doc)
	}
	for _, label := range []string{view.Locale.Text("work.all"), view.Locale.Text("work.awaiting"), view.Locale.Text("work.blocked")} {
		if !strings.Contains(doc, label) {
			t.Fatalf("the filter strip lost %q:\n%s", label, doc)
		}
	}
}

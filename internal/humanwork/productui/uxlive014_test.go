package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// UXLIVE-014's RED was measured on the running server: the Myself page's
// workflow-history filter rendered the history_year select with exactly one
// option, "Any effective year", because the filter always draws the select
// and only fills it from props.Years. A control whose only value is "no
// filter" cannot filter anything; the person select next to it is already
// guarded the same way by ShowPerson.
//
// The Admin page's other half of this todo -- a status badge that reads
// "Available" on five of six cards -- is covered by the badge assertions
// below.

func uxlive014Filter(t *testing.T, years []HistoryFilterOption) string {
	t.Helper()
	doc, err := ui.RenderToString(WorkflowHistoryFilter(WorkflowHistoryFilterProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Action:    "/workspace/app/myself",
		Years:     years,
	}))
	if err != nil {
		t.Fatalf("render history filter: %v", err)
	}
	return doc
}

// TestTodo_UXLIVE_014 is the primary red/green test: a filter with no values
// to choose between is not rendered.
func TestTodo_UXLIVE_014(t *testing.T) {
	empty := uxlive014Filter(t, nil)
	if strings.Contains(empty, `name="history_year"`) {
		t.Fatalf("a year filter with no years is still offered:\n%s", empty)
	}
	if strings.Contains(empty, ResolveProductLocale("en-US").Text("history.any_year")) {
		t.Fatalf("the year filter's placeholder option survives without any years:\n%s", empty)
	}
	// The rest of the bar is untouched: this removes a dead control, not a
	// working one.
	for _, want := range []string{`name="history_q"`, `name="outcome"`, ResolveProductLocale("en-US").Text("history.apply")} {
		if !strings.Contains(empty, want) {
			t.Fatalf("the filter bar lost %q:\n%s", want, empty)
		}
	}

	populated := uxlive014Filter(t, []HistoryFilterOption{{Value: "2026", Label: "2026"}})
	if !strings.Contains(populated, `name="history_year"`) {
		t.Fatalf("a year filter with a year to choose is missing:\n%s", populated)
	}
	if !strings.Contains(populated, ">2026<") {
		t.Fatalf("the year option is missing:\n%s", populated)
	}
	if !strings.Contains(populated, ResolveProductLocale("en-US").Text("history.any_year")) {
		t.Fatalf("the populated year filter lost its no-filter option:\n%s", populated)
	}
}

// TestTodo_UXLIVE_014_Browser covers the Admin page's badge: a status that
// is the same on every card carries no information, so only a card whose
// state differs from available says anything.
func TestTodo_UXLIVE_014_Browser(t *testing.T) {
	view := testView(PageAdmin)
	doc, err := ui.RenderToString(adminPage(view))
	if err != nil {
		t.Fatalf("render admin: %v", err)
	}
	available := view.Locale.Text("admin.available")
	if got := strings.Count(doc, ">"+available+"<"); got != 0 {
		t.Fatalf("the Admin page prints the %q badge %d times; it varies on no card:\n%s", available, got, doc)
	}
	planned := view.Locale.Text("admin.planned")
	if !strings.Contains(doc, planned) {
		t.Fatalf("the Admin page dropped the %q state, which is the one that carries information:\n%s", planned, doc)
	}
}

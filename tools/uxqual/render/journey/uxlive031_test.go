package journey

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// UXLIVE-031 renders the Journeys tracker's page-level filter: a search
// landmark whose controls are named by the product route keys, one live
// statement of the result count, and an explicit empty result.

func uxlive031Filter(narrowed bool, shown, total int) *JourneyFilterView {
	filter := &JourneyFilterView{
		Label: "Find requests",
		Fields: []Field{
			{ID: "journey-filter-q", Name: "journey_q", Kind: fieldKindText, Label: "Person or request reference", Value: "amara"},
			{ID: "journey-filter-status", Name: "journey_status", Kind: fieldKindSelect, Label: "Status", Value: "review",
				Options: []Option{{Value: "", Label: "All statuses"}, {Value: "review", Label: "In review", Selected: true}}},
			{ID: "journey-filter-from", Name: "journey_from", Kind: fieldKindDate, Label: "Updated from"},
			{ID: "journey-filter-to", Name: "journey_to", Kind: fieldKindDate, Label: "Updated to"},
			{ID: "journey-filter-sort", Name: "journey_sort", Kind: fieldKindSelect, Label: "Order", Value: "recent",
				Options: []Option{{Value: "recent", Label: "Most recently updated", Selected: true}, {Value: "oldest", Label: "Least recently updated"}}},
			{ID: "journey-filter-group", Name: "journey_group", Kind: fieldKindSelect, Label: "Group by", Value: "person",
				Options: []Option{{Value: "person", Label: "Person", Selected: true}, {Value: "status", Label: "Status"}, {Value: "none", Label: "No grouping"}}},
		},
		Submit: "Apply filters", Narrowed: narrowed, Shown: shown, Total: total,
		Result: "1 of 4 requests",
	}
	if narrowed {
		filter.ClearHref, filter.ClearLabel = "#/journeys", "Clear filters"
	}
	if narrowed && shown == 0 {
		filter.Result = "0 of 4 requests"
		filter.EmptyTitle, filter.EmptyDetail = "No requests match these filters", "Try another name or reference, widen the dates, or clear the filters."
	}
	return filter
}

func uxlive031Render(t *testing.T, view ListView) string {
	t.Helper()
	markup, err := ui.RenderToString(journeysSection("en-US", view))
	if err != nil {
		t.Fatalf("render journeys section: %v", err)
	}
	return markup
}

// TestTodo_UXLIVE_031_Browser proves the filter reaches the rendered page as
// a working form: without the live client it is a real GET whose controls
// carry the product route keys, the result count is on the page, and a
// narrowed empty result shows the filter's own empty state with a way back
// instead of the "no requests at all" copy.
func TestTodo_UXLIVE_031_Browser(t *testing.T) {
	card := uxlive010Card("01a0b189-e04c-7265-8926-9095318cf888")
	markup := uxlive031Render(t, ListView{Journeys: []JourneyCard{card}, Filter: uxlive031Filter(true, 1, 4)})
	for _, want := range []string{
		`<form aria-label="Find requests" class="jn-journey-filter" method="get" role="search">`,
		`name="journey_q"`, `name="journey_status"`, `name="journey_from"`, `name="journey_to"`, `name="journey_sort"`, `name="journey_group"`,
		`type="date"`, `>Apply filters<`, `href="#/journeys"`, `>1 of 4 requests<`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("filtered tracker is missing %q:\n%s", want, markup)
		}
	}

	empty := uxlive031Render(t, ListView{Filter: uxlive031Filter(true, 0, 4), Empty: "No promotion requests are visible to track yet."})
	if !strings.Contains(empty, "No requests match these filters") || strings.Contains(empty, "No promotion requests are visible to track yet.") {
		t.Fatalf("a filtered empty result reused the unfiltered empty state:\n%s", empty)
	}
	if !strings.Contains(empty, `class="jn-btn" data-variant="secondary" href="#/journeys"`) {
		t.Fatalf("the filtered empty result offers no way back:\n%s", empty)
	}

	// Wire hands the form to the client and the clear link to navigation.
	var submitted string
	var submittedValues map[string]string
	var navigated string
	store := NewStore(Page{})
	page := Wire(store, Page{List: &ListView{Filter: uxlive031Filter(true, 1, 4)}},
		func(href string) { navigated = href },
		func(id string, values map[string]string) { submitted, submittedValues = id, values })
	page.List.Filter.OnSubmit(map[string]string{"journey_q": "amara"})
	page.List.Filter.OnClear()
	if submitted != actionFilterList || submittedValues["journey_q"] != "amara" || navigated != "#/journeys" {
		t.Fatalf("filter wiring: submit=%q values=%v nav=%q", submitted, submittedValues, navigated)
	}
	// With the live client every control applies itself, so the form has no
	// Apply button; the SSR form above keeps it for a browser without script.
	live, err := ui.RenderToString(journeysSection("en-US", ListView{Journeys: []JourneyCard{card}, Filter: page.List.Filter}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(live, ">Apply filters<") || !strings.Contains(live, `class="jn-journey-filter-actions"`) {
		t.Fatalf("the live form still asks for Apply:\n%s", live)
	}
}

// TestTodo_UXLIVE_031_Accessibility checks the filter's semantics: a named
// search landmark, a label for every control, a polite atomic status for the
// count, and no filter at all over an empty tracker.
func TestTodo_UXLIVE_031_Accessibility(t *testing.T) {
	filter := uxlive031Filter(true, 1, 4)
	markup := uxlive031Render(t, ListView{Journeys: []JourneyCard{uxlive010Card("int-1")}, Filter: filter})
	for _, field := range filter.Fields {
		if !strings.Contains(markup, `<label class="jn-label" for="`+field.ID+`">`+field.Label) {
			t.Fatalf("control %s has no visible label:\n%s", field.ID, markup)
		}
	}
	status := regexp.MustCompile(`<span aria-atomic="true" aria-live="polite" class="jn-chip jn-journey-result"[^>]*role="status">`)
	if !status.MatchString(markup) {
		t.Fatalf("the result count is not a polite atomic status:\n%s", markup)
	}
	if strings.Count(markup, "1 of 4 requests") != 1 {
		t.Fatalf("the result count is stated more than once:\n%s", markup)
	}
	empty := uxlive031Render(t, ListView{Filter: uxlive031Filter(true, 0, 4)})
	if !strings.Contains(empty, `class="jn-panel jn-empty jn-journey-filter-empty" role="status"`) {
		t.Fatalf("the filtered empty result is not announced:\n%s", empty)
	}
	none := uxlive031Render(t, ListView{Filter: &JourneyFilterView{Label: "Find requests", Total: 0, Result: "0 requests"}})
	if strings.Contains(none, `role="search"`) {
		t.Fatalf("an empty tracker offers a filter over nothing:\n%s", none)
	}
}

// TestTodoUXLIVE031StylesheetKeepsFilterResponsive pins the deliberate
// layout: four columns (search wider) on desktop, two on a tablet, one on
// a phone.
func TestTodoUXLIVE031StylesheetKeepsFilterResponsive(t *testing.T) {
	sheet := Stylesheet()
	for _, want := range []string{
		".jn-journey-filter-fields{align-items:end;display:grid;gap:0.75rem;grid-template-columns:minmax(0,1fr);}",
		"@media (min-width:40rem){.jn-journey-filter-fields{grid-template-columns:repeat(2,minmax(0,1fr));}}",
		"@media (min-width:68.75rem){.jn-journey-filter-fields{grid-template-columns:repeat(5,minmax(0,1fr));}}",
		"@media (min-width:68.75rem){.jn-journey-filter-fields>.jn-field:first-child{grid-column:span 2;}}",
		"@media (min-width:68.75rem){.jn-journey-filter-actions{grid-column:span 3;}}",
		`.jn-journey-filter-panel[data-open="false"]{display:none;}`,
		"@media (min-width:40rem){.jn-journey-filter-toggle{display:none;}}",
	} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("the filter grid is missing %q", want)
		}
	}
}

// TestUXLIVE031NarrowFilterDisclosure: on a phone the controls other than
// search sit behind a "Filters" toggle with aria-expanded, open by default
// only when a filter is set, the reader's own choice wins, and a live form
// with nothing to clear renders no empty actions row.
func TestUXLIVE031NarrowFilterDisclosure(t *testing.T) {
	render := func(page Page) string {
		t.Helper()
		markup, err := ui.RenderToString(journeysSectionLive(liveOf(page), *page.List))
		if err != nil {
			t.Fatal(err)
		}
		return markup
	}
	card := uxlive010Card("int-1")
	quiet := uxlive031Filter(false, 4, 4)
	quiet.ToggleLabel, quiet.OnSubmit = "Filters", func(map[string]string) {}
	markup := render(Page{List: &ListView{Journeys: []JourneyCard{card}, Filter: quiet}, OnFieldChange: func(string, string) {}})
	if !strings.Contains(markup, `aria-controls="journey-filter-panel" aria-expanded="false"`) || !strings.Contains(markup, `data-open="false"`) || !strings.Contains(markup, ">Filters<") {
		t.Fatalf("an unfiltered list does not start with the panel closed:\n%s", markup)
	}
	if strings.Contains(markup, `class="jn-journey-filter-actions"`) {
		t.Fatalf("an empty actions row still holds space:\n%s", markup)
	}
	if strings.Index(markup, `name="journey_q"`) > strings.Index(markup, "jn-journey-filter-toggle") {
		t.Fatalf("the search is not above the toggle:\n%s", markup)
	}

	active := uxlive031Filter(true, 1, 4)
	active.ToggleLabel, active.PanelActive = "Filters (2)", 2
	markup = render(Page{List: &ListView{Journeys: []JourneyCard{card}, Filter: active}})
	if !strings.Contains(markup, `aria-expanded="true"`) || !strings.Contains(markup, `data-open="true"`) || !strings.Contains(markup, ">Filters (2)<") {
		t.Fatalf("an active filter is hidden behind a closed toggle:\n%s", markup)
	}
	markup = render(Page{List: &ListView{Journeys: []JourneyCard{card}, Filter: active}, Values: map[string]string{JourneyFilterPanelField: "closed"}})
	if !strings.Contains(markup, `aria-expanded="false"`) || !strings.Contains(markup, `data-open="false"`) {
		t.Fatalf("the reader's choice to close the panel was ignored:\n%s", markup)
	}
}

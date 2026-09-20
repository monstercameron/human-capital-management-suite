package journey

// JourneyFilterView is the Journeys tracker's page-level filter form
// (UXLIVE-031): search by person or request reference, a status, an updated
// date range, a recency order and a grouping, plus the explicit statement
// of how many requests the list shows. Every string is display-ready; the
// renderer decides nothing about which requests match.
type JourneyFilterView struct {
	// Label names the search landmark.
	Label  string
	Fields []Field
	Submit string
	// ToggleLabel is the narrow-screen disclosure's label, e.g. "Filters (2)",
	// and PanelActive the number of filters set behind it (all but search).
	ToggleLabel string
	PanelActive int
	// OnSubmit receives the form's values keyed by field Name. Nil leaves
	// the form a plain GET, which is what the SSR path renders.
	OnSubmit func(values map[string]string)
	// ClearHref returns to the unfiltered list; empty when nothing is set.
	ClearHref  string
	ClearLabel string
	OnClear    func()
	// Narrowed is true when the filter hides at least one kind of request,
	// so the list is a subset rather than every visible request.
	Narrowed bool
	// Shown and Total are the counts Result states.
	Shown int
	Total int
	// Result is the one sentence that states the count, e.g. "3 of 12
	// requests" when narrowed or "12 requests" when not.
	Result string
	// EmptyTitle and EmptyDetail replace the list when a narrowing filter
	// matches nothing. They are distinct from ListView.Empty, which says
	// no request is visible at all.
	EmptyTitle  string
	EmptyDetail string
}

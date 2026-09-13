package productui

// WorkCollectionFilter is the typed work-collection filter
// behind the My Work tabs. The zero value is unknown and
// matches nothing fail-closed.
type WorkCollectionFilter int

const (
	// WorkCollectionAll is the tasks view, the page's empty-filter tab:
	// open work the server says needs the viewer's action
	// (PROMOUX-012, WorkNeedsViewerAction).
	WorkCollectionAll WorkCollectionFilter = iota + 1
	// WorkCollectionReview is the approvals view: actionable items whose
	// next step is any approval decision (manager, finance, repeat or
	// generic), by server next-step code.
	WorkCollectionReview
	// WorkCollectionBlocked is the blocked view: actionable proposals the
	// viewer must correct.
	WorkCollectionBlocked
	// WorkCollectionComplete is the completed view.
	WorkCollectionComplete
	// WorkCollectionMine is UXAUDIT-017's "assigned to me" view: open items
	// whose current work item the server says the viewer holds or may claim.
	WorkCollectionMine
	// WorkCollectionTracked is PROMOUX-012's tracked requests view: open
	// promotions the server names the viewer as initiating, whatever their
	// next step -- including passive waits, which appear here and never in
	// the actionable views.
	WorkCollectionTracked
)

// ParseWorkCollectionFilter resolves a request filter string
// to the typed contract. The empty request is the tasks view;
// anything unrecognized is unknown and matches nothing.
func ParseWorkCollectionFilter(raw string) WorkCollectionFilter {
	switch raw {
	case "":
		return WorkCollectionAll
	case "review":
		return WorkCollectionReview
	case "blocked":
		return WorkCollectionBlocked
	case "complete":
		return WorkCollectionComplete
	case "mine":
		return WorkCollectionMine
	case "tracked":
		return WorkCollectionTracked
	}
	return WorkCollectionFilter(0)
}

// FilterWorkCollection projects one work stream through a
// typed collection filter in admission order. Items pass
// through untouched.
func FilterWorkCollection(items []WorkItem, filter WorkCollectionFilter) []WorkItem {
	filtered := make([]WorkItem, 0, len(items))
	for _, item := range items {
		var include bool
		switch filter {
		case WorkCollectionAll:
			include = WorkNeedsViewerAction(item)
		case WorkCollectionReview:
			include = WorkNeedsViewerAction(item) && WorkAwaitsDecision(item)
		case WorkCollectionBlocked:
			include = WorkNeedsViewerAction(item) && item.NextStep == "correct_proposal"
		case WorkCollectionComplete:
			include = item.Terminal
		case WorkCollectionMine:
			include = !item.Terminal && WorkViewerOwnershipRank(item) < 2
		case WorkCollectionTracked:
			include = !item.Terminal && WorkViewerInitiated(item)
		}
		if include {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

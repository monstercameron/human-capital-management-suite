package productui

// WorkCollectionFilter is the typed work-collection filter
// behind the My Work tabs. The zero value is unknown and
// matches nothing fail-closed.
type WorkCollectionFilter int

const (
	// WorkCollectionAll is the tasks view: open work in
	// admission order, matching the page's empty-filter tab.
	WorkCollectionAll WorkCollectionFilter = iota + 1
	// WorkCollectionReview is the approvals view: items
	// awaiting a decision.
	WorkCollectionReview
	// WorkCollectionBlocked is the blocked view.
	WorkCollectionBlocked
	// WorkCollectionComplete is the completed view.
	WorkCollectionComplete
	// WorkCollectionMine is UXAUDIT-017's "assigned to me" view: open items
	// whose current work item the server says the viewer holds or may claim.
	WorkCollectionMine
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
			include = !item.Terminal
		case WorkCollectionReview:
			include = item.Status == "Awaiting approval"
		case WorkCollectionBlocked:
			include = item.Status == "Blocked"
		case WorkCollectionComplete:
			include = item.Terminal
		case WorkCollectionMine:
			include = !item.Terminal && WorkViewerOwnershipRank(item) < 2
		}
		if include {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

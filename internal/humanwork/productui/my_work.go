package productui

import "sort"

// WorkUrgencyRank ranks one work item's urgency for the My Work queue: lower
// values sort first. UXAUDIT-017 requires the queue to order by urgency
// rather than by admission/recency, and requires that ordering to be a
// computed, testable property of the data rather than a status list buried
// in the renderer -- so this is that property, and workCollectionProps calls
// it rather than switching on Status itself.
//
// The rank is derived from Tone, the presentation dimension the server's own
// stage already resolves to (see tools/uxqual/productclient's
// stagePresentation): Tone is shared with the row's status chip, so ranking
// from it is ordering what the server already disclosed, never a new client
// guess about what a status permits. Terminal items always rank last
// regardless of tone: a completed or historical journey never outranks an
// item still open for the viewer's attention. Danger-toned items (a journey
// that needs repair) outrank warning-toned ones (blocked, or awaiting
// someone's approval), which in turn outrank neutral/success-toned items
// merely waiting on a future date or step.
func WorkUrgencyRank(item WorkItem) int {
	if item.Terminal {
		return 3
	}
	switch item.Tone {
	case "danger":
		return 0
	case "warning":
		return 1
	default:
		return 2
	}
}

// SortWorkByUrgency orders a work stream by WorkUrgencyRank, most urgent
// first. Items of equal rank keep their relative (server-admission) order --
// urgency is the only dimension this changes -- and the input is never
// mutated.
func SortWorkByUrgency(items []WorkItem) []WorkItem {
	sorted := make([]WorkItem, len(items))
	copy(sorted, items)
	sort.SliceStable(sorted, func(i, j int) bool {
		return WorkUrgencyRank(sorted[i]) < WorkUrgencyRank(sorted[j])
	})
	return sorted
}

// MyWorkItems scopes one work stream to the viewer's
// collection for the My Work page: items whose PersonRef
// equals the viewer's PersonID, in admission order. Empty
// viewer identities and empty refs match nothing
// fail-closed, so unassigned work never leaks into a
// collection and logged-out viewers see none. Items pass
// through untouched.
func MyWorkItems(items []WorkItem, viewer ViewerProfile) []WorkItem {
	mine := make([]WorkItem, 0, len(items))
	if viewer.PersonID == "" {
		return mine
	}
	for _, item := range items {
		if item.PersonRef != "" && item.PersonRef == viewer.PersonID {
			mine = append(mine, item)
		}
	}
	return mine
}

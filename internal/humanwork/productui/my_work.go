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

// WorkViewerOwnershipRank ranks how the viewer stands to an item's current
// work item, from the server's summary: 0 when the viewer holds it (claimant
// or direct assignee), 1 when the viewer may claim it (candidate), 2
// otherwise -- including every item the server disclosed no summary for.
func WorkViewerOwnershipRank(item WorkItem) int {
	switch item.ViewerMembership {
	case "CLAIMANT", "ASSIGNEE":
		return 0
	case "CANDIDATE":
		return 1
	default:
		return 2
	}
}

// WorkNextAction is the single next executable action for the viewer: the
// most decisive token the server placed in PermittedActions, or "" when the
// viewer has none. It never infers an action the server did not grant.
func WorkNextAction(item WorkItem) string {
	for _, preferred := range []string{"decide_approval", "complete", "claim", "release"} {
		for _, granted := range item.PermittedActions {
			if granted == preferred {
				return preferred
			}
		}
	}
	return ""
}

// SortWorkByUrgency orders a work stream as an action queue, most urgent
// first. The keys, in order:
//
//  1. WorkUrgencyRank (repair, then blocked/approval, then the rest, then
//     terminal);
//  2. ownership from the server's work item summary
//     (WorkViewerOwnershipRank): held by the viewer, then claimable by the
//     viewer, then everything else;
//  3. the stage-derived fallback: an item whose next step a person holds
//     (AwaitsPerson) before one the workflow or the calendar is carrying;
//  4. the work item's real deadline (WorkDue), soonest first, undated last;
//  5. the row's effective date (Due), soonest first, undated last.
//
// Dates are ISO-8601 (YYYY-MM-DD), so string order is date order. Items equal
// on every key keep their relative (server-admission) order, and the input is
// never mutated.
func SortWorkByUrgency(items []WorkItem) []WorkItem {
	sorted := make([]WorkItem, len(items))
	copy(sorted, items)
	sort.SliceStable(sorted, func(i, j int) bool {
		return workQueueLess(sorted[i], sorted[j])
	})
	return sorted
}

func workQueueLess(a, b WorkItem) bool {
	if rankA, rankB := WorkUrgencyRank(a), WorkUrgencyRank(b); rankA != rankB {
		return rankA < rankB
	}
	if ownA, ownB := WorkViewerOwnershipRank(a), WorkViewerOwnershipRank(b); ownA != ownB {
		return ownA < ownB
	}
	if a.AwaitsPerson != b.AwaitsPerson {
		return a.AwaitsPerson
	}
	if a.WorkDue != b.WorkDue {
		return isoDateLess(a.WorkDue, b.WorkDue)
	}
	if a.Due != b.Due {
		return isoDateLess(a.Due, b.Due)
	}
	return false
}

// isoDateLess orders two distinct ISO dates soonest first, with "" last.
func isoDateLess(a, b string) bool {
	if a == "" || b == "" {
		return b == ""
	}
	return a < b
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

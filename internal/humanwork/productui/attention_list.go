package productui

import (
	"sort"
	"strings"
	"time"
)

// WorkDisposition describes how an admitted work item is presented in My
// Work. It is deliberately derived from the server-projected status; it is
// not a workflow state and grants no authority.
type WorkDisposition string

const (
	WorkDispositionAction  WorkDisposition = "action"
	WorkDispositionDraft   WorkDisposition = "draft"
	WorkDispositionTracked WorkDisposition = "tracked"
	WorkDispositionPassive WorkDisposition = "passive"
)

// ClassifyWork keeps the action queue distinct from journeys that are merely
// being tracked. Unknown non-terminal statuses remain actionable so a new
// server status is not silently hidden from a person who may need to act.
func ClassifyWork(item WorkItem) WorkDisposition {
	if item.Terminal {
		return WorkDispositionTracked
	}
	status := strings.ToLower(strings.TrimSpace(item.Status))
	if strings.Contains(status, "draft") {
		return WorkDispositionDraft
	}
	for _, token := range []string{"waiting", "pending", "in progress", "processing", "running", "queued", "scheduled", "snoozed"} {
		if token == "waiting" && strings.Contains(status, "awaiting") {
			continue
		}
		if strings.Contains(status, token) {
			return WorkDispositionPassive
		}
	}
	for _, token := range []string{"tracked", "watching", "following"} {
		if strings.Contains(status, token) {
			return WorkDispositionTracked
		}
	}
	return WorkDispositionAction
}

// ActionQueue returns open items that require a human decision or next step.
// Drafts and passive waits have dedicated My Work sections. Admission order
// is retained here; use PrioritizeDueWork when a surface explicitly wants a
// due-first view.
func ActionQueue(items []WorkItem) []WorkItem {
	queue := make([]WorkItem, 0, len(items))
	for _, item := range items {
		if ClassifyWork(item) == WorkDispositionAction {
			queue = append(queue, item)
		}
	}
	return queue
}

// PassiveWaits returns open journeys whose latest server status says they
// are waiting on another party or system. They remain trackable but do not
// compete with the viewer's actionable queue.
func PassiveWaits(items []WorkItem) []WorkItem {
	waits := make([]WorkItem, 0, len(items))
	for _, item := range items {
		if ClassifyWork(item) == WorkDispositionPassive {
			waits = append(waits, item)
		}
	}
	return waits
}

// PrioritizeDueWork returns a stable, non-aliasing due-first projection.
// Valid ISO dates/timestamps sort first, then items without a due date. Ties
// preserve admission order, making a refresh predictable for keyboard and
// screen-reader users.
func PrioritizeDueWork(items []WorkItem) []WorkItem {
	result := append([]WorkItem(nil), items...)
	type due struct {
		item  WorkItem
		index int
		stamp time.Time
		has   bool
	}
	decorated := make([]due, len(result))
	for i, item := range result {
		stamp, ok := parseWorkDue(item.Due)
		decorated[i] = due{item: item, index: i, stamp: stamp, has: ok}
	}
	sort.SliceStable(decorated, func(i, j int) bool {
		if decorated[i].has != decorated[j].has {
			return decorated[i].has
		}
		if decorated[i].has && !decorated[i].stamp.Equal(decorated[j].stamp) {
			return decorated[i].stamp.Before(decorated[j].stamp)
		}
		return decorated[i].index < decorated[j].index
	})
	for i := range decorated {
		result[i] = decorated[i].item
	}
	return result
}

func parseWorkDue(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{time.DateOnly, time.RFC3339, "2 Jan 2006 · 15:04 MST"} {
		if stamp, err := time.Parse(layout, raw); err == nil {
			return stamp, true
		}
	}
	return time.Time{}, false
}

// AttentionList returns the unified attention list: the
// non-terminal items of one admitted stream in admission order.
// Terminal items belong to continuity and history, never
// attention. Order stays server-ranked — presentation assigns
// no priority — and the output never aliases the input.
func AttentionList(items []WorkItem) []WorkItem {
	list := make([]WorkItem, 0, len(items))
	for _, item := range items {
		if !item.Terminal {
			list = append(list, item)
		}
	}
	return list
}

package productui

// CompletedEntry is the derived-only history record for one
// terminal work item: identity plus completion evidence. It
// carries exactly what the completed-work history needs and
// covers precisely the recent-work list (WEB-099).
type CompletedEntry struct {
	ID          string
	CompletedAt string
}

// CompletedHistory projects one admitted work stream to its
// completed-work history in admission order. Evidence passes
// through untouched — including empty stamps, which stay
// empty rather than invented.
func CompletedHistory(items []WorkItem) []CompletedEntry {
	history := make([]CompletedEntry, 0, len(items))
	for _, item := range items {
		if item.Terminal {
			history = append(history, CompletedEntry{ID: item.ID, CompletedAt: item.CompletedAt})
		}
	}
	return history
}

// completedActivityProps maps one completed-work history to its
// recent-activity rail rows, resolving each entry to its admitted
// item by ID for the human label, localized status, link and tone.
// Entries without an admitted item drop fail-closed, so the rail
// can only show completion evidence CompletedHistory derived —
// pages never hand-pick evidence per row again. The output keeps
// history order; callers bound it to the rail limit.
func completedActivityProps(view View, history []CompletedEntry, items []WorkItem) []ActivityProps {
	byID := make(map[string]WorkItem, len(items))
	for _, item := range items {
		if _, seen := byID[item.ID]; !seen {
			byID[item.ID] = item
		}
	}
	activities := make([]ActivityProps, 0, len(history))
	for _, entry := range history {
		item, ok := byID[entry.ID]
		if !ok {
			continue
		}
		activities = append(activities, ActivityProps{Title: homeTaskLabel(view, item), Status: localizedWorkStatus(view.Locale, item), Href: item.Href, Navigate: view.Navigate, Tone: item.Tone})
	}
	return activities
}

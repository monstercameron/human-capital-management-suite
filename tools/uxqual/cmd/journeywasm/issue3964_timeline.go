package main

import (
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Event timeline (#3964), the part that needs no browser. The timeline is the
// Chat search's Message and Thread kinds scoped to the open conversation and a
// range of days, oldest first, so the server applies membership, history,
// deletion, audience and message access exactly as it does for a search. This
// file builds those requests and reads their answers.

// timelinePageSize is how many rows one search page asks for, and
// timelineMaxPages how many pages one kind is read for.
const (
	timelinePageSize = 50
	timelineMaxPages = chatui.TimelineMaxEntries / timelinePageSize
	// timelineLeadPages is how many more pages may be read to get past messages
	// that sit before the range (the search works in whole UTC days), so a busy
	// stretch before the range cannot use up the budget meant for the range.
	timelineLeadPages = 8
)

// timelineKeepReading says whether another page of one kind should be read:
// there is one, the range is not yet passed, fewer than the limit of messages
// inside the range are in hand, and the page cap is not reached. Messages before
// the range count against the page cap only. inRange is the count so far.
func timelineKeepReading(more bool, rows []chatui.TimelineEntry, inRange, pages int, start, end time.Time) (keep bool, inRangeNow int) {
	for _, row := range rows {
		if !row.At.Before(start) && row.At.Before(end) {
			inRange++
		}
	}
	pastEnd := len(rows) > 0 && !rows[len(rows)-1].At.Before(end)
	return more && !pastEnd && inRange < chatui.TimelineMaxEntries && pages < timelineMaxPages+timelineLeadPages, inRange
}

// timelineQuery is the search that lists one kind of message of one conversation
// in a range, oldest first. The days it names sit outside the range (the search
// compares calendar days in UTC and excludes both ends); the entries are then
// kept by exact time.
func timelineQuery(conversationID string, kind chatsearch.Kind, start, end time.Time) (string, error) {
	if conversationID == "" || strings.ContainsAny(conversationID, "\"\x00 \t\n") {
		return "", chatsearch.ErrInvalid
	}
	if kind != chatsearch.Message && kind != chatsearch.Thread {
		return "", chatsearch.ErrInvalid
	}
	after, before := chatui.TimelineSearchDays(start, end)
	return `in:"` + conversationID + `" kind:` + string(kind) + ` sort:oldest after:` + after + ` before:` + before, nil
}

// timelineFromResponse reads a page of the answer. Rows of another conversation,
// and rows that are not a message, are dropped; unavailable is true when the
// message source itself could not answer.
func timelineFromResponse(response chatsearch.Response, conversationID string) (entries []chatui.TimelineEntry, more, unavailable bool) {
	for _, kind := range response.Unavailable {
		if kind == chatsearch.Message || kind == chatsearch.Thread {
			unavailable = true
		}
	}
	for _, group := range response.Groups {
		for _, row := range group.Rows {
			if row.Kind != chatsearch.Message && row.Kind != chatsearch.Thread {
				continue
			}
			if row.Target.ConversationID != conversationID || row.ID == "" || row.At.IsZero() {
				continue
			}
			entries = append(entries, chatui.TimelineEntry{
				ID: row.ID, AuthorID: row.AuthorID, Text: row.Text, At: row.At, Href: chatui.ChatSearchRowHref(row),
				Edited: row.Edited, Restricted: row.Restricted, InThread: row.InThread || row.Kind == chatsearch.Thread || row.Target.ThreadID != "",
				Sequence: row.Target.Sequence, ThreadID: row.Target.ThreadID, ThreadSequence: row.Target.ThreadSequence,
			})
		}
	}
	return entries, response.NextCursor != "", unavailable
}

// timelineKindRead is everything read for one kind: its entries, oldest first,
// and whether the kind has more beyond the last one read.
type timelineKindRead struct {
	entries []chatui.TimelineEntry
	more    bool
}

// timelineMerge puts the kinds in one time order, keeps what was posted inside
// [start, end) and cuts at the limit. When a kind stopped short, nothing later
// than its last row is kept from the others, so the timeline never skips what
// that kind would have added; truncated says the range was cut.
func timelineMerge(reads []timelineKindRead, start, end time.Time) (entries []chatui.TimelineEntry, truncated bool) {
	var cutoff time.Time
	var all []chatui.TimelineEntry
	for _, read := range reads {
		all = append(all, read.entries...)
		if read.more && len(read.entries) > 0 {
			last := read.entries[len(read.entries)-1].At
			if cutoff.IsZero() || last.Before(cutoff) {
				cutoff = last
			}
			truncated = true
		}
	}
	seen := map[string]bool{}
	for _, entry := range chatui.TimelineWithinRange(all, start, end) {
		if !cutoff.IsZero() && entry.At.After(cutoff) || seen[entry.ID] {
			continue
		}
		seen[entry.ID] = true
		entries = append(entries, entry)
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].At.Before(entries[j].At) })
	if len(entries) > chatui.TimelineMaxEntries {
		entries, truncated = entries[:chatui.TimelineMaxEntries], true
	}
	return entries, truncated
}

// timelineChanged counts how many of the asked-for entries can no longer be
// shared as they were shown: gone from a fresh read, worded differently, edited
// since, or now addressed to part of the room.
func timelineChanged(ids []string, shown, fresh []chatui.TimelineEntry) int {
	was, now := map[string]chatui.TimelineEntry{}, map[string]chatui.TimelineEntry{}
	for _, entry := range shown {
		was[entry.ID] = entry
	}
	for _, entry := range fresh {
		now[entry.ID] = entry
	}
	changed := 0
	for _, id := range ids {
		before, known := was[id]
		current, readable := now[id]
		if !known || !readable || current.Text != before.Text || current.Edited != before.Edited || current.Restricted != before.Restricted {
			changed++
		}
	}
	return changed
}

// timelineEntryByID is an entry of the open conversation's timeline.
func timelineEntryByID(model chatui.Model, id string) (chatui.TimelineEntry, bool) {
	if model.Timeline.ConversationID != model.SelectedID || id == "" {
		return chatui.TimelineEntry{}, false
	}
	for _, entry := range model.Timeline.Entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return chatui.TimelineEntry{}, false
}

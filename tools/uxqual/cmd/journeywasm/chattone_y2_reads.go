package main

import (
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chattoneUnsettled names the messages whose reworded view is still on its way:
// the server said "pending" for a reader whose channel does not let them read the
// message as written (so the page shows its "preparing" line), or the writer's
// own "Others see" is still being prepared (CHATTONE-002). They are asked about
// again, a few times, so the reworded text replaces the line when it is ready.
// A reworded view the server gave up on ("unavailable") is settled, and so is
// one that fell back to the original: the reader has the message either way.
func chattoneUnsettled(selections map[string]chatui.ReaderSelection) []string {
	var ids []string
	for id, selection := range selections {
		reword := false
		for _, kind := range selection.Mark.Wanted {
			reword = reword || kind == chatrender.Reword
		}
		if (reword && selection.Mark.State == "pending") || (selection.OthersSee != nil && selection.OthersSee.State == "pending") {
			ids = append(ids, id)
		}
	}
	return ids
}

// readerUnsettled is every message the page asks about again: translations and
// rewordings that are still on their way.
func readerUnsettled(selections map[string]chatui.ReaderSelection) []string {
	seen := map[string]bool{}
	var ids []string
	for _, id := range append(chatlang004Unsettled(selections), chattoneUnsettled(selections)...) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// readerRetryLimit is how many times a read that only refines text already on
// the page (a rendering of it) is asked again after it fails, at the retry
// rhythm of every read (1 s, 2 s, 5 s, 15 s, 15 s). The page has the message as
// written all along, so a read that keeps failing is given up, not asked for
// as long as the page is open.
const readerRetryLimit = 5

// boundedRead stops a retried read after limit failed attempts.
func boundedRead(limit int, attempt func() bool) func() bool {
	failures := 0
	return func() bool {
		if attempt() {
			return true
		}
		failures++
		return failures >= limit
	}
}

package main

import "time"

// Issue 3950. A write of the drafts that failed (the backend was away for a few
// seconds) used to be tried again only by the next keystroke, so edits made
// during the outage were gone after a reload. The writer now tries again on its
// own, waiting longer each time, until a write reaches the account; the page
// shows the person that the latest edits are not saved while that goes on
// (chatState.noteDraftSaveFailed). A retry only writes what is current when it
// runs, and a newer keystroke or a send replaces it, so a stale retry can never
// land over a cleared draft.

const (
	chatDraftRetryFirst = 2 * time.Second
	chatDraftRetryMax   = 30 * time.Second
)

// chatDraftRetryDelay is the wait before the n-th retry in a row (n starts at 1):
// 2s, 4s, 8s, 16s, then 30s.
func chatDraftRetryDelay(failures int) time.Duration {
	delay := chatDraftRetryFirst
	for i := 1; i < failures && delay < chatDraftRetryMax; i++ {
		delay *= 2
	}
	return min(delay, chatDraftRetryMax)
}

// Retry schedules another write after one failed. It leaves a write that is
// already pending alone, because that one is newer than the failure.
func (w *chatDraftWriter) Retry() {
	if w == nil || w.schedule == nil || w.write == nil {
		return
	}
	w.mu.Lock()
	w.failures++
	if w.timer != nil {
		w.mu.Unlock()
		return
	}
	w.generation++
	generation := w.generation
	w.timer = w.schedule(chatDraftRetryDelay(w.failures), func() {
		w.mu.Lock()
		if generation != w.generation {
			w.mu.Unlock()
			return
		}
		w.timer = nil
		write := w.write
		w.mu.Unlock()
		write()
	})
	w.mu.Unlock()
}

// Succeeded ends a run of failures: the next failure starts the wait over.
func (w *chatDraftWriter) Succeeded() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.failures = 0
	w.mu.Unlock()
}

// Failures reports how many writes in a row failed. It is here for the tests.
func (w *chatDraftWriter) Failures() int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.failures
}

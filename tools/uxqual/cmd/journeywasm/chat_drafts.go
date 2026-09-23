package main

import (
	"sync"
	"time"
)

// chatDraftWriter is the composer's persistence, and it exists because a
// debounced write is a race with the send that empties the draft.
//
// The sequence that lost a reader's text: they type, a persist is scheduled;
// they press Enter, the draft is cleared locally; the scheduled write -- which
// captured nothing, but reads state a moment later -- or a projection reload
// that read the server's older copy puts the sent text back in the composer,
// where it is typed over and sent again as part of the next message.
//
// Two rules fix it, and both live here. A send flushes: it cancels whatever is
// pending for that conversation and writes the cleared state at once, so no
// older write can land after it. And every write is stamped with the draft
// revision it carries, so a slower write cannot be mistaken for the current
// state (see chatState.draftRevision).
type chatDraftWriter struct {
	mu       sync.Mutex
	schedule debounceScheduler
	delay    time.Duration
	// write persists the current drafts. It takes no arguments on purpose:
	// the state it writes is whatever is current when it runs, never a value
	// captured when the keystroke happened.
	write      func()
	timer      debounceTimer
	generation uint64
}

func newChatDraftWriter(schedule debounceScheduler, delay time.Duration, write func()) *chatDraftWriter {
	return &chatDraftWriter{schedule: schedule, delay: delay, write: write}
}

// Schedule joins or restarts the pending write.
func (w *chatDraftWriter) Schedule() {
	if w == nil || w.schedule == nil || w.write == nil {
		return
	}
	w.mu.Lock()
	w.generation++
	generation := w.generation
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = w.schedule(w.delay, func() {
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

// Flush cancels the pending write and persists the current state now.
//
// A send calls this. Cancelling first is what matters: a pending write that
// fired afterwards would persist over the cleared draft only if it ran later,
// and a timer nobody stopped always runs later.
func (w *chatDraftWriter) Flush() {
	if w == nil || w.write == nil {
		return
	}
	w.mu.Lock()
	w.generation++
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	write := w.write
	w.mu.Unlock()
	write()
}

// Pending reports whether a write is waiting. It is here for the tests; the
// production path never needs to ask.
func (w *chatDraftWriter) Pending() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.timer != nil
}

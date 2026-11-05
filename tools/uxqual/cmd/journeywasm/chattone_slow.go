package main

import (
	"sync"
	"time"
)

// chattoneSlowDelay is how long a rewrite may run before the status line above
// the composer's text escalates from "working" to the "taking longer than
// usual" notice (CHATTONE-006).
const chattoneSlowDelay = 5 * time.Second

// chattoneSlowWatcher arms one taking-longer notice per rewrite. It reuses the
// injected debounceScheduler so native tests drive it with the fake scheduler
// while the wasm adapter passes the browser's setTimeout; the generation guard
// mirrors navigationDebouncer so a superseded or cancelled fire stays silent.
type chattoneSlowWatcher struct {
	mu         sync.Mutex
	generation uint64
	timer      debounceTimer
	schedule   debounceScheduler
}

func newChattoneSlowWatcher(schedule debounceScheduler) *chattoneSlowWatcher {
	return &chattoneSlowWatcher{schedule: schedule}
}

// Begin arms the notice for one rewrite; arming again (a second rewrite on the
// same scope) stops the previous timer and retires its fire.
func (w *chattoneSlowWatcher) Begin(onSlow func()) {
	if w == nil || w.schedule == nil {
		return
	}
	w.mu.Lock()
	w.generation++
	generation := w.generation
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = w.schedule(chattoneSlowDelay, func() {
		w.mu.Lock()
		if generation != w.generation {
			w.mu.Unlock()
			return
		}
		w.timer = nil
		w.mu.Unlock()
		if onSlow != nil {
			onSlow()
		}
	})
	w.mu.Unlock()
}

// Cancel retires the armed notice, if any, as when the rewrite completes.
func (w *chattoneSlowWatcher) Cancel() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.generation++
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	w.mu.Unlock()
}

// MarkSlow records that the rewrite serial was still running when the
// taking-longer delay passed. A serial from an older rewrite, or a preview
// that already completed, is left alone.
func (p *chattonePreview) MarkSlow(serial uint64) bool {
	if serial != p.Serial || !p.Busy {
		return false
	}
	p.Slow = true
	return true
}

// chattoneBusyStatus selects the code the busy line shows: "slow" once the
// taking-longer notice has fired for the current rewrite, "working" before
// that. The wasm paint path calls this so a repaint never clobbers an
// escalation back to "working".
func chattoneBusyStatus(p *chattonePreview) string {
	if p != nil && p.Busy && p.Slow {
		return "slow"
	}
	return "working"
}

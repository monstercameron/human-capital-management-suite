package main

import (
	"sync"
	"time"
)

// CHATBUG-014: one thread does everything in the browser, so a read that the
// first paint does not need still delays it: its answer is decoded, folded into
// the model and repainted in between the steps that draw the page. Measured on
// the review machine, 127 calls went out before the first message was drawn and
// six of the page's renders ran before the one that had messages in it.
//
// chatperfGate holds such work until the first timeline is on screen (messages,
// an empty conversation or an error), then lets it through in the order it was
// asked for. After that, work passes straight through, so a conversation switch
// is not slowed by it.
type chatperfGate struct {
	mu      sync.Mutex
	open    bool
	shown   time.Time
	waiting []func()
}

// hold returns true when work has been queued for the release; false means the
// gate is open and the caller runs the work itself.
func (g *chatperfGate) hold(work func()) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.open {
		return false
	}
	g.waiting = append(g.waiting, work)
	return true
}

// release opens the gate and returns what was waiting, oldest first. A second
// release returns nothing.
func (g *chatperfGate) release() []func() {
	g.mu.Lock()
	defer g.mu.Unlock()
	waiting := g.waiting
	if !g.open {
		g.shown = time.Now()
	}
	g.open, g.waiting = true, nil
	return waiting
}

// shownAt is when the first timeline was drawn, the zero time until it is.
func (g *chatperfGate) shownAt() time.Time {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.shown
}

// reset closes the gate again and forgets what was waiting: the page was left,
// and whatever it had queued belongs to a view nobody is looking at.
func (g *chatperfGate) reset() {
	g.mu.Lock()
	g.open, g.waiting, g.shown = false, nil, time.Time{}
	g.mu.Unlock()
}

// chatperfTurn lets one piece of work wait for another that was started for the
// same conversation: the stream waits for the reaction read. A stream that
// starts first replays every reaction the conversation ever had as an event,
// and each of those is a change to draw; once the read has landed the same
// events change nothing.
type chatperfTurn struct {
	mu   sync.Mutex
	room string
	done chan struct{}
}

// begin marks the work for room as under way. A turn left open for another room
// is ended, so nothing waits on work for a conversation that was left.
func (t *chatperfTurn) begin(room string) {
	t.mu.Lock()
	if t.done != nil {
		close(t.done)
	}
	t.room, t.done = room, make(chan struct{})
	t.mu.Unlock()
}

// end marks the work for room as finished, whether or not it succeeded.
func (t *chatperfTurn) end(room string) {
	t.mu.Lock()
	if t.done != nil && t.room == room {
		close(t.done)
		t.done = nil
	}
	t.mu.Unlock()
}

// wait returns a channel that is closed when the work for room has finished. It
// is already closed when no such work is under way.
func (t *chatperfTurn) wait(room string) <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done == nil || t.room != room {
		finished := make(chan struct{})
		close(finished)
		return finished
	}
	return t.done
}

// isOpen reports whether the first timeline has been shown.
func (g *chatperfGate) isOpen() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.open
}

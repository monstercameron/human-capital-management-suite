package main

import "sync"

// CHATBUG-014: the first load asks for the open conversation's messages as soon
// as the conversation list has been read, instead of when the page has mounted.
// Measured on the review machine, the list is adopted about 1.5 s before the
// page mounts, and the messages took another 1.8 s after that because their
// read then shared the one thread with everything else the mount starts. Asked
// for at adoption, the answer is in the model before the page first draws, so
// the first draw of the page already has its messages.
//
// chatperfFirstOpen is the bookkeeping for that open: who starts it and that it
// is started once. The loader records the conversation and starts the open
// ahead of the paint; the page, when it mounts, starts it only if nobody has.
type chatperfFirstOpen struct {
	mu      sync.Mutex
	id      string
	owed    bool
	started bool
}

// record notes the conversation the first load left without its messages.
func (o *chatperfFirstOpen) record(id string) {
	o.mu.Lock()
	o.id, o.owed, o.started = id, true, false
	o.mu.Unlock()
}

// ahead returns the conversation to open now, before the page is on screen. It
// reports false when nothing is owed or the open has already been started.
func (o *chatperfFirstOpen) ahead() (string, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.owed || o.started {
		return "", false
	}
	o.started = true
	return o.id, true
}

// mounted is called when the page is on screen. owed reports that the first
// load deferred a conversation, and start that the page must open it itself
// because the loader did not. Either way the debt is settled.
func (o *chatperfFirstOpen) mounted() (id string, owed, start bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	id, owed, start = o.id, o.owed, o.owed && !o.started
	o.id, o.owed, o.started = "", false, false
	return id, owed, start
}

// chatperfDrawn is what one render of the Chat page showed of the timeline. The
// page compares it with the model when it mounts: a change committed between
// the first render and the mount has no page to repaint yet, so the mount
// repaints once for it.
type chatperfDrawn struct {
	State    string
	Selected string
	Messages int
}

// chatperfBehind reports whether the page drew less than the model now holds.
func chatperfBehind(drawn, now chatperfDrawn) bool {
	return drawn != now
}

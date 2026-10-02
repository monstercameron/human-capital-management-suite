package main

import (
	"sync"
	"time"
)

// CHATBUG-014 / CHATSCALE-003: the new-message sound was found by reading the
// posts of every conversation in the sidebar every twenty seconds, eighteen
// calls a tick on the review data whether or not anybody had written anything.
// The conversation list already says when each conversation's newest message
// was sent, so one read of the list tells which conversations have anything to
// read, and only those are read.

// chatperfSoundActivity remembers, for each conversation, the newest-message
// time the list reported when the conversation was last read to its end.
type chatperfSoundActivity struct {
	mu   sync.Mutex
	read map[string]time.Time
}

// quiet reports whether nothing has been posted to id since it was last read to
// its end. A conversation that has not been read through yet is never quiet.
func (a *chatperfSoundActivity) quiet(id string, newest time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	read, ok := a.read[id]
	return ok && !newest.After(read)
}

// settled records that id has been read through the message the list reported
// as its newest. It is called only after a read that got its answer, so a read
// that failed is made again on the next tick.
func (a *chatperfSoundActivity) settled(id string, newest time.Time) {
	a.mu.Lock()
	if a.read == nil {
		a.read = make(map[string]time.Time)
	}
	// Never move back: a list answer that is older than the last one must not
	// make an already-read conversation look as if it had news.
	if newest.After(a.read[id]) || a.read[id].IsZero() {
		a.read[id] = newest
	}
	a.mu.Unlock()
}

// reset forgets everything, for a new sign-in.
func (a *chatperfSoundActivity) reset() {
	a.mu.Lock()
	a.read = nil
	a.mu.Unlock()
}

// chatperfSoundSkip decides whether a tick may leave one conversation unread:
// the list answered for it, it is not the open conversation (which the stream
// covers), its starting point is already known, and nothing is newer than what
// was last read.
func chatperfSoundSkip(seen *chatperfSoundActivity, id, selected string, newest time.Time, listed, baselined bool) bool {
	return listed && baselined && id != selected && seen.quiet(id, newest)
}

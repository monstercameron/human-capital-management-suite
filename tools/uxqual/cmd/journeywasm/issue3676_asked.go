package main

import (
	"strconv"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// issue3676Asked remembers which messages (at which revision, in which
// conversation) a per-page read has already been answered for, so a new page of
// messages, a pin or a thread reply asks only for what it has not asked yet
// instead of re-sending every message id (#3676).
type issue3676Asked struct {
	mu   sync.Mutex
	seen map[string]bool
}

func issue3676AskedKey(room string, msg chatui.Message) string {
	return room + "\x00" + msg.ID + ":" + strconv.FormatUint(msg.Revision, 10)
}

// Unasked returns the messages with an id that were not yet answered, each id once.
func (a *issue3676Asked) Unasked(room string, messages []chatui.Message) []chatui.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]chatui.Message, 0, len(messages))
	queued := map[string]bool{}
	for _, msg := range messages {
		if msg.ID == "" || queued[msg.ID] || a.seen[issue3676AskedKey(room, msg)] {
			continue
		}
		queued[msg.ID] = true
		out = append(out, msg)
	}
	return out
}

// Answered records messages the server answered (a refusal that cannot change counts).
func (a *issue3676Asked) Answered(room string, messages []chatui.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.seen == nil {
		a.seen = map[string]bool{}
	}
	for _, msg := range messages {
		a.seen[issue3676AskedKey(room, msg)] = true
	}
}

// Forget un-records messages whose answer was not kept, so they are asked again.
func (a *issue3676Asked) Forget(room string, messages []chatui.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, msg := range messages {
		delete(a.seen, issue3676AskedKey(room, msg))
	}
}

// Reset forgets everything, for a new session or tenant.
func (a *issue3676Asked) Reset() {
	a.mu.Lock()
	a.seen = nil
	a.mu.Unlock()
}

// issue3676MergeLocations keeps the locations already known for messages not asked
// this time, and replaces the ones that were asked with the fresh answer.
func issue3676MergeLocations(current map[string][]chatui.ChatmapEmbed, asked []chatui.Message, found map[string][]chatui.ChatmapEmbed) map[string][]chatui.ChatmapEmbed {
	next := make(map[string][]chatui.ChatmapEmbed, len(current)+len(found))
	for id, embeds := range current {
		next[id] = embeds
	}
	for _, msg := range asked {
		delete(next, msg.ID)
	}
	for id, embeds := range found {
		next[id] = embeds
	}
	return next
}

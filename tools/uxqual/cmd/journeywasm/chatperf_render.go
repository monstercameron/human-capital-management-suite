package main

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// CHATBUG-014: opening a conversation is followed by a burst of changes. The
// stream replays the conversation's recent events (183 for #general on the
// review data), and the held reads, the agent list and the attachment grants
// land in the same seconds. Each change asked for a repaint of the whole page,
// and a repaint was given 120 ms after the first change that asked: eight to ten
// repaints of 250 ms and more, back to back, after every open.
//
// chatperfRenderCoalescer gathers a burst instead. While changes keep arriving
// it waits for a pause, and it never waits longer than its limit, so a long
// burst still repaints at a steady, slower rate. Outside a burst's window the
// pause and the limit are the same short wait as before, and a new message is
// drawn as promptly as it always was.

// Most of that replay changes nothing: the reactions, edits and pins it carries
// are already in the page that was just read. An event that leaves what the
// timeline shows exactly as it was does not ask for a repaint at all.

// chatperfEventDrew records whether the stream event applied last changed what
// the timeline shows. applyStreamEvent sets it; the stream's own goroutine reads
// it straight after, and there is one stream at a time.
var chatperfEventDrew atomic.Bool

// chatperfEventMark reduces what one event can change on screen to a string
// that can be compared before and after the event is applied. comparable is
// false for the kinds that are not reduced this way (a new post, a deletion, a
// rename): those always repaint.
func chatperfEventMark(model *chatui.Model, event *chatv1.ConversationEvent) (mark string, comparable bool) {
	switch event.GetKind() {
	case chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_REACTION_CHANGED:
		return chatperfMessageMark(model, event.GetReaction().GetPostId()), true
	case chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_PIN_CHANGED:
		return chatperfMessageMark(model, event.GetPin().GetPostId()), true
	case chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_EDITED:
		return chatperfMessageMark(model, event.GetPost().GetId()), true
	case chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_MEMBERSHIP_CHANGED:
		// The event itself changes nothing in the timeline; the member list it
		// invalidates is read again and repaints when it lands.
		return "", true
	}
	return "", false
}

// chatperfMessageMark is everything the page draws from one post: its row in
// the timeline, its row in the open thread, and the thread's parent.
func chatperfMessageMark(model *chatui.Model, id string) string {
	if model == nil || id == "" {
		return ""
	}
	var mark strings.Builder
	write := func(where string, message *chatui.Message) {
		// The actor is compared by what it says, not by where it is kept: an
		// equal actor at a new address is not a change.
		flat := *message
		actor := flat.PersonaActor
		flat.PersonaActor = nil
		fmt.Fprintf(&mark, "%s:%+v", where, flat)
		if actor != nil {
			fmt.Fprintf(&mark, "/%+v", *actor)
		}
	}
	for i := range model.Messages {
		if model.Messages[i].ID == id {
			write("timeline", &model.Messages[i])
		}
	}
	for i := range model.ThreadMessages {
		if model.ThreadMessages[i].ID == id {
			write("thread", &model.ThreadMessages[i])
		}
	}
	if model.ThreadParent != nil && model.ThreadParent.ID == id {
		write("parent", model.ThreadParent)
	}
	return mark.String()
}

// chatperfEventChanged reports whether an event changed what is drawn, given
// its mark before and after it was applied.
func chatperfEventChanged(before, after string, comparable bool) bool {
	return !comparable || before != after
}

// chatperfRenderPace is how long a repaint waits: quiet after the latest change
// that asked for it, and never more than limit after the first.
type chatperfRenderPace struct {
	quiet, limit time.Duration
}

const (
	// chatperfLiveRenderWait is the wait outside a burst's window.
	chatperfLiveRenderWait = 120 * time.Millisecond
	// chatperfBurstQuiet and chatperfBurstLimit pace repaints while a
	// conversation has just been opened.
	chatperfBurstQuiet = 250 * time.Millisecond
	chatperfBurstLimit = time.Second
	// chatperfBurstWindow is how long after a conversation opens its changes
	// count as that open's burst.
	chatperfBurstWindow = 3 * time.Second
)

// chatperfPaceAt chooses the pace for a repaint asked for at now. opened is when
// the conversation on screen was last opened (its first timeline drawn, or its
// stream started), the zero time when none has been.
func chatperfPaceAt(opened, now time.Time) chatperfRenderPace {
	if opened.IsZero() || now.Sub(opened) < chatperfBurstWindow {
		return chatperfRenderPace{quiet: chatperfBurstQuiet, limit: chatperfBurstLimit}
	}
	return chatperfRenderPace{quiet: chatperfLiveRenderWait, limit: chatperfLiveRenderWait}
}

// chatperfRenderCoalescer turns the repaints a burst asks for into few.
type chatperfRenderCoalescer struct {
	schedule debounceScheduler
	now      func() time.Time
	pace     func(now time.Time) chatperfRenderPace
	render   func()

	mu      sync.Mutex
	pending bool
	first   time.Time
	last    time.Time
}

func newChatperfRenderCoalescer(schedule debounceScheduler, now func() time.Time, pace func(time.Time) chatperfRenderPace, render func()) *chatperfRenderCoalescer {
	return &chatperfRenderCoalescer{schedule: schedule, now: now, pace: pace, render: render}
}

// Schedule asks for a repaint. Every request made before the repaint happens
// shares it.
func (c *chatperfRenderCoalescer) Schedule() {
	if c == nil || c.schedule == nil || c.render == nil {
		return
	}
	now := c.now()
	c.mu.Lock()
	c.last = now
	if c.pending {
		c.mu.Unlock()
		return
	}
	c.pending, c.first = true, now
	pace := c.pace(now)
	c.mu.Unlock()
	c.schedule(min(pace.quiet, pace.limit), c.tick)
}

// tick runs when a wait ends: it repaints if the changes have paused or the
// limit is up, and otherwise waits out what is left.
func (c *chatperfRenderCoalescer) tick() {
	now := c.now()
	c.mu.Lock()
	pace := c.pace(now)
	sinceLast, sinceFirst := now.Sub(c.last), now.Sub(c.first)
	if sinceLast < pace.quiet && sinceFirst < pace.limit {
		wait := min(pace.quiet-sinceLast, pace.limit-sinceFirst)
		c.mu.Unlock()
		c.schedule(wait, c.tick)
		return
	}
	c.pending = false
	c.mu.Unlock()
	c.render()
}

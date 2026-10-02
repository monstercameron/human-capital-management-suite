//go:build !(js && wasm)

package main

import (
	"sort"
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATBUG_014_ReplayedEvent: a stream event that leaves what the
// timeline shows as it was (a reaction, an edit or a pin the page already has,
// a membership change) asks for no repaint; one that changes anything does.
func TestTodo_CHATBUG_014_ReplayedEvent(t *testing.T) {
	const room = "room"
	reaction := func(post, subject, emoji string, removed bool) *chatv1.ConversationEvent {
		return &chatv1.ConversationEvent{Kind: chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_REACTION_CHANGED, Removed: removed,
			Reaction: &chatv1.Reaction{ConversationId: room, PostId: post, HomeTenantId: "tenant", SubjectId: subject, Emoji: emoji}}
	}
	state := &chatState{}
	state.cfg.Tenant = "tenant"
	state.model = chatui.Model{SelectedID: room, CurrentUser: "me", State: chatui.StateReady, Messages: []chatui.Message{
		{ID: "p1", Body: "first", Revision: 1, Sequence: 1},
		{ID: "p2", Body: "second", Revision: 1, Sequence: 2, Pinned: true},
	}}
	state.cursor = chatCursor{ConversationID: room, Seen: map[string]uint64{"p1": 1, "p2": 2}, LastSequence: 2}
	apply := func(event *chatv1.ConversationEvent) bool {
		t.Helper()
		if _, active := state.applyStreamEvent(state.generation, room, event, "en-US", nil, time.Now(), ""); !active {
			t.Fatal("the stream was not the live one")
		}
		return chatperfEventDrew.Load()
	}

	// A reaction the page does not have yet is drawn; the same one replayed is not.
	if !apply(reaction("p1", "ann", "+1", false)) {
		t.Fatal("a new reaction asked for no repaint")
	}
	if got := state.model.Messages[0].Chips; len(got) != 1 || got[0].Count != 1 {
		t.Fatalf("chips after one reaction: %+v", got)
	}
	for i := 0; i < 3; i++ {
		if apply(reaction("p1", "ann", "+1", false)) {
			t.Fatal("a replayed reaction the page already shows asked for a repaint")
		}
	}
	if got := state.model.Messages[0].Chips; len(got) != 1 || got[0].Count != 1 {
		t.Fatalf("a replayed reaction changed the chips: %+v", got)
	}
	if !apply(reaction("p1", "sam", "+1", false)) || !apply(reaction("p1", "ann", "+1", true)) {
		t.Fatal("a second person's reaction, or a removal, asked for no repaint")
	}
	if apply(reaction("not-on-screen", "ann", "+1", false)) {
		t.Fatal("a reaction to a post that is not on screen asked for a repaint")
	}

	// Pins and edits: the state the page already has is not drawn again.
	pin := func(post string, removed bool) *chatv1.ConversationEvent {
		return &chatv1.ConversationEvent{Kind: chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_PIN_CHANGED, Removed: removed, Pin: &chatv1.Pin{ConversationId: room, PostId: post}}
	}
	if apply(pin("p2", false)) {
		t.Fatal("a replayed pin of a post already shown as pinned asked for a repaint")
	}
	if !apply(pin("p2", true)) || !apply(pin("p1", false)) {
		t.Fatal("an unpin, or a new pin, asked for no repaint")
	}
	edit := func(body string, revision uint64) *chatv1.ConversationEvent {
		return &chatv1.ConversationEvent{Kind: chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_EDITED, Post: &chatv1.Post{Id: "p1", ConversationId: room, Body: body, Revision: revision, Sequence: 1}}
	}
	if !apply(edit("first, corrected", 2)) {
		t.Fatal("an edit asked for no repaint")
	}
	if apply(edit("first, corrected", 2)) {
		t.Fatal("the same edit replayed asked for a repaint")
	}
	if state.model.Messages[0].Body != "first, corrected" {
		t.Fatalf("the edit was not applied: %q", state.model.Messages[0].Body)
	}

	// A membership change does not touch the timeline; its member list repaints
	// when it has been read again.
	if apply(&chatv1.ConversationEvent{Kind: chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_MEMBERSHIP_CHANGED, Membership: &chatv1.Membership{ConversationId: room, SubjectId: "ann"}}) {
		t.Fatal("a membership event asked for a repaint of the timeline")
	}

	// Anything else always repaints: a new post, a deletion, a rename.
	created := &chatv1.ConversationEvent{Kind: chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_CREATED, Sequence: 3, Post: &chatv1.Post{Id: "p3", ConversationId: room, Body: "third", Revision: 1, Sequence: 3}}
	if !apply(created) || len(state.model.Messages) != 3 {
		t.Fatalf("a new post asked for no repaint or was not added: %d messages", len(state.model.Messages))
	}
	for name, event := range map[string]*chatv1.ConversationEvent{
		"a deletion": {Kind: chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_DELETED, Post: &chatv1.Post{Id: "p2", ConversationId: room}},
		"a rename":   {Kind: chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_CONVERSATION_UPDATED, Conversation: &chatv1.Conversation{Id: room, Name: "renamed"}},
	} {
		if !apply(event) {
			t.Errorf("%s asked for no repaint", name)
		}
	}

	// The stream's own hook is where the answer is used.
	events := chatperfBody(t, "chat_stream_wasm.go", "func receiveChatEvents(")
	chatperfInOrder(t, "receiveChatEvents", events,
		"chatBrowser.applyStreamEvent(",
		"drew = chatperfEventDrew.Load()",
		"if !drew {",
		"return",
		"chatStreamRender.Schedule()",
	)
}

// chatperfClock is a clock and a timer queue a test moves by hand.
type chatperfClock struct {
	now    time.Time
	timers []chatperfTimer
}

type chatperfTimer struct {
	at   time.Time
	fire func()
}

func (chatperfTimer) Stop() bool { return false }

func (c *chatperfClock) schedule(delay time.Duration, fire func()) debounceTimer {
	timer := chatperfTimer{at: c.now.Add(delay), fire: fire}
	c.timers = append(c.timers, timer)
	return timer
}

// advance moves the clock to until, firing the timers that come due on the way.
func (c *chatperfClock) advance(until time.Time) {
	for {
		sort.SliceStable(c.timers, func(i, j int) bool { return c.timers[i].at.Before(c.timers[j].at) })
		if len(c.timers) == 0 || c.timers[0].at.After(until) {
			c.now = until
			return
		}
		next := c.timers[0]
		c.timers = c.timers[1:]
		c.now = next.at
		next.fire()
	}
}

// TestTodo_CHATBUG_014_RenderBurst: the burst of changes that follows an open
// repaints the page a few times, not once per change, and a change that arrives
// on its own later is still drawn promptly.
func TestTodo_CHATBUG_014_RenderBurst(t *testing.T) {
	start := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	clock := &chatperfClock{now: start}
	opened := start
	var renders []time.Duration
	coalescer := newChatperfRenderCoalescer(clock.schedule, func() time.Time { return clock.now },
		func(now time.Time) chatperfRenderPace { return chatperfPaceAt(opened, now) },
		func() { renders = append(renders, clock.now.Sub(start)) })

	// The replay after an open: 183 changes, one every 10 ms, for 1.83 s.
	for i := 0; i < 183; i++ {
		clock.advance(start.Add(time.Duration(i) * 10 * time.Millisecond))
		coalescer.Schedule()
	}
	clock.advance(start.Add(2500 * time.Millisecond))
	if len(renders) < 2 || len(renders) > 3 {
		t.Fatalf("183 changes over 1.83 s repainted %d times at %v, want two or three (one a second while they arrive, one when they stop)", len(renders), renders)
	}
	if first := renders[0]; first > chatperfBurstLimit+10*time.Millisecond {
		t.Fatalf("the first repaint of a burst waited %v, over the %v limit", first, chatperfBurstLimit)
	}
	last := renders[len(renders)-1]
	if last < 1820*time.Millisecond || last > 1820*time.Millisecond+chatperfBurstQuiet+10*time.Millisecond {
		t.Fatalf("the last repaint came at %v; the last change was at 1.82 s and must be drawn within the pause after it", last)
	}

	// A single change during the burst window waits for the pause, no longer.
	renders = nil
	clock.advance(start.Add(2600 * time.Millisecond))
	coalescer.Schedule()
	clock.advance(start.Add(2600*time.Millisecond + chatperfBurstQuiet))
	if len(renders) != 1 {
		t.Fatalf("one change in the burst window repainted %d times", len(renders))
	}

	// Long after the open, a change is drawn after the short wait, exactly once,
	// and a steady trickle is not held back.
	renders = nil
	settled := start.Add(chatperfBurstWindow + time.Second)
	clock.advance(settled)
	coalescer.Schedule()
	clock.advance(settled.Add(50 * time.Millisecond))
	coalescer.Schedule()
	clock.advance(settled.Add(chatperfLiveRenderWait))
	if len(renders) != 1 || renders[0] != settled.Add(chatperfLiveRenderWait).Sub(start) {
		t.Fatalf("a settled page repainted %v after two changes 50 ms apart, want once, %v after the first", renders, chatperfLiveRenderWait)
	}
	clock.advance(settled.Add(time.Second))
	if len(renders) != 1 || len(clock.timers) != 0 {
		t.Fatalf("a repaint or a timer was left over: renders %v, timers %d", renders, len(clock.timers))
	}

	// The pace itself.
	for name, tc := range map[string]struct {
		opened, now time.Time
		want        chatperfRenderPace
	}{
		"nothing opened yet":   {time.Time{}, start, chatperfRenderPace{chatperfBurstQuiet, chatperfBurstLimit}},
		"just opened":          {start, start.Add(time.Second), chatperfRenderPace{chatperfBurstQuiet, chatperfBurstLimit}},
		"the window is over":   {start, start.Add(chatperfBurstWindow), chatperfRenderPace{chatperfLiveRenderWait, chatperfLiveRenderWait}},
		"an hour into reading": {start, start.Add(time.Hour), chatperfRenderPace{chatperfLiveRenderWait, chatperfLiveRenderWait}},
	} {
		if got := chatperfPaceAt(tc.opened, tc.now); got != tc.want {
			t.Errorf("%s: pace %+v, want %+v", name, got, tc.want)
		}
	}
}

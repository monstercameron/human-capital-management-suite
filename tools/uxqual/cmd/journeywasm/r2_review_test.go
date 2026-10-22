package main

import (
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// A conversation's event numbers have holes for any one reader: the store writes
// moderation and permission events that the watch never delivers, and a post
// restricted to other people sends this reader nothing at all. The cursor the
// server holds is past the hole, so a resubscribe that carries it sees the very
// same jump again. A client that measured the new stream against the old
// position would call that a gap every time, catch up, resubscribe and never
// apply another live event. After a gap the next stream learns its own position.
func TestR2_GapIsNotAskedAgainOnTheResubscribe(t *testing.T) {
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	state := newChatStateForTest(t)
	state.mu.Lock()
	state.model.SelectedID, state.model.State = "c1", chatui.StateReady
	state.cursor = chatCursor{ConversationID: "c1", LastSequence: 2, StreamSequence: 2, Baselined: true, Seen: map[string]uint64{"p2": 2}}
	state.mu.Unlock()
	generation := state.currentGeneration()
	event := &chatv1.ConversationEvent{Kind: chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_CREATED, Sequence: 4, Post: chatTestPost("p4", 4, "blake", "after the hole", "", now)}

	if outcome, active := state.applyStreamEvent(generation, "c1", event, "en-US", nil, now, "resume-1"); !active || outcome != chatEventGap {
		t.Fatalf("an event two past the position: outcome %d active %t, want a gap", outcome, active)
	}
	// The resubscribe resumes after the last event this page applied, so the server
	// sends the same event again. It is the next one this stream will ever get.
	if outcome, active := state.applyStreamEvent(generation, "c1", event, "en-US", nil, now, "resume-2"); !active || outcome != chatEventApplied {
		t.Fatalf("the same jump on the resubscribed stream: outcome %d active %t, want applied", outcome, active)
	}
	if got := state.snapshot().Messages; len(got) != 1 || got[0].ID != "p4" {
		t.Fatalf("the event after the hole is not on the page: %+v", got)
	}
}

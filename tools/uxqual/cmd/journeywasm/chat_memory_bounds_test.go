package main

import (
	"fmt"
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func memoryTestMessage(sequence uint64) chatui.Message {
	return chatui.Message{ID: fmt.Sprintf("post-%d", sequence), Sequence: sequence}
}

func assertContiguousChatWindow(t *testing.T, model chatui.Model, first, last uint64) {
	t.Helper()
	if len(model.Messages) != int(last-first+1) {
		t.Fatalf("timeline has %d posts, want %d (%d..%d)", len(model.Messages), last-first+1, first, last)
	}
	for i, message := range model.Messages {
		want := first + uint64(i)
		if message.ID != fmt.Sprintf("post-%d", want) || message.Sequence != want {
			t.Fatalf("timeline[%d] = %q/%d, want post-%d/%d", i, message.ID, message.Sequence, want, want)
		}
	}
	if cap(model.Messages) > 2*chatMessageWindow {
		t.Fatalf("retained timeline backing capacity = %d, want <= %d", cap(model.Messages), 2*chatMessageWindow)
	}
}

func TestChatMemoryBoundedTimelineDropsLargeBacking(t *testing.T) {
	all := make([]chatui.Message, 10_000)
	for i := range all {
		all[i] = memoryTestMessage(uint64(i + 1))
	}
	retained, hasOlder := boundChatMessages(all, chatMessageWindow, false)
	if !hasOlder {
		t.Fatal("trimming historical posts did not preserve the Older affordance")
	}
	assertContiguousChatWindow(t, chatui.Model{Messages: retained}, 9_501, 10_000)
	if cap(retained) >= cap(all) {
		t.Fatalf("retained %d posts still hold backing for %d", len(retained), cap(retained))
	}
}

func TestChatMemoryLongStreamOlderAndNewerRecovery(t *testing.T) {
	state := newChatStateForTest(t)
	state.mutate(func(model *chatui.Model) { model.SelectedID = "c1"; model.State = chatui.StateReady })
	state.mu.Lock()
	state.cursor = chatCursor{ConversationID: "c1", Seen: map[string]uint64{}}
	state.mu.Unlock()
	const total = 10_200
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	for n := uint64(1); n <= total; n++ {
		post := chatTestPost(fmt.Sprintf("post-%d", n), n, "avery", "message", "", now)
		event := &chatv1.ConversationEvent{Kind: chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_CREATED, Sequence: n, Post: post}
		if outcome, current := state.applyStreamEvent(state.currentGeneration(), "c1", event, "en-US", nil, now, ""); !current || outcome != chatEventApplied {
			t.Fatalf("event %d: current=%v outcome=%d", n, current, outcome)
		}
	}
	model := state.snapshot()
	assertContiguousChatWindow(t, model, total-chatMessageWindow+1, total)
	if !model.HasOlder || model.HasNewer || len(state.streamCursor().Seen) > chatMessageWindow+128 {
		t.Fatalf("long stream bounds: older=%v newer=%v seen=%d", model.HasOlder, model.HasNewer, len(state.streamCursor().Seen))
	}
	first := total - chatMessageWindow + 1
	older := make([]chatui.Message, 0, chatMessageWindow)
	for n := uint64(first - chatMessageWindow); n < uint64(first); n++ {
		older = append(older, memoryTestMessage(n))
	}
	if !state.prependOlderChatMessages("c1", older, "more-history") {
		t.Fatal("older page was not admitted")
	}
	model = state.snapshot()
	assertContiguousChatWindow(t, model, uint64(first-chatMessageWindow), uint64(first-1))
	if !model.HasNewer || !model.HasOlder || state.oldestHeldSequence() != uint64(first-chatMessageWindow) {
		t.Fatalf("older window lost navigation edges: newer=%v older=%v oldest=%d", model.HasNewer, model.HasOlder, state.oldestHeldSequence())
	}
	// A live event beyond this older window must be available via Newer,
	// without replacing the page the reader deliberately requested.
	next := uint64(total + 1)
	event := &chatv1.ConversationEvent{Kind: chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_CREATED, Sequence: next, Post: chatTestPost(fmt.Sprintf("post-%d", next), next, "avery", "new", "", now)}
	if outcome, current := state.applyStreamEvent(state.currentGeneration(), "c1", event, "en-US", nil, now, ""); !current || outcome != chatEventApplied {
		t.Fatalf("new live event while reading older history: current=%v outcome=%d", current, outcome)
	}
	assertContiguousChatWindow(t, state.snapshot(), uint64(first-chatMessageWindow), uint64(first-1))
	newer := make([]chatui.Message, 0, chatMessageWindow)
	for n := uint64(first); n <= total; n++ {
		newer = append(newer, memoryTestMessage(n))
	}
	if !state.appendNewerChatMessages("c1", newer, "newer-page", total) {
		t.Fatal("newer page was not admitted")
	}
	assertContiguousChatWindow(t, state.snapshot(), uint64(first), total)
	if !state.snapshot().HasNewer {
		t.Fatal("newest live post should still be reachable")
	}
	if !state.appendNewerChatMessages("c1", []chatui.Message{memoryTestMessage(next)}, "", next) {
		t.Fatal("last newer page was not admitted")
	}
	model = state.snapshot()
	assertContiguousChatWindow(t, model, uint64(first+1), next)
	if model.HasNewer || !model.HasOlder || state.newestHeldSequence() != next {
		t.Fatalf("newer recovery lost edge: newer=%v older=%v newest=%d", model.HasNewer, model.HasOlder, state.newestHeldSequence())
	}
}

func TestChatMemoryFutureGappedSendsKeepSeenBounded(t *testing.T) {
	state := newChatStateForTest(t)
	state.mutate(func(model *chatui.Model) {
		model.SelectedID = "c1"
		model.Messages = []chatui.Message{memoryTestMessage(1)}
		model.HasNewer = true
	})
	state.mu.Lock()
	state.cursor = chatCursor{ConversationID: "c1", LastSequence: 1, Seen: map[string]uint64{"post-1": 1}}
	state.mu.Unlock()
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	for n := uint64(2); n <= 10_002; n++ {
		post := chatTestPost(fmt.Sprintf("future-%d", n), 20_000+n, "avery", "sent", "", now)
		if applied, gapped := state.applySentChatPost("c1", post, "en-US", nil, now); !applied || !gapped {
			t.Fatalf("future send %d: applied=%v gapped=%v", n, applied, gapped)
		}
	}
	model := state.snapshot()
	if len(model.Messages) != 1 || model.Messages[0].ID != "post-1" || !model.HasNewer || state.streamCursor().LastSequence != 1 {
		t.Fatalf("older page or gap position was lost: %+v", model)
	}
	if seen := len(state.streamCursor().Seen); seen > chatMessageWindow+128 {
		t.Fatalf("gapped send IDs grew without bound: %d", seen)
	}
}

func TestChatMemoryOldestEdgeIgnoresThreadAndReplayIndex(t *testing.T) {
	state := newChatStateForTest(t)
	state.mutate(func(model *chatui.Model) {
		model.SelectedID = "c1"
		model.Messages = []chatui.Message{memoryTestMessage(400), memoryTestMessage(401)}
		model.ThreadMessages = []chatui.Message{memoryTestMessage(2)}
	})
	state.mu.Lock()
	state.cursor = chatCursor{ConversationID: "c1", LastSequence: 1000, Seen: map[string]uint64{"replay-old": 1, "thread-2": 2, "post-400": 400, "future": 20_000}}
	state.mu.Unlock()
	if got := state.oldestHeldSequence(); got != 400 {
		t.Fatalf("backward history edge = %d, want visible timeline 400", got)
	}
	if got := state.newestHeldSequence(); got != 401 {
		t.Fatalf("forward history edge = %d, want visible timeline 401", got)
	}
}

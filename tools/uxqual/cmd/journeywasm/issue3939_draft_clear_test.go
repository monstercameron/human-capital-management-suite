package main

import (
	"testing"
	"time"
)

// Issue 3939: a cleared or cancelled draft must reach the account at once. The
// 1.5 second debounce is right for typing and wrong for emptying the composer,
// because a reload inside that window restored the text the person had just
// cleared.
func TestIssue3939_ClearedDraftIsWrittenWithoutWaitingForTheDebounce(t *testing.T) {
	scheduler := &chatTestScheduler{}
	writes := 0
	writer := newChatDraftWriter(scheduler.schedule, 1500*time.Millisecond, func() { writes++ })

	writer.Changed("/todo Pack the van")
	if !writer.Pending() || writes != 0 {
		t.Fatalf("typing must stay debounced: pending=%v writes=%d", writer.Pending(), writes)
	}

	// Cancel in the to-do preview, or deleting the last character, empties the draft.
	writer.Changed("")
	if writes != 1 {
		t.Fatalf("a cleared draft was not written immediately: writes=%d", writes)
	}
	if writer.Pending() {
		t.Fatal("the debounced write of the old text is still pending after the clear")
	}
}

func TestIssue3939_WhitespaceOnlyDraftCountsAsCleared(t *testing.T) {
	scheduler := &chatTestScheduler{}
	writes := 0
	writer := newChatDraftWriter(scheduler.schedule, time.Second, func() { writes++ })
	writer.Changed("  \n ")
	if writes != 1 || writer.Pending() {
		t.Fatalf("whitespace-only draft: writes=%d pending=%v", writes, writer.Pending())
	}
}

func TestIssue3939_SettledWriteControlStillDebounces(t *testing.T) {
	scheduler := &chatTestScheduler{}
	writes := 0
	writer := newChatDraftWriter(scheduler.schedule, time.Second, func() { writes++ })
	writer.Changed("a")
	writer.Changed("ab")
	if writes != 0 || !writer.Pending() {
		t.Fatalf("typing wrote early: writes=%d pending=%v", writes, writer.Pending())
	}
}

package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestIssue3987_JumpingToNewestClearsTheOldTarget: after a pinned or searched
// message was opened, going back to the newest messages must not leave that
// message marked as the target, or a later render marks it again.
func TestIssue3987_JumpingToNewestClearsTheOldTarget(t *testing.T) {
	t.Run("page replaced", func(t *testing.T) {
		state := newChatStateForTest(t)
		state.mutate(func(m *chatui.Model) {
			m.SelectedID, m.FocusMessageID, m.HasNewer = "room", "pin", true
			m.Messages = []chatui.Message{{ID: "pin", Sequence: 3}}
		})
		state.cursor = chatCursor{ConversationID: "room", Seen: map[string]uint64{}}
		if !state.replaceLatestChatMessages("room", []chatui.Message{{ID: "new", Sequence: 90}}, "") {
			t.Fatal("the newest page was not adopted")
		}
		if got := state.snapshot().FocusMessageID; got != "" {
			t.Fatalf("the earlier target %q is still marked after jumping to newest", got)
		}
	})
	t.Run("already loaded", func(t *testing.T) {
		state := newChatStateForTest(t)
		state.mutate(func(m *chatui.Model) { m.SelectedID, m.FocusMessageID = "room", "pin" })
		state.clearJumpTarget()
		if got := state.snapshot().FocusMessageID; got != "" {
			t.Fatalf("the earlier target %q is still marked", got)
		}
	})
}

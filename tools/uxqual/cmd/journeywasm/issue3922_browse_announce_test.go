package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestIssue3922_BrowseAnnouncementWaitsForTheQueryToSettle: every filter change
// silences the status; only the last change's ticket turns it on again.
func TestIssue3922_BrowseAnnouncementWaitsForTheQueryToSettle(t *testing.T) {
	state := newChatStateForTest(t)
	state.mutate(func(model *chatui.Model) { model.ShowBrowse = true })
	first := state.startChatBrowseAnnounce()
	second := state.startChatBrowseAnnounce()
	if state.settleChatBrowseAnnounce(first) || state.snapshot().BrowseAnnounce {
		t.Fatal("a superseded keystroke announced the count")
	}
	if !state.settleChatBrowseAnnounce(second) || !state.snapshot().BrowseAnnounce {
		t.Fatal("the settled query did not turn the status on")
	}
	third := state.startChatBrowseAnnounce()
	if state.snapshot().BrowseAnnounce {
		t.Fatal("typing again left the old count announced")
	}
	state.mutate(func(model *chatui.Model) { model.ShowBrowse = false })
	if state.settleChatBrowseAnnounce(third) || state.snapshot().BrowseAnnounce {
		t.Fatal("a closed browser announced a count")
	}
}

package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Issue 3768: on a phone the conversation list is a screen of its own, so it is
// a step in the browser history: Back from a conversation shows the list before
// it leaves Chat.

func withPhoneViewport(t *testing.T, phone bool) {
	t.Helper()
	previous := chatNavigationPhone
	chatNavigationPhone = func() bool { return phone }
	t.Cleanup(func() { chatNavigationPhone = previous })
}

func TestIssue3768_ListOnPhoneIsItsOwnHistoryState(t *testing.T) {
	model := chatui.Model{CurrentTenantID: "t", CurrentUser: "u", SelectedID: "room-1", SidebarOpen: true}
	withPhoneViewport(t, true)
	list := chatNavigationStateFromModel(model)
	if !list.ShowList {
		t.Fatalf("list on a phone is not recorded: %#v", list)
	}
	model.SidebarOpen = false
	conversation := chatNavigationStateFromModel(model)
	if conversation.ShowList || conversation == list {
		t.Fatalf("conversation state must differ from the list state: %#v vs %#v", conversation, list)
	}
	if !validChatNavigationState(list) {
		t.Fatalf("list state is rejected as invalid: %#v", list)
	}
}

func TestIssue3768_RailOnWiderScreenIsNotAHistoryStep(t *testing.T) {
	withPhoneViewport(t, false)
	model := chatui.Model{CurrentTenantID: "t", CurrentUser: "u", SelectedID: "room-1", SidebarOpen: true}
	if got := chatNavigationStateFromModel(model); got.ShowList {
		t.Fatalf("rail on a wide screen recorded as a list step: %#v", got)
	}
}

func TestIssue3768_BackRestoresListOrConversation(t *testing.T) {
	cases := []struct {
		name                  string
		phone, entryList, now bool
		wantSet, wantOpen     bool
	}{
		{"back to the list entry from a conversation", true, true, false, true, true},
		{"back to a conversation entry from the list", true, false, true, true, false},
		{"already matching", true, true, true, false, false},
		{"a wide screen never toggles the rail", false, true, false, false, false},
		{"a wide screen never closes the rail", false, false, true, false, false},
	}
	for _, c := range cases {
		set, open := chatListRestore(chatNavigationState{ShowList: c.entryList}, c.now, c.phone)
		if set != c.wantSet || open != c.wantOpen {
			t.Errorf("%s: got set=%v open=%v, want set=%v open=%v", c.name, set, open, c.wantSet, c.wantOpen)
		}
	}
}

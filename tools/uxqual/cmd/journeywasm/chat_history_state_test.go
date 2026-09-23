package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestChatNavigationStateFromModel(t *testing.T) {
	model := chatui.Model{CurrentTenantID: "tenant-1", CurrentUser: "subject-1", SelectedID: "room-1", ShowDetails: true, ShowThread: true, ThreadParentID: "post-4"}
	want := chatNavigationState{OwnerTenantID: "tenant-1", OwnerSubject: "subject-1", ConversationID: "room-1", ShowDetails: true, ShowThread: true, ThreadParentID: "post-4"}
	if got := chatNavigationStateFromModel(model); got != want {
		t.Fatalf("chatNavigationStateFromModel() = %#v, want %#v", got, want)
	}
	model.ShowThread = false
	if got := chatNavigationStateFromModel(model); got.ShowThread || got.ThreadParentID != "" {
		t.Fatalf("closed thread leaked into navigation state: %#v", got)
	}
}

func TestValidChatNavigationState(t *testing.T) {
	for _, test := range []struct {
		name  string
		state chatNavigationState
		valid bool
	}{
		{name: "conversation", state: chatNavigationState{OwnerTenantID: "tenant", OwnerSubject: "subject", ConversationID: "room-1"}, valid: true},
		{name: "thread", state: chatNavigationState{OwnerTenantID: "tenant", OwnerSubject: "subject", ConversationID: "room-1", ShowThread: true, ThreadParentID: "post-1"}, valid: true},
		{name: "missing owner", state: chatNavigationState{ConversationID: "room-1"}},
		{name: "thread without room", state: chatNavigationState{OwnerTenantID: "tenant", OwnerSubject: "subject", ShowThread: true, ThreadParentID: "post-1"}},
		{name: "thread without parent", state: chatNavigationState{OwnerTenantID: "tenant", OwnerSubject: "subject", ConversationID: "room-1", ShowThread: true}},
		{name: "oversized conversation", state: chatNavigationState{OwnerTenantID: "tenant", OwnerSubject: "subject", ConversationID: string(make([]byte, 257))}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := validChatNavigationState(test.state); got != test.valid {
				t.Fatalf("validChatNavigationState(%#v) = %v, want %v", test.state, got, test.valid)
			}
		})
	}
}

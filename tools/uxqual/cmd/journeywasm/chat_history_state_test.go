package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestChatNavigationStateFromModel(t *testing.T) {
	model := chatui.Model{CurrentTenantID: "tenant-1", CurrentUser: "subject-1", SelectedID: "room-1", ShowDetails: true, ShowThread: true, ThreadParentID: "post-4", ShowPerson: true, PersonDetails: &chatui.PersonDetails{ID: "person-2"}}
	want := chatNavigationState{OwnerTenantID: "tenant-1", OwnerSubject: "subject-1", ConversationID: "room-1", ShowDetails: true, ShowThread: true, ThreadParentID: "post-4", PersonID: "person-2"}
	if got := chatNavigationStateFromModel(model); got != want {
		t.Fatalf("chatNavigationStateFromModel() = %#v, want %#v", got, want)
	}
	model.ShowThread = false
	if got := chatNavigationStateFromModel(model); got.ShowThread || got.ThreadParentID != "" {
		t.Fatalf("closed thread leaked into navigation state: %#v", got)
	}
	model.ShowPerson = false
	if got := chatNavigationStateFromModel(model); got.PersonID != "" {
		t.Fatalf("closed person pane leaked into navigation state: %#v", got)
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
		{name: "person", state: chatNavigationState{OwnerTenantID: "tenant", OwnerSubject: "subject", ConversationID: "room-1", PersonID: "person-1"}, valid: true},
		{name: "search message", state: chatNavigationState{OwnerTenantID: "tenant", OwnerSubject: "subject", ConversationID: "room-1", FocusMessageID: "post-1", FocusSequence: 42}, valid: true},
		{name: "person without room", state: chatNavigationState{OwnerTenantID: "tenant", OwnerSubject: "subject", PersonID: "person-1"}, valid: true},
		{name: "missing owner", state: chatNavigationState{ConversationID: "room-1"}},
		{name: "thread without room", state: chatNavigationState{OwnerTenantID: "tenant", OwnerSubject: "subject", ShowThread: true, ThreadParentID: "post-1"}},
		{name: "thread without parent", state: chatNavigationState{OwnerTenantID: "tenant", OwnerSubject: "subject", ConversationID: "room-1", ShowThread: true}},
		{name: "oversized conversation", state: chatNavigationState{OwnerTenantID: "tenant", OwnerSubject: "subject", ConversationID: string(make([]byte, 257))}},
		{name: "oversized person", state: chatNavigationState{OwnerTenantID: "tenant", OwnerSubject: "subject", ConversationID: "room-1", PersonID: string(make([]byte, 257))}},
		{name: "search sequence without message", state: chatNavigationState{OwnerTenantID: "tenant", OwnerSubject: "subject", ConversationID: "room-1", FocusSequence: 42}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := validChatNavigationState(test.state); got != test.valid {
				t.Fatalf("validChatNavigationState(%#v) = %v, want %v", test.state, got, test.valid)
			}
		})
	}
}

// TestChatChannelFragmentRoundTrip is NAV-01's pure URL<->state mapping for
// the chat "#channel=" fragment: a uniquely named channel is written by its
// readable name and read back to its id; every other room keeps its id; an id
// fragment (older history entries, pasted links) still resolves.
func TestChatChannelFragmentRoundTrip(t *testing.T) {
	rooms := []chatui.Conversation{
		{ID: "id-announcements", Name: "announcements", Kind: chatui.PublicChannel, Joined: true},
		{ID: "id-benefits", Name: "benefits", Kind: chatui.PublicChannel, Joined: true},
		{ID: "id-payroll", Name: "payroll-close", Kind: chatui.PrivateChannel, Joined: true},
		{ID: "id-dm", Name: "Ana Lopez", Kind: chatui.DirectMessage, Joined: true},
		{ID: "id-group", Name: "Q4 hiring huddle", Kind: chatui.GroupChat, Joined: true},
		{ID: "id-dup-1", Name: "ops", Kind: chatui.PublicChannel, Joined: true},
		{ID: "id-dup-2", Name: "OPS", Kind: chatui.PrivateChannel, Joined: true},
		{ID: "id-blank", Name: " ", Kind: chatui.PublicChannel, Joined: true},
		{ID: "id-shadow", Name: "id-benefits", Kind: chatui.PublicChannel, Joined: true},
	}
	for _, test := range []struct {
		name, id, want string
	}{
		{name: "public channel by name", id: "id-announcements", want: "announcements"},
		{name: "private channel by name", id: "id-payroll", want: "payroll-close"},
		{name: "direct message keeps id", id: "id-dm", want: "id-dm"},
		{name: "group keeps id", id: "id-group", want: "id-group"},
		{name: "ambiguous name keeps id", id: "id-dup-1", want: "id-dup-1"},
		{name: "blank name keeps id", id: "id-blank", want: "id-blank"},
		{name: "name equal to another room id keeps id", id: "id-shadow", want: "id-shadow"},
		{name: "unlisted room keeps id", id: "id-elsewhere", want: "id-elsewhere"},
		{name: "no room", id: "", want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := chatChannelFragmentValue(rooms, test.id)
			if got != test.want {
				t.Fatalf("chatChannelFragmentValue(%q) = %q, want %q", test.id, got, test.want)
			}
			if test.id == "" {
				return
			}
			back, _ := resolveChatChannelFragment(rooms, got)
			if back != test.id {
				t.Fatalf("resolveChatChannelFragment(%q) = %q, want round trip to %q", got, back, test.id)
			}
		})
	}
	for _, test := range []struct {
		name, value, want string
		ok                bool
	}{
		{name: "legacy id form", value: "id-benefits", want: "id-benefits", ok: true},
		{name: "name is case-insensitive", value: "Benefits", want: "id-benefits", ok: true},
		{name: "ambiguous name is unresolved", value: "ops", want: "ops"},
		{name: "direct message name is not a channel", value: "Ana Lopez", want: "Ana Lopez"},
		{name: "unknown value passes through as an id", value: "some-uuid", want: "some-uuid"},
		{name: "empty", value: "", want: ""},
	} {
		t.Run("resolve "+test.name, func(t *testing.T) {
			got, ok := resolveChatChannelFragment(rooms, test.value)
			if got != test.want || ok != test.ok {
				t.Fatalf("resolveChatChannelFragment(%q) = %q/%v, want %q/%v", test.value, got, ok, test.want, test.ok)
			}
		})
	}
}

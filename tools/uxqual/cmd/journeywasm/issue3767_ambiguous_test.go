package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestIssue3767_AmbiguousChannelNameOpensOneMatch: #channel=general with two
// channels named "general" opens one of them instead of treating the name as an
// id the server cannot find ("This channel is no longer available").
func TestIssue3767_AmbiguousChannelNameOpensOneMatch(t *testing.T) {
	rooms := []chatui.Conversation{
		{ID: "id-dm", Name: "general", Kind: chatui.DirectMessage, Joined: true},
		{ID: "id-gen-1", Name: "General", Kind: chatui.PublicChannel},
		{ID: "id-gen-2", Name: "general", Kind: chatui.PublicChannel, Joined: true},
		{ID: "id-gen-3", Name: "general", Kind: chatui.PrivateChannel, Joined: true},
		{ID: "id-eng", Name: "engineering", Kind: chatui.PublicChannel, Joined: true},
		{ID: "id-qa-1", Name: "qa-smoke", Kind: chatui.PublicChannel},
		{ID: "id-qa-2", Name: "qa-smoke", Kind: chatui.PublicChannel},
	}
	for _, test := range []struct{ name, value, want string }{
		{"exact name and joined wins", "general", "id-gen-2"},
		{"case-insensitive prefers a joined one", "GENERAL", "id-gen-2"},
		{"none joined opens the first listed", "qa-smoke", "id-qa-1"},
		{"unique name unchanged", "engineering", "id-eng"},
		{"id wins", "id-gen-3", "id-gen-3"},
		{"unknown passes through", "nothing-here", "nothing-here"},
		{"direct message name is not a channel", "Ana", "Ana"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got, _ := resolveChatChannelAddress(rooms, test.value); got != test.want {
				t.Fatalf("resolveChatChannelAddress(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
	if got := integrate2AddressSelection(rooms, "general", ""); got != "id-gen-2" {
		t.Fatalf("integrate2AddressSelection = %q, want id-gen-2", got)
	}
	if got, _ := chatListingSelection("", "qa-smoke", rooms, false, false, false, true); got != "id-qa-1" {
		t.Fatalf("chatListingSelection = %q, want id-qa-1", got)
	}
}

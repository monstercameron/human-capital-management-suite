package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestIssue3708_LeftChannelStaysInBrowse: leaving a channel from Browse must
// leave it in the listing as a Join row, so a filter for its name still finds
// it and the row flips to Join at once instead of the dialog going empty.
func TestIssue3708_LeftChannelStaysInBrowse(t *testing.T) {
	random := chatui.Conversation{ID: "c-random", Name: "random", Kind: chatui.PublicChannel, Joined: true, MemberCount: 16}
	listing := []chatui.Conversation{{ID: "c-ops", Name: "ops", Kind: chatui.PublicChannel}}

	after := chatux3708ListingAfterLeave(listing, random)
	if len(after) != 2 {
		t.Fatalf("listing after leave has %d rows, want 2", len(after))
	}
	found := filterChatBrowse(after, "random")
	if len(found) != 1 || found[0].ID != "c-random" {
		t.Fatalf("filter for random found %+v, want the left channel", found)
	}
	if found[0].Joined {
		t.Fatalf("left channel is still marked Joined; its row would show no Join button")
	}
	if found[0].MemberCount != 16 {
		t.Fatalf("left channel lost its member count: %d", found[0].MemberCount)
	}
	if len(listing) != 1 {
		t.Fatalf("the listing already held was changed in place: %d rows", len(listing))
	}
}

// TestIssue3708_LeaveDoesNotDuplicateOrInventListing: a second copy is not
// added, and a Browse listing never read this session stays unread (nil), so
// the read that follows is the one that decides what is listed.
func TestIssue3708_LeaveDoesNotDuplicateOrInventListing(t *testing.T) {
	random := chatui.Conversation{ID: "c-random", Name: "random", Kind: chatui.PublicChannel, Joined: true}
	listing := []chatui.Conversation{{ID: "c-random", Name: "random", Kind: chatui.PublicChannel}}

	if got := chatux3708ListingAfterLeave(listing, random); len(got) != 1 {
		t.Fatalf("channel already listed was added again: %d rows", len(got))
	}
	if got := chatux3708ListingAfterLeave(nil, random); got != nil {
		t.Fatalf("an unread listing became %+v, want nil", got)
	}
	if got := chatux3708ListingAfterLeave(listing, chatui.Conversation{}); len(got) != 1 {
		t.Fatalf("an empty channel was listed: %+v", got)
	}
}

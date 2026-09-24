package main

import (
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
)

func TestChatSearchAnchorRequestsLoadBalancedContext(t *testing.T) {
	older, newer := chatSearchAnchorRequests("tenant-a", "room-a", 501)
	if older.GetTenantId() != "tenant-a" || older.GetConversationId() != "room-a" || !older.GetDescending() || older.GetBeforeSequence() != 502 || older.GetPageSize() != chatSearchContextPageSize {
		t.Fatalf("older request = %+v", older)
	}
	if newer.GetTenantId() != "tenant-a" || newer.GetConversationId() != "room-a" || newer.GetDescending() || newer.GetAfterSequence() != 500 || newer.GetPageSize() != chatSearchContextPageSize {
		t.Fatalf("newer request = %+v", newer)
	}
	_, first := chatSearchAnchorRequests("tenant-a", "room-a", 1)
	if first.GetAfterSequence() != 0 {
		t.Fatalf("first-message request after sequence = %d, want 0", first.GetAfterSequence())
	}
}

func TestMergeChatSearchAnchorPostsDeduplicatesAndOrdersBothSides(t *testing.T) {
	anchor := &chatv1.Post{Id: "anchor", Sequence: 10}
	got := mergeChatSearchAnchorPosts(
		[]*chatv1.Post{{Id: "old", Sequence: 9}, anchor, nil},
		[]*chatv1.Post{anchor, {Id: "new", Sequence: 11}},
	)
	if len(got) != 3 {
		t.Fatalf("merged %d posts, want 3: %+v", len(got), got)
	}
	for i, want := range []string{"old", "anchor", "new"} {
		if got[i].GetId() != want {
			t.Fatalf("post %d = %q, want %q", i, got[i].GetId(), want)
		}
	}
}

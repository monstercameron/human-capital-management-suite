package chatstore

import (
	"context"
	"fmt"
	"testing"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestWatchEventsAreNumberedPerConversation_Integration is the client's
// resubscribe loop. Events used to be numbered by the outbox row identifier, a
// counter every conversation on the shard shares, so a quiet room's events
// arrived with holes the size of whatever another room had been doing — which a
// client checking continuity correctly read as a gap.
func TestWatchEventsAreNumberedPerConversation_Integration(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	tenant := "tenant-a"
	watched := chat.Conversation{ID: "c-watched", TenantID: tenant, Kind: chat.PublicChannel, Name: "watched", OwnerID: "alice", Revision: 1}
	noisy := chat.Conversation{ID: "c-noisy", TenantID: tenant, Kind: chat.PublicChannel, Name: "noisy", OwnerID: "alice", Revision: 1}
	for _, c := range []chat.Conversation{watched, noisy} {
		member := chat.Membership{ConversationID: c.ID, TenantID: tenant, HomeTenantID: tenant, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
		if _, err := s.CreateConversation(ctx, c, []chat.Membership{member}, ""); err != nil {
			t.Fatal(err)
		}
	}
	// Interleave: one post in the watched room, then a burst in the other, so the
	// shared outbox counter jumps between every pair of watched events.
	for round := 0; round < 4; round++ {
		if _, err := s.SendPost(ctx, chat.SendPostRequest{TenantID: tenant, ConversationID: watched.ID, IdempotencyKey: fmt.Sprintf("w-%d", round)}, chat.Post{AuthorID: "alice", Body: fmt.Sprintf("watched %d", round)}); err != nil {
			t.Fatal(err)
		}
		for burst := 0; burst < 5; burst++ {
			if _, err := s.SendPost(ctx, chat.SendPostRequest{TenantID: tenant, ConversationID: noisy.ID, IdempotencyKey: fmt.Sprintf("n-%d-%d", round, burst)}, chat.Post{AuthorID: "alice", Body: "noise"}); err != nil {
				t.Fatal(err)
			}
		}
	}

	page, err := s.ReadConversationEvents(ctx, chat.WatchConversationRequest{Principal: chat.Principal{TenantID: tenant, SubjectID: "alice"}, TenantID: tenant, ConversationID: watched.ID}, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) < 5 {
		t.Fatalf("events = %d, want the conversation's own history", len(page.Events))
	}
	previous := uint64(0)
	for i, event := range page.Events {
		got := event.Event.Sequence
		if got == 0 {
			t.Fatalf("event %d has no sequence: %+v", i, event.Event)
		}
		if previous != 0 && got != previous+1 {
			t.Fatalf("event %d jumped from %d to %d: the client reads that as a gap", i, previous, got)
		}
		previous = got
	}
	// The noisy room numbers itself independently, starting from its own first
	// event rather than continuing the watched room's count.
	other, err := s.ReadConversationEvents(ctx, chat.WatchConversationRequest{Principal: chat.Principal{TenantID: tenant, SubjectID: "alice"}, TenantID: tenant, ConversationID: noisy.ID}, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Events) == 0 || other.Events[0].Event.Sequence != page.Events[0].Event.Sequence {
		t.Fatalf("the two rooms do not number themselves independently: %d vs %d", other.Events[0].Event.Sequence, page.Events[0].Event.Sequence)
	}
}

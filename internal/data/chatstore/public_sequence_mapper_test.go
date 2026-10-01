package chatstore

import (
	"context"
	"errors"
	"math"
	"testing"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func TestTodo_AGENTP_011_Fault(t *testing.T) {
	var nilAdapter *Adapter
	for _, tc := range []struct {
		name string
		call func() (uint64, error)
	}{
		{"nil adapter", func() (uint64, error) {
			return nilAdapter.ResolvePublicSequence(context.Background(), "tenant", "conversation", 1)
		}},
		{"empty tenant", func() (uint64, error) {
			return (&Adapter{}).ResolvePublicSequence(context.Background(), "", "conversation", 1)
		}},
		{"empty conversation", func() (uint64, error) {
			return (&Adapter{}).ResolvePublicSequence(context.Background(), "tenant", "", 1)
		}},
		{"overflow", func() (uint64, error) {
			return (&Adapter{}).ResolvePublicSequence(context.Background(), "tenant", "conversation", math.MaxUint64)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.call(); !errors.Is(err, chat.ErrInvalidArgument) {
				t.Fatalf("error=%v, want invalid argument", err)
			}
		})
	}
}

func TestTodo_AGENTP_011_Integration(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "mapper-room", TenantID: "tenant-a", Kind: chat.PublicChannel, OwnerID: "alice", Revision: 1}
	m := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: c.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{m}, ""); err != nil {
		t.Fatal(err)
	}
	_, err := s.SendPost(ctx, chat.SendPostRequest{TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "mapper-first"}, chat.Post{AuthorID: "alice", Body: "first"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SendPost(ctx, chat.SendPostRequest{TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "mapper-second"}, chat.Post{AuthorID: "alice", Body: "second"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.ReadConversationEvents(ctx, chat.WatchConversationRequest{Principal: chat.Principal{TenantID: c.TenantID, SubjectID: "alice"}, TenantID: c.TenantID, ConversationID: c.ID}, 0, 100)
	if err != nil || len(page.Events) < 3 {
		t.Fatalf("read mapper events=%d err=%v", len(page.Events), err)
	}
	firstPublic, secondPublic := page.Events[len(page.Events)-2].Event.Sequence, page.Events[len(page.Events)-1].Event.Sequence
	firstOffset, err := s.ResolvePublicSequence(ctx, c.TenantID, c.ID, firstPublic)
	if err != nil {
		t.Fatal(err)
	}
	secondOffset, err := s.ResolvePublicSequence(ctx, c.TenantID, c.ID, secondPublic)
	if err != nil {
		t.Fatal(err)
	}
	if firstOffset == 0 || secondOffset <= firstOffset {
		t.Fatalf("offsets=%d,%d, want increasing nonzero offsets", firstOffset, secondOffset)
	}
	old, err := s.ResolvePublicSequence(ctx, c.TenantID, c.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if old != 0 {
		t.Fatalf("pre-history sequence mapped to %d, want zero", old)
	}
	missing, err := s.ResolvePublicSequence(ctx, c.TenantID, c.ID, secondPublic+100)
	if err != nil {
		t.Fatal(err)
	}
	if missing != secondOffset {
		t.Fatalf("ahead sequence mapped to %d, want current head %d", missing, secondOffset)
	}
}

func TestTodo_AGENTP_011_Security(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	for _, tenantID := range []string{"tenant-a", "tenant-b"} {
		c := chat.Conversation{ID: "mapper-" + tenantID, TenantID: tenantID, Kind: chat.PublicChannel, OwnerID: "alice", Revision: 1}
		m := chat.Membership{ConversationID: c.ID, TenantID: tenantID, HomeTenantID: tenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
		if _, err := s.CreateConversation(ctx, c, []chat.Membership{m}, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SendPost(ctx, chat.SendPostRequest{TenantID: tenantID, ConversationID: c.ID, IdempotencyKey: "mapper-post-" + tenantID}, chat.Post{AuthorID: "alice", Body: tenantID}); err != nil {
			t.Fatal(err)
		}
	}
	foreign, err := s.ResolvePublicSequence(ctx, "tenant-a", "mapper-tenant-b", 100)
	if err != nil {
		t.Fatal(err)
	}
	if foreign != 0 {
		t.Fatalf("foreign conversation resolved offset %d", foreign)
	}
	foreign, err = s.ResolvePublicSequence(ctx, "tenant-b", "mapper-tenant-a", 100)
	if err != nil {
		t.Fatal(err)
	}
	if foreign != 0 {
		t.Fatalf("foreign tenant resolved offset %d", foreign)
	}
}

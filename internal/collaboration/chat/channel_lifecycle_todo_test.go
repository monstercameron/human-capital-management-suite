package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTodo_CHAT_014(t *testing.T) {
	joined := time.Unix(100, 0).UTC()
	f := &fakeStore{
		conversation: Conversation{ID: "stable-channel", TenantID: "t1", Kind: PublicChannel, OwnerID: "departed-owner", Revision: 4},
		membership:   Membership{ConversationID: "stable-channel", TenantID: "t1", HomeTenantID: "t1", SubjectID: "u1", Role: Manager, JoinedAt: &joined, Revision: 2},
	}
	s := newTestService(f, func() time.Time { return joined.Add(time.Hour) })
	updated, err := s.UpdateConversation(context.Background(), UpdateConversationRequest{
		Principal: principal(), Conversation: Conversation{ID: "stable-channel", TenantID: "t1", Kind: PublicChannel, Name: "Operations", OwnerID: "departed-owner", Archived: true}, ExpectedRevision: 4,
	})
	if err != nil {
		t.Fatalf("manager lifecycle update: %v", err)
	}
	if updated.ID != "stable-channel" || updated.Name != "Operations" || !updated.Archived || updated.OwnerID != "departed-owner" || f.mutations != 1 {
		t.Fatalf("updated=%+v mutations=%d; stable identity and owner should be preserved", updated, f.mutations)
	}
}

func TestTodo_CHAT_014_Security(t *testing.T) {
	joined := time.Unix(100, 0).UTC()
	tests := []struct {
		name       string
		membership Membership
	}{
		{name: "ordinary member", membership: Membership{ConversationID: "stable-channel", TenantID: "t1", HomeTenantID: "t1", SubjectID: "u1", Role: Member, JoinedAt: &joined, Revision: 2}},
		{name: "left manager", membership: Membership{ConversationID: "stable-channel", TenantID: "t1", HomeTenantID: "t1", SubjectID: "u1", Role: Manager, JoinedAt: &joined, LeftAt: timePtr(joined.Add(time.Minute)), Revision: 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeStore{conversation: Conversation{ID: "stable-channel", TenantID: "t1", Kind: PrivateChannel, OwnerID: "u1", Revision: 4}, membership: tt.membership}
			s := newTestService(f, func() time.Time { return joined.Add(time.Hour) })
			_, err := s.UpdateConversation(context.Background(), UpdateConversationRequest{
				Principal: principal(), Conversation: Conversation{ID: "stable-channel", TenantID: "t1", Kind: PrivateChannel, Name: "Renamed", Archived: true}, ExpectedRevision: 4,
			})
			if !errors.Is(err, ErrPermissionDenied) || f.mutations != 0 {
				t.Fatalf("err=%v mutations=%d; wanted permission denial before mutation", err, f.mutations)
			}
		})
	}
}

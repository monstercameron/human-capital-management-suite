package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func TestAgentUXDemo_SeedAgentMessage_Integration(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	tenant, conversation := "tenant-a", "demo-general"
	c := chat.Conversation{ID: conversation, TenantID: tenant, Kind: chat.PublicChannel, OwnerID: "alice", Revision: 1}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{{TenantID: tenant, HomeTenantID: tenant, ConversationID: conversation, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}}, ""); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 24, 9, 30, 0, 0, time.UTC)
	question, err := s.SeedAgentMessage(ctx, tenant, conversation, "assistant", "Heads up: Labor Day is Monday.", "seed-1", "", at)
	if err != nil || question.ID == "" || question.AuthorID != "assistant" || !question.CreatedAt.Equal(at) {
		t.Fatalf("seeded message: %+v %v", question, err)
	}
	again, err := s.SeedAgentMessage(ctx, tenant, conversation, "assistant", "Heads up: Labor Day is Monday.", "seed-1", "", at.Add(time.Hour))
	if err != nil || again.ID != question.ID {
		t.Fatalf("a second run wrote another message: %+v %v", again, err)
	}
	reply, err := s.SeedAgentMessage(ctx, tenant, conversation, "policy-helper", "Up to 40 hours.", "seed-2", question.ID, at.Add(5*time.Minute))
	if err != nil || reply.ParentID != question.ID {
		t.Fatalf("seeded reply: %+v %v", reply, err)
	}
	if _, err := s.SeedAgentMessage(ctx, tenant, conversation, "assistant", " ", "seed-3", "", at); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("an empty message was written: %v", err)
	}
	if _, err := s.SeedAgentMessage(ctx, tenant, "no-such-room", "assistant", "text", "seed-4", "", at); !errors.Is(err, ErrNotMember) {
		t.Fatalf("a message was written into a room that is not a public channel: %v", err)
	}
}

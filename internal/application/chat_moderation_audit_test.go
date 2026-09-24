package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
)

func TestTodo_CHAT_047_ServedModerationAudit(t *testing.T) {
	repo := chatrecords.NewMemoryRepository()
	s := &ChatExtensions{Conversations: extensionConversations{}, Records: &chatrecords.Service{Repo: repo, Auth: ChatRecordAuthority{}}}
	err := s.Moderate(context.Background(), chat.Principal{TenantID: "tenant", SubjectID: "owner"}, "conv", "case", "remove", "post-9", "policy violation", "evidence://case")
	if err != nil {
		t.Fatal(err)
	}
	events, err := repo.Events(context.Background(), "tenant")
	if err != nil || len(events) != 1 {
		t.Fatalf("moderation audit = %+v, %v", events, err)
	}
	e := events[0]
	if e.Action != "moderation.remove" || e.ActorID != "owner" || e.TargetID != "post-9" || e.PriorRevision != 1 || e.PolicyEvidence != "chat:conv:1" {
		t.Fatalf("served audit not bound to current authorization: %+v", e)
	}
}

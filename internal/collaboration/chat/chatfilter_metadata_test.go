package chat

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

func TestTodo_CHATMOD_002_Metadata(t *testing.T) {
	now := time.Now().UTC()
	f := &fakeStore{conversation: Conversation{ID: "c", TenantID: "t", Kind: PublicChannel, Name: "General", Revision: 1}, membership: Membership{SubjectID: "writer", Role: Manager, JoinedAt: &now, Revision: 1}}
	core := newChatfilterTestService(f, func() time.Time { return now })
	repo := &chatfilterFixture{action: "block"}
	core.SetContentPolicy(&FilterContentPolicy{Filters: &chatfilter.Service{Store: repo, Registry: chatfilter.NewRegistry()}})
	s := WithFilterMetadata(core)
	p := Principal{TenantID: "t", SubjectID: "writer"}
	_, err := s.CreateConversation(t.Context(), CreateConversationRequest{Principal: p, TenantID: "t", Kind: PublicChannel, Name: "quartz"})
	if !errors.Is(err, chatfilter.ErrBlocked) || f.mutations != 0 {
		t.Fatal("channel creation bypass", err)
	}
	c := f.conversation
	c.Name = "quartz"
	_, err = s.UpdateConversation(t.Context(), UpdateConversationRequest{Principal: p, Conversation: c, ExpectedRevision: 1})
	if !errors.Is(err, chatfilter.ErrBlocked) || f.mutations != 0 {
		t.Fatal("channel rename bypass", err)
	}
	f.membership.Role = Member
	before := len(repo.records)
	_, err = s.UpdateConversation(t.Context(), UpdateConversationRequest{Principal: p, Conversation: c, ExpectedRevision: 1})
	if !errors.Is(err, ErrPermissionDenied) || len(repo.records) != before {
		t.Fatal("unauthorized filter side effect", err)
	}
}

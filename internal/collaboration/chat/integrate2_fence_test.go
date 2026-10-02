package chat

import (
	"context"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"testing"
)

func TestTodo_CHATSTATE_001_Integrate2MutationRecheck(t *testing.T) {
	if err := RecheckChannelMutation(t.Context()); err != nil {
		t.Fatal(err)
	}
	refused := errors.New("current status refuses mutation")
	calls := 0
	ctx := WithChannelMutationCheck(t.Context(), func(context.Context) error { calls++; return refused })
	if err := RecheckChannelMutation(ctx); !errors.Is(err, refused) || calls != 1 {
		t.Fatal("authority recheck lost", calls, err)
	}
}

func TestTodo_CHATSTATE_001_Integrate2PublicAdmission(t *testing.T) {
	s, f, _, _ := chatstateService()
	p := Principal{TenantID: "tenant", SubjectID: "new-person"}
	f.membership = Membership{}
	if err := s.CheckChannelMembershipAdmissionByID(t.Context(), p, f.conversation.TenantID, f.conversation.ID); err != nil {
		t.Fatal("open public admission", err)
	}
	f.status.Status = chatpolicy.StatusLocked
	if err := s.CheckChannelMembershipAdmission(t.Context(), p, f.conversation); err != nil {
		t.Fatal("locked channel forbade allowed membership management", err)
	}
	f.status.Status = chatpolicy.StatusArchived
	if err := s.CheckChannelMembershipAdmission(t.Context(), p, f.conversation); !errors.Is(err, ErrChannelStatus) {
		t.Fatal("archived public admission", err)
	}
}

func TestTodo_CHATSTATE_001_Integrate2PersonaCapability(t *testing.T) {
	s, f, _, _ := chatstateService()
	r := PersonaReplyCommitRequest{TenantID: f.conversation.TenantID, ConversationID: f.conversation.ID, AuthorID: "agent", ExpectedAudienceRevision: 1, OutputDigest: "digest", Body: "Answer", ParentID: "thread"}
	r.Proof = personaDeliveryProof{tenantID: r.TenantID, conversationID: r.ConversationID, authorID: r.AuthorID, audienceRevision: 1, outputDigest: r.OutputDigest, bodyDigest: digestBody(r.Body, r.ParentID), parentID: r.ParentID}
	for _, status := range []chatpolicy.ChannelStatus{chatpolicy.StatusOpen, chatpolicy.StatusAnnouncements} {
		f.status.Status = status
		if err := s.CheckPersonaReplyChannelStatus(t.Context(), r); err != nil {
			t.Fatal("authorized public proof refused", status, err)
		}
	}
	f.status.Status = chatpolicy.StatusLocked
	if err := s.CheckPersonaReplyChannelStatus(t.Context(), r); !errors.Is(err, ErrChannelStatus) {
		t.Fatal("locked delivery allowed", err)
	}
	f.status.Status = chatpolicy.StatusOpen
	r.Body = "forged"
	if err := s.CheckPersonaReplyChannelStatus(t.Context(), r); !errors.Is(err, ErrPermissionDenied) {
		t.Fatal("forged capability allowed", err)
	}
}

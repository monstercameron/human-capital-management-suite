package chatstore

import (
	"context"
	"errors"
	"testing"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func TestTodo_AGENTP_AudiencePolicyAcceptsCanonicalPrivateKinds(t *testing.T) {
	for _, kind := range []string{"PRIVATE_CHANNEL", "DIRECT", "GROUP", "DIRECT_MESSAGE", "GROUP_DM", "DM"} {
		if !audiencePolicyConversation(kind) {
			t.Errorf("audience policy rejected supported conversation kind %q", kind)
		}
	}
	for _, kind := range []string{"PUBLIC_CHANNEL", "PUBLIC", "UNKNOWN"} {
		if audiencePolicyConversation(kind) {
			t.Errorf("audience policy accepted unsupported conversation kind %q", kind)
		}
	}
}

func TestTodo_AGENTP_PrivateConversationCreationPersistsExplicitPolicy(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	conversation := chat.Conversation{ID: "private-with-policy", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "Payroll", OwnerID: "owner", Revision: 1}
	members := []chat.Membership{{ConversationID: conversation.ID, TenantID: conversation.TenantID, HomeTenantID: conversation.TenantID, SubjectID: conversation.OwnerID, Role: chat.Manager, HistoryVisibility: chat.FullHistory}}
	policy := AudiencePolicy{RequiredRoles: []string{"payroll"}, RoleMode: 1, Classification: "T3", Residency: "us"}
	if _, err := NewAdapter(s).CreateConversationWithAudiencePolicy(ctx, conversation, members, "create-private", policy); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.CaptureAudienceSnapshot(ctx, conversation.TenantID, conversation.ID)
	if err != nil {
		t.Fatalf("capture created conversation audience: %v", err)
	}
	if snapshot.PolicyRevision != 1 || snapshot.ConversationRevision != 1 || snapshot.Classification != policy.Classification || len(snapshot.Members) != 1 || snapshot.Members[0].MemberID != conversation.OwnerID {
		t.Fatalf("created audience snapshot = %#v; want initial policy and exact member", snapshot)
	}
}

func TestTodo_AGENTP_PrivateConversationCreationRequiresExplicitFacts(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	conversation := chat.Conversation{ID: "missing-facts", TenantID: "tenant-a", Kind: chat.Group, OwnerID: "owner", Revision: 1}
	members := []chat.Membership{{ConversationID: conversation.ID, TenantID: conversation.TenantID, HomeTenantID: conversation.TenantID, SubjectID: conversation.OwnerID, Role: chat.Manager, HistoryVisibility: chat.FullHistory}}
	if _, err := NewAdapter(s).CreateConversationWithAudiencePolicy(ctx, conversation, members, "", AudiencePolicy{RoleMode: 1}); err == nil {
		t.Fatal("creation accepted missing classification")
	}
	if exists, err := s.ConversationExists(ctx, conversation.TenantID, conversation.ID); err != nil || exists {
		t.Fatalf("invalid creation persisted conversation: exists=%t err=%v", exists, err)
	}
	public := conversation
	public.ID = "public-policy"
	public.Kind = chat.PublicChannel
	if _, err := NewAdapter(s).CreateConversationWithAudiencePolicy(ctx, public, members, "", AudiencePolicy{RoleMode: 1, Classification: "T3"}); err == nil {
		t.Fatal("private audience policy API accepted public conversation")
	}
	if exists, err := s.ConversationExists(ctx, conversation.TenantID, conversation.ID); err != nil || exists {
		t.Fatalf("rejected creation persisted conversation: exists=%t err=%v", exists, err)
	}
	if _, err := NewAdapter(s).CreateConversation(ctx, conversation, members, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CaptureAudienceSnapshot(ctx, "tenant-a", conversation.ID); !errors.Is(err, ErrAudienceEligibilityUnavailable) {
		t.Fatalf("legacy creation without authoritative policy = %v; want fail closed", err)
	}
}

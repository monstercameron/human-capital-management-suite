package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

type personaChatScopeAudienceFake struct {
	*personaPrivateScopeAudienceFake
	public chatstore.PublicAudienceSnapshot
	err    error
}

func (f *personaChatScopeAudienceFake) CapturePublicAudienceSnapshot(_ context.Context, tenant, room string) (chatstore.PublicAudienceSnapshot, error) {
	f.tenant, f.room = tenant, room
	return f.public, f.err
}

func TestTodo_AGENTP_008_PublicChatScopeUsesCurrentAdmissionAndVisiblePost(t *testing.T) {
	_, request, conversation, private, thread := personaPrivateScopeFixture(t)
	audience := &personaChatScopeAudienceFake{personaPrivateScopeAudienceFake: private,
		public: chatstore.PublicAudienceSnapshot{TenantID: "tenant-a", ConversationID: "room-a", Revision: 9, PolicyRevision: 5,
			Current:  []chatstore.PublicAudiencePrincipal{{HomeTenantID: "tenant-a", SubjectID: "alice"}},
			Eligible: []chatstore.PublicAudiencePrincipal{{HomeTenantID: "tenant-a", SubjectID: "alice"}, {HomeTenantID: "tenant-a", SubjectID: "bob"}}}}
	scope, err := NewPersonaChatScopeAuthorizer(conversation, audience, thread)
	if err != nil {
		t.Fatal(err)
	}
	if evidence, err := scope.AuthorizePersonaChat(request.ctx, request.scope); err != nil || !evidence.PrivateConversation || evidence.PublicConversation {
		t.Fatalf("private scope=%#v err=%v", evidence, err)
	}
	conversation.conversation.Kind = chat.PublicChannel
	evidence, err := scope.AuthorizePersonaChat(request.ctx, request.scope)
	if err != nil || !evidence.PublicConversation || evidence.PrivateConversation || evidence.PublicPolicyRev != 5 || evidence.ConversationRev != 9 || evidence.MembershipRev != 9 || evidence.InvokingPostAuthor != "alice" {
		t.Fatalf("public scope=%#v err=%v", evidence, err)
	}
	for _, tc := range []struct {
		name   string
		mutate func()
	}{
		{"no explicit admission", func() { audience.err = errors.New("admission unavailable") }},
		{"missing future population", func() { audience.public.Eligible = nil }},
		{"foreign snapshot", func() { audience.public.TenantID = "tenant-b" }},
		{"missing policy revision", func() { audience.public.PolicyRevision = 0 }},
		{"not current member", func() { audience.public.Current = nil }},
		{"invoker is only future member", func() {
			audience.public.Current = []chatstore.PublicAudiencePrincipal{{HomeTenantID: "tenant-a", SubjectID: "bob"}}
		}},
		{"current member omitted from admission", func() { audience.public.Eligible = audience.public.Eligible[1:] }},
		{"guest invoker", func() { audience.public.Current[0].Guest = true; audience.public.Eligible[0].Guest = true }},
		{"duplicate current principal", func() { audience.public.Current = append(audience.public.Current, audience.public.Current[0]) }},
		{"duplicate eligible principal", func() { audience.public.Eligible = append(audience.public.Eligible, audience.public.Eligible[0]) }},
		{"membership raced thread read", func() { thread.snapshot.AuthorityRevision++ }},
		{"archived channel", func() { conversation.conversation.Archived = true }},
		{"invisible invoking post", func() { thread.err = chat.ErrPermissionDenied }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previousPublic, previousThread, previousConversation := audience.public, thread.snapshot, conversation.conversation
			audience.public.Current = append([]chatstore.PublicAudiencePrincipal(nil), audience.public.Current...)
			audience.public.Eligible = append([]chatstore.PublicAudiencePrincipal(nil), audience.public.Eligible...)
			tc.mutate()
			defer func() {
				audience.public = previousPublic
				thread.snapshot = previousThread
				conversation.conversation = previousConversation
				audience.err = nil
				thread.err = nil
			}()
			if _, err := scope.AuthorizePersonaChat(request.ctx, request.scope); err == nil {
				t.Fatal("invalid public scope admitted")
			}
		})
	}
	if _, err := scope.AuthorizePersonaChat(context.Background(), request.scope); err == nil {
		t.Fatal("unverified invoker admitted")
	}
	if _, err := scope.AuthorizePersonaChat(request.ctx, agentgate.PrivateChatScopeRequest{}); err == nil {
		t.Fatal("empty tuple admitted")
	}
	if _, err := NewPersonaChatScopeAuthorizer(nil, nil, nil); err == nil {
		t.Fatal("missing owners admitted")
	}
}

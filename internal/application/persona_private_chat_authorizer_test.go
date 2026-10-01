package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaPrivateScopeConversationFake struct {
	conversation chat.Conversation
	err          error
	request      chat.GetConversationRequest
}

func (f *personaPrivateScopeConversationFake) GetConversation(_ context.Context, request chat.GetConversationRequest) (chat.Conversation, error) {
	f.request = request
	return f.conversation, f.err
}

type personaPrivateScopeAudienceFake struct {
	snapshot chatstore.AudienceSnapshot
	err      error
	tenant   string
	room     string
}

func (f *personaPrivateScopeAudienceFake) CaptureAudienceSnapshot(_ context.Context, tenant, room string) (chatstore.AudienceSnapshot, error) {
	f.tenant, f.room = tenant, room
	return f.snapshot, f.err
}

type personaPrivateScopeThreadFake struct {
	snapshot chat.ThreadSnapshot
	err      error
	request  chat.ThreadSnapshotRequest
}

func (f *personaPrivateScopeThreadFake) CaptureThreadSnapshot(_ context.Context, request chat.ThreadSnapshotRequest) (chat.ThreadSnapshot, error) {
	f.request = request
	return f.snapshot, f.err
}

func TestTodo_AGENTP_008_PrivateChatScopeAuthorizesExactCurrentPost(t *testing.T) {
	authorizer, request, conversation, audience, thread := personaPrivateScopeFixture(t)
	evidence, err := authorizer.AuthorizePrivateChat(request.ctx, request.scope)
	if err != nil {
		t.Fatalf("authorize private chat: %v", err)
	}
	if !evidence.Allowed || evidence.Tenant != request.scope.Tenant || evidence.InvokerID != "alice" ||
		evidence.ConversationID != "room-a" || evidence.ThreadID != "root-a" || evidence.InvokingPostID != "post-a" ||
		evidence.InvokingPostAuthor != "alice" || !evidence.PrivateConversation || !evidence.ActiveMember || !evidence.PostVisible ||
		evidence.ConversationRev != 9 || evidence.MembershipRev != 4 || evidence.PostDigest == "" || evidence.EvaluatedAt != request.scope.At {
		t.Fatalf("evidence = %+v", evidence)
	}
	if conversation.request.TenantID != "tenant-a" || conversation.request.ConversationID != "room-a" || conversation.request.Principal.SubjectID != "alice" {
		t.Fatalf("conversation was not read for trusted invoker: %+v", conversation.request)
	}
	if audience.tenant != "tenant-a" || audience.room != "room-a" || thread.request.InvokingPostID != "post-a" || thread.request.ThreadID != "root-a" || thread.request.Limit != chat.MaxThreadSnapshotPosts {
		t.Fatalf("current chat snapshots were not bound to the requested tuple: audience=%+v thread=%+v", audience, thread.request)
	}
}

func TestTodo_AGENTP_008_PrivateChatScopeFailsClosedOnUnavailableOrMismatchedAuthority(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*personaPrivateScopeInputs)
	}{
		{name: "public conversation", mutate: func(in *personaPrivateScopeInputs) { in.conversation.conversation.Kind = chat.PublicChannel }},
		{name: "foreign tenant conversation", mutate: func(in *personaPrivateScopeInputs) { in.conversation.conversation.TenantID = "tenant-b" }},
		{name: "missing active membership", mutate: func(in *personaPrivateScopeInputs) { in.audience.snapshot.Members = nil }},
		{name: "invoker is not a current member", mutate: func(in *personaPrivateScopeInputs) { in.audience.snapshot.Members[0].MemberID = "mallory" }},
		{name: "membership revision unavailable", mutate: func(in *personaPrivateScopeInputs) { in.audience.snapshot.Members[0].Revision = 0 }},
		{name: "foreign invoker membership", mutate: func(in *personaPrivateScopeInputs) { in.audience.snapshot.Members[0].External = true }},
		{name: "duplicate membership", mutate: func(in *personaPrivateScopeInputs) {
			in.audience.snapshot.Members = append(in.audience.snapshot.Members, in.audience.snapshot.Members[0])
		}},
		{name: "audience revision mismatch", mutate: func(in *personaPrivateScopeInputs) { in.thread.snapshot.AuthorityRevision++ }},
		{name: "malformed audience digest", mutate: func(in *personaPrivateScopeInputs) { in.audience.snapshot.Digest = "invalid" }},
		{name: "wrong thread reader", mutate: func(in *personaPrivateScopeInputs) {
			in.thread.snapshot.PrincipalID = "mallory"
			in.refreshThreadDigest(t)
		}},
		{name: "stale thread digest", mutate: func(in *personaPrivateScopeInputs) { in.thread.snapshot.Digest = "sha256:" + strings.Repeat("b", 64) }},
		{name: "invoking post by peer", mutate: func(in *personaPrivateScopeInputs) {
			in.thread.snapshot.Posts[0].AuthorID = "mallory"
			in.refreshThreadDigest(t)
		}},
		{name: "invoking post outside requested thread", mutate: func(in *personaPrivateScopeInputs) {
			in.thread.snapshot.Posts[0].ParentID = "other-root"
			in.refreshThreadDigest(t)
		}},
		{name: "post deleted", mutate: func(in *personaPrivateScopeInputs) {
			in.thread.snapshot.Posts[0].Deleted = true
			in.refreshThreadDigest(t)
		}},
		{name: "snapshot read failed", mutate: func(in *personaPrivateScopeInputs) { in.thread.err = errors.New("store unavailable") }},
		{name: "policy read failed", mutate: func(in *personaPrivateScopeInputs) { in.audience.err = errors.New("store unavailable") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			authorizer, request, conversation, audience, thread := personaPrivateScopeFixture(t)
			inputs := personaPrivateScopeInputs{conversation: conversation, audience: audience, thread: thread}
			tc.mutate(&inputs)
			if _, err := authorizer.AuthorizePrivateChat(request.ctx, request.scope); err == nil || !strings.Contains(err.Error(), "private chat scope unavailable") {
				t.Fatalf("invalid authority error = %v", err)
			}
		})
	}
}

func TestTodo_AGENTP_008_PrivateChatScopeRequiresVerifiedCurrentHuman(t *testing.T) {
	authorizer, request, _, _, _ := personaPrivateScopeFixture(t)
	if _, err := authorizer.AuthorizePrivateChat(context.Background(), request.scope); err == nil {
		t.Fatal("unverified context received private chat authority")
	}
	other, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: "mallory", SubjectKind: trust.SubjectKindHuman,
		Purposes: []string{"persona-mention"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceHigh, SessionRef: "session-other", IssuedAt: request.scope.At.Add(-time.Minute),
		ExpiresAt: request.scope.At.Add(time.Hour), CredentialDigest: "cred:sha256:other",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authorizer.AuthorizePrivateChat(trust.WithPrincipal(context.Background(), other), request.scope); err == nil {
		t.Fatal("different context principal received private chat authority")
	}
}

func TestTodo_AGENTP_008_PrivateChatScopeConstructorRequiresAllAuthorities(t *testing.T) {
	if _, err := NewPersonaPrivateChatScopeAuthorizer(nil, nil, nil); err == nil {
		t.Fatal("constructor accepted missing authority sources")
	}
}

type personaPrivateScopeInputs struct {
	conversation *personaPrivateScopeConversationFake
	audience     *personaPrivateScopeAudienceFake
	thread       *personaPrivateScopeThreadFake
}

func (in personaPrivateScopeInputs) refreshThreadDigest(t *testing.T) {
	t.Helper()
	digest, err := chat.ThreadSnapshotDigest(in.thread.snapshot)
	if err != nil {
		t.Fatal(err)
	}
	in.thread.snapshot.Digest = digest
	in.thread.snapshot.SnapshotID = "chat-thread-" + digest
}

type personaPrivateScopeRequest struct {
	ctx   context.Context
	scope agentgate.PrivateChatScopeRequest
}

func personaPrivateScopeFixture(t *testing.T) (*PersonaPrivateChatScopeAuthorizer, personaPrivateScopeRequest, *personaPrivateScopeConversationFake, *personaPrivateScopeAudienceFake, *personaPrivateScopeThreadFake) {
	t.Helper()
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: "alice", SubjectKind: trust.SubjectKindHuman,
		Purposes: []string{"persona-mention"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceHigh, SessionRef: "session-a", IssuedAt: at.Add(-time.Minute),
		ExpiresAt: at.Add(time.Hour), CredentialDigest: "cred:sha256:alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	scope := agentgate.PrivateChatScopeRequest{
		User: agentgate.UserContext{Principal: principal}, Purpose: "persona-mention", Tenant: values.TenantId("tenant-a"),
		ConversationID: "room-a", ThreadID: "root-a", InvokingPostID: "post-a", At: at,
	}
	conversation := &personaPrivateScopeConversationFake{conversation: chat.Conversation{ID: "room-a", TenantID: "tenant-a", Kind: chat.PrivateChannel, Revision: 3}}
	audience := &personaPrivateScopeAudienceFake{snapshot: chatstore.AudienceSnapshot{
		TenantID: "tenant-a", ConversationID: "room-a", Kind: string(chat.PrivateChannel), ConversationRevision: 9,
		PolicyRevision: 2, Digest: "sha256:" + strings.Repeat("a", 64), SnapshotID: "chat-audience-sha256:" + strings.Repeat("a", 64),
		Members: []chatstore.AudienceMember{{HomeTenantID: "tenant-a", MemberID: "alice", Revision: 4}},
	}}
	thread := &personaPrivateScopeThreadFake{snapshot: chat.ThreadSnapshot{
		TenantID: "tenant-a", ConversationID: "room-a", ThreadID: "root-a", InvokingPostID: "post-a",
		PrincipalTenantID: "tenant-a", PrincipalID: "alice", Revision: 21, AuthorityRevision: 9,
		Posts: []chat.Post{{ID: "post-a", TenantID: "tenant-a", ConversationID: "room-a", AuthorID: "alice", AuthorHomeTenantID: "tenant-a", Body: "question", Sequence: 20, Revision: 2, ParentID: "root-a"}},
	}}
	inputs := personaPrivateScopeInputs{conversation: conversation, audience: audience, thread: thread}
	inputs.refreshThreadDigest(t)
	authorizer, err := NewPersonaPrivateChatScopeAuthorizer(conversation, audience, thread)
	if err != nil {
		t.Fatal(err)
	}
	return authorizer, personaPrivateScopeRequest{ctx: trust.WithPrincipal(context.Background(), principal), scope: scope}, conversation, audience, thread
}

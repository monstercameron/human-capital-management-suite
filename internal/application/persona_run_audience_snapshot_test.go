package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaRunAudienceChatFake struct {
	conversation chat.Conversation
	err          error
	request      chat.GetConversationRequest
}

func (f *personaRunAudienceChatFake) GetConversation(_ context.Context, request chat.GetConversationRequest) (chat.Conversation, error) {
	f.request = request
	return f.conversation, f.err
}

type personaRunChatAudienceSnapshotFake struct {
	snapshot chatstore.AudienceSnapshot
	err      error
	tenantID string
	roomID   string
}

func (f *personaRunChatAudienceSnapshotFake) CaptureAudienceSnapshot(_ context.Context, tenantID, conversationID string) (chatstore.AudienceSnapshot, error) {
	f.tenantID, f.roomID = tenantID, conversationID
	return f.snapshot, f.err
}

func personaRunAudienceContext(t *testing.T, tenant, subject string) context.Context {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(tenant), Subject: subject, SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "org", Purposes: []string{"persona-mention"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session", IssuedAt: time.Unix(100, 0), ExpiresAt: time.Unix(200, 0), CredentialDigest: "cred:sha256:1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), p)
}

func TestTodo_AGENT_015_AudienceSnapshot(t *testing.T) {
	conversation := chat.Conversation{ID: "room", TenantID: "tenant-a", Kind: chat.PublicChannel, Revision: 7}
	snapshot := personaAudienceFloorSnapshot()
	snapshot.CurrentMembers[0].TenantID = "tenant-a"
	snapshot.CurrentMembers[0].SubjectID = "alice"
	snapshot.EligibleFutureMembers[0].TenantID = "tenant-a"
	snapshot.EligibleFutureMembers[0].SubjectID = "alice"
	chatReader := &personaRunAudienceChatFake{conversation: conversation}
	source := &PersonaRunAudienceSnapshotSource{Chat: chatReader, Snapshot: &personaAudienceFloorSnapshotFake{snapshot: snapshot}}
	invocation := agentinvoke.RunRequest{TenantID: "tenant-a", ConversationID: "room", InvokerID: "alice"}
	got, err := source.ResolvePersonaRunAudience(personaRunAudienceContext(t, "tenant-a", "alice"), invocation)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "room" || got.SnapshotID == "" || len(got.SnapshotID) > 256 || !personaRequestDigest(got.Digest) {
		t.Fatalf("audience scope = %+v", got)
	}
	if chatReader.request.TenantID != "tenant-a" || chatReader.request.ConversationID != "room" || chatReader.request.Principal.SubjectID != "alice" {
		t.Fatalf("conversation read was not bound to invocation: %+v", chatReader.request)
	}
	// Membership ordering is not authority, so equivalent snapshots have the
	// same identity and digest even if their database row order changes.
	reordered := personaAudienceFloorSnapshot()
	reordered.CurrentMembers[0].TenantID = "tenant-a"
	reordered.CurrentMembers[0].SubjectID = "alice"
	reordered.EligibleFutureMembers[0].TenantID = "tenant-a"
	reordered.EligibleFutureMembers[0].SubjectID = "alice"
	reordered.EligibleFutureMembers[0], reordered.EligibleFutureMembers[1] = reordered.EligibleFutureMembers[1], reordered.EligibleFutureMembers[0]
	source.Snapshot = &personaAudienceFloorSnapshotFake{snapshot: reordered}
	second, err := source.ResolvePersonaRunAudience(personaRunAudienceContext(t, "tenant-a", "alice"), invocation)
	if err != nil || second != got {
		t.Fatalf("reordered snapshot = %+v, %v; want %+v", second, err, got)
	}
	changed := personaAudienceFloorSnapshot()
	changed.CurrentMembers[0].TenantID = "tenant-a"
	changed.CurrentMembers[0].SubjectID = "alice"
	changed.EligibleFutureMembers[0].TenantID = "tenant-a"
	changed.EligibleFutureMembers[0].SubjectID = "alice"
	changed.Revision++
	source.Snapshot = &personaAudienceFloorSnapshotFake{snapshot: changed}
	third, err := source.ResolvePersonaRunAudience(personaRunAudienceContext(t, "tenant-a", "alice"), invocation)
	if err != nil || third.Digest == got.Digest || third.SnapshotID == got.SnapshotID {
		t.Fatalf("new audience revision did not change snapshot identity: %+v, %v", third, err)
	}
}

func TestTodo_AGENT_015_AudienceSnapshot_Security(t *testing.T) {
	invocation := agentinvoke.RunRequest{TenantID: "tenant-a", ConversationID: "room", InvokerID: "alice"}
	validConversation := chat.Conversation{ID: "room", TenantID: "tenant-a", Kind: chat.PublicChannel, Revision: 7}
	valid := personaAudienceFloorSnapshot()
	tests := []struct {
		name         string
		ctx          context.Context
		conversation chat.Conversation
		snapshot     PersonaAudienceFloorSnapshot
		chatErr      error
		snapshotErr  error
	}{
		{name: "missing verified principal", ctx: context.Background(), conversation: validConversation, snapshot: valid},
		{name: "wrong tenant principal", ctx: personaRunAudienceContext(t, "tenant-b", "alice"), conversation: validConversation, snapshot: valid},
		{name: "wrong invoker principal", ctx: personaRunAudienceContext(t, "tenant-a", "bob"), conversation: validConversation, snapshot: valid},
		{name: "foreign conversation", ctx: personaRunAudienceContext(t, "tenant-a", "alice"), conversation: chat.Conversation{ID: "room", TenantID: "tenant-b", Kind: chat.PublicChannel, Revision: 7}, snapshot: valid},
		{name: "private channel cannot use public eligibility image", ctx: personaRunAudienceContext(t, "tenant-a", "alice"), conversation: chat.Conversation{ID: "room", TenantID: "tenant-a", Kind: chat.PrivateChannel, Revision: 7}, snapshot: valid},
		{name: "incomplete audience", ctx: personaRunAudienceContext(t, "tenant-a", "alice"), conversation: validConversation, snapshot: func() PersonaAudienceFloorSnapshot { s := valid; s.EligibilityComplete = false; return s }()},
		{name: "unfenced audience", ctx: personaRunAudienceContext(t, "tenant-a", "alice"), conversation: validConversation, snapshot: func() PersonaAudienceFloorSnapshot { s := valid; s.FenceComplete = false; return s }()},
		{name: "reader failure", ctx: personaRunAudienceContext(t, "tenant-a", "alice"), conversation: validConversation, snapshot: valid, chatErr: errors.New("unavailable")},
		{name: "snapshot failure", ctx: personaRunAudienceContext(t, "tenant-a", "alice"), conversation: validConversation, snapshot: valid, snapshotErr: errors.New("unavailable")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := &PersonaRunAudienceSnapshotSource{
				Chat:     &personaRunAudienceChatFake{conversation: tc.conversation, err: tc.chatErr},
				Snapshot: &personaAudienceFloorSnapshotFake{snapshot: tc.snapshot, err: tc.snapshotErr},
			}
			if got, err := source.ResolvePersonaRunAudience(tc.ctx, invocation); err == nil || got != (agentrun.AudienceScope{}) {
				t.Fatalf("ResolvePersonaRunAudience = %+v, %v; want empty scope and error", got, err)
			}
		})
	}
}

func TestTodo_AGENT_015_AudienceSnapshot_PrivateConversation(t *testing.T) {
	tests := []chat.ConversationKind{chat.PrivateChannel, chat.Group, chat.Direct}
	for _, kind := range tests {
		t.Run(string(kind), func(t *testing.T) {
			conversation := chat.Conversation{ID: "room", TenantID: "tenant-a", Kind: kind, Revision: 4}
			digest := "sha256:" + strings.Repeat("a", 64)
			chatSnapshot := chatstore.AudienceSnapshot{
				TenantID: "tenant-a", ConversationID: "room", Kind: string(kind), ConversationRevision: 4,
				PolicyRevision: 3, Members: []chatstore.AudienceMember{{HomeTenantID: "tenant-a", MemberID: "alice", Role: "MEMBER", Revision: 2}},
				Digest: digest, SnapshotID: "chat-audience-" + digest,
			}
			reader := &personaRunChatAudienceSnapshotFake{snapshot: chatSnapshot}
			source := &PersonaRunAudienceSnapshotSource{Chat: &personaRunAudienceChatFake{conversation: conversation}, ChatStore: reader}
			invocation := agentinvoke.RunRequest{TenantID: "tenant-a", ConversationID: "room", InvokerID: "alice"}
			got, err := source.ResolvePersonaRunAudience(personaRunAudienceContext(t, "tenant-a", "alice"), invocation)
			if err != nil {
				t.Fatal(err)
			}
			if got != (agentrun.AudienceScope{ID: "room", SnapshotID: chatSnapshot.SnapshotID, Digest: digest}) {
				t.Fatalf("audience scope = %+v", got)
			}
			if reader.tenantID != "tenant-a" || reader.roomID != "room" {
				t.Fatalf("snapshot read scope = %s/%s", reader.tenantID, reader.roomID)
			}
		})
	}
}

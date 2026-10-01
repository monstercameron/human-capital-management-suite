package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaBackgroundAudienceFake struct {
	snapshot chatstore.AudienceSnapshot
	err      error
	tenant   string
	room     string
	hasHuman bool
}

func (f *personaBackgroundAudienceFake) CaptureAudienceSnapshot(ctx context.Context, tenant, room string) (chatstore.AudienceSnapshot, error) {
	f.tenant, f.room = tenant, room
	principal, ok := trust.FromContext(ctx)
	f.hasHuman = ok && principal != nil && principal.SubjectKind() == trust.SubjectKindHuman
	return f.snapshot, f.err
}

type personaBackgroundThreadFake struct {
	snapshot chat.BackgroundThreadSnapshot
	err      error
	request  chat.BackgroundThreadSnapshotRequest
	hasHuman bool
}

func (f *personaBackgroundThreadFake) CaptureBackgroundThreadSnapshot(ctx context.Context, request chat.BackgroundThreadSnapshotRequest) (chat.BackgroundThreadSnapshot, error) {
	f.request = request
	principal, ok := trust.FromContext(ctx)
	f.hasHuman = ok && principal != nil && principal.SubjectKind() == trust.SubjectKindHuman
	return f.snapshot, f.err
}

func TestPersonaBackgroundChatSnapshotReadCurrentUsesStoreIdentityWithoutTrustPrincipal(t *testing.T) {
	reader, expected, audience, thread := personaBackgroundSnapshotFixture(t)
	got, err := reader.ReadCurrent(context.Background(), expected)
	if err != nil {
		t.Fatalf("ReadCurrent: %v", err)
	}
	if got.Thread.Digest != thread.snapshot.Digest || got.Audience.Digest != audience.snapshot.Digest {
		t.Fatalf("snapshot did not preserve canonical store images: %+v", got)
	}
	if audience.tenant != "tenant-a" || audience.room != "room-a" || thread.request.TenantID != "tenant-a" ||
		thread.request.ReaderID != "alice" || thread.request.InvokingPostID != "post-a" || thread.request.Limit != chat.MaxThreadSnapshotPosts ||
		audience.hasHuman || thread.hasHuman {
		t.Fatalf("background store request was not bound to admitted identity: audience=%+v thread=%+v", audience, thread.request)
	}
}

func TestPersonaBackgroundChatSnapshotReadCurrentFailsClosedOnStaleState(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*agentgate.PrivateChatScopeEvidence, *personaBackgroundAudienceFake, *personaBackgroundThreadFake)
	}{
		{name: "membership revoked", mutate: func(_ *agentgate.PrivateChatScopeEvidence, audience *personaBackgroundAudienceFake, _ *personaBackgroundThreadFake) {
			audience.snapshot.Members = nil
		}},
		{name: "membership revision changed", mutate: func(expected *agentgate.PrivateChatScopeEvidence, audience *personaBackgroundAudienceFake, _ *personaBackgroundThreadFake) {
			audience.snapshot.Members[0].Revision++
		}},
		{name: "conversation revision changed", mutate: func(expected *agentgate.PrivateChatScopeEvidence, audience *personaBackgroundAudienceFake, _ *personaBackgroundThreadFake) {
			audience.snapshot.ConversationRevision++
		}},
		{name: "invoking post edited", mutate: func(_ *agentgate.PrivateChatScopeEvidence, _ *personaBackgroundAudienceFake, thread *personaBackgroundThreadFake) {
			thread.snapshot.Posts[1].Body = "edited"
			refreshPersonaBackgroundThread(t, &thread.snapshot)
		}},
		{name: "invoking post deleted", mutate: func(_ *agentgate.PrivateChatScopeEvidence, _ *personaBackgroundAudienceFake, thread *personaBackgroundThreadFake) {
			thread.snapshot.Posts[1].Deleted = true
		}},
		{name: "thread authority revision changed", mutate: func(_ *agentgate.PrivateChatScopeEvidence, _ *personaBackgroundAudienceFake, thread *personaBackgroundThreadFake) {
			thread.snapshot.AuthorityRevision++
		}},
		{name: "audience unavailable", mutate: func(_ *agentgate.PrivateChatScopeEvidence, audience *personaBackgroundAudienceFake, _ *personaBackgroundThreadFake) {
			audience.err = errors.New("offline")
		}},
		{name: "thread unavailable", mutate: func(_ *agentgate.PrivateChatScopeEvidence, _ *personaBackgroundAudienceFake, thread *personaBackgroundThreadFake) {
			thread.err = errors.New("offline")
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reader, expected, audience, thread := personaBackgroundSnapshotFixture(t)
			tc.mutate(&expected, audience, thread)
			if _, err := reader.ReadCurrent(context.Background(), expected); !errors.Is(err, errPersonaBackgroundChatSnapshot) {
				t.Fatalf("ReadCurrent error = %v, want fail-closed sentinel", err)
			}
		})
	}
}

func TestPersonaBackgroundChatSnapshotReadCurrentRejectsIncompleteBinding(t *testing.T) {
	reader, expected, _, _ := personaBackgroundSnapshotFixture(t)
	expected.PostDigest = ""
	if _, err := reader.ReadCurrent(context.Background(), expected); !errors.Is(err, errPersonaBackgroundChatSnapshot) {
		t.Fatalf("incomplete binding error = %v", err)
	}
}

func personaBackgroundSnapshotFixture(t *testing.T) (PersonaBackgroundChatSnapshotReader, agentgate.PrivateChatScopeEvidence, *personaBackgroundAudienceFake, *personaBackgroundThreadFake) {
	t.Helper()
	evidence := agentgate.PrivateChatScopeEvidence{
		Allowed: true, Tenant: values.TenantId("tenant-a"), InvokerID: "alice", ConversationID: "room-a", ThreadID: "root-a", InvokingPostID: "post-a",
		InvokingPostAuthor: "alice", PrivateConversation: true, ActiveMember: true, PostVisible: true,
		ConversationRev: 9, MembershipRev: 4,
	}
	audience := &personaBackgroundAudienceFake{snapshot: chatstore.AudienceSnapshot{
		TenantID: "tenant-a", ConversationID: "room-a", Kind: string(chat.PrivateChannel), ConversationRevision: 9, PolicyRevision: 2,
		Members: []chatstore.AudienceMember{{HomeTenantID: "tenant-a", MemberID: "alice", Revision: 4}},
	}}
	// Use a valid opaque store digest. The real chatstore computes this from
	// tenant-owned rows; the application adapter verifies its binding and shape.
	audience.snapshot.Digest = "sha256:" + strings.Repeat("a", 64)
	audience.snapshot.SnapshotID = "chat-audience-" + audience.snapshot.Digest
	thread := &personaBackgroundThreadFake{snapshot: chat.BackgroundThreadSnapshot{
		TenantID: "tenant-a", ConversationID: "room-a", ThreadID: "root-a", InvokingPostID: "post-a", ReaderTenantID: "tenant-a", ReaderID: "alice",
		Revision: 21, AuthorityRevision: 9, Posts: []chat.Post{{ID: "root-a", TenantID: "tenant-a", ConversationID: "room-a", AuthorID: "bob", AuthorHomeTenantID: "tenant-a", Sequence: 18, Revision: 1, Body: "question follows"},
			{ID: "post-a", TenantID: "tenant-a", ConversationID: "room-a", AuthorID: "alice", AuthorHomeTenantID: "tenant-a", Sequence: 20, Revision: 2, ParentID: "root-a", Body: "question"}},
	}}
	refreshPersonaBackgroundThread(t, &thread.snapshot)
	postDigest, err := privatePersonaPostDigest(thread.snapshot.Posts[1])
	if err != nil {
		t.Fatal(err)
	}
	evidence.PostDigest = postDigest
	return PersonaBackgroundChatSnapshotReader{Audience: audience, Threads: thread}, evidence, audience, thread
}

func refreshPersonaBackgroundThread(t *testing.T, snapshot *chat.BackgroundThreadSnapshot) {
	t.Helper()
	digest, err := chat.BackgroundThreadSnapshotDigest(*snapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Digest = digest
	snapshot.SnapshotID = "chat-background-thread-" + digest
}

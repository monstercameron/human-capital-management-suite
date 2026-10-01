package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type personaAtomicSnapshotFake struct {
	snapshot chat.ThreadSnapshot
	request  chat.ThreadSnapshotRequest
	err      error
}

func (f *personaAtomicSnapshotFake) CaptureThreadSnapshot(_ context.Context, request chat.ThreadSnapshotRequest) (chat.ThreadSnapshot, error) {
	f.request = request
	return f.snapshot, f.err
}

func personaSnapshot(req chat.ThreadSnapshotRequest, posts []chat.Post) chat.ThreadSnapshot {
	snapshot := chat.ThreadSnapshot{
		TenantID: req.TenantID, ConversationID: req.ConversationID, ThreadID: req.ThreadID,
		InvokingPostID: req.InvokingPostID, PrincipalTenantID: req.Principal.TenantID,
		PrincipalID: req.Principal.SubjectID, PrincipalRoles: req.Principal.Roles,
		PrincipalQualifications: req.Principal.Qualifications, Revision: 4, AuthorityRevision: 2,
		Posts: posts,
	}
	digest, _ := chat.ThreadSnapshotDigest(snapshot)
	snapshot.Digest = digest
	snapshot.SnapshotID = "chat-thread-" + digest
	return snapshot
}

func TestTodo_AGENTP_010_AtomicThreadReaderUsesTrustedBoundedSnapshot(t *testing.T) {
	ctx := personaThreadContext(t, "tenant-a", "invoker")
	request := agentinvoke.ThreadReadRequest{TenantID: "tenant-a", ConversationID: "room", ThreadID: "root", InvokingPostID: "root", InvokerID: "invoker", Limit: 500}
	chatRequest := chat.ThreadSnapshotRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "invoker"}, TenantID: "tenant-a", ConversationID: "room", ThreadID: "root", InvokingPostID: "root", Limit: agentinvoke.MaxThreadPosts}
	fake := &personaAtomicSnapshotFake{snapshot: personaSnapshot(chatRequest, []chat.Post{
		{ID: "root", TenantID: "tenant-a", ConversationID: "room", AuthorID: "invoker", Body: "my goal", Revision: 1},
		{ID: "reply", TenantID: "tenant-a", ConversationID: "room", ParentID: "root", AuthorID: "peer", Body: "peer context", Revision: 2},
	})}
	reader := PersonaAtomicThreadReader{Snapshots: fake}
	posts, err := reader.ReadThread(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if fake.request.Limit != agentinvoke.MaxThreadPosts || fake.request.Principal.SubjectID != "invoker" || fake.request.Principal.TenantID != "tenant-a" {
		t.Fatalf("snapshot request escaped trusted bounded reader: %+v", fake.request)
	}
	if len(posts) != 2 || posts[0].ID != "root" || posts[1].ID != "reply" || posts[1].Body != "peer context" || posts[1].ThreadID != "root" {
		t.Fatalf("mapped thread posts = %#v", posts)
	}
}

func TestTodo_AGENTP_010_AtomicThreadReaderRejectsForgedAuthorityAndInvalidSnapshot(t *testing.T) {
	baseRequest := agentinvoke.ThreadReadRequest{TenantID: "tenant-a", ConversationID: "room", ThreadID: "root", InvokingPostID: "root", InvokerID: "invoker"}
	validChatRequest := chat.ThreadSnapshotRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "invoker"}, TenantID: "tenant-a", ConversationID: "room", ThreadID: "root", InvokingPostID: "root", Limit: agentinvoke.MaxThreadPosts}
	valid := personaSnapshot(validChatRequest, []chat.Post{{ID: "root", TenantID: "tenant-a", ConversationID: "room", AuthorID: "invoker", Body: "goal", Revision: 1}})
	for _, tc := range []struct {
		name   string
		ctx    context.Context
		mutate func(*chat.ThreadSnapshot)
		want   error
	}{
		{name: "forged invoker", ctx: personaThreadContext(t, "tenant-a", "someone-else"), want: errPersonaAtomicThreadReader},
		{name: "wrong scope", ctx: personaThreadContext(t, "tenant-a", "invoker"), mutate: func(snapshot *chat.ThreadSnapshot) { snapshot.ConversationID = "other-room" }, want: errPersonaAtomicThreadReader},
		{name: "forged reader roles", ctx: personaThreadContext(t, "tenant-a", "invoker"), mutate: func(snapshot *chat.ThreadSnapshot) {
			snapshot.PrincipalRoles = []string{"tenant-owner"}
			digest, _ := chat.ThreadSnapshotDigest(*snapshot)
			snapshot.Digest, snapshot.SnapshotID = digest, "chat-thread-"+digest
		}, want: errPersonaAtomicThreadReader},
		{name: "forged digest", ctx: personaThreadContext(t, "tenant-a", "invoker"), mutate: func(snapshot *chat.ThreadSnapshot) { snapshot.Digest = "sha256:forged" }, want: errPersonaAtomicThreadReader},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := valid
			if tc.mutate != nil {
				tc.mutate(&snapshot)
			}
			reader := PersonaAtomicThreadReader{Snapshots: &personaAtomicSnapshotFake{snapshot: snapshot}}
			if _, err := reader.ReadThread(tc.ctx, baseRequest); !errors.Is(err, tc.want) {
				t.Fatalf("invalid snapshot error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestTodo_AGENTP_010_AtomicThreadReaderFailsClosedWhenSnapshotUnavailable(t *testing.T) {
	failure := errors.New("snapshot unavailable")
	reader := PersonaAtomicThreadReader{Snapshots: &personaAtomicSnapshotFake{err: failure}}
	_, err := reader.ReadThread(personaThreadContext(t, "tenant-a", "invoker"), agentinvoke.ThreadReadRequest{
		TenantID: "tenant-a", ConversationID: "room", ThreadID: "root", InvokingPostID: "root", InvokerID: "invoker",
	})
	if !errors.Is(err, failure) {
		t.Fatalf("snapshot failure was not preserved: %v", err)
	}
}

package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaThreadChatStub struct {
	chat.ConversationService
	responses []chat.ListPostsResponse
	requests  []chat.ListPostsRequest
	err       error
}

func (s *personaThreadChatStub) ListPosts(_ context.Context, request chat.ListPostsRequest) (chat.ListPostsResponse, error) {
	s.requests = append(s.requests, request)
	if s.err != nil {
		return chat.ListPostsResponse{}, s.err
	}
	if len(s.responses) == 0 {
		return chat.ListPostsResponse{}, nil
	}
	response := s.responses[0]
	s.responses = s.responses[1:]
	return response, nil
}

func personaThreadContext(t *testing.T, tenant, subject string) context.Context {
	t.Helper()
	now := time.Now().UTC()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(tenant), Subject: subject, SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "thread-reader-test", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "credential",
	})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), principal)
}

func TestTodo_AGENTP_010_ReadsAuthorizedLastThreadPostsWithProvenance(t *testing.T) {
	chatStub := &personaThreadChatStub{responses: []chat.ListPostsResponse{{Posts: []chat.Post{
		{ID: "reply-2", TenantID: "tenant-a", ConversationID: "room", ParentID: "root", AuthorID: "peer", Body: "peer text", Revision: 2, SourceAttribution: &chat.SourceAttribution{TenantID: "tenant-a", ConversationID: "source-room", PostID: "source-post", PostRevision: 4, OriginalAuthorID: "source-author"}},
		{ID: "root", TenantID: "tenant-a", ConversationID: "room", AuthorID: "invoker", Body: "goal", Revision: 1},
		{ID: "other-room", TenantID: "tenant-b", ConversationID: "room", ParentID: "root", AuthorID: "other", Body: "must not escape"},
		{ID: "other-thread", TenantID: "tenant-a", ConversationID: "room", ParentID: "different-root", AuthorID: "peer", Body: "not this thread"},
	}}}}
	reader := PersonaThreadReader{Chat: chatStub}
	result, err := reader.ReadThreadWithProvenance(personaThreadContext(t, "tenant-a", "invoker"), agentinvoke.ThreadReadRequest{
		TenantID: "tenant-a", ConversationID: "room", ThreadID: "root", InvokerID: "invoker", InvokingPostID: "root", Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Posts) != 2 || result.Posts[0].ID != "root" || result.Posts[1].ID != "reply-2" {
		t.Fatalf("thread posts = %#v", result.Posts)
	}
	if len(result.Provenance) != 2 || result.Provenance[0].PostID != "root" || result.Provenance[0].Version != 1 || result.Provenance[0].Citation == "" {
		t.Fatalf("provenance = %#v", result.Provenance)
	}
	if result.Provenance[1].Source == nil || result.Provenance[1].Source.PostID != "source-post" || result.Provenance[1].Source.PostRevision != 4 {
		t.Fatalf("source attribution = %#v", result.Provenance[1].Source)
	}
	request := chatStub.requests[0]
	if request.Principal.TenantID != "tenant-a" || request.Principal.SubjectID != "invoker" || request.TenantID != "tenant-a" {
		t.Fatalf("chat request authority = %#v", request)
	}
}

func TestTodo_AGENTP_010_RejectsForgedRequestAuthority(t *testing.T) {
	reader := PersonaThreadReader{Chat: &personaThreadChatStub{}}
	_, err := reader.ReadThread(personaThreadContext(t, "tenant-a", "trusted"), agentinvoke.ThreadReadRequest{
		TenantID: "tenant-a", ConversationID: "room", ThreadID: "root", InvokerID: "forged", InvokingPostID: "root",
	})
	if !errors.Is(err, errPersonaThreadReaderNoAuth) {
		t.Fatalf("forged invoker error = %v", err)
	}
}

func TestTodo_AGENTP_010_PropagatesCurrentMembershipDenial(t *testing.T) {
	reader := PersonaThreadReader{Chat: &personaThreadChatStub{err: chat.ErrPermissionDenied}}
	_, err := reader.ReadThread(personaThreadContext(t, "tenant-a", "invoker"), agentinvoke.ThreadReadRequest{
		TenantID: "tenant-a", ConversationID: "private-room", ThreadID: "root", InvokerID: "invoker",
	})
	if !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("membership denial = %v", err)
	}
}

func TestTodo_AGENTP_010_ReadThreadUsesBoundedLimit(t *testing.T) {
	posts := make([]chat.Post, agentinvoke.MaxThreadPosts+1)
	for i := range posts {
		posts[i] = chat.Post{ID: "post", TenantID: "tenant-a", ConversationID: "room", ParentID: "root", AuthorID: "peer"}
	}
	reader := PersonaThreadReader{Chat: &personaThreadChatStub{responses: []chat.ListPostsResponse{{Posts: posts}}}}
	got, err := reader.ReadThread(personaThreadContext(t, "tenant-a", "invoker"), agentinvoke.ThreadReadRequest{TenantID: "tenant-a", ConversationID: "room", ThreadID: "root", InvokerID: "invoker", Limit: agentinvoke.MaxThreadPosts + 10})
	if err != nil || len(got) != agentinvoke.MaxThreadPosts {
		t.Fatalf("bounded posts=%d err=%v", len(got), err)
	}
}

package chat

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// listRecorder records the core request one listing RPC produced and answers
// with a row that carries the discovery flag back out.
type listRecorder struct {
	*transportChatFake
	conversations chatcore.ListConversationsRequest
	posts         chatcore.ListPostsRequest
}

func (r *listRecorder) ListConversations(_ context.Context, req chatcore.ListConversationsRequest) (chatcore.ListConversationsResponse, error) {
	r.conversations = req
	return chatcore.ListConversationsResponse{Conversations: []chatcore.Conversation{
		{ID: "mine", TenantID: "server", Kind: chatcore.PrivateChannel, Joined: true},
		{ID: "open", TenantID: "server", Kind: chatcore.PublicChannel},
	}}, nil
}

func (r *listRecorder) ListPosts(_ context.Context, req chatcore.ListPostsRequest) (chatcore.ListPostsResponse, error) {
	r.posts = req
	return chatcore.ListPostsResponse{}, nil
}

// TestTodo_CHAT_018_DiscoveryAndBackwardPagingAreWired proves the two new request
// fields reach the core unchanged and that a listed row tells the client whether
// it is already a member, which is what turns a channel list into a directory
// with a join affordance.
func TestTodo_CHAT_018_DiscoveryAndBackwardPagingAreWired(t *testing.T) {
	rec := &listRecorder{transportChatFake: &transportChatFake{}}
	s := &server{deps: Dependencies{Service: rec}}
	ctx := admittedChatContext(t)

	out, err := s.ListConversations(ctx, &chatv1.ListConversationsRequest{TenantId: "server", IncludeDiscoverable: true, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	if !rec.conversations.IncludeDiscoverable {
		t.Fatalf("include_discoverable did not reach the core: %+v", rec.conversations)
	}
	if len(out.GetConversations()) != 2 || !out.GetConversations()[0].GetJoined() || out.GetConversations()[1].GetJoined() {
		t.Fatalf("joined did not project: %+v", out.GetConversations())
	}

	if _, err := s.ListPosts(ctx, &chatv1.ListPostsRequest{TenantId: "server", ConversationId: "c", Descending: true, PageSize: 200}); err != nil {
		t.Fatal(err)
	}
	if !rec.posts.Descending || rec.posts.BeforeSequence != 0 {
		t.Fatalf("newest-page request = %+v", rec.posts)
	}
	if _, err := s.ListPosts(ctx, &chatv1.ListPostsRequest{TenantId: "server", ConversationId: "c", Descending: true, BeforeSequence: 801}); err != nil {
		t.Fatal(err)
	}
	if !rec.posts.Descending || rec.posts.BeforeSequence != 801 {
		t.Fatalf("older-page request = %+v", rec.posts)
	}
}

// summaryService answers both conversation reads with a row that carries the
// derived summary the store computes, so the test can prove the transport
// projects it instead of dropping it on the floor.
type summaryService struct {
	*transportChatFake
	at time.Time
}

func (s summaryService) ListConversations(context.Context, chatcore.ListConversationsRequest) (chatcore.ListConversationsResponse, error) {
	return chatcore.ListConversationsResponse{Conversations: []chatcore.Conversation{
		{ID: "busy", TenantID: "server", Kind: chatcore.PublicChannel, MemberCount: 12, LastActivityAt: &s.at},
		{ID: "quiet", TenantID: "server", Kind: chatcore.PublicChannel, MemberCount: 1},
	}}, nil
}

func (s summaryService) GetConversation(context.Context, chatcore.GetConversationRequest) (chatcore.Conversation, error) {
	return chatcore.Conversation{ID: "busy", TenantID: "server", Kind: chatcore.PublicChannel, MemberCount: 12, LastActivityAt: &s.at}, nil
}

// TestConversationSummaryProjectsToTheWire proves member_count and
// last_activity_at survive the domain-to-proto conversion on every listing that
// returns a conversation, that a room with no posts sends no timestamp rather
// than the zero time, and that neither field is caller-settable on the way in.
func TestConversationSummaryProjectsToTheWire(t *testing.T) {
	at := time.Date(2026, 3, 1, 13, 0, 0, 0, time.UTC)
	s := &server{deps: Dependencies{Service: summaryService{transportChatFake: &transportChatFake{}, at: at}}}
	ctx := admittedChatContext(t)

	list, err := s.ListConversations(ctx, &chatv1.ListConversationsRequest{TenantId: "server", IncludeDiscoverable: true, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	rows := list.GetConversations()
	if len(rows) != 2 {
		t.Fatalf("listing = %+v", rows)
	}
	if rows[0].GetMemberCount() != 12 {
		t.Fatalf("member_count = %d, want 12", rows[0].GetMemberCount())
	}
	if !rows[0].GetLastActivityAt().AsTime().Equal(at) {
		t.Fatalf("last_activity_at = %v, want %s", rows[0].GetLastActivityAt(), at)
	}
	if rows[1].GetLastActivityAt() != nil {
		t.Fatalf("a channel with no posts sent last_activity_at = %v, want unset", rows[1].GetLastActivityAt())
	}
	if rows[1].GetMemberCount() != 1 {
		t.Fatalf("unjoined channel member_count = %d, want 1", rows[1].GetMemberCount())
	}

	got, err := s.GetConversation(ctx, &chatv1.GetConversationRequest{TenantId: "server", ConversationId: "busy"})
	if err != nil {
		t.Fatal(err)
	}
	if got.GetConversation().GetMemberCount() != 12 || !got.GetConversation().GetLastActivityAt().AsTime().Equal(at) {
		t.Fatalf("GetConversation = %+v", got.GetConversation())
	}

	// The summary is derived, not supplied: a request message that carries it
	// must not seed the domain value the core acts on.
	in := conversationIn(&chatv1.Conversation{Id: "busy", TenantId: "server", MemberCount: 9999, LastActivityAt: timestamppb.New(at)})
	if in.MemberCount != 0 || in.LastActivityAt != nil {
		t.Fatalf("caller-supplied summary reached the core: %+v", in)
	}
}

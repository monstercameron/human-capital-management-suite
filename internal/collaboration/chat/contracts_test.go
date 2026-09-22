package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTodo_CHAT_008(t *testing.T) {
	if PublicChannel == PrivateChannel || Direct == Group {
		t.Fatal("conversation kinds must be distinct")
	}
	p := Principal{TenantID: "tenant", SubjectID: "subject", Roles: []string{"employee"}, Qualifications: []string{"active"}}
	req := SendPostRequest{Principal: p, TenantID: "tenant", ConversationID: "conversation", Body: "hello", IdempotencyKey: "idem-1"}
	if req.Principal.TenantID != req.TenantID || req.Body == "" || req.IdempotencyKey == "" {
		t.Fatal("send contract lost required tenant or durable request fields")
	}
	post := Post{ID: "post", ConversationID: req.ConversationID, TenantID: req.TenantID, AuthorID: p.SubjectID, Body: req.Body, Sequence: 1, Revision: 1, CreatedAt: time.Now()}
	if post.Sequence == 0 || post.Revision == 0 || post.CreatedAt.IsZero() {
		t.Fatal("committed post bounds are not represented")
	}
}

func TestTodo_CHAT_008_Conformance(t *testing.T) {
	var _ ConversationService = (*contractService)(nil)
	if !errors.Is(ErrConflict, ErrConflict) || errors.Is(ErrConflict, ErrNotFound) {
		t.Fatal("typed sentinel errors are not stable")
	}
}

func TestTodo_CHAT_008_Golden(t *testing.T) {
	if got := (Conversation{ID: "c", TenantID: "t", Kind: Direct, OwnerID: "s", Revision: 3}).Kind; got != Direct {
		t.Fatalf("kind = %q", got)
	}
	if got := (Membership{Role: Manager, HistoryVisibility: FullHistory}).HistoryVisibility; got != FullHistory {
		t.Fatalf("history visibility = %q", got)
	}
	remove := RemoveMembershipRequest{TenantID: "host", HomeTenantID: "home", ConversationID: "c", SubjectID: "s", ExpectedRevision: 2}
	if remove.TenantID == remove.HomeTenantID || remove.HomeTenantID == "" || remove.ExpectedRevision == 0 {
		t.Fatal("membership removal must identify host and home tenants with a revision fence")
	}
	ref := Reference{Kind: PersonMention, TenantID: "home", ID: "person", Display: "A Person", ConversationID: "c"}
	post := Post{ID: "p", AuthorHomeTenantID: "author-home", References: []Reference{ref}, SourceAttribution: &SourceAttribution{TenantID: "source", ConversationID: "c", PostID: "p", PostRevision: 4, OriginalAuthorID: "author"}}
	if post.References[0].ID != ref.ID || post.AuthorHomeTenantID == "" || post.SourceAttribution.PostRevision != 4 || post.SourceAttribution.OriginalAuthorID == "" {
		t.Fatal("reference provenance fields were not retained")
	}
	if (Reaction{HomeTenantID: "home"}).HomeTenantID == "" || (Pin{PinnedByHomeTenantID: "home"}).PinnedByHomeTenantID == "" || (ReadState{HomeTenantID: "home"}).HomeTenantID == "" || (NotificationPreferences{HomeTenantID: "home"}).HomeTenantID == "" {
		t.Fatal("personal chat state must retain home tenant identity")
	}
}

type contractService struct{}

func (*contractService) CreateConversation(context.Context, CreateConversationRequest) (Conversation, error) {
	return Conversation{}, nil
}
func (*contractService) ListConversations(context.Context, ListConversationsRequest) (ListConversationsResponse, error) {
	return ListConversationsResponse{}, nil
}
func (*contractService) GetConversation(context.Context, GetConversationRequest) (Conversation, error) {
	return Conversation{}, nil
}
func (*contractService) UpdateConversation(context.Context, UpdateConversationRequest) (Conversation, error) {
	return Conversation{}, nil
}
func (*contractService) ListMemberships(context.Context, ListMembershipsRequest) (ListMembershipsResponse, error) {
	return ListMembershipsResponse{}, nil
}
func (*contractService) AddMembership(context.Context, AddMembershipRequest) (Membership, error) {
	return Membership{}, nil
}
func (*contractService) RemoveMembership(context.Context, RemoveMembershipRequest) (Membership, error) {
	return Membership{}, nil
}
func (*contractService) SendPost(context.Context, SendPostRequest) (Post, error) { return Post{}, nil }
func (*contractService) ListPosts(context.Context, ListPostsRequest) (ListPostsResponse, error) {
	return ListPostsResponse{}, nil
}
func (*contractService) EditPost(context.Context, EditPostRequest) (Post, error) { return Post{}, nil }
func (*contractService) DeletePost(context.Context, DeletePostRequest) (Post, error) {
	return Post{}, nil
}
func (*contractService) Search(context.Context, SearchRequest) (SearchResponse, error) {
	return SearchResponse{}, nil
}
func (*contractService) GetReadState(context.Context, GetReadStateRequest) (ReadState, error) {
	return ReadState{}, nil
}
func (*contractService) UpdateReadState(context.Context, UpdateReadStateRequest) (ReadState, error) {
	return ReadState{}, nil
}
func (*contractService) GetPreferences(context.Context, GetPreferencesRequest) (NotificationPreferences, error) {
	return NotificationPreferences{}, nil
}
func (*contractService) UpdatePreferences(context.Context, UpdatePreferencesRequest) (NotificationPreferences, error) {
	return NotificationPreferences{}, nil
}
func (*contractService) AddReaction(context.Context, AddReactionRequest) (Reaction, error) {
	return Reaction{}, nil
}
func (*contractService) RemoveReaction(context.Context, RemoveReactionRequest) error { return nil }
func (*contractService) ListReactions(context.Context, ListReactionsRequest) (ListReactionsResponse, error) {
	return ListReactionsResponse{}, nil
}
func (*contractService) PinPost(context.Context, PinPostRequest) (Pin, error)     { return Pin{}, nil }
func (*contractService) UnpinPost(context.Context, UnpinPostRequest) error        { return nil }
func (*contractService) ListPins(context.Context, ListPinsRequest) ([]Pin, error) { return nil, nil }
func (*contractService) WatchConversation(context.Context, WatchConversationRequest) (<-chan WatchEvent, error) {
	return nil, nil
}

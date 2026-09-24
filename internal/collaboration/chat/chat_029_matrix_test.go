package chat

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

type referenceProjectionStore struct {
	fakeStore
	conversations map[string]Conversation
	posts         []Post
}

func (s *referenceProjectionStore) GetConversation(_ context.Context, tenant, id string) (Conversation, error) {
	c, ok := s.conversations[tenant+"\x00"+id]
	if !ok {
		return Conversation{}, ErrNotFound
	}
	return c, nil
}

func (s *referenceProjectionStore) ListPosts(context.Context, Principal, string, string, uint64, Page, PostWindow) (ListPostsResponse, error) {
	return ListPostsResponse{Posts: append([]Post(nil), s.posts...)}, nil
}

func (s *referenceProjectionStore) Watch(context.Context, WatchConversationRequest) (<-chan WatchEvent, error) {
	events := make(chan WatchEvent, len(s.posts))
	for i := range s.posts {
		post := s.posts[i]
		events <- WatchEvent{Event: ConversationEvent{Kind: PostCreated, Post: &post}}
	}
	close(events)
	return events, nil
}

type referenceProjectionAuthority struct{}

func (referenceProjectionAuthority) Authorize(ctx context.Context, p Principal, c Conversation, action chatpolicy.Action, at time.Time) (chatpolicy.Input, error) {
	if c.ID == "secret" || p.SubjectID == "other" {
		return chatpolicy.Input{}, chatpolicy.ErrNotAuthorized
	}
	return referenceAuthority{}.Authorize(ctx, p, c, action, at)
}

func projectionService() *Service {
	source := conversation()
	target := Conversation{ID: "secret", TenantID: "t1", Name: "Layoff Planning", Kind: PrivateChannel, Revision: 1}
	visible := Conversation{ID: "team", TenantID: "t1", Name: "Team", Kind: PublicChannel, Revision: 1}
	store := &referenceProjectionStore{
		conversations: map[string]Conversation{
			"t1\x00" + source.ID:  source,
			"t1\x00" + target.ID:  target,
			"t1\x00" + visible.ID: visible,
		},
		posts: []Post{{ID: "p1", TenantID: "t1", ConversationID: source.ID, References: []Reference{
			{Kind: ConversationMention, TenantID: "t1", ID: target.ID, ConversationID: target.ID, Display: target.Name},
			{Kind: ConversationMention, TenantID: "t1", ID: visible.ID, ConversationID: visible.ID, Display: visible.Name},
		}}},
	}
	s := NewService(store, func() time.Time { return time.Unix(20, 0).UTC() })
	s.SetAuthority(referenceProjectionAuthority{})
	return s
}

func TestTodo_CHAT_029(t *testing.T) {
	s := projectionService()
	got, err := s.ListPosts(context.Background(), ListPostsRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Posts) != 1 || len(got.Posts[0].References) != 2 {
		t.Fatalf("posts = %+v, want one post with two references", got.Posts)
	}
	ref := got.Posts[0].References[0]
	if ref.Display != restrictedConversationReference || ref.ID != "" || ref.TenantID != "" || ref.ConversationID != "" {
		t.Fatalf("restricted reference = %+v, want inert placeholder without target data", ref)
	}
	if got := got.Posts[0].References[1]; got.ID != "team" || got.TenantID != "t1" || got.ConversationID != "team" || got.Display != "Team" {
		t.Fatalf("visible reference = %+v, want original authorized target", got)
	}
	events, err := s.WatchConversation(context.Background(), WatchConversationRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	for event := range events {
		if event.Event.Post == nil || event.Event.Post.References[0] != (Reference{Kind: ConversationMention, Display: restrictedConversationReference}) {
			t.Fatalf("live reference event = %+v, want inert projection", event.Event.Post)
		}
	}
}

func TestTodo_CHAT_029_Security(t *testing.T) {
	t.Run("unknown target is indistinguishable from a restricted target", func(t *testing.T) {
		s := projectionService()
		s.store.(*referenceProjectionStore).posts[0].References[0].ConversationID = "missing"
		s.store.(*referenceProjectionStore).posts[0].References[0].ID = "missing"
		got, err := s.ListPosts(context.Background(), ListPostsRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1"})
		if err != nil {
			t.Fatal(err)
		}
		ref := got.Posts[0].References[0]
		if ref != (Reference{Kind: ConversationMention, Display: restrictedConversationReference}) {
			t.Fatalf("unknown reference projection = %+v, want same inert placeholder", ref)
		}
	})

	t.Run("inaccessible reader receives no target identity", func(t *testing.T) {
		s := projectionService()
		p := Principal{TenantID: "t1", SubjectID: "other"}
		got, err := s.ListPosts(context.Background(), ListPostsRequest{Principal: p, TenantID: "t1", ConversationID: "c1"})
		if err == nil {
			t.Fatal("reader without access to source conversation received posts")
		}
		if len(got.Posts) != 0 {
			t.Fatalf("unauthorized posts = %+v", got.Posts)
		}
	})
}

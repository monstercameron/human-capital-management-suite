package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

// maskStore answers reads with fixed posts, so the service's reader projection
// is what the test looks at.
type maskStore struct {
	chatfilterTestStore
	posts []Post
	pins  []Pin
	sent  Post
}

func (s *maskStore) ListPosts(context.Context, Principal, string, string, uint64, Page, PostWindow) (ListPostsResponse, error) {
	return ListPostsResponse{Posts: append([]Post(nil), s.posts...)}, nil
}
func (s *maskStore) ListPins(context.Context, string, string) ([]Pin, error) { return s.pins, nil }
func (s *maskStore) GetPost(_ context.Context, _, _, id string) (Post, error) {
	for _, p := range s.posts {
		if p.ID == id {
			return p, nil
		}
	}
	return Post{}, ErrNotFound
}
func (s *maskStore) SendPost(_ context.Context, _ SendPostRequest, p Post) (Post, error) {
	s.sent = p
	return p, nil
}
func (s *maskStore) EditPost(_ context.Context, r EditPostRequest) (Post, error) {
	return Post{ID: r.PostID, TenantID: r.TenantID, ConversationID: r.ConversationID, AuthorID: "writer", AuthorHomeTenantID: "t", Body: r.Body, Revision: r.ExpectedRevision + 1}, nil
}

func maskFixture(t *testing.T, action string) (*Service, *maskStore, *chatfilterFixture) {
	t.Helper()
	now := time.Now().UTC()
	base := &fakeStore{conversation: Conversation{ID: "c", TenantID: "t", Kind: PublicChannel, Name: "General", Revision: 1}, membership: Membership{SubjectID: "reader", Role: Member, JoinedAt: &now, Revision: 1}}
	store := &maskStore{chatfilterTestStore: chatfilterTestStore{fakeStore: base}}
	service := NewService(chatfilterTestStore{fakeStore: base}, func() time.Time { return now })
	service.store = store
	service.SetAuthority(verifiedAuthority{store: base})
	repo := &chatfilterFixture{action: action}
	service.SetContentPolicy(&FilterContentPolicy{Filters: &chatfilter.Service{Store: repo, Registry: chatfilter.NewRegistry()}})
	return service, store, repo
}

func maskPost(id, author, body string) Post {
	return Post{ID: id, TenantID: "t", ConversationID: "c", AuthorID: author, AuthorHomeTenantID: "t", Body: body, Revision: 1}
}

// TestTodo_CHATMOD_002_ReaderMask: every place a person reads a message shows the
// masked text, to readers and to the author alike, and the stored post is never
// altered.
func TestTodo_CHATMOD_002_ReaderMask(t *testing.T) {
	ctx := t.Context()
	service, store, _ := maskFixture(t, "mask")
	store.posts = []Post{maskPost("p1", "writer", "hello quartz friends"), maskPost("p2", "writer", "nothing to see"), {ID: "p3", TenantID: "t", ConversationID: "c", AuthorID: "writer", AuthorHomeTenantID: "t", Body: "quartz", Deleted: true}}
	reader := Principal{TenantID: "t", SubjectID: "reader"}
	author := Principal{TenantID: "t", SubjectID: "writer"}
	const masked = "hello [removed word] friends"

	for name, who := range map[string]Principal{"reader": reader, "author": author} {
		page, err := service.ListPosts(ctx, ListPostsRequest{Principal: who, TenantID: "t", ConversationID: "c", Page: Page{PageSize: 50}})
		if err != nil || len(page.Posts) != 3 {
			t.Fatalf("%s list: %v %d", name, err, len(page.Posts))
		}
		if page.Posts[0].Body != masked || page.Posts[1].Body != "nothing to see" || page.Posts[2].Body != "quartz" {
			t.Fatalf("%s list bodies: %q %q %q", name, page.Posts[0].Body, page.Posts[1].Body, page.Posts[2].Body)
		}
	}
	if store.posts[0].Body != "hello quartz friends" {
		t.Fatal("the stored post was altered")
	}

	// Pins.
	store.pins = []Pin{{TenantID: "t", ConversationID: "c", PostID: "p1"}}
	pins, err := service.ListPins(ctx, ListPinsRequest{Principal: reader, TenantID: "t", ConversationID: "c"})
	if err != nil || len(pins) != 1 || pins[0].Post == nil || pins[0].Post.Body != masked {
		t.Fatalf("pins: %v %+v", err, pins)
	}

	// Live events, post and pin.
	post := maskPost("p1", "writer", "hello quartz friends")
	event := service.MaskWatchEvent(ctx, reader, "t", "c", WatchEvent{Event: ConversationEvent{Post: &post, Pin: &Pin{Post: &post}}})
	if event.Event.Post.Body != masked || event.Event.Pin.Post.Body != masked || post.Body != "hello quartz friends" {
		t.Fatalf("watch event: %q %q original %q", event.Event.Post.Body, event.Event.Pin.Post.Body, post.Body)
	}
	projected := service.ProjectWatchEvent(ctx, reader, "t", "c", WatchEvent{Event: ConversationEvent{Post: &post}})
	if projected.Event.Post.Body != masked {
		t.Fatalf("projected watch event: %q", projected.Event.Post.Body)
	}

	// The author's own responses show what readers see, and what was stored is the original.
	sent, err := service.SendPost(ctx, SendPostRequest{Principal: author, TenantID: "t", ConversationID: "c", Body: "hello quartz friends", IdempotencyKey: "k"})
	if err != nil || sent.Body != masked || store.sent.Body != "hello quartz friends" {
		t.Fatalf("send response %q stored %q %v", sent.Body, store.sent.Body, err)
	}
}

func TestTodo_CHATMOD_002_ReaderMask_BlockAndFlagDoNotAlterReads(t *testing.T) {
	for _, action := range []string{"block", "flag"} {
		service, store, _ := maskFixture(t, action)
		store.posts = []Post{maskPost("p1", "writer", "hello quartz friends")}
		page, err := service.ListPosts(t.Context(), ListPostsRequest{Principal: Principal{TenantID: "t", SubjectID: "reader"}, TenantID: "t", ConversationID: "c", Page: Page{PageSize: 50}})
		if err != nil || page.Posts[0].Body != "hello quartz friends" {
			t.Fatalf("%s read altered the text: %q %v", action, page.Posts[0].Body, err)
		}
	}
}

func TestTodo_CHATMOD_002_ReaderMask_FailsClosed(t *testing.T) {
	service, store, _ := maskFixture(t, "mask")
	policy := service.contentPolicy.(*FilterContentPolicy)
	policy.Filters = &chatfilter.Service{} // no store, no registry: the filters cannot be asked
	store.posts = []Post{maskPost("p1", "writer", "hello quartz friends")}
	page, err := service.ListPosts(t.Context(), ListPostsRequest{Principal: Principal{TenantID: "t", SubjectID: "reader"}, TenantID: "t", ConversationID: "c", Page: Page{PageSize: 50}})
	if err != nil || strings.Contains(page.Posts[0].Body, "quartz") || page.Posts[0].Body != "[removed word]" {
		t.Fatalf("a failed filter lookup must never expose the text: %q %v", page.Posts[0].Body, err)
	}
}

func TestTodo_CHATMOD_002_AgentPost(t *testing.T) {
	service, _, repo := maskFixture(t, "block")
	policy := service.contentPolicy.(*FilterContentPolicy)
	err := policy.CheckAgentPost(t.Context(), "t", "c", "assistant", "an answer with quartz in it")
	var blocked *chatfilter.BlockedError
	if !errors.As(err, &blocked) || blocked.Span.Start != 15 || blocked.Span.End != 21 {
		t.Fatalf("agent output must pass the same filters as a person's: %v", err)
	}
	if len(repo.records) == 0 {
		t.Fatal("the blocked agent output left no hit record")
	}
	if err = policy.CheckAgentPost(t.Context(), "t", "c", "assistant", "a clean answer"); err != nil {
		t.Fatal(err)
	}
	if err = policy.CheckAgentPost(t.Context(), "t", "c", "assistant", "   "); err != nil {
		t.Fatal("blank text is not a filter decision")
	}
}

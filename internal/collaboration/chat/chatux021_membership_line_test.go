package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// newcomerStore answers "not a member" for the person being added and the
// fixture's own answer for everyone else, so the add looks like a first join.
type newcomerStore struct {
	*fakeStore
	sends   []SendPostRequest
	sendErr error
}

func (s *newcomerStore) GetMembership(ctx context.Context, tenant, conversation, home, subject string) (Membership, error) {
	if subject == "u2" {
		return Membership{}, ErrNotFound
	}
	return s.fakeStore.GetMembership(ctx, tenant, conversation, home, subject)
}

func (s *newcomerStore) SendPost(ctx context.Context, r SendPostRequest, p Post) (Post, error) {
	s.sends = append(s.sends, r)
	if s.sendErr != nil {
		return Post{}, s.sendErr
	}
	return s.fakeStore.SendPost(ctx, r, p)
}

func chatux021Service(f *newcomerStore) *Service {
	s := NewService(f, func() time.Time { return time.Unix(10, 0).UTC() })
	s.SetAuthority(verifiedAuthority{store: f.fakeStore})
	s.SetMembershipAnnouncements(true)
	return s
}

func chatux021Add(subject string, role MembershipRole) AddMembershipRequest {
	return AddMembershipRequest{Principal: principal(), Membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: subject, Role: role}}
}

func TestTodo_CHATUX_021_AddingAPersonPostsASystemLine(t *testing.T) {
	base := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Role: Manager, JoinedAt: timePtr(time.Unix(1, 0))}}
	f := &newcomerStore{fakeStore: base}
	got, err := chatux021Service(f).AddMembership(context.Background(), chatux021Add("u2", Member))
	if err != nil || got.SubjectID != "u2" {
		t.Fatalf("add = %+v, %v", got, err)
	}
	if len(f.sends) != 1 {
		t.Fatalf("%d system lines posted, want 1", len(f.sends))
	}
	line := f.sends[0]
	subject, ok := ParseMembershipAdded(line.Body)
	if !ok || subject != "u2" || line.ConversationID != "c1" || line.Principal.SubjectID != "u1" || line.IdempotencyKey == "" {
		t.Fatalf("system line = %+v (subject %q, ok %v)", line, subject, ok)
	}
	if base.sent.AuthorID != "u1" || base.sent.Body != line.Body {
		t.Fatalf("stored post = %+v", base.sent)
	}
}

// Nothing is posted for a person who joined themselves, for an add that only
// changed the role of somebody already in, or when the add itself fails, and a
// line that cannot be stored never fails the add.
func TestTodo_CHATUX_021_SystemLineIsOnlyForAPersonSomeoneElseAdded(t *testing.T) {
	base := &fakeStore{conversation: Conversation{ID: "c1", TenantID: "t1", Kind: PublicChannel, OwnerID: "u1", Revision: 1}}
	f := &newcomerStore{fakeStore: base}
	selfJoin := AddMembershipRequest{Principal: Principal{TenantID: "t1", SubjectID: "u2"}, Membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u2"}}
	if _, err := chatux021Service(f).AddMembership(context.Background(), selfJoin); err != nil || len(f.sends) != 0 {
		t.Fatalf("a self-join posted %d lines (%v)", len(f.sends), err)
	}

	active := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Role: Manager, JoinedAt: timePtr(time.Unix(1, 0))}}
	g := &newcomerStore{fakeStore: active}
	if _, err := chatux021Service(g).AddMembership(context.Background(), chatux021Add("u1", Manager)); err != nil || len(g.sends) != 0 {
		t.Fatalf("a role change for an active member posted %d lines (%v)", len(g.sends), err)
	}

	h := &newcomerStore{fakeStore: active, sendErr: errors.New("store down")}
	if got, err := chatux021Service(h).AddMembership(context.Background(), chatux021Add("u2", Member)); err != nil || got.SubjectID != "u2" {
		t.Fatalf("a failing system line failed the add: %+v, %v", got, err)
	}
}

func TestTodo_CHATUX_021_MembershipLineBodyRoundTrips(t *testing.T) {
	for _, id := range []string{"u2", " u3 "} {
		got, ok := ParseMembershipAdded(MembershipAddedBody(id))
		if !ok || got != strings.TrimSpace(id) {
			t.Errorf("round trip of %q = %q, %v", id, got, ok)
		}
	}
	for _, body := range []string{"", "hello", MembershipAddedMarker, MembershipAddedMarker + "  "} {
		if _, ok := ParseMembershipAdded(body); ok {
			t.Errorf("%q parsed as a system line", body)
		}
	}
}

// A service that was not asked to announce adds posts nothing, and neither does
// an add to a direct message, however the service was built.
func TestTodo_CHATUX_021_AnnouncementIsOptInAndNotForDirectMessages(t *testing.T) {
	base := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Role: Manager, JoinedAt: timePtr(time.Unix(1, 0))}}
	f := &newcomerStore{fakeStore: base}
	plain := NewService(f, func() time.Time { return time.Unix(10, 0).UTC() })
	plain.SetAuthority(verifiedAuthority{store: base})
	if _, err := plain.AddMembership(context.Background(), chatux021Add("u2", Member)); err != nil || len(f.sends) != 0 {
		t.Fatalf("a service built without announcements posted %d lines (%v)", len(f.sends), err)
	}
	direct := &fakeStore{conversation: Conversation{ID: "c1", TenantID: "t1", Kind: Direct, OwnerID: "u1", Revision: 1}, membership: base.membership}
	g := &newcomerStore{fakeStore: direct}
	if _, err := chatux021Service(g).AddMembership(context.Background(), chatux021Add("u2", Member)); err != nil || len(g.sends) != 0 {
		t.Fatalf("an add to a direct message posted %d lines (%v)", len(g.sends), err)
	}
}

// The system lines are in a person's own timeline and out of everything that
// reads the conversation for context (an agent, a translation, a rewrite), and
// out of search.
type linesStore struct {
	*fakeStore
	posts   []Post
	results []SearchResult
}

func (s *linesStore) ListPosts(context.Context, Principal, string, string, uint64, Page, PostWindow) (ListPostsResponse, error) {
	return ListPostsResponse{Posts: append([]Post(nil), s.posts...)}, nil
}

func (s *linesStore) Search(_ context.Context, r SearchRequest) (SearchResponse, error) {
	if r.SkipMessages {
		return SearchResponse{}, nil
	}
	return SearchResponse{Results: append([]SearchResult(nil), s.results...)}, nil
}

func TestTodo_CHATUX_021_SystemLinesAreLeftOutUnlessAsked(t *testing.T) {
	line := Post{ID: "line", ConversationID: "c1", TenantID: "t1", AuthorID: "u1", Body: MembershipAddedBody("u2"), Sequence: 1}
	hello := Post{ID: "hello", ConversationID: "c1", TenantID: "t1", AuthorID: "u1", Body: "hello", Sequence: 2}
	base := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Role: Manager, JoinedAt: timePtr(time.Unix(1, 0))}}
	f := &linesStore{fakeStore: base, posts: []Post{line, hello}, results: []SearchResult{{Post: line, ConversationName: "room"}, {Post: hello, ConversationName: "room"}}}
	s := NewService(f, func() time.Time { return time.Unix(10, 0).UTC() })
	s.SetAuthority(verifiedAuthority{store: base})
	read := func(include bool) []string {
		got, err := s.ListPosts(context.Background(), ListPostsRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Page: Page{PageSize: 10}, IncludeSystem: include})
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, p := range got.Posts {
			ids = append(ids, p.ID)
		}
		return ids
	}
	if got := read(false); len(got) != 1 || got[0] != "hello" {
		t.Errorf("a reader that did not ask for system lines sees %v", got)
	}
	if got := read(true); len(got) != 2 || got[0] != "line" {
		t.Errorf("a person's timeline sees %v", got)
	}
	found, err := s.Search(context.Background(), SearchRequest{Principal: principal(), TenantID: "t1", Query: "hello", Page: Page{PageSize: 10}})
	if err != nil || len(found.Results) != 1 || found.Results[0].Post.ID != "hello" {
		t.Errorf("search finds %v (%v)", found.Results, err)
	}
	if kept := withoutSystemPosts([]Post{hello}); len(kept) != 1 {
		t.Error("a page with no system line lost a post")
	}
}

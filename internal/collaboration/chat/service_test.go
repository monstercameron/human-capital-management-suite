package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

type fakeStore struct {
	conversation Conversation
	membership   Membership
	post         Post
	pins         []Pin
	mutations    int
	created      []Membership
	// conversations is what a listing answers with; scope and window record what
	// the service asked the store for, and put records the membership row it was
	// handed, so a coerced self-join role is provable rather than assumed.
	conversations []Conversation
	scope         ConversationScope
	window        PostWindow
	put           Membership
	sent          Post
	search        SearchResponse
	searchCalls   int
	messagePages  map[string]SearchResponse
	channelPages  map[string]SearchResponse
}

type verifiedAuthority struct{ store *fakeStore }
type denyConversationAuthority struct {
	base verifiedAuthority
	deny string
}

func (a denyConversationAuthority) Authorize(ctx context.Context, p Principal, c Conversation, action chatpolicy.Action, now time.Time) (chatpolicy.Input, error) {
	if c.ID == a.deny {
		return chatpolicy.Input{}, ErrPermissionDenied
	}
	return a.base.Authorize(ctx, p, c, action, now)
}

func (a verifiedAuthority) Authorize(_ context.Context, p Principal, c Conversation, _ chatpolicy.Action, now time.Time) (chatpolicy.Input, error) {
	if c.Kind != PublicChannel && (a.store.membership.SubjectID != p.SubjectID || a.store.membership.LeftAt != nil) {
		return chatpolicy.Input{}, ErrPermissionDenied
	}
	return chatpolicy.Input{
		Principal:     chatpolicy.Principal{ID: p.SubjectID, Tenant: p.TenantID, Active: true, AuthorityRevision: 1},
		Channel:       chatpolicy.Channel{ID: c.ID, HostTenant: c.TenantID, Enabled: !c.Archived, Private: c.Kind != PublicChannel, Revision: c.Revision},
		Membership:    chatpolicy.Membership{ConversationID: c.ID, PrincipalID: p.SubjectID, Tenant: p.TenantID, State: chatpolicy.MembershipCurrent, Revision: 1},
		HasMembership: true,
		Now:           now,
	}, nil
}

func newTestService(f *fakeStore, clock Clock) *Service {
	s := NewService(f, clock)
	s.SetAuthority(verifiedAuthority{store: f})
	return s
}

func (f *fakeStore) CreateConversation(_ context.Context, c Conversation, ms []Membership, _ string) (Conversation, error) {
	f.mutations++
	f.conversation = c
	f.created = ms
	return c, nil
}
func (f *fakeStore) ListConversations(_ context.Context, _ Principal, _ string, _ Page, scope ConversationScope) (ListConversationsResponse, error) {
	f.scope = scope
	return ListConversationsResponse{Conversations: append([]Conversation(nil), f.conversations...)}, nil
}
func (f *fakeStore) GetConversation(_ context.Context, _, id string) (Conversation, error) {
	if f.conversation.ID == "" || (id != "" && f.conversation.ID != id) {
		return Conversation{}, ErrNotFound
	}
	return f.conversation, nil
}
func (f *fakeStore) UpdateConversation(context.Context, Conversation, uint64) (Conversation, error) {
	f.mutations++
	return f.conversation, nil
}
func (f *fakeStore) ListMemberships(context.Context, string, string, Page) (ListMembershipsResponse, error) {
	return ListMembershipsResponse{}, nil
}
func (f *fakeStore) GetMembership(context.Context, string, string, string, string) (Membership, error) {
	if f.membership.SubjectID == "" {
		return Membership{}, ErrNotFound
	}
	return f.membership, nil
}
func (f *fakeStore) PutMembership(_ context.Context, _ Principal, m Membership) (Membership, error) {
	f.mutations++
	f.put = m
	return m, nil
}
func (f *fakeStore) RemoveMembership(context.Context, Principal, string, string, string, string, uint64) (Membership, error) {
	f.mutations++
	return f.membership, nil
}
func (f *fakeStore) GetPost(context.Context, string, string, string) (Post, error) {
	if f.post.ID == "" {
		return Post{}, ErrNotFound
	}
	return f.post, nil
}
func (f *fakeStore) SendPost(_ context.Context, _ SendPostRequest, p Post) (Post, error) {
	f.mutations++
	// The post the service composed, recorded as handed over: a fixture return
	// value cannot show what the service decided to persist.
	f.sent = p
	if f.post.ID == "" {
		return p, nil
	}
	return f.post, nil
}
func (f *fakeStore) ListPosts(_ context.Context, _ Principal, _, _ string, _ uint64, _ Page, w PostWindow) (ListPostsResponse, error) {
	f.window = w
	return ListPostsResponse{}, nil
}
func (f *fakeStore) EditPost(context.Context, EditPostRequest) (Post, error) {
	f.mutations++
	return f.post, nil
}
func (f *fakeStore) DeletePost(context.Context, DeletePostRequest) (Post, error) {
	f.mutations++
	return f.post, nil
}
func (f *fakeStore) Search(_ context.Context, r SearchRequest) (SearchResponse, error) {
	f.searchCalls++
	if r.SkipChannels && f.messagePages != nil {
		return f.messagePages[r.Page.Cursor], nil
	}
	if r.SkipMessages && f.channelPages != nil {
		return f.channelPages[r.ChannelCursor], nil
	}
	return f.search, nil
}
func (f *fakeStore) GetReadState(context.Context, string, string, string, string) (ReadState, error) {
	return ReadState{}, nil
}
func (f *fakeStore) PutReadState(context.Context, ReadState, uint64) (ReadState, error) {
	f.mutations++
	return ReadState{}, nil
}
func (f *fakeStore) GetPreferences(context.Context, string, string, string, string) (NotificationPreferences, error) {
	return NotificationPreferences{}, nil
}
func (f *fakeStore) PutPreferences(context.Context, NotificationPreferences, uint64) (NotificationPreferences, error) {
	f.mutations++
	return NotificationPreferences{}, nil
}
func (f *fakeStore) PutReaction(context.Context, Reaction) (Reaction, error) {
	f.mutations++
	return Reaction{}, nil
}
func (f *fakeStore) RemoveReaction(context.Context, string, string, string, string, string, string) error {
	f.mutations++
	return nil
}
func (f *fakeStore) ListReactions(context.Context, Principal, string, string, string, Page) (ListReactionsResponse, error) {
	return ListReactionsResponse{}, nil
}
func (f *fakeStore) PutPin(context.Context, Pin) (Pin, error) { f.mutations++; return Pin{}, nil }
func (f *fakeStore) RemovePin(context.Context, Principal, string, string, string, string, uint64) error {
	f.mutations++
	return nil
}
func (f *fakeStore) ListPins(context.Context, string, string) ([]Pin, error) { return f.pins, nil }
func (f *fakeStore) Watch(context.Context, WatchConversationRequest) (<-chan WatchEvent, error) {
	ch := make(chan WatchEvent)
	close(ch)
	return ch, nil
}

func principal() Principal { return Principal{TenantID: "t1", SubjectID: "u1"} }
func conversation() Conversation {
	return Conversation{ID: "c1", TenantID: "t1", Kind: PrivateChannel, OwnerID: "u1", Revision: 1}
}

func TestTodo_CHAT_015_SelfDirectKeepsOnePrivateMember(t *testing.T) {
	f := &fakeStore{}
	s := newTestService(f, time.Now)
	created, err := s.CreateConversation(context.Background(), CreateConversationRequest{
		Principal: principal(), TenantID: "t1", Kind: Direct,
		Members: []MemberRef{{TenantID: "t1", SubjectID: "u1"}},
	})
	if err != nil {
		t.Fatalf("create self direct: %v", err)
	}
	if created.Kind != Direct || created.OwnerID != "u1" || len(f.created) != 1 || f.created[0].SubjectID != "u1" || f.created[0].HomeTenantID != "t1" || f.mutations != 1 {
		t.Fatalf("self direct exposed another member: room=%+v members=%+v mutations=%d", created, f.created, f.mutations)
	}
	if err := s.ValidateCreate(context.Background(), CreateConversationRequest{
		Principal: principal(), TenantID: "t1", Kind: Direct,
		Members: []MemberRef{{TenantID: "t1", SubjectID: "u1"}, {TenantID: "t1", SubjectID: "u2"}},
	}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("direct with third participant = %v, want invalid argument", err)
	}
}

func TestTodo_CHAT_013_ServiceAuthorizationBeforeMembershipMutation(t *testing.T) {
	f := &fakeStore{conversation: conversation()}
	s := newTestService(f, func() time.Time { return time.Unix(10, 0).UTC() })
	_, err := s.AddMembership(context.Background(), AddMembershipRequest{Principal: principal(), Membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u2"}})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("err = %v, want permission denied", err)
	}
	if f.mutations != 0 {
		t.Fatalf("store mutations = %d, want 0", f.mutations)
	}
}

func TestTodo_CHAT_013_RemovedMemberCannotReadPrivateConversation(t *testing.T) {
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", LeftAt: timePtr(time.Unix(2, 0)), Revision: 4}}
	s := newTestService(f, time.Now)
	_, err := s.GetConversation(context.Background(), GetConversationRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1"})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("err = %v, want permission denied", err)
	}
}

func TestTodo_CHAT_013_CreateValidatesOwnerAndInitialMembers(t *testing.T) {
	f := &fakeStore{}
	s := newTestService(f, time.Now)
	_, err := s.CreateConversation(context.Background(), CreateConversationRequest{Principal: principal(), TenantID: "t1", OwnerID: "u2", Kind: PrivateChannel})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("err = %v, want permission denied", err)
	}
	if f.mutations != 0 {
		t.Fatalf("store mutations = %d, want 0", f.mutations)
	}
	got, err := s.CreateConversation(context.Background(), CreateConversationRequest{Principal: principal(), TenantID: "t1", Kind: PrivateChannel, IdempotencyKey: "k1", Members: []MemberRef{{TenantID: "t1", SubjectID: "u2"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.OwnerID != "u1" || len(f.created) != 2 {
		t.Fatalf("conversation/members = %#v/%d", got, len(f.created))
	}
}

func TestTodo_CHAT_021_SearchGlobalResultsRecheckConversationAuthorization(t *testing.T) {
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1"}}
	f.search = SearchResponse{
		Channels: []ChannelSearchResult{{ConversationID: "c1", Name: "Operations", Kind: PublicChannel}, {ConversationID: "private-secret", Name: "Secret", Kind: PrivateChannel}},
		Results:  []SearchResult{{Post: Post{ID: "p1", ConversationID: "c1", TenantID: "t1", Body: "visible"}, ConversationName: "Operations"}, {Post: Post{ID: "p2", ConversationID: "private-secret", TenantID: "t1", Body: "secret"}, ConversationName: "Secret"}},
	}
	s := newTestService(f, time.Now)
	got, err := s.Search(context.Background(), SearchRequest{Principal: principal(), TenantID: "t1", Query: "visible"})
	if err != nil {
		t.Fatal(err)
	}
	if f.searchCalls != 2 || len(got.Channels) != 1 || got.Channels[0].ConversationID != "c1" || len(got.Results) != 1 || got.Results[0].Post.ID != "p1" {
		t.Fatalf("search results exposed unauthorized hits or dropped visible hits: calls=%d response=%+v", f.searchCalls, got)
	}
}

func TestTodo_CHAT_021_SearchCapsPageSize(t *testing.T) {
	f := &fakeStore{conversation: conversation()}
	s := newTestService(f, time.Now)
	_, err := s.Search(context.Background(), SearchRequest{Principal: principal(), TenantID: "t1", Query: "message", Page: Page{PageSize: 51}})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("err = %v, want invalid argument", err)
	}
	if f.searchCalls != 0 {
		t.Fatalf("store search calls = %d, want 0", f.searchCalls)
	}
}

func TestTodo_CHAT_021_SearchRefillsAfterPolicyFilteredCandidates(t *testing.T) {
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1"}}
	f.messagePages = map[string]SearchResponse{
		"":             {Results: []SearchResult{{Post: Post{ID: "private", ConversationID: "restricted", TenantID: "t1", Body: "hidden"}}}, NextCursor: "message-next"},
		"message-next": {Results: []SearchResult{{Post: Post{ID: "visible", ConversationID: "c1", TenantID: "t1", Body: "shown"}, ConversationName: "Operations"}}},
	}
	f.channelPages = map[string]SearchResponse{
		"":             {Channels: []ChannelSearchResult{{ConversationID: "restricted", Name: "Restricted", Kind: PrivateChannel}}, ChannelNextCursor: "channel-next"},
		"channel-next": {Channels: []ChannelSearchResult{{ConversationID: "c1", Name: "Operations", Kind: PrivateChannel, Joined: true}}},
	}
	s := newTestService(f, time.Now)
	got, err := s.Search(context.Background(), SearchRequest{Principal: principal(), TenantID: "t1", Query: "message", Page: Page{PageSize: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Results) != 1 || got.Results[0].Post.ID != "visible" || len(got.Channels) != 1 || got.Channels[0].ConversationID != "c1" || f.searchCalls != 4 {
		t.Fatalf("search failed to refill visible categories after denied candidates: calls=%d response=%+v", f.searchCalls, got)
	}
}

func TestTodo_CHAT_021_SearchFiltersRevokedChannel(t *testing.T) {
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1"}}
	f.search = SearchResponse{
		Channels: []ChannelSearchResult{{ConversationID: "c1", Name: "Operations", Kind: PrivateChannel}},
		Results:  []SearchResult{{Post: Post{ID: "p1", ConversationID: "c1", TenantID: "t1", Body: "private"}, ConversationName: "Operations"}},
	}
	s := newTestService(f, time.Now)
	s.SetAuthority(denyConversationAuthority{base: verifiedAuthority{store: f}, deny: "c1"})
	got, err := s.Search(context.Background(), SearchRequest{Principal: principal(), TenantID: "t1", Query: "private"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Channels) != 0 || len(got.Results) != 0 {
		t.Fatalf("search returned hits from a currently denied channel: %+v", got)
	}
}

func TestTodo_CHAT_025_EditChecksPostAuthorBeforeMutation(t *testing.T) {
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Revision: 1}, post: Post{ID: "p1", ConversationID: "c1", TenantID: "t1", AuthorID: "u2", AuthorHomeTenantID: "t1"}}
	s := newTestService(f, time.Now)
	_, err := s.EditPost(context.Background(), EditPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", PostID: "p1", Body: "changed", ExpectedRevision: 1})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("err = %v, want permission denied", err)
	}
	if f.mutations != 0 {
		t.Fatalf("store mutations = %d, want 0", f.mutations)
	}
}

func TestTodo_CHAT_017_ServiceCRUDAndCollaborationPath(t *testing.T) {
	now := time.Unix(20, 0).UTC()
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Role: Manager, Revision: 2}, post: Post{ID: "p1", ConversationID: "c1", TenantID: "t1", AuthorID: "u1", AuthorHomeTenantID: "t1", Revision: 1}}
	s := newTestService(f, func() time.Time { return now })
	ctx := context.Background()
	if _, err := s.ListConversations(ctx, ListConversationsRequest{Principal: principal(), TenantID: "t1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateConversation(ctx, UpdateConversationRequest{Principal: principal(), Conversation: conversation(), ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListMemberships(ctx, ListMembershipsRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddMembership(ctx, AddMembershipRequest{Principal: principal(), Membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u2"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveMembership(ctx, RemoveMembershipRequest{Principal: principal(), TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u2", ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SendPost(ctx, SendPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Body: "hello", IdempotencyKey: "k"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListPosts(ctx, ListPostsRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EditPost(ctx, EditPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", PostID: "p1", Body: "edited", ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeletePost(ctx, DeletePostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", PostID: "p1", ExpectedRevision: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Search(ctx, SearchRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Query: "hello"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetReadState(ctx, GetReadStateRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateReadState(ctx, UpdateReadStateRequest{Principal: principal(), ReadState: ReadState{TenantID: "t1", ConversationID: "c1"}, ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPreferences(ctx, GetPreferencesRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePreferences(ctx, UpdatePreferencesRequest{Principal: principal(), Preferences: NotificationPreferences{TenantID: "t1", ConversationID: "c1"}, ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddReaction(ctx, AddReactionRequest{Principal: principal(), Reaction: Reaction{TenantID: "t1", ConversationID: "c1", PostID: "p1", Emoji: ":+1:"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveReaction(ctx, RemoveReactionRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", PostID: "p1", Emoji: ":+1:"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PinPost(ctx, PinPostRequest{Principal: principal(), Pin: Pin{TenantID: "t1", ConversationID: "c1", PostID: "p1"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UnpinPost(ctx, UnpinPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", PostID: "p1", ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListPins(ctx, ListPinsRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1"}); err != nil {
		t.Fatal(err)
	}
	ch, err := s.WatchConversation(ctx, WatchConversationRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := <-ch; ok {
		t.Fatal("watch channel should be closed")
	}
	if f.mutations == 0 {
		t.Fatal("expected durable mutation calls")
	}
}

func TestTodo_CHAT_017_ServiceRejectsBoundsBeforeStore(t *testing.T) {
	f := &fakeStore{conversation: conversation()}
	s := newTestService(f, time.Now)
	_, err := s.ListPosts(context.Background(), ListPostsRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Page: Page{PageSize: 201}})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("err = %v", err)
	}
	if f.mutations != 0 {
		t.Fatalf("store calls = %d", f.mutations)
	}
	_, err = s.SendPost(context.Background(), SendPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Body: "x"})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("missing idempotency err = %v", err)
	}
}

type rejectingAuthority struct{}

func (rejectingAuthority) Authorize(context.Context, Principal, Conversation, chatpolicy.Action, time.Time) (chatpolicy.Input, error) {
	return chatpolicy.Input{}, ErrPermissionDenied
}

func TestTodo_CHAT_011_AuthorityResolverFailsClosed(t *testing.T) {
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Revision: 1}}
	s := newTestService(f, time.Now)
	s.SetAuthority(rejectingAuthority{})
	_, err := s.GetConversation(context.Background(), GetConversationRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1"})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("err = %v", err)
	}
}

func TestTodo_CHAT_024_ReactionAndPinBoundsFailBeforeStore(t *testing.T) {
	f := &fakeStore{conversation: conversation()}
	s := NewService(f, time.Now)
	_, err := s.AddReaction(context.Background(), AddReactionRequest{Principal: principal(), Reaction: Reaction{TenantID: "t1", ConversationID: "c1", PostID: "p1"}})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("reaction err = %v", err)
	}
	_, err = s.PinPost(context.Background(), PinPostRequest{Principal: principal(), Pin: Pin{TenantID: "t1", ConversationID: "c1"}})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("pin err = %v", err)
	}
	if f.mutations != 0 {
		t.Fatalf("store calls = %d", f.mutations)
	}
}

func TestTodo_CHAT_013_ForeignSameSubjectCannotManageHostConversation(t *testing.T) {
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t2", ConversationID: "c1", SubjectID: "u1", Role: Member, Revision: 1}}
	s := newTestService(f, time.Now)
	foreign := Principal{TenantID: "t2", SubjectID: "u1"}
	_, err := s.UpdateConversation(context.Background(), UpdateConversationRequest{Principal: foreign, Conversation: conversation(), ExpectedRevision: 1})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("update err = %v", err)
	}
	_, err = s.AddMembership(context.Background(), AddMembershipRequest{Principal: foreign, Membership: Membership{TenantID: "t1", HomeTenantID: "t2", ConversationID: "c1", SubjectID: "u2"}})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("add member err = %v", err)
	}
	if f.mutations != 0 {
		t.Fatalf("store mutations = %d", f.mutations)
	}
}

func TestTodo_CHAT_011_InvalidIdentityAndMissingAuthorityFailClosed(t *testing.T) {
	f := &fakeStore{conversation: conversation()}
	s := NewService(f, time.Now)
	_, err := s.GetConversation(context.Background(), GetConversationRequest{Principal: Principal{TenantID: "t1"}, TenantID: "t1", ConversationID: "c1"})
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("invalid identity err = %v", err)
	}
	_, err = s.GetConversation(context.Background(), GetConversationRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing authority err = %v", err)
	}
}

func TestTodo_CHAT_014_CreateAndUpdateTransitionValidation(t *testing.T) {
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Revision: 1}}
	s := newTestService(f, time.Now)
	_, err := s.CreateConversation(context.Background(), CreateConversationRequest{Principal: principal(), TenantID: "t1", Kind: ConversationKind("bad")})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid kind err = %v", err)
	}
	c := conversation()
	c.Kind = Group
	_, err = s.UpdateConversation(context.Background(), UpdateConversationRequest{Principal: principal(), Conversation: c, ExpectedRevision: 1})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("kind transition err = %v", err)
	}
}

package chat

import (
	"context"
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type transportChatFake struct{ chatcore.ConversationService }

type foreignTargetService struct {
	*transportChatFake
	gotTenant string
	deny      bool
}

type coreOnlyService struct{ chatcore.ConversationService }

func (s *foreignTargetService) GetConversation(_ context.Context, r chatcore.GetConversationRequest) (chatcore.Conversation, error) {
	s.gotTenant = r.TenantID
	if s.deny {
		return chatcore.Conversation{}, chatcore.ErrPermissionDenied
	}
	return chatcore.Conversation{ID: r.ConversationID, TenantID: r.TenantID, Kind: chatcore.Group}, nil
}

func (transportChatFake) CreateConversation(context.Context, chatcore.CreateConversationRequest) (chatcore.Conversation, error) {
	return chatcore.Conversation{ID: "c", TenantID: "server", Kind: chatcore.Group}, nil
}
func (transportChatFake) ListConversations(context.Context, chatcore.ListConversationsRequest) (chatcore.ListConversationsResponse, error) {
	return chatcore.ListConversationsResponse{Conversations: []chatcore.Conversation{{ID: "c", TenantID: "server", Kind: chatcore.Group}}, NextCursor: "next"}, nil
}
func (transportChatFake) GetConversation(context.Context, chatcore.GetConversationRequest) (chatcore.Conversation, error) {
	return chatcore.Conversation{ID: "c", TenantID: "server", Kind: chatcore.Group}, nil
}
func (transportChatFake) UpdateConversation(context.Context, chatcore.UpdateConversationRequest) (chatcore.Conversation, error) {
	return chatcore.Conversation{ID: "c", TenantID: "server", Kind: chatcore.Group}, nil
}
func (transportChatFake) ListMemberships(context.Context, chatcore.ListMembershipsRequest) (chatcore.ListMembershipsResponse, error) {
	return chatcore.ListMembershipsResponse{Memberships: []chatcore.Membership{{ConversationID: "c", HomeTenantID: "home", SubjectID: "u", Role: chatcore.Member, Revision: 1}}}, nil
}
func (transportChatFake) AddMembership(context.Context, chatcore.AddMembershipRequest) (chatcore.Membership, error) {
	return chatcore.Membership{ConversationID: "c", HomeTenantID: "home", SubjectID: "u", Role: chatcore.Member, Revision: 1}, nil
}
func (transportChatFake) RemoveMembership(context.Context, chatcore.RemoveMembershipRequest) (chatcore.Membership, error) {
	return chatcore.Membership{ConversationID: "c", HomeTenantID: "home", SubjectID: "u", Role: chatcore.Member, Revision: 1}, nil
}
func (transportChatFake) SendPost(context.Context, chatcore.SendPostRequest) (chatcore.Post, error) {
	return chatcore.Post{ID: "p", ConversationID: "c", TenantID: "server", AuthorID: "u", Body: "hello", Sequence: 1, Revision: 1, CreatedAt: time.Now()}, nil
}
func (transportChatFake) ListPosts(context.Context, chatcore.ListPostsRequest) (chatcore.ListPostsResponse, error) {
	return chatcore.ListPostsResponse{Posts: []chatcore.Post{{ID: "p", ConversationID: "c", TenantID: "server", Body: "hello", CreatedAt: time.Now()}}, NextCursor: "next"}, nil
}
func (transportChatFake) EditPost(context.Context, chatcore.EditPostRequest) (chatcore.Post, error) {
	return chatcore.Post{ID: "p", ConversationID: "c", TenantID: "server", Body: "edited", CreatedAt: time.Now()}, nil
}
func (transportChatFake) DeletePost(context.Context, chatcore.DeletePostRequest) (chatcore.Post, error) {
	return chatcore.Post{ID: "p", ConversationID: "c", TenantID: "server", Deleted: true, CreatedAt: time.Now()}, nil
}
func (transportChatFake) Search(context.Context, chatcore.SearchRequest) (chatcore.SearchResponse, error) {
	return chatcore.SearchResponse{Results: []chatcore.SearchResult{{Post: chatcore.Post{ID: "p", ConversationID: "c", TenantID: "server", CreatedAt: time.Now()}}}}, nil
}
func (transportChatFake) GetReadState(context.Context, chatcore.GetReadStateRequest) (chatcore.ReadState, error) {
	return chatcore.ReadState{ConversationID: "c", TenantID: "server", SubjectID: "u"}, nil
}
func (transportChatFake) UpdateReadState(context.Context, chatcore.UpdateReadStateRequest) (chatcore.ReadState, error) {
	return chatcore.ReadState{ConversationID: "c", TenantID: "server", SubjectID: "u"}, nil
}
func (transportChatFake) GetPreferences(context.Context, chatcore.GetPreferencesRequest) (chatcore.NotificationPreferences, error) {
	return chatcore.NotificationPreferences{ConversationID: "c", TenantID: "server", SubjectID: "u"}, nil
}
func (transportChatFake) UpdatePreferences(context.Context, chatcore.UpdatePreferencesRequest) (chatcore.NotificationPreferences, error) {
	return chatcore.NotificationPreferences{ConversationID: "c", TenantID: "server", SubjectID: "u"}, nil
}
func (transportChatFake) AddReaction(context.Context, chatcore.AddReactionRequest) (chatcore.Reaction, error) {
	return chatcore.Reaction{ConversationID: "c", PostID: "p", TenantID: "server", SubjectID: "u", Emoji: "+1"}, nil
}
func (transportChatFake) RemoveReaction(context.Context, chatcore.RemoveReactionRequest) error {
	return nil
}
func (transportChatFake) ListReactions(context.Context, chatcore.ListReactionsRequest) (chatcore.ListReactionsResponse, error) {
	return chatcore.ListReactionsResponse{}, nil
}
func (transportChatFake) PinPost(context.Context, chatcore.PinPostRequest) (chatcore.Pin, error) {
	return chatcore.Pin{ConversationID: "c", PostID: "p", TenantID: "server", PinnedBy: "u"}, nil
}
func (transportChatFake) UnpinPost(context.Context, chatcore.UnpinPostRequest) error { return nil }
func (transportChatFake) ListPins(context.Context, chatcore.ListPinsRequest) ([]chatcore.Pin, error) {
	return []chatcore.Pin{{ConversationID: "c", PostID: "p", TenantID: "server", PinnedBy: "u"}}, nil
}
func (transportChatFake) WatchConversation(context.Context, chatcore.WatchConversationRequest) (<-chan chatcore.WatchEvent, error) {
	ch := make(chan chatcore.WatchEvent, 1)
	ch <- chatcore.WatchEvent{Event: chatcore.ConversationEvent{Kind: chatcore.PostCreated, Sequence: 1, Post: &chatcore.Post{ID: "p", ConversationID: "c", TenantID: "server"}}, ResumeCursor: "cs1"}
	close(ch)
	return ch, nil
}
func (transportChatFake) SuggestReferences(context.Context, chatcore.SuggestReferencesRequest) ([]chatcore.ReferenceCandidate, error) {
	return []chatcore.ReferenceCandidate{{Reference: chatcore.Reference{Kind: chatcore.PersonMention, TenantID: "home", ID: "u", Display: "User"}, Eligible: true}}, nil
}
func (transportChatFake) SendPostWithReferences(_ context.Context, r chatcore.SendPostWithReferencesRequest) (chatcore.Post, error) {
	return chatcore.Post{ID: "p", TenantID: r.TenantID, ConversationID: r.ConversationID, References: r.References}, nil
}
func (transportChatFake) CreateShareLink(context.Context, chatcore.Principal, string, string, string) (chatcore.ConversationLink, error) {
	return chatcore.ConversationLink{URL: "/chat/share/token", TenantID: "server", ConversationID: "c", PostID: "p"}, nil
}
func (transportChatFake) ResolveShareLink(context.Context, chatcore.Principal, string) (chatcore.Conversation, *chatcore.Post, error) {
	return chatcore.Conversation{ID: "c", TenantID: "server", Kind: chatcore.Group}, &chatcore.Post{ID: "p", TenantID: "server", ConversationID: "c"}, nil
}
func (transportChatFake) ForwardPost(context.Context, chatcore.ForwardPostRequest) (chatcore.Post, error) {
	return chatcore.Post{ID: "forwarded", TenantID: "server", ConversationID: "c"}, nil
}

func admittedChatContext(t *testing.T) context.Context {
	t.Helper()
	now := time.Now()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("server"), Subject: "u", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	c, _, e := transport.Admit(context.Background(), transport.Config{Verifier: trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) { return p, nil })}, transport.AdmissionRequest{Metadata: transport.MapMetadata{"authorization": {"Bearer token"}}, Method: "/hcmnext.chat.v1.ConversationService/GetConversation", Kind: transport.KindGRPC})
	if e != nil {
		t.Fatal(e)
	}
	return c
}

func TestTodo_CHAT_008_Conformance(t *testing.T) {
	if got := kind(chatv1.ConversationKind_CONVERSATION_KIND_PUBLIC_CHANNEL); got != chatcore.PublicChannel {
		t.Fatalf("kind conversion = %q", got)
	}
	if got := conversation(chatcore.Conversation{ID: "c1", TenantID: "server-tenant", Kind: chatcore.Group, Revision: 3}); got.GetId() != "c1" || got.GetTenantId() != "server-tenant" || got.GetRevision() != 3 {
		t.Fatalf("conversation projection = %+v", got)
	}
}

func TestTodo_CHAT_009_Security(t *testing.T) {
	s := &server{}
	_, err := s.GetConversation(context.Background(), &chatv1.GetConversationRequest{TenantId: "caller-selected-tenant", ConversationId: "c1"})
	if err == nil {
		t.Fatal("missing trusted context accepted")
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("status = %v, want unauthenticated", status.Code(err))
	}
}

func TestTodo_CHAT_018_TransportErrors(t *testing.T) {
	for _, tc := range []struct {
		in   error
		want codes.Code
	}{
		{chatcore.ErrInvalidArgument, codes.InvalidArgument},
		{chatcore.ErrPermissionDenied, codes.PermissionDenied},
		{chatcore.ErrNotFound, codes.NotFound},
		{chatcore.ErrAlreadyExists, codes.AlreadyExists},
		{chatcore.ErrConflict, codes.Aborted},
	} {
		if got := status.Code(callErr(tc.in)); got != tc.want {
			t.Errorf("callErr(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
	if got := status.Code(callErr(chatcore.ErrUnavailable)); got != codes.Unavailable {
		t.Fatalf("unavailable = %v, want unavailable", got)
	}
}

func TestTodo_CHAT_009_GrpcHttpParity(t *testing.T) {
	ctx := admittedChatContext(t)
	s := &server{deps: Dependencies{Service: transportChatFake{}}}
	if v, err := s.CreateConversation(ctx, &chatv1.CreateConversationRequest{TenantId: "forged", Kind: chatv1.ConversationKind_CONVERSATION_KIND_GROUP, Members: []*chatv1.MemberRef{{TenantId: "home", SubjectId: "u"}}, IdempotencyKey: "k"}); err != nil || v.GetConversation().GetTenantId() != "server" {
		t.Fatalf("create: %+v %v", v, err)
	}
	if v, err := s.ListConversations(ctx, &chatv1.ListConversationsRequest{TenantId: "forged", PageSize: 2}); err != nil || len(v.GetConversations()) != 1 {
		t.Fatalf("list conversations: %+v %v", v, err)
	}
	if _, err := s.GetConversation(ctx, &chatv1.GetConversationRequest{TenantId: "forged", ConversationId: "c"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateConversation(ctx, &chatv1.UpdateConversationRequest{Conversation: &chatv1.Conversation{Id: "c"}}); err != nil {
		t.Fatal(err)
	}
	if v, err := s.ListMemberships(ctx, &chatv1.ListMembershipsRequest{ConversationId: "c"}); err != nil || v.GetMemberships()[0].GetHomeTenantId() != "home" {
		t.Fatalf("members: %+v %v", v, err)
	}
	if _, err := s.AddMembership(ctx, &chatv1.AddMembershipRequest{Membership: &chatv1.Membership{ConversationId: "c", HomeTenantId: "home", SubjectId: "u"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveMembership(ctx, &chatv1.RemoveMembershipRequest{ConversationId: "c", SubjectId: "u"}); err != nil {
		t.Fatal(err)
	}
	if v, err := s.SendPost(ctx, &chatv1.SendPostRequest{ConversationId: "c", Body: "hello", IdempotencyKey: "k"}); err != nil || v.GetPost().GetAuthorId() != "u" {
		t.Fatalf("send: %+v %v", v, err)
	}
	if _, err := s.ListPosts(ctx, &chatv1.ListPostsRequest{ConversationId: "c"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EditPost(ctx, &chatv1.EditPostRequest{Post: &chatv1.Post{Id: "p", ConversationId: "c"}, Body: "edited"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeletePost(ctx, &chatv1.DeletePostRequest{ConversationId: "c", PostId: "p"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Search(ctx, &chatv1.SearchRequest{ConversationId: "c", Query: "hello"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetReadState(ctx, &chatv1.GetReadStateRequest{ConversationId: "c"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateReadState(ctx, &chatv1.UpdateReadStateRequest{State: &chatv1.ReadState{ConversationId: "c"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPreferences(ctx, &chatv1.GetPreferencesRequest{ConversationId: "c"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePreferences(ctx, &chatv1.UpdatePreferencesRequest{Preferences: &chatv1.NotificationPreferences{ConversationId: "c"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddReaction(ctx, &chatv1.AddReactionRequest{Reaction: &chatv1.Reaction{ConversationId: "c", PostId: "p", Emoji: "+1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveReaction(ctx, &chatv1.RemoveReactionRequest{ConversationId: "c", PostId: "p", Emoji: "+1"}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ListReactions(ctx, &chatv1.ListReactionsRequest{ConversationId: "c", PostId: "p", PageSize: 1}); err != nil || got == nil {
		t.Fatalf("list reactions=%+v %v", got, err)
	}
	if _, err := s.PinPost(ctx, &chatv1.PinPostRequest{Pin: &chatv1.Pin{ConversationId: "c", PostId: "p"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UnpinPost(ctx, &chatv1.UnpinPostRequest{ConversationId: "c", PostId: "p"}); err != nil {
		t.Fatal(err)
	}
	if v, err := s.ListPins(ctx, &chatv1.ListPinsRequest{ConversationId: "c"}); err != nil || len(v.GetPins()) != 1 {
		t.Fatalf("pins: %+v %v", v, err)
	}
	if v, err := s.SendPost(ctx, &chatv1.SendPostRequest{TenantId: "server", ConversationId: "c", Body: "ref", IdempotencyKey: "ref-key", References: []*chatv1.Reference{{Kind: chatv1.ReferenceKind_REFERENCE_KIND_PERSON_MENTION, TenantId: "home", Id: "u"}}}); err != nil || len(v.GetPost().GetReferences()) != 1 {
		t.Fatalf("reference send: %+v %v", v, err)
	}
	if v, err := s.ResolveReferences(ctx, &chatv1.ResolveReferencesRequest{TenantId: "server", ConversationId: "c", Kind: chatv1.ReferenceKind_REFERENCE_KIND_PERSON_MENTION, Query: "u"}); err != nil || len(v.GetCandidates()) != 1 {
		t.Fatalf("references: %+v %v", v, err)
	}
	if v, err := s.CreateShareLink(ctx, &chatv1.CreateShareLinkRequest{TenantId: "server", ConversationId: "c", PostId: "p"}); err != nil || v.GetLink().GetUrl() == "" {
		t.Fatalf("share create: %+v %v", v, err)
	}
	if v, err := s.ResolveShareLink(ctx, &chatv1.ResolveShareLinkRequest{Token: "token"}); err != nil || v.GetPost().GetId() != "p" {
		t.Fatalf("share resolve: %+v %v", v, err)
	}
	if v, err := s.ForwardPost(ctx, &chatv1.ForwardPostRequest{SourceTenantId: "server", SourceConversationId: "c", SourcePostId: "p", DestinationTenantId: "server", DestinationConversationId: "c", IdempotencyKey: "forward-key"}); err != nil || v.GetPost().GetId() != "forwarded" {
		t.Fatalf("forward: %+v %v", v, err)
	}
	ch, _, err := s.watch(ctx, &chatv1.WatchConversationRequest{ConversationId: "c", TenantId: "forged"})
	if err != nil {
		t.Fatal(err)
	}
	if e := <-ch; e.ResumeCursor != "cs1" || e.Event.Post.ID != "p" {
		t.Fatalf("watch: %+v", e)
	}
	h := NewHandler(Dependencies{Service: transportChatFake{}})
	if h == nil {
		t.Fatal("nil HTTP projection")
	}
}

func TestTodo_CHAT_051_ForeignHostTargetUsesCurrentPrincipal(t *testing.T) {
	ctx := admittedChatContext(t)
	for _, deny := range []bool{false, true} {
		f := &foreignTargetService{transportChatFake: &transportChatFake{}, deny: deny}
		s := &server{deps: Dependencies{Service: f}}
		v, err := s.GetConversation(ctx, &chatv1.GetConversationRequest{TenantId: "foreign-host", ConversationId: "c"})
		if f.gotTenant != "foreign-host" {
			t.Fatalf("host target = %q, want foreign-host", f.gotTenant)
		}
		if deny {
			if status.Code(err) != codes.PermissionDenied || v != nil {
				t.Fatalf("denied foreign host = %#v %v", v, err)
			}
		} else if err != nil || v.GetConversation().GetTenantId() != "foreign-host" {
			t.Fatalf("authorized foreign host = %#v %v", v, err)
		}
	}
}

func TestTodo_CHAT_009_MalformedAndOptionalFields(t *testing.T) {
	ctx := admittedChatContext(t)
	s := &server{deps: Dependencies{Service: transportChatFake{}}}
	if _, err := s.SendPost(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateConversation(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddMembership(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EditPost(ctx, &chatv1.EditPostRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateReadState(ctx, &chatv1.UpdateReadStateRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePreferences(ctx, &chatv1.UpdatePreferencesRequest{}); err != nil {
		t.Fatal(err)
	}
	for _, k := range []chatv1.ReferenceKind{chatv1.ReferenceKind_REFERENCE_KIND_PERSON_MENTION, chatv1.ReferenceKind_REFERENCE_KIND_AGENT_MENTION, chatv1.ReferenceKind_REFERENCE_KIND_CONVERSATION_REFERENCE, chatv1.ReferenceKind_REFERENCE_KIND_UNSPECIFIED} {
		_ = referenceKind(k)
	}
	_ = referenceIn(nil)
	_ = referencesIn([]*chatv1.Reference{nil})
	_ = sourceAttributionIn(nil)
	_ = sourceAttributionValue(nil)
	_ = sourceAttribution(nil)
	if got := membershipIn(nil); got.ConversationID != "" {
		t.Fatal("nil membership projected")
	}
	if got := reactionIn(nil); got.Emoji != "" {
		t.Fatal("nil reaction projected")
	}
	if got := pinIn(nil); got.PostID != "" {
		t.Fatal("nil pin projected")
	}
	if got := readStateIn(nil); got.ConversationID != "" {
		t.Fatal("nil read state projected")
	}
	if got := prefsIn(nil); got.ConversationID != "" {
		t.Fatal("nil prefs projected")
	}
}

func TestTodo_CHAT_018_UnconfiguredReferencePort(t *testing.T) {
	ctx := admittedChatContext(t)
	s := &server{deps: Dependencies{Service: coreOnlyService{ConversationService: transportChatFake{}}}}
	if _, err := s.ResolveReferences(ctx, &chatv1.ResolveReferencesRequest{}); status.Code(err) != codes.Unavailable {
		t.Fatalf("resolve unavailable = %v", err)
	}
	if _, err := s.CreateShareLink(ctx, &chatv1.CreateShareLinkRequest{}); status.Code(err) != codes.Unavailable {
		t.Fatalf("create unavailable = %v", err)
	}
	if _, err := s.ResolveShareLink(ctx, &chatv1.ResolveShareLinkRequest{}); status.Code(err) != codes.Unavailable {
		t.Fatalf("resolve link unavailable = %v", err)
	}
	if _, err := s.ForwardPost(ctx, &chatv1.ForwardPostRequest{}); status.Code(err) != codes.Unavailable {
		t.Fatalf("forward unavailable = %v", err)
	}
}

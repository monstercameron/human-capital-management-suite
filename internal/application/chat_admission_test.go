package application

import (
	"context"
	"errors"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatadmission"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatstream"
)

// chatServiceStub is a minimal conversation and reference service for the
// decorator tests. Every method records that it was reached, so a test can tell
// an admission refusal from a call that went through.
type chatServiceStub struct {
	chatcore.ConversationService
	calls      map[string]int
	membership chatcore.Membership
}

func newChatServiceStub() *chatServiceStub {
	return &chatServiceStub{calls: map[string]int{}}
}
func (s *chatServiceStub) note(name string) { s.calls[name]++ }

func (s *chatServiceStub) GetConversation(_ context.Context, r chatcore.GetConversationRequest) (chatcore.Conversation, error) {
	s.note("GetConversation")
	return chatcore.Conversation{ID: r.ConversationID, TenantID: r.TenantID, Revision: 1}, nil
}
func (s *chatServiceStub) ListPosts(_ context.Context, _ chatcore.ListPostsRequest) (chatcore.ListPostsResponse, error) {
	s.note("ListPosts")
	return chatcore.ListPostsResponse{}, nil
}
func (s *chatServiceStub) ListMemberships(_ context.Context, _ chatcore.ListMembershipsRequest) (chatcore.ListMembershipsResponse, error) {
	s.note("ListMemberships")
	return chatcore.ListMembershipsResponse{}, nil
}
func (s *chatServiceStub) RemoveMembership(_ context.Context, _ chatcore.RemoveMembershipRequest) (chatcore.Membership, error) {
	s.note("RemoveMembership")
	return s.membership, nil
}
func (s *chatServiceStub) SuggestReferences(_ context.Context, _ chatcore.SuggestReferencesRequest) ([]chatcore.ReferenceCandidate, error) {
	s.note("SuggestReferences")
	return nil, nil
}
func (s *chatServiceStub) SendPostWithReferences(_ context.Context, r chatcore.SendPostWithReferencesRequest) (chatcore.Post, error) {
	s.note("SendPostWithReferences")
	return chatcore.Post{ID: "p", TenantID: r.TenantID, ConversationID: r.ConversationID, Revision: 1}, nil
}
func (s *chatServiceStub) CreateShareLink(context.Context, chatcore.Principal, string, string, string) (chatcore.ConversationLink, error) {
	s.note("CreateShareLink")
	return chatcore.ConversationLink{}, nil
}
func (s *chatServiceStub) ResolveShareLink(context.Context, chatcore.Principal, string) (chatcore.Conversation, *chatcore.Post, error) {
	s.note("ResolveShareLink")
	return chatcore.Conversation{}, nil, nil
}
func (s *chatServiceStub) ForwardPost(_ context.Context, r chatcore.ForwardPostRequest) (chatcore.Post, error) {
	s.note("ForwardPost")
	return chatcore.Post{ID: "p", TenantID: r.DestinationTenantID, ConversationID: r.DestinationConversationID, Revision: 1}, nil
}

func laneRuntime(t *testing.T, budgets chatadmission.Config) *ChatStreamRuntime {
	t.Helper()
	cfg := runtimeConfig("lane-key")
	cfg.Budgets = budgets
	r, err := NewChatStreamRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// TestTodo_CHAT_046_ReadAndDerivedLanesAreAdmitted proves the read path is
// metered too, and that an exhausted lane is reported as the chat contract's
// retryable unavailable rather than a denial.
func TestTodo_CHAT_046_ReadAndDerivedLanesAreAdmitted(t *testing.T) {
	runtime := laneRuntime(t, chatadmission.Config{TenantConcurrent: 8, ConversationConcurrent: 8, SendConcurrent: 4, WatchConcurrent: 4, ReadConcurrent: 1, DerivedConcurrent: 1, ReadShedFraction: 1, DerivedShedFraction: 1})
	inner := newChatServiceStub()
	service := &streamingChatService{ConversationService: inner, runtime: runtime}
	ctx := context.Background()
	if _, err := service.ListPosts(ctx, chatcore.ListPostsRequest{TenantID: "t", ConversationID: "c"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetConversation(ctx, chatcore.GetConversationRequest{TenantID: "t", ConversationID: "c"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListMemberships(ctx, chatcore.ListMembershipsRequest{TenantID: "t", ConversationID: "c"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SuggestReferences(ctx, chatcore.SuggestReferencesRequest{TenantID: "t", ConversationID: "c"}); err != nil {
		t.Fatal(err)
	}
	if inner.calls["ListPosts"] != 1 || inner.calls["GetConversation"] != 1 || inner.calls["ListMemberships"] != 1 || inner.calls["SuggestReferences"] != 1 {
		t.Fatalf("calls=%v", inner.calls)
	}
	// Hold the only read slot: the next read is refused, retryably.
	held, err := runtime.AcquireRead(ctx, "t", "c")
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ListPosts(ctx, chatcore.ListPostsRequest{TenantID: "t", ConversationID: "c"})
	if !errors.Is(err, chatcore.ErrUnavailable) || !errors.Is(err, chatadmission.ErrOverloaded) {
		t.Fatalf("saturated read=%v, want retryable unavailable", err)
	}
	if inner.calls["ListPosts"] != 1 {
		t.Fatalf("refused read still reached the store: %v", inner.calls)
	}
	if err := held.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListPosts(ctx, chatcore.ListPostsRequest{TenantID: "t", ConversationID: "c"}); err != nil {
		t.Fatalf("read after release=%v", err)
	}
	// Derived work is refused on its own lane without touching the read lane.
	derived, err := runtime.AcquireDerived(ctx, "t", "c")
	if err != nil {
		t.Fatal(err)
	}
	defer derived.Release()
	if _, err := service.SuggestReferences(ctx, chatcore.SuggestReferencesRequest{TenantID: "t", ConversationID: "c"}); !errors.Is(err, chatcore.ErrUnavailable) {
		t.Fatalf("saturated derived=%v", err)
	}
	if _, err := service.GetConversation(ctx, chatcore.GetConversationRequest{TenantID: "t", ConversationID: "c"}); err != nil {
		t.Fatalf("read starved by derived work: %v", err)
	}
}

// TestTodo_CHAT_046_AdmissionConfigIsOverridable proves a deployment can
// override single bounds through the chat composition input without zeroing the
// rest, which is what makes defaultChatAdmissionConfig a default.
func TestTodo_CHAT_046_AdmissionConfigIsOverridable(t *testing.T) {
	base := defaultChatAdmissionConfig()
	got := mergeChatAdmissionConfig(base, ChatAdmissionConfig{SendConcurrent: 7, DerivedShedFraction: 0.4})
	if got.SendConcurrent != 7 || got.DerivedShedFraction != 0.4 {
		t.Fatalf("override not applied: %+v", got)
	}
	if got.TenantConcurrent != base.TenantConcurrent || got.WatchConcurrent != base.WatchConcurrent || got.ReadConcurrent != base.ReadConcurrent {
		t.Fatalf("unset fields lost their defaults: %+v", got)
	}
	if unchanged := mergeChatAdmissionConfig(base, ChatAdmissionConfig{}); unchanged != base {
		t.Fatalf("zero override changed the defaults: %+v", unchanged)
	}
}

// TestTodo_CHAT_020_MembershipRemovalRevokesTheStream proves revocation is event
// driven: removing a membership closes that principal's live subscription and
// drops its cached authority before the call returns.
func TestTodo_CHAT_020_MembershipRemovalRevokesTheStream(t *testing.T) {
	runtime := laneRuntime(t, defaultChatAdmissionConfig())
	cache := newChatAuthorityCache(time.Minute, nil)
	cachedChatPut(cache, cache.facts, chatCacheKey("home", "subject", "session"), chatPrincipalFixture())
	inner := newChatServiceStub()
	inner.membership = chatcore.Membership{TenantID: "host", ConversationID: "c", HomeTenantID: "home", SubjectID: "subject", Revision: 9}
	service := &streamingChatService{ConversationService: inner, runtime: runtime, authority: cache}
	sub, lease, err := runtime.Watch(context.Background(), chatstream.WatchRequest{TenantID: "host", HomeTenantID: "home", SubjectID: "subject", ConversationID: "c", MembershipEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if _, err := service.RemoveMembership(context.Background(), chatcore.RemoveMembershipRequest{TenantID: "host", ConversationID: "c", SubjectID: "subject"}); err != nil {
		t.Fatal(err)
	}
	if _, err := sub.Next(context.Background()); !errors.Is(err, chatstream.ErrRevoked) {
		t.Fatalf("subscription after membership removal=%v, want ErrRevoked", err)
	}
	if _, ok := cachedChatGet(cache, cache.facts, chatCacheKey("home", "subject", "session")); ok {
		t.Fatal("removed principal kept its cached authority")
	}
}

// TestTodo_CHAT_012_GrantRevocationRevokesTheStream proves a revoked
// cross-company grant drops the cached policy and facts and closes every live
// subscription that consumer tenant holds on the conversation.
func TestTodo_CHAT_012_GrantRevocationRevokesTheStream(t *testing.T) {
	cache := newChatAuthorityCache(time.Minute, nil)
	cachedChatPut(cache, cache.policies, chatCacheKey("host", "c"), cachedChatPolicy{absent: true})
	cachedChatPut(cache, cache.facts, chatCacheKey("consumer", "guest", "session"), chatPrincipalFixture())
	cachedChatPut(cache, cache.facts, chatCacheKey("host", "employee", "session"), chatPrincipalFixture())
	revoker := &recordingRevoker{}
	grants := (&ChatCompanyGrants{}).withRevocation(cache, revoker)
	grants.invalidateGrant("host", "consumer", "c")
	if revoker.host != "host" || revoker.home != "consumer" || revoker.conversation != "c" {
		t.Fatalf("stream revocation=%+v", revoker)
	}
	if _, ok := cachedChatGet(cache, cache.policies, chatCacheKey("host", "c")); ok {
		t.Fatal("revoked grant kept the cached channel policy")
	}
	if _, ok := cachedChatGet(cache, cache.facts, chatCacheKey("consumer", "guest", "session")); ok {
		t.Fatal("revoked consumer tenant kept its cached facts")
	}
	if _, ok := cachedChatGet(cache, cache.facts, chatCacheKey("host", "employee", "session")); !ok {
		t.Fatal("host tenant facts dropped by a consumer-tenant revocation")
	}
}

type recordingRevoker struct{ host, home, conversation string }

func (r *recordingRevoker) RevokeTenant(host, home, conversation string) {
	r.host, r.home, r.conversation = host, home, conversation
}

// TestTodo_CHAT_010_ComposeChatFailsClosedWithoutAuthority proves an enabled
// chat surface is no longer composed with an authority that denies every call:
// outside local-dev, absent facts fail composition (and therefore startup)
// instead of mounting a route that can never answer. Disabled chat still
// composes to nothing.
func TestTodo_CHAT_010_ComposeChatFailsClosedWithoutAuthority(t *testing.T) {
	cfg := ServeConfig{ChatEnabled: true, Profile: "standard", ChatDatabaseURL: "postgres://chat@127.0.0.1:1/chat", DatabaseURL: "postgres://core@127.0.0.1:1/core"}
	_, err := composeChat(context.Background(), cfg, time.Now, nil, nil)
	if !errors.Is(err, ErrChatAuthorityUnavailable) {
		t.Fatalf("standard profile without facts=%v, want ErrChatAuthorityUnavailable", err)
	}
	disabled, err := composeChat(context.Background(), ServeConfig{}, time.Now, nil, nil)
	if err != nil || disabled.service != nil || disabled.extensions != nil || disabled.close != nil {
		t.Fatalf("disabled chat composed something: %+v err=%v", disabled, err)
	}
	// Local dev is the one profile allowed to run without a governance reader,
	// and it still fails on the unreachable chat database rather than silently.
	if _, err := composeChat(context.Background(), ServeConfig{ChatEnabled: true, Profile: ServeProfileLocalDev, ChatDatabaseURL: cfg.ChatDatabaseURL, DatabaseURL: cfg.DatabaseURL}, time.Now, nil, nil); errors.Is(err, ErrChatAuthorityUnavailable) {
		t.Fatalf("local dev refused for absent facts: %v", err)
	}
}

// TestTodo_CHAT_047_WatchReadsTravelTheAuditedRoutedChain proves the live
// stream's read and authorization ports are bound to the same decorated service
// as writes, rather than the bare chat service: a read through them reaches the
// store only after passing the routed/audited wrapper.
func TestTodo_CHAT_047_WatchReadsTravelTheAuditedRoutedChain(t *testing.T) {
	inner := newChatServiceStub()
	decorated := &auditedChatService{ConversationService: inner, records: &chatrecords.Service{Repo: chatrecords.NewMemoryRepository(), Auth: ChatRecordAuthority{}}, atomicCore: true}
	resolver := membershipResolverStub{member: chatcore.Membership{TenantID: "host", ConversationID: "c", HomeTenantID: "home", SubjectID: "subject", Revision: 4}}
	reader, authorizer := chatStreamPorts(decorated, resolver)
	if reader.service != chatcore.ConversationService(decorated) || authorizer.service != chatcore.ConversationService(decorated) {
		t.Fatal("stream ports bypass the routed and audited decorator chain")
	}
	if err := authorizer.Authorize(context.Background(), chatstream.Access{TenantID: "host", HomeTenantID: "home", SubjectID: "subject", ConversationID: "c", MembershipEpoch: 4}); err != nil {
		t.Fatalf("authorize through the chain: %v", err)
	}
	if inner.calls["GetConversation"] != 1 {
		t.Fatalf("authorization did not reach the store through the chain: %v", inner.calls)
	}
	// A stale epoch is still refused, and the durable event source is mandatory.
	if err := authorizer.Authorize(context.Background(), chatstream.Access{TenantID: "host", HomeTenantID: "home", SubjectID: "subject", ConversationID: "c", MembershipEpoch: 3}); !errors.Is(err, chatcore.ErrPermissionDenied) {
		t.Fatalf("stale epoch=%v", err)
	}
	if _, err := reader.Read(context.Background(), chatstream.ReadRequest{TenantID: "host", SubjectID: "subject", ConversationID: "c", Limit: 10}); !errors.Is(err, ErrChatStreamingDisabled) {
		t.Fatalf("reader without a durable event source=%v", err)
	}
}

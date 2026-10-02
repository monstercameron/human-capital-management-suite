package chatroutingadapter

import (
	"context"
	"errors"
	"testing"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
)

func TestStableConversationIDIsRetryStableAndTenantScoped(t *testing.T) {
	a := stableConversationID("tenant-a", "create-1")
	if a == "" || a != stableConversationID("tenant-a", "create-1") {
		t.Fatalf("id is not stable: %q", a)
	}
	if a == stableConversationID("tenant-b", "create-1") {
		t.Fatal("tenant crossed id namespace")
	}
	if _, err := New(nil, Options{Directory: chatrouting.NewMemoryDirectory(), DefaultShard: "s1"}); err == nil {
		t.Fatal("nil service accepted")
	}
}

func TestRequestConversationIDSeparatesActorsWithSameKey(t *testing.T) {
	a := chat.CreateConversationRequest{TenantID: "t1", IdempotencyKey: "same", Principal: chat.Principal{TenantID: "t1", SubjectID: "a"}}
	b := a
	b.Principal.SubjectID = "b"
	if requestConversationID(a) == requestConversationID(b) {
		t.Fatal("same key crossed actor boundary")
	}
}

type fakeService struct {
	chat.ConversationService
	createErr   error
	createCalls int
	sendCalls   int
	createLease chatrouting.WriteLease
	lastLease   chatrouting.WriteLease
}

func (f *fakeService) CreateConversation(ctx context.Context, r chat.CreateConversationRequest) (chat.Conversation, error) {
	f.createCalls++
	if lease, ok := chatrouting.WriteLeaseFromContext(ctx); ok {
		f.createLease = lease
	}
	if f.createErr != nil {
		return chat.Conversation{}, f.createErr
	}
	return chat.Conversation{ID: r.ConversationID, TenantID: r.TenantID, Kind: r.Kind, Name: r.Name, OwnerID: r.Principal.SubjectID}, nil
}
func (f *fakeService) SendPost(ctx context.Context, r chat.SendPostRequest) (chat.Post, error) {
	f.sendCalls++
	lease, ok := chatrouting.WriteLeaseFromContext(ctx)
	if !ok {
		return chat.Post{}, errors.New("missing route lease")
	}
	f.lastLease = lease
	return chat.Post{ID: "p1", TenantID: r.TenantID, ConversationID: r.ConversationID, AuthorID: r.Principal.SubjectID, Body: r.Body}, nil
}

type personaCommitFake struct {
	*fakeService
	commitCalls int
	commitLease chatrouting.WriteLease
}

func (f *personaCommitFake) CommitPersonaReply(ctx context.Context, r chat.PersonaReplyCommitRequest) (chat.Post, error) {
	f.commitCalls++
	lease, ok := chatrouting.WriteLeaseFromContext(ctx)
	if !ok {
		return chat.Post{}, errors.New("missing route lease")
	}
	f.commitLease = lease
	return chat.Post{ID: "persona-post", TenantID: r.TenantID, ConversationID: r.ConversationID, AuthorID: r.AuthorID, Body: r.Body}, nil
}

type validatingFake struct {
	*fakeService
	validationErr error
}

func (f validatingFake) ValidateCreate(context.Context, chat.CreateConversationRequest) error {
	return f.validationErr
}

// openStatus reports every channel as open, so tests of routing are not
// refused by the channel-status check that now runs before a write.
type openStatus struct{}

func (openStatus) GetChannelStatus(_ context.Context, r chat.GetConversationRequest) (chat.ChannelStatus, error) {
	return chat.ChannelStatus{TenantID: r.TenantID, ConversationID: r.ConversationID, Revision: 1}, nil
}
func (openStatus) AllowedStatusTransitions(context.Context, chat.GetConversationRequest) ([]chat.StatusTransition, error) {
	return nil, nil
}
func (openStatus) ChangeChannelStatus(context.Context, chat.ChangeChannelStatusRequest) (chat.ChannelStatus, error) {
	return chat.ChannelStatus{}, chat.ErrUnavailable
}
func (openStatus) CheckPersonaReplyChannelStatus(context.Context, chat.PersonaReplyCommitRequest) error {
	return nil
}

func newAdapter(t *testing.T, f chat.ConversationService) (*Service, *chatrouting.MemoryDirectory) {
	t.Helper()
	d := chatrouting.NewMemoryDirectory()
	s, err := New(f, Options{Directory: d, DefaultShard: "s1", ChannelStatus: openStatus{}})
	if err != nil {
		t.Fatal(err)
	}
	return s, d
}

func principalFor(tenant, subject string) chat.Principal {
	return chat.Principal{TenantID: tenant, SubjectID: subject}
}

func TestCreateReservesThenActivatesAndRetryUsesSameRoute(t *testing.T) {
	f := &fakeService{}
	s, d := newAdapter(t, f)
	r := chat.CreateConversationRequest{TenantID: "t1", Principal: principalFor("t1", "u1"), Kind: chat.PublicChannel, Name: "general", IdempotencyKey: "k1"}
	got, err := s.CreateConversation(context.Background(), r)
	if err != nil || got.ID == "" || f.createCalls != 1 {
		t.Fatalf("create = %+v err=%v calls=%d", got, err, f.createCalls)
	}
	if f.createLease.Route.ShardID != "s1" || f.createLease.Route.Epoch != 1 {
		t.Fatalf("create lease = %+v", f.createLease)
	}
	route, err := d.Lookup(context.Background(), got.ID, "t1")
	if err != nil || route.State != chatrouting.StateActive {
		t.Fatalf("route = %+v err=%v", route, err)
	}
	got, err = s.CreateConversation(context.Background(), r)
	if err != nil || got.ID != route.ConversationID || f.createCalls != 2 {
		t.Fatalf("retry = %+v err=%v calls=%d", got, err, f.createCalls)
	}
}

func TestCreateFailureLeavesInspectablePendingRoute(t *testing.T) {
	f := &fakeService{createErr: errors.New("chat unavailable")}
	s, d := newAdapter(t, f)
	r := chat.CreateConversationRequest{TenantID: "t1", Principal: principalFor("t1", "u1"), Kind: chat.PublicChannel, IdempotencyKey: "k1"}
	got, err := s.CreateConversation(context.Background(), r)
	if err == nil || got.ID != "" {
		t.Fatalf("failure = %+v err=%v", got, err)
	}
	id := requestConversationID(r)
	route, lookupErr := d.Lookup(context.Background(), id, "t1")
	if lookupErr != nil || route.State != chatrouting.StatePending {
		t.Fatalf("pending route = %+v err=%v", route, lookupErr)
	}
}

func TestCreatePreflightRejectsBeforeRouteReservation(t *testing.T) {
	f := &validatingFake{fakeService: &fakeService{}, validationErr: chat.ErrPermissionDenied}
	s, d := newAdapter(t, f)
	r := chat.CreateConversationRequest{TenantID: "t1", Principal: principalFor("t1", "u1"), Kind: chat.PublicChannel, IdempotencyKey: "k1"}
	if _, err := s.CreateConversation(context.Background(), r); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("error = %v", err)
	}
	if _, err := d.Lookup(context.Background(), requestConversationID(r), "t1"); !errors.Is(err, chatrouting.ErrNotFound) {
		t.Fatalf("route was reserved: %v", err)
	}
}

func TestSendPassesLeaseAndRejectsMovedEpoch(t *testing.T) {
	f := &fakeService{}
	s, d := newAdapter(t, f)
	route, _ := d.Reserve(context.Background(), chatrouting.ReserveRequest{ConversationID: "c1", HostTenantID: "t1", ShardID: "s1", IdempotencyKey: "k"})
	_, _ = d.Activate(context.Background(), "c1", "t1", route.Epoch)
	req := chat.SendPostRequest{TenantID: "t1", ConversationID: "c1", Principal: principalFor("t1", "u1"), Body: "hello"}
	if _, err := s.SendPost(context.Background(), req); err != nil || f.sendCalls != 1 || f.lastLease.Route.Epoch != 1 {
		t.Fatalf("send err=%v calls=%d lease=%+v", err, f.sendCalls, f.lastLease)
	}
	_, err := d.BeginMove(context.Background(), "c1", "t1", 1, "s2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SendPost(context.Background(), req); !errors.Is(err, chatrouting.ErrStaleEpoch) && !errors.Is(err, chatrouting.ErrNotWritable) {
		t.Fatalf("moved send = %v", err)
	}
	if f.sendCalls != 1 {
		t.Fatalf("stale send reached chat: %d", f.sendCalls)
	}
}

func personaReplyRequest(tenant, conversation string) chat.PersonaReplyCommitRequest {
	return chat.PersonaReplyCommitRequest{
		TenantID: tenant, ConversationID: conversation, AuthorID: "persona", AuthorHomeTenantID: tenant,
		Body: "answer", IdempotencyKey: "persona-reply-1", ExpectedAudienceRevision: 1,
	}
}

func TestCommitPersonaReplyPassesCurrentRouteLease(t *testing.T) {
	f := &personaCommitFake{fakeService: &fakeService{}}
	s, d := newAdapter(t, f)
	route, err := d.Reserve(context.Background(), chatrouting.ReserveRequest{ConversationID: "c-persona", HostTenantID: "t1", ShardID: "s1", IdempotencyKey: "persona-route"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Activate(context.Background(), "c-persona", "t1", route.Epoch); err != nil {
		t.Fatal(err)
	}
	post, err := s.CommitPersonaReply(context.Background(), personaReplyRequest("t1", "c-persona"))
	if err != nil || post.ID != "persona-post" {
		t.Fatalf("commit = %+v err=%v", post, err)
	}
	if f.commitCalls != 1 || f.commitLease.Route.HostTenantID != "t1" || f.commitLease.Route.Epoch != route.Epoch {
		t.Fatalf("calls=%d lease=%+v", f.commitCalls, f.commitLease)
	}
}

func TestCommitPersonaReplyRejectsStaleRouteBeforeStore(t *testing.T) {
	f := &personaCommitFake{fakeService: &fakeService{}}
	s, d := newAdapter(t, f)
	route, err := d.Reserve(context.Background(), chatrouting.ReserveRequest{ConversationID: "c-persona", HostTenantID: "t1", ShardID: "s1", IdempotencyKey: "persona-route"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Activate(context.Background(), "c-persona", "t1", route.Epoch); err != nil {
		t.Fatal(err)
	}
	if _, err = d.BeginMove(context.Background(), "c-persona", "t1", route.Epoch, "s2"); err != nil {
		t.Fatal(err)
	}
	_, err = s.CommitPersonaReply(context.Background(), personaReplyRequest("t1", "c-persona"))
	if !errors.Is(err, chatrouting.ErrStaleEpoch) && !errors.Is(err, chatrouting.ErrNotWritable) {
		t.Fatalf("stale commit = %v", err)
	}
	if f.commitCalls != 0 {
		t.Fatalf("stale commit reached chat: %d", f.commitCalls)
	}
}

func TestCommitPersonaReplyRejectsForeignTenantRouteBeforeStore(t *testing.T) {
	f := &personaCommitFake{fakeService: &fakeService{}}
	s, d := newAdapter(t, f)
	route, err := d.Reserve(context.Background(), chatrouting.ReserveRequest{ConversationID: "c-persona", HostTenantID: "t1", ShardID: "s1", IdempotencyKey: "persona-route"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Activate(context.Background(), "c-persona", "t1", route.Epoch); err != nil {
		t.Fatal(err)
	}
	_, err = s.CommitPersonaReply(context.Background(), personaReplyRequest("t2", "c-persona"))
	if !errors.Is(err, chatrouting.ErrTenant) {
		t.Fatalf("foreign-tenant commit = %v", err)
	}
	if f.commitCalls != 0 {
		t.Fatalf("foreign-tenant commit reached chat: %d", f.commitCalls)
	}
}

func TestReferenceMethodsFailClosedWithoutExtension(t *testing.T) {
	s, _ := newAdapter(t, &fakeService{})
	ctx := context.Background()
	if _, err := s.SuggestReferences(ctx, chat.SuggestReferencesRequest{}); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("suggest = %v", err)
	}
	if _, err := s.SendPostWithReferences(ctx, chat.SendPostWithReferencesRequest{}); !errors.Is(err, chat.ErrNotFound) && !errors.Is(err, chat.ErrUnavailable) && !errors.Is(err, chatrouting.ErrInvalid) {
		t.Fatalf("send refs = %v", err)
	}
	if _, err := s.CreateShareLink(ctx, chat.Principal{}, "", "", ""); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("create link = %v", err)
	}
	if _, _, err := s.ResolveShareLink(ctx, chat.Principal{}, ""); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("resolve link = %v", err)
	}
	if _, err := s.ForwardPost(ctx, chat.ForwardPostRequest{}); !errors.Is(err, chat.ErrNotFound) && !errors.Is(err, chat.ErrUnavailable) && !errors.Is(err, chatrouting.ErrInvalid) {
		t.Fatalf("forward = %v", err)
	}
}

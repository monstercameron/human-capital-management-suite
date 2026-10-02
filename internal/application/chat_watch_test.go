package application

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatadmission"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatstream"
)

func watchService(t *testing.T) (*streamingChatService, *chatServiceStub) {
	t.Helper()
	config := runtimeConfig("lane-key")
	config.Budgets = defaultChatAdmissionConfig()
	capture := &watchSequenceReader{offsets: make(chan uint64, 8)}
	config.Reader = capture
	runtime, err := NewChatStreamRuntime(config)
	if err != nil {
		t.Fatal(err)
	}
	inner := newChatServiceStub()
	resolver := &watchSequenceResolver{membershipResolverStub: membershipResolverStub{member: chatcore.Membership{TenantID: "t", ConversationID: "c", HomeTenantID: "t", SubjectID: "u", Revision: 3}}}
	return &streamingChatService{ConversationService: inner, runtime: runtime, membership: resolver}, inner
}

type watchSequenceReader struct{ offsets chan uint64 }

func (r *watchSequenceReader) Read(_ context.Context, request chatstream.ReadRequest) (chatstream.Page, error) {
	select {
	case r.offsets <- request.AfterSequence:
	default:
	}
	return chatstream.Page{Complete: true}, nil
}

type watchSequenceResolver struct {
	membershipResolverStub
	called bool
	public uint64
}

func (r *watchSequenceResolver) ResolvePublicSequence(_ context.Context, _, _ string, sequence uint64) (uint64, error) {
	r.called, r.public = true, sequence
	return sequence + 100, nil
}

func watchRequest() chatcore.WatchConversationRequest {
	return chatcore.WatchConversationRequest{Principal: chatcore.Principal{TenantID: "t", SubjectID: "u"}, TenantID: "t", ConversationID: "c"}
}

type watchRouteEpochService struct {
	*chatServiceStub
	epoch atomic.Uint64
}

func (s *watchRouteEpochService) RouteEpoch(context.Context, string, string) (uint64, error) {
	return s.epoch.Load(), nil
}

func TestTodo_CHAT_019_ApplicationRouteEpochInvalidatesResumeAndLiveWatch(t *testing.T) {
	inner := newChatServiceStub()
	routeService := &watchRouteEpochService{chatServiceStub: inner}
	routeService.epoch.Store(1)
	resolver := membershipResolverStub{member: chatcore.Membership{TenantID: "t", ConversationID: "c", HomeTenantID: "t", SubjectID: "u", Revision: 3}}
	config := runtimeConfig("route-epoch-key")
	config.RecheckInterval = 10 * time.Millisecond
	config.Authorizer = chatServiceStreamAuthorizer{service: routeService, membership: resolver}
	runtime, err := NewChatStreamRuntime(config)
	if err != nil {
		t.Fatal(err)
	}
	service := &streamingChatService{ConversationService: routeService, runtime: runtime, membership: resolver}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	sub, lease, err := runtime.Watch(ctx, chatstream.WatchRequest{TenantID: "t", HomeTenantID: "t", SubjectID: "u", ConversationID: "c", MembershipEpoch: 3, RouteEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	cursor := sub.Cursor()
	sub.Close()
	// The runtime releases the watch lease itself once the subscription is
	// done, so by now the lease is either still ours to release or already
	// released; any other answer is a fault.
	if err := lease.Release(); err != nil && !errors.Is(err, chatadmission.ErrReleased) {
		t.Fatal(err)
	}
	routeService.epoch.Store(2)
	resume := watchRequest()
	resume.ResumeCursor = cursor
	if _, _, err := service.WatchConversationWithErrors(ctx, resume); !errors.Is(err, chatcore.ErrInvalidArgument) || !errors.Is(err, chatstream.ErrInvalidCursor) {
		t.Fatalf("resume after route epoch change = %v; want owned invalid cursor", err)
	}

	routeService.epoch.Store(1)
	events, failures, err := service.WatchConversationWithErrors(ctx, watchRequest())
	if err != nil {
		t.Fatal(err)
	}
	routeService.epoch.Store(2)
	select {
	case failure, ok := <-failures:
		if !ok || !errors.Is(failure, chatcore.ErrPermissionDenied) {
			t.Fatalf("live stream after route epoch change = %v (open=%v); want permission denied", failure, ok)
		}
	case event, ok := <-events:
		if ok {
			t.Fatalf("event delivered after route epoch change: %+v", event)
		}
		t.Fatal("stream closed without a route epoch failure")
	case <-ctx.Done():
		t.Fatal("route epoch change did not close the live stream")
	}
}

// TestTodo_CHAT_018_WatchResumesFromAfterSequence proves a client that knows the
// last sequence it rendered can resubscribe. after_sequence used to be refused
// outright here, and the transport worked around it by handing the number over
// as if it were a signed resume cursor, which the stream then rejected: every
// such resubscribe died as an unclassified internal failure.
func TestTodo_CHAT_018_WatchResumesFromAfterSequence(t *testing.T) {
	service, _ := watchService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := watchRequest()
	req.AfterSequence = 41
	ch, err := service.WatchConversation(ctx, req)
	if err != nil || ch == nil {
		t.Fatalf("watch with after_sequence = %v", err)
	}
	mapper := service.membership.(*watchSequenceResolver)
	if !mapper.called || mapper.public != 41 {
		t.Fatalf("public after_sequence was not resolved to the stream offset: %+v", mapper)
	}
	reader := service.runtime.bridge.Reader.(*watchSequenceReader)
	select {
	case offset := <-reader.offsets:
		if offset != 141 {
			t.Fatalf("reader received offset %d; want resolved global offset 141", offset)
		}
	case <-time.After(time.Second):
		t.Fatal("stream reader did not receive the mapped resume offset")
	}
	service.membership = membershipResolverStub{member: chatcore.Membership{TenantID: "t", ConversationID: "c", HomeTenantID: "t", SubjectID: "u", Revision: 3}}
	if _, _, err := service.WatchConversationWithErrors(ctx, watchRequestWithAfterSequence(42)); !errors.Is(err, chatcore.ErrUnavailable) {
		t.Fatalf("public after_sequence without mapper = %v; want fail-closed unavailable", err)
	}

	// Naming both positions is still a bad request rather than a precedence rule.
	both := watchRequest()
	both.AfterSequence, both.ResumeCursor = 41, "cs1.x.y"
	if _, err := service.WatchConversation(ctx, both); !errors.Is(err, chatcore.ErrInvalidArgument) {
		t.Fatalf("both positions = %v, want ErrInvalidArgument", err)
	}
}

func watchRequestWithAfterSequence(sequence uint64) chatcore.WatchConversationRequest {
	req := watchRequest()
	req.AfterSequence = sequence
	return req
}

// TestTodo_CHAT_018_WatchSubscribeFailuresAreNamed proves the cause of a failed
// subscribe reaches the caller as a chat condition instead of an opaque wrapped
// sentinel the transport can only log as transport.unclassified_failure.
func TestTodo_CHAT_018_WatchSubscribeFailuresAreNamed(t *testing.T) {
	service, _ := watchService(t)
	bad := watchRequest()
	bad.ResumeCursor = "cs1.not-a-cursor.signature"
	_, err := service.WatchConversation(context.Background(), bad)
	if !errors.Is(err, chatcore.ErrInvalidArgument) {
		t.Fatalf("unverifiable cursor = %v, want ErrInvalidArgument", err)
	}
	if !errors.Is(err, chatstream.ErrInvalidCursor) {
		t.Fatalf("the stream cause was thrown away: %v", err)
	}
}

// TestTodo_CHAT_046_WatchesDoNotShedConversationReads is the browser session's
// ListPosts failure in one test. A hundred watches opened and cancelled on one
// conversation used to fill that conversation's request budget, whose
// utilisation is what sheds the read lane, so a plain ListPosts came back as
// chat.service.unavailable. Subscriptions hold their own scope now.
func TestTodo_CHAT_046_WatchesDoNotShedConversationReads(t *testing.T) {
	service, inner := watchService(t)
	runtime := service.runtime
	ctx := context.Background()
	req := chatstream.WatchRequest{TenantID: "t", HomeTenantID: "t", SubjectID: "u", ConversationID: "c", MembershipEpoch: 3}
	// A hundred watches held open at once: far past ConversationConcurrent, which
	// is what used to make this a refusal.
	held := make([]*chatstream.Subscription, 0, 100)
	for i := 0; i < 100; i++ {
		watchCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		sub, _, err := runtime.Watch(watchCtx, req)
		if err != nil {
			t.Fatalf("watch %d = %v", i, err)
		}
		held = append(held, sub)
	}
	if _, err := service.ListPosts(ctx, chatcore.ListPostsRequest{TenantID: "t", ConversationID: "c"}); err != nil {
		t.Fatalf("ListPosts with 100 open watches = %v", err)
	}
	// And after they all go away, the read still lands and the scopes settle.
	for _, sub := range held {
		sub.Close()
		<-sub.Done()
	}
	if _, err := service.ListPosts(ctx, chatcore.ListPostsRequest{TenantID: "t", ConversationID: "c"}); err != nil {
		t.Fatalf("ListPosts after 100 cancelled watches = %v", err)
	}
	if inner.calls["ListPosts"] != 2 {
		t.Fatalf("reads that reached the store = %v", inner.calls)
	}
}

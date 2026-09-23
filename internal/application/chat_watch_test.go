package application

import (
	"context"
	"errors"
	"testing"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatstream"
)

func watchService(t *testing.T) (*streamingChatService, *chatServiceStub) {
	t.Helper()
	runtime := laneRuntime(t, defaultChatAdmissionConfig())
	inner := newChatServiceStub()
	resolver := membershipResolverStub{member: chatcore.Membership{TenantID: "t", ConversationID: "c", HomeTenantID: "t", SubjectID: "u", Revision: 3}}
	return &streamingChatService{ConversationService: inner, runtime: runtime, membership: resolver}, inner
}

func watchRequest() chatcore.WatchConversationRequest {
	return chatcore.WatchConversationRequest{Principal: chatcore.Principal{TenantID: "t", SubjectID: "u"}, TenantID: "t", ConversationID: "c"}
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

	// Naming both positions is still a bad request rather than a precedence rule.
	both := watchRequest()
	both.AfterSequence, both.ResumeCursor = 41, "cs1.x.y"
	if _, err := service.WatchConversation(ctx, both); !errors.Is(err, chatcore.ErrInvalidArgument) {
		t.Fatalf("both positions = %v, want ErrInvalidArgument", err)
	}
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

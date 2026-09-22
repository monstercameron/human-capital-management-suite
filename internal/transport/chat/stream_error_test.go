package chat

import (
	"context"
	"errors"
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"google.golang.org/grpc"
)

// fakeServerStream is the gRPC streaming surface the generated handler is given.
// Send fails on demand, which is the one failure the stream loop produces itself.
type fakeServerStream struct {
	grpc.ServerStream
	ctx      context.Context
	sendErr  error
	sendCall int
}

func (s *fakeServerStream) Context() context.Context { return s.ctx }
func (s *fakeServerStream) Send(*chatv1.WatchConversationResponse) error {
	s.sendCall++
	return s.sendErr
}
func (s *fakeServerStream) SendMsg(any) error { return s.sendErr }
func (s *fakeServerStream) RecvMsg(any) error { return nil }

// TestTodo_CHAT_018_GRPCStreamPathOwnsItsErrors proves the native gRPC streaming
// handler — the path the workspace tunnel actually bridges — names its failures.
// It used to hand grpc-go a raw error, which the edge coerced into
// transport.unclassified_failure with no reason at all, so the whole class of
// watch failures was invisible in the request log.
func TestTodo_CHAT_018_GRPCStreamPathOwnsItsErrors(t *testing.T) {
	ctx := admittedChatContext(t)

	// A subscribe failure the core names.
	denied := &watchRecorder{transportChatFake: &transportChatFake{}, err: chatcore.ErrPermissionDenied}
	s := &server{deps: Dependencies{Service: denied}}
	err := s.WatchConversation(&chatv1.WatchConversationRequest{TenantId: "server", ConversationId: "c"}, &fakeServerStream{ctx: ctx})
	owned, ok := envelope.As(err)
	if !ok || owned.Code() != envelope.CodePermissionDenied {
		t.Fatalf("denied subscribe over gRPC = %v (owned=%v)", err, ok)
	}

	// A subscribe failure nothing recognises still leaves owned, with a reason
	// and a non-success code, so the log record can classify it.
	unknown := &watchRecorder{transportChatFake: &transportChatFake{}, err: errors.New("outbox payload key missing")}
	s = &server{deps: Dependencies{Service: unknown}}
	err = s.WatchConversation(&chatv1.WatchConversationRequest{TenantId: "server", ConversationId: "c"}, &fakeServerStream{ctx: ctx})
	owned, ok = envelope.As(err)
	if !ok {
		t.Fatalf("unrecognised subscribe failure is unowned: %v", err)
	}
	if owned.Code() != envelope.CodeUnavailable || owned.ReasonRef() != reasonStreamFailure {
		t.Fatalf("unrecognised subscribe failure = %v/%v", owned.Code(), owned.ReasonRef())
	}
	if _, hasDiagnostic := owned.Diagnostic(allowDiagnostics{}); !hasDiagnostic {
		t.Fatal("the cause was thrown away instead of kept as a diagnostic")
	}

	// A send failure mid-stream is named too, on the same path.
	feed := &watchRecorder{transportChatFake: &transportChatFake{}, events: []chatcore.WatchEvent{{Event: chatcore.ConversationEvent{Kind: chatcore.PostCreated, Sequence: 1}, ResumeCursor: "cs1.a.b"}}}
	s = &server{deps: Dependencies{Service: feed}}
	stream := &fakeServerStream{ctx: ctx, sendErr: errors.New("tunnel write failed")}
	err = s.WatchConversation(&chatv1.WatchConversationRequest{TenantId: "server", ConversationId: "c"}, stream)
	owned, ok = envelope.As(err)
	if !ok || owned.ReasonRef() != reasonStreamSend {
		t.Fatalf("send failure = %v (owned=%v)", err, ok)
	}
	if stream.sendCall != 1 {
		t.Fatalf("sends attempted = %d", stream.sendCall)
	}

	// The Connect projection shares the loop, so it answers identically.
	connectErr := streamErr(errors.New("outbox payload key missing"))
	connectOwned, ok := envelope.As(connectErr)
	if !ok || connectOwned.ReasonRef() != reasonStreamFailure {
		t.Fatalf("connect path = %v (owned=%v)", connectErr, ok)
	}
}

type allowDiagnostics struct{}

func (allowDiagnostics) AllowsInternalDiagnostics() bool { return true }

package chat_test

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportchat "github.com/monstercameron/human-capital-management-suite/internal/transport/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// watchHarness is the real gRPC boundary: the shared unary and stream
// interceptors, a TCP listener and a generated client, which is the path the
// workspace tunnel bridges. A shortened stream cap keeps the deadline assertion
// fast without pretending about the mechanism.
type watchHarness struct {
	client chatv1.ConversationServiceClient
	f      fixture
	logged []transport.LogRecord
}

func newWatchHarness(t *testing.T, streamCap time.Duration) *watchHarness {
	t.Helper()
	h := &watchHarness{f: newFixture(t)}
	cfg := transport.Config{
		Verifier: chatVerifier{}, NewRequestID: func() string { return "watch-lifetime" },
		MaxDeadline: 300 * time.Millisecond, MaxStreamDeadline: streamCap,
		Logger: transport.LoggerFunc(func(r transport.LogRecord) { h.logged = append(h.logged, r) }),
	}
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(grpcserver.UnaryInterceptor(cfg)),
		grpc.ChainStreamInterceptor(grpcserver.StreamInterceptor(cfg)),
	)
	transportchat.Register(srv, transportchat.Dependencies{Service: h.f.service})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() { srv.Stop(); _ = lis.Close() })
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	h.client = chatv1.NewConversationServiceClient(conn)
	return h
}

func (h *watchHarness) callContext(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer chat-token", transport.RequestIDMetadataKey, "watch-lifetime-1")
}

// TestTodo_CHAT_018_WatchStaysOpenThroughTheGRPCBoundary is the browser's
// give-up loop. Every watch ended cleanly with EOF 10-150 ms after it opened, so
// the client retried five times and stopped. Both shapes the client opens are
// covered: a brand-new room with no posts, and a room that already has one, each
// with no cursor and after_sequence 0.
func TestTodo_CHAT_018_WatchStaysOpenThroughTheGRPCBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, key string
		seed      bool
	}{
		{name: "empty conversation", key: "watch-empty", seed: false},
		{name: "conversation with a post", key: "watch-seeded", seed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newWatchHarness(t, 10*time.Minute)
			ctx := context.Background()
			p := principal("company-a", "alice")
			c, err := h.f.service.CreateConversation(ctx, chatcore.CreateConversationRequest{Principal: p, TenantID: p.TenantID, Kind: chatcore.PublicChannel, Name: tc.key, OwnerID: p.SubjectID, IdempotencyKey: tc.key})
			if err != nil {
				t.Fatal(err)
			}
			if tc.seed {
				if _, err := h.f.service.SendPost(ctx, chatcore.SendPostRequest{Principal: p, TenantID: p.TenantID, ConversationID: c.ID, Body: "already here", IdempotencyKey: tc.key + "-seed"}); err != nil {
					t.Fatal(err)
				}
			}
			streamCtx, cancel := context.WithCancel(h.callContext(ctx))
			defer cancel()
			stream, err := h.client.WatchConversation(streamCtx, &chatv1.WatchConversationRequest{TenantId: p.TenantID, ConversationId: c.ID})
			if err != nil {
				t.Fatalf("open watch: %v", err)
			}
			opened := time.Now()

			// A post sent five seconds into the stream, well past the window in
			// which every live watch used to die.
			sent := make(chan error, 1)
			go func() {
				time.Sleep(5 * time.Second)
				_, sendErr := h.f.service.SendPost(ctx, chatcore.SendPostRequest{Principal: p, TenantID: p.TenantID, ConversationID: c.ID, Body: "live", IdempotencyKey: tc.key + "-live"})
				sent <- sendErr
			}()

			for {
				msg, recvErr := stream.Recv()
				if recvErr != nil {
					if errors.Is(recvErr, io.EOF) || status.Code(recvErr) == codes.OK {
						t.Fatalf("stream ended cleanly after %v without the live post", time.Since(opened))
					}
					t.Fatalf("stream ended after %v: %v", time.Since(opened), recvErr)
				}
				if post := msg.GetEvent().GetPost(); post != nil && post.GetBody() == "live" {
					break
				}
			}
			if elapsed := time.Since(opened); elapsed < 5*time.Second {
				t.Fatalf("the live post arrived after %v, before it was sent", elapsed)
			}
			if err := <-sent; err != nil {
				t.Fatalf("send while watching: %v", err)
			}
		})
	}
}

// TestTodo_CHAT_018_StreamIsNotCutByTheUnaryDeadlineCap proves a server stream is
// bounded by the stream ceiling and not by the unary request budget. Both caps
// are shortened here; the stream must outlive the unary one, which is what ended
// every chat and journey watch at exactly thirty seconds.
func TestTodo_CHAT_018_StreamIsNotCutByTheUnaryDeadlineCap(t *testing.T) {
	h := newWatchHarness(t, 4*time.Second)
	ctx := context.Background()
	p := principal("company-a", "alice")
	c, err := h.f.service.CreateConversation(ctx, chatcore.CreateConversationRequest{Principal: p, TenantID: p.TenantID, Kind: chatcore.PublicChannel, Name: "cap", OwnerID: p.SubjectID, IdempotencyKey: "watch-cap"})
	if err != nil {
		t.Fatal(err)
	}
	streamCtx, cancel := context.WithCancel(h.callContext(ctx))
	defer cancel()
	stream, err := h.client.WatchConversation(streamCtx, &chatv1.WatchConversationRequest{TenantId: p.TenantID, ConversationId: c.ID})
	if err != nil {
		t.Fatal(err)
	}
	opened := time.Now()
	for {
		if _, recvErr := stream.Recv(); recvErr != nil {
			elapsed := time.Since(opened)
			// The unary cap is 300 ms. A stream that respected it would be gone
			// long before the stream cap.
			if elapsed < 2*time.Second {
				t.Fatalf("stream ended after %v: %v — the unary cap is still being applied", elapsed, recvErr)
			}
			return
		}
	}
}

package cell

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/edge"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

type chat009ParityOwner struct {
	chatcore.ConversationService
	updates []chatcore.UpdateConversationRequest
	creates []chatcore.CreateConversationRequest
	created map[string]chatcore.Conversation
}

func (s *chat009ParityOwner) CreateConversation(_ context.Context, r chatcore.CreateConversationRequest) (chatcore.Conversation, error) {
	s.creates = append(s.creates, r)
	if s.created == nil {
		s.created = make(map[string]chatcore.Conversation)
	}
	if existing, ok := s.created[r.IdempotencyKey]; ok {
		if existing.TenantID != r.TenantID || existing.Kind != r.Kind || existing.Name != r.Name {
			return chatcore.Conversation{}, chatcore.ErrConflict
		}
		return existing, nil
	}
	created := chatcore.Conversation{
		ID: "conversation-1", TenantID: r.TenantID, Kind: r.Kind,
		Name: r.Name, OwnerID: r.Principal.SubjectID, Revision: 1,
	}
	s.created[r.IdempotencyKey] = created
	return created, nil
}

func (s *chat009ParityOwner) UpdateConversation(_ context.Context, r chatcore.UpdateConversationRequest) (chatcore.Conversation, error) {
	s.updates = append(s.updates, r)
	if r.ExpectedRevision != 1 {
		return chatcore.Conversation{}, chatcore.ErrConflict
	}
	r.Conversation.Revision = 2
	return r.Conversation, nil
}

// TestTodo_CHAT_009_IntegrationHTTPAndRPCRevisionParity reaches the real chat
// gRPC registration and production HTTP overlay. Both paths use the shared
// admission function and same owner contract; successful revisions and stale
// revision errors must retain the same public result.
func TestTodo_CHAT_009_IntegrationHTTPAndRPCRevisionParity(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	verifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	token, err := transporttest.BearerToken(verifier, transporttest.DefaultClaims(now))
	if err != nil {
		t.Fatal(err)
	}
	cfg := transporttest.Config(verifier, func() time.Time { return now }, "chat-parity", nil)

	for _, expected := range []uint64{1, 0} {
		t.Run(map[uint64]string{1: "current_revision", 0: "stale_revision"}[expected], func(t *testing.T) {
			rpcOwner := &chat009ParityOwner{}
			rpcServer := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
				incoming, _ := metadata.FromIncomingContext(ctx)
				md := transport.MapMetadata{}
				for key, values := range incoming {
					md[key] = append([]string(nil), values...)
				}
				message, ok := req.(proto.Message)
				if !ok {
					return nil, envelope.New(envelope.CodeInvalidArgument, "chat.invalid_argument", "request is not a protobuf message")
				}
				admitted, _, admitErr := transport.Admit(ctx, cfg, transport.AdmissionRequest{
					Metadata: md, Method: info.FullMethod, Kind: transport.KindGRPC, Message: message,
				})
				if admitErr != nil {
					return nil, admitErr
				}
				return handler(admitted, req)
			}))
			chat.Register(rpcServer, chat.Dependencies{Service: rpcOwner})
			listener := bufconn.Listen(1 << 20)
			go func() { _ = rpcServer.Serve(listener) }()
			t.Cleanup(func() {
				rpcServer.Stop()
				_ = listener.Close()
			})
			conn, err := grpc.NewClient("passthrough:///chat-parity",
				grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
				grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			rpcClient := chatv1.NewConversationServiceClient(conn)

			httpOwner := &chat009ParityOwner{}
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
			httpServer := httptest.NewServer(chatHTTPOverlay(next, cfg, httpOwner))
			t.Cleanup(httpServer.Close)
			httpClient := connect.NewClient[chatv1.UpdateConversationRequest, chatv1.UpdateConversationResponse](
				httpServer.Client(), httpServer.URL+chatv1.ConversationService_UpdateConversation_FullMethodName, connect.WithProtoJSON(),
			)
			rpcCreateClient := chatv1.NewConversationServiceClient(conn)
			httpCreateClient := connect.NewClient[chatv1.CreateConversationRequest, chatv1.CreateConversationResponse](
				httpServer.Client(), httpServer.URL+chatv1.ConversationService_CreateConversation_FullMethodName, connect.WithProtoJSON(),
			)

			request := &chatv1.UpdateConversationRequest{
				Conversation:     &chatv1.Conversation{Id: "conversation-1", TenantId: "server", Name: "Operations"},
				ExpectedRevision: expected,
			}
			rpcCtx := metadata.AppendToOutgoingContext(context.Background(), transport.AuthorizationMetadataKey, token)
			rpcResponse, rpcErr := rpcClient.UpdateConversation(rpcCtx, request)
			httpRequest := connect.NewRequest(proto.Clone(request).(*chatv1.UpdateConversationRequest))
			httpRequest.Header().Set(transport.AuthorizationMetadataKey, token)
			httpResponse, httpErr := httpClient.CallUnary(context.Background(), httpRequest)
			if (rpcErr == nil) != (httpErr == nil) {
				t.Fatalf("gRPC error = %v; HTTP error = %v", rpcErr, httpErr)
			}
			if rpcErr != nil {
				rpcOwned, ok := envelope.FromGRPC(rpcErr)
				if !ok {
					t.Fatalf("gRPC error is not an owned error: %v", rpcErr)
				}
				httpOwned, ok := edge.FromConnectError(httpErr)
				if !ok {
					t.Fatalf("HTTP error is not an owned Connect error: %v", httpErr)
				}
				rpcDetail, httpDetail := rpcOwned.Detail(), httpOwned.Detail()
				// HTTP admission adds its request correlation and credential
				// evidence; those values are deliberately surface-specific.
				rpcDetail.CorrelationId, httpDetail.CorrelationId = "", ""
				rpcDetail.EvidenceRef, httpDetail.EvidenceRef = nil, nil
				if rpcOwned.Code() != envelope.CodeAborted || httpOwned.Code() != rpcOwned.Code() || httpOwned.ReasonRef() != rpcOwned.ReasonRef() || !proto.Equal(httpDetail, rpcDetail) {
					t.Fatalf("owned failures differ: gRPC=%+v HTTP=%+v", rpcOwned.Detail(), httpOwned.Detail())
				}
			} else if !proto.Equal(rpcResponse, httpResponse.Msg) {
				t.Fatalf("HTTP response = %v, gRPC response = %v", httpResponse.Msg, rpcResponse)
			}
			if len(rpcOwner.updates) != 1 || len(httpOwner.updates) != 1 {
				t.Fatalf("owner calls: gRPC %d, HTTP %d", len(rpcOwner.updates), len(httpOwner.updates))
			}
			if rpcOwner.updates[0].ExpectedRevision != expected || httpOwner.updates[0].ExpectedRevision != expected {
				t.Fatalf("expected revisions: gRPC %d, HTTP %d, want %d", rpcOwner.updates[0].ExpectedRevision, httpOwner.updates[0].ExpectedRevision, expected)
			}
			if rpcOwner.updates[0].Principal.TenantID != httpOwner.updates[0].Principal.TenantID || rpcOwner.updates[0].Principal.SubjectID != httpOwner.updates[0].Principal.SubjectID {
				t.Fatalf("owner principals differ: gRPC %+v, HTTP %+v", rpcOwner.updates[0].Principal, httpOwner.updates[0].Principal)
			}
			if rpcOwner.updates[0].Principal.TenantID == "" || rpcOwner.updates[0].Principal.SubjectID == "" {
				t.Fatal("authenticated actor was not passed to the owner")
			}

			// Repeating the same create key must reuse the owner's result on
			// both surfaces; changing its payload under that key must preserve
			// the canonical conflict outcome.
			createReq := &chatv1.CreateConversationRequest{
				TenantId: "server", Kind: chatv1.ConversationKind_CONVERSATION_KIND_GROUP,
				Name: "Operations", IdempotencyKey: "idem-1",
			}
			for range 2 {
				rpcCreated, rpcCreateErr := rpcCreateClient.CreateConversation(rpcCtx, createReq)
				httpCreateReq := connect.NewRequest(proto.Clone(createReq).(*chatv1.CreateConversationRequest))
				httpCreateReq.Header().Set(transport.AuthorizationMetadataKey, token)
				httpCreated, httpCreateErr := httpCreateClient.CallUnary(context.Background(), httpCreateReq)
				if rpcCreateErr != nil || httpCreateErr != nil {
					t.Fatalf("idempotent create errors: gRPC=%v HTTP=%v", rpcCreateErr, httpCreateErr)
				}
				if !proto.Equal(rpcCreated, httpCreated.Msg) || rpcCreated.GetConversation().GetId() != "conversation-1" {
					t.Fatalf("idempotent create differs: gRPC=%v HTTP=%v", rpcCreated, httpCreated.Msg)
				}
			}
			changedReq := proto.Clone(createReq).(*chatv1.CreateConversationRequest)
			changedReq.Name = "Payroll"
			_, rpcCreateErr := rpcCreateClient.CreateConversation(rpcCtx, changedReq)
			httpChangedReq := connect.NewRequest(changedReq)
			httpChangedReq.Header().Set(transport.AuthorizationMetadataKey, token)
			_, httpCreateErr := httpCreateClient.CallUnary(context.Background(), httpChangedReq)
			rpcOwned, rpcOwnedOK := envelope.FromGRPC(rpcCreateErr)
			httpOwned, httpOwnedOK := edge.FromConnectError(httpCreateErr)
			if !rpcOwnedOK || !httpOwnedOK || rpcOwned.Code() != envelope.CodeAborted || httpOwned.Code() != rpcOwned.Code() || httpOwned.ReasonRef() != rpcOwned.ReasonRef() {
				t.Fatalf("idempotency conflict differs: gRPC=%v HTTP=%v", rpcCreateErr, httpCreateErr)
			}
			if len(rpcOwner.creates) != 3 || len(httpOwner.creates) != 3 {
				t.Fatalf("idempotency owner calls: gRPC %d HTTP %d, want 3 each", len(rpcOwner.creates), len(httpOwner.creates))
			}
		})
	}
}

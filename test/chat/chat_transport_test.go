package chat_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportchat "github.com/monstercameron/human-capital-management-suite/internal/transport/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

func TestTodo_CHAT_051_TransportConformance(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p := principal("company-a", "alice")

	verifier := chatVerifier{}
	cfg := transport.Config{Verifier: verifier, NewRequestID: func() string { return "chat-transport-test" }}

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(grpcserver.UnaryInterceptor(cfg)),
		grpc.ChainStreamInterceptor(grpcserver.StreamInterceptor(cfg)),
	)
	transportchat.Register(grpcServer, transportchat.Dependencies{Service: f.service})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen gRPC: %v", err)
	}
	go func() { _ = grpcServer.Serve(lis) }()
	t.Cleanup(func() { grpcServer.Stop(); _ = lis.Close() })
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial gRPC: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	grpcClient := chatv1.NewConversationServiceClient(conn)

	// The HTTP request goes through the same real transport.Admit credential
	// verifier as gRPC. The wrapper is intentionally tiny because the canonical
	// edge composition owns the production mount; this test exercises the chat
	// projection over an actual TCP HTTP server.
	chatHandler := transportchat.NewHandler(transportchat.Dependencies{Service: f.service})
	httpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admitted, _, admissionErr := transport.Admit(r.Context(), cfg, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if admissionErr != nil {
			connect.NewErrorWriter().Write(w, r, admissionErr)
			return
		}
		chatHandler.ServeHTTP(w, r.WithContext(admitted))
	})
	httpServer := httptest.NewServer(httpHandler)
	t.Cleanup(httpServer.Close)
	c, err := f.service.CreateConversation(ctx, chatcore.CreateConversationRequest{Principal: p, TenantID: p.TenantID, Kind: chatcore.PublicChannel, Name: "transport"})
	if err != nil {
		t.Fatalf("seed transport conversation: %v", err)
	}
	httpClient := connect.NewClient[chatv1.SendPostRequest, chatv1.SendPostResponse](httpServer.Client(), httpServer.URL+"/hcmnext.chat.v1.ConversationService/SendPost", connect.WithProtoJSON())
	forgedReq := connect.NewRequest(&chatv1.SendPostRequest{TenantId: p.TenantID, ConversationId: c.ID, Body: "forged", IdempotencyKey: "transport-forged-1", Principal: &chatv1.Principal{TenantId: "company-forged", SubjectId: "mallory"}})
	forgedReq.Header().Set(transport.AuthorizationMetadataKey, "Bearer chat-token")
	forgedReq.Header().Set(transport.RequestIDMetadataKey, "chat-forged-1")
	if _, err := httpClient.CallUnary(ctx, forgedReq); err == nil || !strings.Contains(err.Error(), "trusted_context.caller_selected_authority") {
		t.Fatalf("caller-selected HTTP principal error = %v, want trusted-context rejection", err)
	}

	httpReq := connect.NewRequest(&chatv1.SendPostRequest{TenantId: p.TenantID, ConversationId: c.ID, Body: "over http", IdempotencyKey: "transport-http-1"})
	httpReq.Header().Set(transport.AuthorizationMetadataKey, "Bearer chat-token")
	httpReq.Header().Set(transport.RequestIDMetadataKey, "chat-http-1")
	httpResponse, err := httpClient.CallUnary(ctx, httpReq)
	if err != nil {
		t.Fatalf("authenticated HTTP create: %v", err)
	}
	if httpResponse.Msg.GetPost().GetSequence() != 1 {
		t.Fatalf("HTTP sequence = %d, want 1", httpResponse.Msg.GetPost().GetSequence())
	}
	conversationID := c.ID

	grpcResponse, err := grpcClient.SendPost(metadataContext(ctx), &chatv1.SendPostRequest{TenantId: p.TenantID, ConversationId: conversationID, Body: "over grpc", IdempotencyKey: "transport-1"})
	if err != nil {
		t.Fatalf("authenticated gRPC send: %v", err)
	}
	if grpcResponse.GetPost().GetSequence() != 2 {
		t.Fatalf("gRPC sequence = %d, want 2 after HTTP post", grpcResponse.GetPost().GetSequence())
	}
}

func metadataContext(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer chat-token", transport.RequestIDMetadataKey, "chat-grpc-1")
}

type chatVerifier struct{}

func (chatVerifier) Verify(context.Context, trust.Credential) (*trust.Principal, error) {
	now := time.Now()
	return trust.NewPrincipal(trust.PrincipalSpec{Tenant: "company-a", Subject: "alice", SubjectKind: trust.SubjectKindHuman, OrganizationScopeID: "org-company-a", AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "chat-session", CredentialDigest: "chat-credential", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Purposes: []string{"hcm_operations"}})
}

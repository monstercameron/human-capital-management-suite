package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"connectrpc.com/connect"
	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"google.golang.org/protobuf/proto"
)

// chat009ParityService represents the core owner port. It returns stable
// results so this test can compare the generated RPC method with the mounted
// HTTP projection without involving transport-specific business rules.
type chat009ParityService struct {
	transportChatFake
	creates []chatcore.CreateConversationRequest
	updates []chatcore.UpdateConversationRequest
}

func (s *chat009ParityService) CreateConversation(_ context.Context, r chatcore.CreateConversationRequest) (chatcore.Conversation, error) {
	s.creates = append(s.creates, r)
	return chatcore.Conversation{
		ID: "conversation-1", TenantID: r.TenantID, Kind: r.Kind,
		Name: r.Name, OwnerID: r.Principal.SubjectID, Revision: 1,
	}, nil
}

func (s *chat009ParityService) UpdateConversation(_ context.Context, r chatcore.UpdateConversationRequest) (chatcore.Conversation, error) {
	s.updates = append(s.updates, r)
	if r.ExpectedRevision != 1 {
		return chatcore.Conversation{}, chatcore.ErrConflict
	}
	r.Conversation.Revision = 2
	return r.Conversation, nil
}

func chat009HTTPClient(t *testing.T, service chatcore.ConversationService) (context.Context, *httptest.Server) {
	t.Helper()
	admitted := admittedChatContext(t)
	inner := NewHandler(Dependencies{Service: service})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner.ServeHTTP(w, r.WithContext(admitted))
	}))
	t.Cleanup(httpServer.Close)
	return admitted, httpServer
}

// TestTodo_CHAT_009 exercises the authoritative revision through the RPC
// method and the real mounted Connect route. The HTTP projection must carry
// the expected revision and preserve the owner's success result.
func TestTodo_CHAT_009(t *testing.T) {
	expected := uint64(1)
	nativeService := &chat009ParityService{}
	native := &server{deps: Dependencies{Service: nativeService}}
	request := &chatv1.UpdateConversationRequest{
		Conversation:     &chatv1.Conversation{Id: "conversation-1", TenantId: "server", Name: "Operations"},
		ExpectedRevision: expected,
	}
	nativeResponse, err := native.UpdateConversation(admittedChatContext(t), request)
	if err != nil {
		t.Fatal(err)
	}

	httpService := &chat009ParityService{}
	_, httpServer := chat009HTTPClient(t, httpService)
	client := connect.NewClient[chatv1.UpdateConversationRequest, chatv1.UpdateConversationResponse](
		httpServer.Client(), httpServer.URL+chatv1.ConversationService_UpdateConversation_FullMethodName, connect.WithProtoJSON(),
	)
	httpResponse, err := client.CallUnary(context.Background(), connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(nativeResponse, httpResponse.Msg) {
		t.Fatalf("HTTP response = %v, RPC response = %v", httpResponse.Msg, nativeResponse)
	}
	if len(nativeService.updates) != 1 || len(httpService.updates) != 1 {
		t.Fatalf("owner calls: RPC %d, HTTP %d", len(nativeService.updates), len(httpService.updates))
	}
	if nativeService.updates[0].ExpectedRevision != expected || httpService.updates[0].ExpectedRevision != expected {
		t.Fatalf("expected revisions: RPC %d, HTTP %d, want %d", nativeService.updates[0].ExpectedRevision, httpService.updates[0].ExpectedRevision, expected)
	}
	if !reflect.DeepEqual(nativeService.updates[0].Principal, httpService.updates[0].Principal) {
		t.Fatalf("owner principals differ: RPC %+v, HTTP %+v", nativeService.updates[0].Principal, httpService.updates[0].Principal)
	}
}

// TestTodo_CHAT_009_Conformance proves that HTTP and RPC preserve the same
// idempotency key, actor, and owner result for conversation creation.
func TestTodo_CHAT_009_Conformance(t *testing.T) {
	request := &chatv1.CreateConversationRequest{
		TenantId: "server", Kind: chatv1.ConversationKind_CONVERSATION_KIND_GROUP,
		Name: "Operations", IdempotencyKey: "request-1",
	}
	nativeService := &chat009ParityService{}
	native := &server{deps: Dependencies{Service: nativeService}}
	nativeResponse, err := native.CreateConversation(admittedChatContext(t), request)
	if err != nil {
		t.Fatal(err)
	}

	httpService := &chat009ParityService{}
	_, httpServer := chat009HTTPClient(t, httpService)
	client := connect.NewClient[chatv1.CreateConversationRequest, chatv1.CreateConversationResponse](
		httpServer.Client(), httpServer.URL+chatv1.ConversationService_CreateConversation_FullMethodName, connect.WithProtoJSON(),
	)
	httpResponse, err := client.CallUnary(context.Background(), connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(nativeResponse, httpResponse.Msg) {
		t.Fatalf("HTTP conversation = %v, RPC conversation = %v", httpResponse.Msg, nativeResponse)
	}
	if len(nativeService.creates) != 1 || len(httpService.creates) != 1 {
		t.Fatalf("owner calls: RPC %d, HTTP %d", len(nativeService.creates), len(httpService.creates))
	}
	for surface, got := range map[string]chatcore.CreateConversationRequest{
		"RPC": nativeService.creates[0], "HTTP": httpService.creates[0],
	} {
		if got.IdempotencyKey != "request-1" || got.Principal.TenantID != "server" || got.Principal.SubjectID != "u" || got.TenantID != "server" {
			t.Errorf("%s owner request = %+v", surface, got)
		}
	}
}

// TestTodo_CHAT_009_Integration sends authenticated JSON requests through the
// actual ServeMux path registrations used by the integration HTTP surface.
func TestTodo_CHAT_009_Integration(t *testing.T) {
	service := &chat009ParityService{}
	_, httpServer := chat009HTTPClient(t, service)
	client := connect.NewClient[chatv1.CreateConversationRequest, chatv1.CreateConversationResponse](
		httpServer.Client(), httpServer.URL+chatv1.ConversationService_CreateConversation_FullMethodName, connect.WithProtoJSON(),
	)
	response, err := client.CallUnary(context.Background(), connect.NewRequest(&chatv1.CreateConversationRequest{
		TenantId: "server", Kind: chatv1.ConversationKind_CONVERSATION_KIND_GROUP,
		Name: "Operations", IdempotencyKey: "http-integration",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := response.Msg.GetConversation(); got.GetId() != "conversation-1" || got.GetTenantId() != "server" || got.GetRevision() != 1 {
		t.Fatalf("HTTP route response = %v", got)
	}
	if len(service.creates) != 1 || service.creates[0].IdempotencyKey != "http-integration" {
		t.Fatalf("HTTP route owner call = %+v", service.creates)
	}
}

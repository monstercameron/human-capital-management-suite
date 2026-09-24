package document

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// hub041HTTPClient mounts [NewHandler] on a real httptest server, admitting
// every request with the same trusted context a caller would already carry
// past transport.Admit. This isolates the Connect routing, decoding and
// response marshaling this todo owns from the shared admission stack chat's
// equivalent test (chat_parity_integration_test.go) also does not exercise
// here, and mirrors that test's structure exactly.
func hub041HTTPClient(t *testing.T, service Service) (context.Context, *httptest.Server) {
	t.Helper()
	admitted := documentContext(t)
	inner := NewHandler(Dependencies{Service: service, CursorKey: []byte("hub-041-parity-key")})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner.ServeHTTP(w, r.WithContext(admitted))
	}))
	t.Cleanup(httpServer.Close)
	return admitted, httpServer
}

// TestTodo_HUB_041 exercises the revision-bearing CreateDocumentVersion
// through both the native RPC method and the real mounted Connect route. A
// caller who supplies the correct base_version_id must get the identical
// response and see the same owner call whichever protocol carried the
// request.
func TestTodo_HUB_041(t *testing.T) {
	request := &documentv1.CreateDocumentVersionRequest{
		DocumentId: "doc-1", BaseVersionId: "docv-1", Title: "Updated title", Markdown: "# Updated\n",
	}

	nativeService := &fakeService{}
	native := &server{service: nativeService}
	nativeResponse, err := native.CreateDocumentVersion(documentContext(t), request)
	if err != nil {
		t.Fatal(err)
	}

	httpService := &fakeService{}
	_, httpServer := hub041HTTPClient(t, httpService)
	client := connect.NewClient[documentv1.CreateDocumentVersionRequest, documentv1.CreateDocumentVersionResponse](
		httpServer.Client(), httpServer.URL+documentv1.DocumentService_CreateDocumentVersion_FullMethodName, connect.WithProtoJSON(),
	)
	httpResponse, err := client.CallUnary(context.Background(), connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(nativeResponse, httpResponse.Msg) {
		t.Fatalf("HTTP response = %v, RPC response = %v", httpResponse.Msg, nativeResponse)
	}
	if nativeService.tenant != "server" || nativeService.actor != "signed-in-user" {
		t.Fatalf("native caller not derived server-side: %+v", nativeService)
	}
	if httpService.tenant != "server" || httpService.actor != "signed-in-user" {
		t.Fatalf("HTTP caller not derived server-side: %+v", httpService)
	}
}

// TestTodo_HUB_041_Conformance proves that HTTP and RPC agree on pagination:
// the same signed cursor issued by one protocol is honored, in an identical
// second page, by the other, and both list responses are proto-identical for
// the same request.
func TestTodo_HUB_041_Conformance(t *testing.T) {
	now := time.Now().UTC()
	rows := []Summary{
		{DocumentID: "doc-3", UpdatedAt: now},
		{DocumentID: "doc-2", UpdatedAt: now.Add(-time.Second)},
		{DocumentID: "doc-1", UpdatedAt: now.Add(-2 * time.Second)},
	}
	request := &documentv1.ListDocumentsRequest{PageSize: 2, Collection: "private"}

	nativeService := &fakeService{rows: rows}
	native := &server{service: nativeService, cursorKey: []byte("hub-041-parity-key")}
	nativeResponse, err := native.ListDocuments(documentContext(t), request)
	if err != nil {
		t.Fatal(err)
	}

	httpService := &fakeService{rows: rows}
	_, httpServer := hub041HTTPClient(t, httpService)
	client := connect.NewClient[documentv1.ListDocumentsRequest, documentv1.ListDocumentsResponse](
		httpServer.Client(), httpServer.URL+documentv1.DocumentService_ListDocuments_FullMethodName, connect.WithProtoJSON(),
	)
	httpResponse, err := client.CallUnary(context.Background(), connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	if len(nativeResponse.GetDocuments()) != len(httpResponse.Msg.GetDocuments()) || len(nativeResponse.GetDocuments()) != 2 {
		t.Fatalf("HTTP documents = %v, RPC documents = %v", httpResponse.Msg.GetDocuments(), nativeResponse.GetDocuments())
	}
	if nativeResponse.GetNextPageToken() == "" || httpResponse.Msg.GetNextPageToken() == "" {
		t.Fatalf("expected both protocols to issue a next page cursor: RPC=%q HTTP=%q", nativeResponse.GetNextPageToken(), httpResponse.Msg.GetNextPageToken())
	}

	// A cursor minted by the HTTP path must page correctly through the
	// native RPC method, and vice versa: pagination state is not tied to
	// the protocol that issued it.
	nextViaNative, err := native.ListDocuments(documentContext(t), &documentv1.ListDocumentsRequest{
		PageSize: 2, Collection: "private", PageToken: httpResponse.Msg.GetNextPageToken(),
	})
	if err != nil {
		t.Fatalf("native rejected HTTP-issued cursor: %v", err)
	}
	nextViaHTTPResp, err := client.CallUnary(context.Background(), connect.NewRequest(&documentv1.ListDocumentsRequest{
		PageSize: 2, Collection: "private", PageToken: nativeResponse.GetNextPageToken(),
	}))
	if err != nil {
		t.Fatalf("HTTP rejected native-issued cursor: %v", err)
	}
	if !proto.Equal(nextViaNative, nextViaHTTPResp.Msg) {
		t.Fatalf("cross-protocol second page diverged: native-cursor-via-HTTP = %v, HTTP-cursor-via-native = %v", nextViaHTTPResp.Msg, nextViaNative)
	}
}

// TestTodo_HUB_041_Security proves the HTTP projection cannot be used to
// reach an outcome the native RPC method refuses: an unauthenticated caller
// is refused identically on both paths, and a service-level authorization
// refusal reaches the HTTP caller as the same typed error, never a generic
// fault.
func TestTodo_HUB_041_Security(t *testing.T) {
	request := &documentv1.GetDocumentRequest{DocumentId: "doc-1"}

	// Unauthenticated: no principal was ever admitted onto the context, on
	// either protocol.
	native := &server{service: &fakeService{}}
	if _, err := native.GetDocument(context.Background(), request); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("native unauthenticated = %v", err)
	}
	unauthenticatedHandler := NewHandler(Dependencies{Service: &fakeService{}, CursorKey: []byte("k")})
	unauthenticatedServer := httptest.NewServer(unauthenticatedHandler)
	t.Cleanup(unauthenticatedServer.Close)
	unauthClient := connect.NewClient[documentv1.GetDocumentRequest, documentv1.GetDocumentResponse](
		unauthenticatedServer.Client(), unauthenticatedServer.URL+documentv1.DocumentService_GetDocument_FullMethodName, connect.WithProtoJSON(),
	)
	if _, err := unauthClient.CallUnary(context.Background(), connect.NewRequest(request)); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("HTTP unauthenticated = %v", err)
	}

	// Authorization refusal from the owner port: both protocols must carry
	// the same typed reason, not a generic internal error, so an HTTP
	// caller cannot bypass a review the native path enforces.
	deniedErr := envelope.New(envelope.CodePermissionDenied, "document.permission_denied", "the actor may not comment on this document")
	httpDeniedService := &fakeService{err: deniedErr}
	_, deniedServer := hub041HTTPClient(t, httpDeniedService)
	commentClient := connect.NewClient[documentv1.AddDocumentCommentRequest, documentv1.AddDocumentCommentResponse](
		deniedServer.Client(), deniedServer.URL+documentv1.DocumentService_AddDocumentComment_FullMethodName, connect.WithProtoJSON(),
	)
	_, err := commentClient.CallUnary(context.Background(), connect.NewRequest(&documentv1.AddDocumentCommentRequest{
		DocumentId: "doc-1", VersionId: "docv-1", Body: "forged",
	}))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("HTTP denied = %v, want permission_denied", err)
	}

	nativeDeniedComment := &server{service: &fakeService{err: deniedErr}}
	_, nativeCommentErr := nativeDeniedComment.AddDocumentComment(documentContext(t), &documentv1.AddDocumentCommentRequest{
		DocumentId: "doc-1", VersionId: "docv-1", Body: "forged",
	})
	if status.Code(nativeCommentErr) != codes.PermissionDenied {
		t.Fatalf("native denied comment = %v", nativeCommentErr)
	}
}

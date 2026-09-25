package document

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"google.golang.org/protobuf/proto"
)

// fakeVersionsWithdrawService embeds fakeService (which already satisfies
// the core Service interface every server needs) and adds VersionsService
// and WithdrawService for DOCS-01/DOCS-07, recording what it was called
// with so a test can assert the request reached the port unchanged,
// whichever protocol carried it.
type fakeVersionsWithdrawService struct {
	*fakeService
	tenant, actor, documentID string
	versions                  []DocumentVersionSummary
	withdrawReason            string
	withdrawVersionID         string
	restoreVersionID          string
	err                       error
}

func newFakeVersionsWithdrawService() *fakeVersionsWithdrawService {
	return &fakeVersionsWithdrawService{fakeService: &fakeService{}}
}

func (f *fakeVersionsWithdrawService) ListDocumentVersions(_ context.Context, tenant, actor, documentID string) ([]DocumentVersionSummary, error) {
	f.tenant, f.actor, f.documentID = tenant, actor, documentID
	if f.err != nil {
		return nil, f.err
	}
	return f.versions, nil
}

func (f *fakeVersionsWithdrawService) WithdrawDocument(_ context.Context, tenant, actor, documentID, reason string) (string, error) {
	f.tenant, f.actor, f.documentID, f.withdrawReason = tenant, actor, documentID, reason
	if f.err != nil {
		return "", f.err
	}
	return f.withdrawVersionID, nil
}

func (f *fakeVersionsWithdrawService) RestoreDocument(_ context.Context, tenant, actor, documentID, versionID string) error {
	f.tenant, f.actor, f.documentID, f.restoreVersionID = tenant, actor, documentID, versionID
	return f.err
}

// versionsWithdrawHTTPClient mounts [NewHandler] on a real httptest
// server, admitting every request with the same trusted context a caller
// would already carry past transport.Admit, mirroring hub041HTTPClient.
func versionsWithdrawHTTPClient(t *testing.T, service Service) (context.Context, *httptest.Server) {
	t.Helper()
	admitted := documentContext(t)
	inner := NewHandler(Dependencies{Service: service, CursorKey: []byte("docs-01-07-parity-key")})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner.ServeHTTP(w, r.WithContext(admitted))
	}))
	t.Cleanup(httpServer.Close)
	return admitted, httpServer
}

// TestTodo_DOCS_01_Parity proves ListDocumentVersions gives the identical
// response over the native RPC method and the real mounted Connect route,
// for the same underlying service.
func TestTodo_DOCS_01_Parity(t *testing.T) {
	when := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	versions := []DocumentVersionSummary{
		{VersionID: "v-1", Title: "First", CreatedAt: when},
		{VersionID: "v-2", Redacted: true},
		{VersionID: "v-3", Title: "Current", CreatedAt: when.Add(24 * time.Hour), IsCurrent: true, AuthorID: "hc-1"},
	}
	request := &documentv1.ListDocumentVersionsRequest{DocumentId: "doc-1"}

	nativeService := newFakeVersionsWithdrawService()
	nativeService.versions = versions
	native := &server{service: nativeService}
	nativeResponse, err := native.ListDocumentVersions(documentContext(t), request)
	if err != nil {
		t.Fatal(err)
	}

	httpService := newFakeVersionsWithdrawService()
	httpService.versions = versions
	_, httpServer := versionsWithdrawHTTPClient(t, httpService)
	client := connect.NewClient[documentv1.ListDocumentVersionsRequest, documentv1.ListDocumentVersionsResponse](
		httpServer.Client(), httpServer.URL+documentv1.DocumentService_ListDocumentVersions_FullMethodName, connect.WithProtoJSON(),
	)
	httpResponse, err := client.CallUnary(context.Background(), connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(nativeResponse, httpResponse.Msg) {
		t.Fatalf("HTTP response = %v, RPC response = %v", httpResponse.Msg, nativeResponse)
	}
	if len(nativeResponse.GetVersions()) != 3 || !nativeResponse.GetVersions()[2].GetIsCurrent() || nativeResponse.GetVersions()[1].GetRedacted() != true {
		t.Fatalf("version summaries not carried through: %+v", nativeResponse.GetVersions())
	}
	if nativeService.tenant != "server" || nativeService.actor != "signed-in-user" || nativeService.documentID != "doc-1" {
		t.Fatalf("native caller not derived server-side: %+v", nativeService)
	}
	if httpService.tenant != "server" || httpService.actor != "signed-in-user" {
		t.Fatalf("HTTP caller not derived server-side: %+v", httpService)
	}
}

// TestTodo_DOCS_07_Parity proves WithdrawDocument and RestoreDocument give
// identical responses over the native RPC method and the real mounted
// Connect route, and that the version WithdrawDocument reports round-trips
// unchanged into RestoreDocument's request, which is what the productui
// Undo toast depends on.
func TestTodo_DOCS_07_Parity(t *testing.T) {
	withdrawRequest := &documentv1.WithdrawDocumentRequest{DocumentId: "doc-1", Reason: "no longer needed"}

	nativeService := newFakeVersionsWithdrawService()
	nativeService.withdrawVersionID = "v-old"
	native := &server{service: nativeService}
	nativeWithdraw, err := native.WithdrawDocument(documentContext(t), withdrawRequest)
	if err != nil {
		t.Fatal(err)
	}
	if nativeWithdraw.GetVersionId() != "v-old" {
		t.Fatalf("withdraw did not report the version that was live: %+v", nativeWithdraw)
	}
	if nativeService.withdrawReason != "no longer needed" || nativeService.documentID != "doc-1" {
		t.Fatalf("withdraw request not carried through: %+v", nativeService)
	}

	restoreRequest := &documentv1.RestoreDocumentRequest{DocumentId: "doc-1", VersionId: nativeWithdraw.GetVersionId()}
	if _, err := native.RestoreDocument(documentContext(t), restoreRequest); err != nil {
		t.Fatal(err)
	}
	if nativeService.restoreVersionID != "v-old" {
		t.Fatalf("restore did not use the version withdraw reported: %+v", nativeService)
	}

	httpService := newFakeVersionsWithdrawService()
	httpService.withdrawVersionID = "v-old"
	_, httpServer := versionsWithdrawHTTPClient(t, httpService)
	withdrawClient := connect.NewClient[documentv1.WithdrawDocumentRequest, documentv1.WithdrawDocumentResponse](
		httpServer.Client(), httpServer.URL+documentv1.DocumentService_WithdrawDocument_FullMethodName, connect.WithProtoJSON(),
	)
	httpWithdraw, err := withdrawClient.CallUnary(context.Background(), connect.NewRequest(withdrawRequest))
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(nativeWithdraw, httpWithdraw.Msg) {
		t.Fatalf("HTTP withdraw response = %v, RPC response = %v", httpWithdraw.Msg, nativeWithdraw)
	}

	restoreClient := connect.NewClient[documentv1.RestoreDocumentRequest, documentv1.RestoreDocumentResponse](
		httpServer.Client(), httpServer.URL+documentv1.DocumentService_RestoreDocument_FullMethodName, connect.WithProtoJSON(),
	)
	if _, err := restoreClient.CallUnary(context.Background(), connect.NewRequest(&documentv1.RestoreDocumentRequest{DocumentId: "doc-1", VersionId: httpWithdraw.Msg.GetVersionId()})); err != nil {
		t.Fatal(err)
	}
	if httpService.restoreVersionID != "v-old" {
		t.Fatalf("HTTP restore did not use the version HTTP withdraw reported: %+v", httpService)
	}
}

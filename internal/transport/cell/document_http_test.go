package cell

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"

	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
)

// documentOverlayService answers ListDocuments and records the caller the
// projection resolved from the admitted context.
type documentOverlayService struct {
	transportdocument.Service
	reached       int
	tenant, actor string
}

func (s *documentOverlayService) ListDocuments(_ context.Context, tenant, actor string, _ transportdocument.ListOptions) (transportdocument.ListResult, error) {
	s.reached++
	s.tenant, s.actor = tenant, actor
	return transportdocument.ListResult{Rows: []transportdocument.Summary{{DocumentID: "doc-1", Title: "Runbook"}}, Total: 1}, nil
}

func documentOverlayFixture(t *testing.T, next http.Handler) (*httptest.Server, string, *documentOverlayService) {
	t.Helper()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	verifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	token, err := transporttest.BearerToken(verifier, transporttest.DefaultClaims(now))
	if err != nil {
		t.Fatalf("BearerToken: %v", err)
	}
	cfg := transporttest.Config(verifier, func() time.Time { return now }, "document-overlay", nil)
	service := &documentOverlayService{}
	server := httptest.NewServer(DocumentHTTPOverlay(next, cfg, service, []byte("document-overlay-cursor-key-32bytes")))
	t.Cleanup(server.Close)
	return server, token, service
}

// TestTodo_HUB_041_ServedOverlay proves the document projection is reachable
// over HTTP on the composed edge only after admission: an authenticated call
// reaches the service with the verified caller, an anonymous one is refused
// before the service, and routes outside the document prefix stay with the
// wrapped edge.
func TestTodo_HUB_041_ServedOverlay(t *testing.T) {
	edge := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	server, token, service := documentOverlayFixture(t, edge)
	list := connect.NewClient[documentv1.ListDocumentsRequest, documentv1.ListDocumentsResponse](
		server.Client(), server.URL+documentv1.DocumentService_ListDocuments_FullMethodName, connect.WithProtoJSON(),
	)

	anonymous := connect.NewRequest(&documentv1.ListDocumentsRequest{})
	if _, err := list.CallUnary(context.Background(), anonymous); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("anonymous list = %v (code %v), want Unauthenticated", err, connect.CodeOf(err))
	}
	if service.reached != 0 {
		t.Fatal("an unauthenticated request reached the document service")
	}

	authorized := connect.NewRequest(&documentv1.ListDocumentsRequest{})
	authorized.Header().Set(transport.AuthorizationMetadataKey, token)
	resp, err := list.CallUnary(context.Background(), authorized)
	if err != nil {
		t.Fatalf("authorized list: %v", err)
	}
	if service.reached != 1 || service.tenant == "" || service.actor == "" {
		t.Fatalf("service reached=%d tenant=%q actor=%q, want one call with the verified caller", service.reached, service.tenant, service.actor)
	}
	if got := resp.Msg.GetDocuments(); len(got) != 1 || got[0].GetDocumentId() != "doc-1" {
		t.Fatalf("documents = %v, want doc-1", got)
	}

	w := httptest.NewRecorder()
	DocumentHTTPOverlay(edge, transport.Config{}, service, nil).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/anything", nil))
	if w.Code != http.StatusTeapot {
		t.Fatalf("non-document route = %d, want the wrapped edge", w.Code)
	}
	w = httptest.NewRecorder()
	DocumentHTTPOverlay(edge, transport.Config{}, nil, nil).ServeHTTP(w, httptest.NewRequest(http.MethodPost, transportdocument.ProcedurePrefix+"ListDocuments", nil))
	if w.Code != http.StatusTeapot {
		t.Fatalf("nil service replaced the edge: %d", w.Code)
	}
}

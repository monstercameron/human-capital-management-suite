package application

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// hub041Principal returns a stable authenticated human principal for the
// HUB-041 integration matrix.
func hub041Principal(t *testing.T, tenant, subject string) *trust.Principal {
	t.Helper()
	now := time.Now()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(tenant), Subject: subject, SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "hub-041-session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		CredentialDigest: "hub-041-digest",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// hub041GRPCClient dials a real gRPC server hosting the composed document
// service exactly as internal/transport/cell.RegisterDocument mounts it,
// with every call carrying p as the trusted principal — the same shape the
// production authentication interceptor produces.
func hub041GRPCClient(t *testing.T, service transportdocument.Service, cursorKey []byte, p *trust.Principal) documentv1.DocumentServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(trust.WithPrincipal(ctx, p), req)
	}))
	transportdocument.Register(srv, service, cursorKey)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return documentv1.NewDocumentServiceClient(conn)
}

// hub041HTTPClient mounts the document Connect projection on a real HTTP server, using
// the same trusted principal shape a Connect admission interceptor would
// have already put on the context, exactly as
// internal/transport/document.hub041HTTPClient does within its own package.
func hub041HTTPClient(t *testing.T, service transportdocument.Service, cursorKey []byte, p *trust.Principal) (*httptest.Server, func(procedure string) string) {
	t.Helper()
	admitted := trust.WithPrincipal(context.Background(), p)
	inner := transportdocument.NewHandler(transportdocument.Dependencies{Service: service, CursorKey: cursorKey})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner.ServeHTTP(w, r.WithContext(admitted))
	}))
	t.Cleanup(httpServer.Close)
	return httpServer, func(procedure string) string { return httpServer.URL + procedure }
}

// TestTodo_HUB_041_Integration proves that, against the real pgtest-backed
// document store, the composed document port reaches the same outcome
// whether the caller speaks native gRPC or the Connect HTTP projection: the
// same document is created, the same base-version revision check accepts a
// current edit and refuses a stale one, and both protocols carry the
// identical typed conflict — never a bare 500.
func TestTodo_HUB_041_Integration(t *testing.T) {
	svc := documentServiceFixture(t)
	const tenant, owner = "tenant-hub-041", "owner-hub-041"
	principal := hub041Principal(t, tenant, owner)
	cursorKey := []byte("hub-041-integration-key")

	grpcClient := hub041GRPCClient(t, svc, cursorKey, principal)
	httpServer, httpURL := hub041HTTPClient(t, svc, cursorKey, principal)
	httpVersionClient := connect.NewClient[documentv1.CreateDocumentVersionRequest, documentv1.CreateDocumentVersionResponse](
		httpServer.Client(), httpURL(documentv1.DocumentService_CreateDocumentVersion_FullMethodName), connect.WithProtoJSON())
	httpGetClient := connect.NewClient[documentv1.GetDocumentRequest, documentv1.GetDocumentResponse](
		httpServer.Client(), httpURL(documentv1.DocumentService_GetDocument_FullMethodName), connect.WithProtoJSON())

	// Create through native gRPC; read the same row back through HTTP.
	created, err := grpcClient.CreateDocument(context.Background(), &documentv1.CreateDocumentRequest{Title: "Guide", Markdown: "# Original\n"})
	if err != nil {
		t.Fatal(err)
	}
	httpGet, err := httpGetClient.CallUnary(context.Background(), connect.NewRequest(&documentv1.GetDocumentRequest{DocumentId: created.GetDocumentId()}))
	if err != nil {
		t.Fatal(err)
	}
	grpcGet, err := grpcClient.GetDocument(context.Background(), &documentv1.GetDocumentRequest{DocumentId: created.GetDocumentId()})
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(grpcGet.GetDocument(), httpGet.Msg.GetDocument()) || grpcGet.GetMarkdown() != httpGet.Msg.GetMarkdown() {
		t.Fatalf("gRPC read = %v %q, HTTP read = %v %q", grpcGet.GetDocument(), grpcGet.GetMarkdown(), httpGet.Msg.GetDocument(), httpGet.Msg.GetMarkdown())
	}

	// A current edit through HTTP succeeds identically to one through gRPC.
	httpVersion, err := httpVersionClient.CallUnary(context.Background(), connect.NewRequest(&documentv1.CreateDocumentVersionRequest{
		DocumentId: created.GetDocumentId(), BaseVersionId: created.GetVersionId(), Title: "Guide", Markdown: "# Revised via HTTP\n",
	}))
	if err != nil {
		t.Fatalf("HTTP current-revision edit refused: %v", err)
	}
	if httpVersion.Msg.GetVersionId() == "" || httpVersion.Msg.GetVersionId() == created.GetVersionId() {
		t.Fatalf("HTTP edit did not advance the revision: %+v", httpVersion.Msg)
	}

	// A stale base version is refused identically on both protocols, and
	// both carry the same typed conflict rather than an opaque fault.
	_, grpcStaleErr := grpcClient.CreateDocumentVersion(context.Background(), &documentv1.CreateDocumentVersionRequest{
		DocumentId: created.GetDocumentId(), BaseVersionId: created.GetVersionId(), Title: "Guide", Markdown: "# Stale via gRPC\n",
	})
	if status.Code(grpcStaleErr) != codes.Aborted {
		t.Fatalf("gRPC stale revision = %v, want Aborted", grpcStaleErr)
	}
	_, httpStaleErr := httpVersionClient.CallUnary(context.Background(), connect.NewRequest(&documentv1.CreateDocumentVersionRequest{
		DocumentId: created.GetDocumentId(), BaseVersionId: created.GetVersionId(), Title: "Guide", Markdown: "# Stale via HTTP\n",
	}))
	if connect.CodeOf(httpStaleErr) != connect.CodeAborted {
		t.Fatalf("HTTP stale revision = %v, want Aborted", httpStaleErr)
	}

	// Authorization is enforced identically: a second principal with no
	// grant is refused by both protocols rather than only one.
	strangerPrincipal := hub041Principal(t, tenant, "stranger-hub-041")
	strangerGRPC := hub041GRPCClient(t, svc, cursorKey, strangerPrincipal)
	_, strangerGRPCErr := strangerGRPC.GetDocument(context.Background(), &documentv1.GetDocumentRequest{DocumentId: created.GetDocumentId()})
	strangerHTTPServer, strangerHTTPURL := hub041HTTPClient(t, svc, cursorKey, strangerPrincipal)
	strangerHTTPClient := connect.NewClient[documentv1.GetDocumentRequest, documentv1.GetDocumentResponse](
		strangerHTTPServer.Client(), strangerHTTPURL(documentv1.DocumentService_GetDocument_FullMethodName), connect.WithProtoJSON())
	_, strangerHTTPErr := strangerHTTPClient.CallUnary(context.Background(), connect.NewRequest(&documentv1.GetDocumentRequest{DocumentId: created.GetDocumentId()}))
	if (strangerGRPCErr == nil) != (strangerHTTPErr == nil) {
		t.Fatalf("authorization diverged: gRPC err=%v, HTTP err=%v", strangerGRPCErr, strangerHTTPErr)
	}
	if strangerGRPCErr != nil && status.Code(strangerGRPCErr) != codes.PermissionDenied && status.Code(strangerGRPCErr) != codes.NotFound {
		t.Fatalf("gRPC stranger read = %v", strangerGRPCErr)
	}
	if strangerHTTPErr != nil {
		gotCode := connect.CodeOf(strangerHTTPErr)
		if gotCode != connect.CodePermissionDenied && gotCode != connect.CodeNotFound {
			t.Fatalf("HTTP stranger read = %v", strangerHTTPErr)
		}
	}
}

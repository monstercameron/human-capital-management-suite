package document

import (
	"context"
	"testing"
	"time"

	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeService struct {
	tenant, actor string
	options       ListOptions
	rows          []Summary
	comment       Comment
}

func (f *fakeService) ListDocuments(_ context.Context, tenant, actor string, options ListOptions) ([]Summary, error) {
	f.tenant, f.actor, f.options = tenant, actor, options
	if f.rows != nil {
		return f.rows, nil
	}
	return []Summary{{DocumentID: "doc-1", Title: "Guide", OwnerID: actor, VersionID: "v-1", Status: "private"}}, nil
}

func TestDocumentListCursorAndSelection(t *testing.T) {
	now := time.Now().UTC()
	f := &fakeService{rows: []Summary{{DocumentID: "doc-3", UpdatedAt: now}, {DocumentID: "doc-2", UpdatedAt: now.Add(-time.Second)}, {DocumentID: "doc-1", UpdatedAt: now.Add(-2 * time.Second)}}}
	s := &server{service: f, cursorKey: []byte("test-document-page-key")}
	ctx := documentContext(t)
	first, err := s.ListDocuments(ctx, &documentv1.ListDocumentsRequest{PageSize: 2, Query: "guide", Collection: "private"})
	if err != nil || len(first.GetDocuments()) != 2 || first.GetNextPageToken() == "" || f.options.Limit != 3 {
		t.Fatalf("first page = %+v, %v; options = %+v", first, err, f.options)
	}
	_, err = s.ListDocuments(ctx, &documentv1.ListDocumentsRequest{PageSize: 2, Query: "guide", Collection: "private", PageToken: first.GetNextPageToken()})
	if err != nil || f.options.BeforeID != "doc-2" || !f.options.BeforeTime.Equal(now.Add(-time.Second)) {
		t.Fatalf("next cursor = %+v, %v", f.options, err)
	}
	if _, err := s.ListDocuments(ctx, &documentv1.ListDocumentsRequest{PageToken: first.GetNextPageToken(), Query: "different", Collection: "private"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("changed query cursor = %v", err)
	}
	if _, err := s.ListDocuments(ctx, &documentv1.ListDocumentsRequest{Collection: "team"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unsupported collection = %v", err)
	}
	tampered := first.GetNextPageToken() + "x"
	if _, err := s.ListDocuments(ctx, &documentv1.ListDocumentsRequest{PageToken: tampered, Query: "guide", Collection: "private"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("tampered cursor = %v", err)
	}
	if _, ok := s.verifyCursor(first.GetNextPageToken(), "different-tenant", "signed-in-user", "guide", "private"); ok {
		t.Fatal("cross-tenant cursor accepted")
	}
	if _, ok := s.verifyCursor(first.GetNextPageToken(), "server", "different-actor", "guide", "private"); ok {
		t.Fatal("cross-actor cursor accepted")
	}
}
func (f *fakeService) CreateDocument(_ context.Context, tenant, actor, title, markdown string) (string, string, error) {
	f.tenant, f.actor = tenant, actor
	return "doc-2", "v-2", nil
}
func (f *fakeService) GetDocument(_ context.Context, tenant, actor, id string) (Summary, string, string, error) {
	f.tenant, f.actor = tenant, actor
	return Summary{DocumentID: id, Title: "Guide", OwnerID: actor, VersionID: "v-1", Status: "private"}, "# Guide\n", "sha256", nil
}
func (f *fakeService) ShareDocument(_ context.Context, tenant, actor, id, recipient string) error {
	f.tenant, f.actor = tenant, actor
	return nil
}
func (f *fakeService) ListDocumentComments(_ context.Context, tenant, actor, id, versionID string) ([]Comment, error) {
	f.tenant, f.actor = tenant, actor
	return []Comment{{ID: "docc-1", DocumentID: id, VersionID: versionID, AuthorID: actor, Body: "Looks good", CreatedAt: time.Now().UTC()}}, nil
}
func (f *fakeService) AddDocumentComment(_ context.Context, tenant, actor, id, versionID, body string) (Comment, error) {
	f.tenant, f.actor = tenant, actor
	f.comment = Comment{ID: "docc-2", DocumentID: id, VersionID: versionID, AuthorID: actor, Body: body, CreatedAt: time.Now().UTC()}
	return f.comment, nil
}
func (f *fakeService) CreateDocumentVersion(_ context.Context, tenant, actor, id, baseVersionID, title, markdown string) (string, error) {
	f.tenant, f.actor = tenant, actor
	return "docv-new", nil
}

func TestDocumentVersionCallerAndValidation(t *testing.T) {
	f := &fakeService{}
	s := &server{service: f}
	request := &documentv1.CreateDocumentVersionRequest{DocumentId: "doc-1", BaseVersionId: "docv-1", Title: "Updated title", Markdown: "# Updated\n"}
	if _, err := s.CreateDocumentVersion(context.Background(), request); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("anonymous update = %v", err)
	}
	ctx := documentContext(t)
	if _, err := s.CreateDocumentVersion(ctx, &documentv1.CreateDocumentVersionRequest{DocumentId: "doc-1", Title: "Updated"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing base = %v", err)
	}
	if _, err := s.CreateDocumentVersion(ctx, &documentv1.CreateDocumentVersionRequest{DocumentId: "doc-1", BaseVersionId: "docv-1", Title: " "}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty title = %v", err)
	}
	if _, err := s.CreateDocumentVersion(ctx, &documentv1.CreateDocumentVersionRequest{DocumentId: "doc-1", BaseVersionId: "docv-1", Title: "Updated", Markdown: "  \n"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("blank body = %v", err)
	}
	updated, err := s.CreateDocumentVersion(ctx, request)
	if err != nil || updated.GetVersionId() != "docv-new" || f.tenant != "server" || f.actor != "signed-in-user" {
		t.Fatalf("version caller = %+v, %v; service=%+v", updated, err, f)
	}
}

func TestDocumentCommentCallerAndValidation(t *testing.T) {
	f := &fakeService{}
	s := &server{service: f}
	if _, err := s.ListDocumentComments(context.Background(), &documentv1.ListDocumentCommentsRequest{DocumentId: "doc-1", VersionId: "docv-1"}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("anonymous list = %v", err)
	}
	if _, err := s.AddDocumentComment(context.Background(), &documentv1.AddDocumentCommentRequest{DocumentId: "doc-1", VersionId: "docv-1", Body: "hello"}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("anonymous add = %v", err)
	}
	ctx := documentContext(t)
	if _, err := s.ListDocumentComments(ctx, &documentv1.ListDocumentCommentsRequest{DocumentId: "doc-1"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing version list = %v", err)
	}
	if _, err := s.AddDocumentComment(ctx, &documentv1.AddDocumentCommentRequest{DocumentId: "doc-1", VersionId: "docv-1", Body: "  "}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("blank comment = %v", err)
	}
	if _, err := s.AddDocumentComment(ctx, &documentv1.AddDocumentCommentRequest{DocumentId: "doc-1", VersionId: "docv-1", Body: string(make([]byte, 16001))}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("oversized comment = %v", err)
	}
	added, err := s.AddDocumentComment(ctx, &documentv1.AddDocumentCommentRequest{DocumentId: "doc-1", VersionId: "docv-1", Body: "Please clarify"})
	if err != nil || added.GetComment().GetAuthorId() != "signed-in-user" || added.GetComment().GetBody() != "Please clarify" || added.GetComment().GetCreatedAt() == nil || f.tenant != "server" {
		t.Fatalf("add = %+v, %v; service=%+v", added, err, f)
	}
	listed, err := s.ListDocumentComments(ctx, &documentv1.ListDocumentCommentsRequest{DocumentId: "doc-1", VersionId: "docv-1"})
	if err != nil || len(listed.GetComments()) != 1 || listed.GetComments()[0].GetVersionId() != "docv-1" {
		t.Fatalf("list = %+v, %v", listed, err)
	}
}

func TestDocumentShareCallerAndValidation(t *testing.T) {
	f := &fakeService{}
	s := &server{service: f}
	if _, err := s.ShareDocument(context.Background(), &documentv1.ShareDocumentRequest{DocumentId: "doc-1", RecipientId: "person-2"}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("anonymous share = %v", err)
	}
	ctx := documentContext(t)
	if _, err := s.ShareDocument(ctx, &documentv1.ShareDocumentRequest{DocumentId: "doc-1", RecipientId: "signed-in-user"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("self share = %v", err)
	}
	if _, err := s.ShareDocument(ctx, &documentv1.ShareDocumentRequest{DocumentId: "doc-1", RecipientId: "person-2"}); err != nil || f.tenant != "server" || f.actor != "signed-in-user" {
		t.Fatalf("share caller = %q %q, %v", f.tenant, f.actor, err)
	}
}

func documentContext(t *testing.T) context.Context {
	return documentContextKind(t, trust.SubjectKindHuman)
}

func documentContextKind(t *testing.T, kind trust.SubjectKind) context.Context {
	t.Helper()
	now := time.Now()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("server"), Subject: "signed-in-user", SubjectKind: kind, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, _, e := transport.Admit(context.Background(), transport.Config{Verifier: trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) { return p, nil })}, transport.AdmissionRequest{Metadata: transport.MapMetadata{"authorization": {"Bearer token"}}, Method: documentv1.DocumentService_ListDocuments_FullMethodName, Kind: transport.KindGRPC})
	if e != nil {
		t.Fatal(e)
	}
	return ctx
}

func TestDocumentRPCSecurity(t *testing.T) {
	f := &fakeService{}
	s := &server{service: f}
	if _, err := s.ListDocuments(context.Background(), &documentv1.ListDocumentsRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated list = %v", err)
	}
	if _, err := s.ListDocuments(documentContextKind(t, trust.SubjectKindService), &documentv1.ListDocumentsRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("service principal list = %v", err)
	}
	ctx := documentContext(t)
	got, err := s.ListDocuments(ctx, &documentv1.ListDocumentsRequest{PageSize: 500})
	if err != nil || len(got.Documents) != 1 || f.tenant != "server" || f.actor != "signed-in-user" || f.options.Limit != MaxPageSize+1 {
		t.Fatalf("trusted list = %+v, %v; call = %+v", got, err, f)
	}
	if _, err := s.CreateDocument(ctx, &documentv1.CreateDocumentRequest{Title: " "}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("blank title = %v", err)
	}
	created, err := s.CreateDocument(ctx, &documentv1.CreateDocumentRequest{Title: "Guide", Markdown: "# Guide\n"})
	if err != nil || created.GetDocumentId() != "doc-2" || f.actor != "signed-in-user" {
		t.Fatalf("trusted create = %+v, %v; call = %+v", created, err, f)
	}
	if _, err := s.GetDocument(context.Background(), &documentv1.GetDocumentRequest{DocumentId: "doc-1"}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated get = %v", err)
	}
	if _, err := s.GetDocument(documentContextKind(t, trust.SubjectKindService), &documentv1.GetDocumentRequest{DocumentId: "doc-1"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("service principal get = %v", err)
	}
	if _, err := s.GetDocument(ctx, &documentv1.GetDocumentRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty document ID = %v", err)
	}
	read, err := s.GetDocument(ctx, &documentv1.GetDocumentRequest{DocumentId: "doc-1"})
	if err != nil || read.GetDocument().GetDocumentId() != "doc-1" || read.GetMarkdown() != "# Guide\n" || f.tenant != "server" || f.actor != "signed-in-user" {
		t.Fatalf("trusted read = %+v, %v; call = %+v", read, err, f)
	}
}

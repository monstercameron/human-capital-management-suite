package document

import (
	"context"
	"strings"
	"testing"
	"time"

	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func (f *fakeService) GetDocumentLibrary(_ context.Context, tenant, actor string) (Library, error) {
	f.tenant, f.actor = tenant, actor
	return f.library, f.err
}

func (f *fakeService) CreateDocumentFolder(_ context.Context, tenant, actor, name string) (Folder, error) {
	f.tenant, f.actor, f.name = tenant, actor, name
	return Folder{ID: "docf-1", Name: name, CreatedAt: time.Now().UTC()}, f.err
}

func (f *fakeService) RenameDocumentFolder(_ context.Context, tenant, actor, folderID, name string) error {
	f.tenant, f.actor, f.folderID, f.name = tenant, actor, folderID, name
	return f.err
}

func (f *fakeService) DeleteDocumentFolder(_ context.Context, tenant, actor, folderID string) error {
	f.tenant, f.actor, f.folderID = tenant, actor, folderID
	return f.err
}

func (f *fakeService) MoveDocuments(_ context.Context, tenant, actor string, ids []string, folderID string) error {
	f.tenant, f.actor, f.documentIDs, f.folderID = tenant, actor, ids, folderID
	return f.err
}

func (f *fakeService) SetDocumentStarred(_ context.Context, tenant, actor, id string, starred bool) error {
	f.tenant, f.actor, f.documentID, f.starred = tenant, actor, id, starred
	return f.err
}

func (f *fakeService) ListDocumentAccess(_ context.Context, tenant, actor, id string) ([]AccessEntry, error) {
	f.tenant, f.actor, f.documentID = tenant, actor, id
	return f.access, f.err
}

func (f *fakeService) RevokeDocumentAccess(_ context.Context, tenant, actor, id, kind, subject string) error {
	f.tenant, f.actor, f.documentID, f.subjectKind, f.subjectID = tenant, actor, id, kind, subject
	return f.err
}

func TestDocumentListSelectionCursorBindsEverything(t *testing.T) {
	now := time.Now().UTC()
	f := &fakeService{rows: []Summary{
		{DocumentID: "doc-a", Title: "Alpha", TitleKey: "alpha", UpdatedAt: now, Starred: true, FolderID: "docf-1", CanManageAccess: true, ReaderCount: 3},
		{DocumentID: "doc-b", Title: "Bravo", TitleKey: "bravo", UpdatedAt: now, ReaderCount: 4},
		{DocumentID: "doc-c", Title: "Charlie", TitleKey: "charlie", UpdatedAt: now},
	}}
	s := &server{service: f, cursorKey: []byte("test-document-page-key")}
	ctx := documentContext(t)
	request := &documentv1.ListDocumentsRequest{PageSize: 2, Sort: SortTitle, FolderId: "docf-1", StarredOnly: true, OwnerId: "owner-1"}
	first, err := s.ListDocuments(ctx, request)
	if err != nil || first.GetTotalCount() != 3 || len(first.GetDocuments()) != 2 || first.GetNextPageToken() == "" {
		t.Fatalf("first title page = %+v, %v", first, err)
	}
	if f.options.Sort != SortTitle || f.options.FolderID != "docf-1" || !f.options.StarredOnly || f.options.OwnerID != "owner-1" || f.options.AfterTitle != nil {
		t.Fatalf("title options = %+v", f.options)
	}
	a, b := first.GetDocuments()[0], first.GetDocuments()[1]
	if !a.GetStarred() || a.GetFolderId() != "docf-1" || a.GetReaderCount() != 3 {
		t.Fatalf("managed row = %+v", a)
	}
	if b.GetReaderCount() != 0 {
		t.Fatalf("reader count leaked to a non-manager: %+v", b)
	}
	next := proto.Clone(request).(*documentv1.ListDocumentsRequest)
	next.PageToken = first.GetNextPageToken()
	if _, err := s.ListDocuments(ctx, next); err != nil || f.options.AfterTitle == nil || *f.options.AfterTitle != "bravo" || f.options.BeforeID != "doc-b" {
		t.Fatalf("title cursor = %+v, %v", f.options, err)
	}
	changes := map[string]func(*documentv1.ListDocumentsRequest){
		"folder":  func(r *documentv1.ListDocumentsRequest) { r.FolderId = "docf-2" },
		"starred": func(r *documentv1.ListDocumentsRequest) { r.StarredOnly = false },
		"owner":   func(r *documentv1.ListDocumentsRequest) { r.OwnerId = "owner-2" },
		"sort":    func(r *documentv1.ListDocumentsRequest) { r.Sort = SortUpdated },
	}
	for name, change := range changes {
		changed := proto.Clone(next).(*documentv1.ListDocumentsRequest)
		change(changed)
		if _, err := s.ListDocuments(ctx, changed); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("cursor reused with changed %s = %v", name, err)
		}
	}
	for _, bad := range []*documentv1.ListDocumentsRequest{{Sort: "size"}, {FolderId: strings.Repeat("f", 129)}, {OwnerId: " padded "}, {SearchMode: "regex"}, {Page: -1}, {Page: maxPage + 1}} {
		if _, err := s.ListDocuments(ctx, bad); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("invalid selection %+v = %v", bad, err)
		}
	}
	if _, err := s.ListDocuments(ctx, &documentv1.ListDocumentsRequest{}); err != nil || f.options.Sort != SortUpdated {
		t.Fatalf("default sort = %+v, %v", f.options, err)
	}
}

func TestDocumentGetCarriesOrganization(t *testing.T) {
	msg := summaryMessage(Summary{DocumentID: "doc-1", Starred: true, FolderID: "docf-1", ReaderCount: 2, CanManageAccess: true})
	if !msg.GetStarred() || msg.GetFolderId() != "docf-1" || msg.GetReaderCount() != 2 {
		t.Fatalf("manager summary = %+v", msg)
	}
	msg = summaryMessage(Summary{DocumentID: "doc-1", ReaderCount: 2})
	if msg.GetReaderCount() != 0 {
		t.Fatalf("reader summary leaked audience size = %+v", msg)
	}
	if clampCount(-1) != 0 || clampCount(1<<40) != 1<<31-1 {
		t.Fatal("count clamp is wrong")
	}
}

func TestDocumentLibraryRPCs(t *testing.T) {
	f := &fakeService{library: Library{Folders: []Folder{{ID: "docf-1", Name: "Policies", DocumentCount: 4, CreatedAt: time.Now().UTC()}}, All: 10, Mine: 6, Shared: 4, Starred: 2}}
	s := &server{service: f}
	if _, err := s.GetDocumentLibrary(context.Background(), &documentv1.GetDocumentLibraryRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("anonymous library = %v", err)
	}
	ctx := documentContext(t)
	lib, err := s.GetDocumentLibrary(ctx, &documentv1.GetDocumentLibraryRequest{})
	if err != nil || lib.GetAllCount() != 10 || lib.GetMineCount() != 6 || lib.GetSharedCount() != 4 || lib.GetStarredCount() != 2 || len(lib.GetFolders()) != 1 || lib.GetFolders()[0].GetDocumentCount() != 4 || lib.GetFolders()[0].GetCreatedAt() == nil || f.actor != "signed-in-user" {
		t.Fatalf("library = %+v, %v", lib, err)
	}

	for _, bad := range []string{"", "   ", strings.Repeat("x", 81)} {
		if _, err := s.CreateDocumentFolder(ctx, &documentv1.CreateDocumentFolderRequest{Name: bad}); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("folder name %q = %v", bad, err)
		}
	}
	created, err := s.CreateDocumentFolder(ctx, &documentv1.CreateDocumentFolderRequest{Name: "  Runbooks  "})
	if err != nil || created.GetFolder().GetName() != "Runbooks" || f.name != "Runbooks" {
		t.Fatalf("create folder = %+v, %v", created, err)
	}
	if _, err := s.RenameDocumentFolder(ctx, &documentv1.RenameDocumentFolderRequest{Name: "New"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("rename without folder = %v", err)
	}
	if _, err := s.RenameDocumentFolder(ctx, &documentv1.RenameDocumentFolderRequest{FolderId: "docf-1", Name: " "}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("rename to blank = %v", err)
	}
	if _, err := s.RenameDocumentFolder(ctx, &documentv1.RenameDocumentFolderRequest{FolderId: "docf-1", Name: " Archive "}); err != nil || f.folderID != "docf-1" || f.name != "Archive" {
		t.Fatalf("rename = %q %q, %v", f.folderID, f.name, err)
	}
	if _, err := s.DeleteDocumentFolder(ctx, &documentv1.DeleteDocumentFolderRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("delete without folder = %v", err)
	}
	if _, err := s.DeleteDocumentFolder(ctx, &documentv1.DeleteDocumentFolderRequest{FolderId: "docf-9"}); err != nil || f.folderID != "docf-9" {
		t.Fatalf("delete = %q, %v", f.folderID, err)
	}

	tooMany := make([]string, MaxMoveDocuments+1)
	for i := range tooMany {
		tooMany[i] = "doc"
	}
	for _, bad := range []*documentv1.MoveDocumentsRequest{{}, {DocumentIds: tooMany}, {DocumentIds: []string{"doc-1", ""}}, {DocumentIds: []string{"doc-1"}, FolderId: strings.Repeat("f", 129)}} {
		if _, err := s.MoveDocuments(ctx, bad); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("invalid move %d ids = %v", len(bad.GetDocumentIds()), err)
		}
	}
	if _, err := s.MoveDocuments(ctx, &documentv1.MoveDocumentsRequest{DocumentIds: []string{"doc-1", "doc-2"}}); err != nil || len(f.documentIDs) != 2 || f.folderID != "" {
		t.Fatalf("unfile = %+v %q, %v", f.documentIDs, f.folderID, err)
	}

	if _, err := s.SetDocumentStarred(ctx, &documentv1.SetDocumentStarredRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("star without document = %v", err)
	}
	if _, err := s.SetDocumentStarred(ctx, &documentv1.SetDocumentStarredRequest{DocumentId: "doc-1", Starred: true}); err != nil || f.documentID != "doc-1" || !f.starred {
		t.Fatalf("star = %q %v, %v", f.documentID, f.starred, err)
	}

	f.access = []AccessEntry{{SubjectKind: "person", SubjectID: "owner", Role: "owner"}, {SubjectKind: "person", SubjectID: "reader", Role: RoleViewer, Removable: true}}
	if _, err := s.ListDocumentAccess(ctx, &documentv1.ListDocumentAccessRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("access without document = %v", err)
	}
	access, err := s.ListDocumentAccess(ctx, &documentv1.ListDocumentAccessRequest{DocumentId: "doc-1"})
	if err != nil || len(access.GetEntries()) != 2 || access.GetEntries()[0].GetRemovable() || access.GetEntries()[1].GetRole() != RoleViewer || !access.GetEntries()[1].GetRemovable() {
		t.Fatalf("access = %+v, %v", access, err)
	}
	for _, bad := range []*documentv1.RevokeDocumentAccessRequest{{SubjectId: "reader"}, {DocumentId: "doc-1"}, {DocumentId: "doc-1", SubjectKind: "team", SubjectId: "t-1"}} {
		if _, err := s.RevokeDocumentAccess(ctx, bad); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("invalid revoke %+v = %v", bad, err)
		}
	}
	if _, err := s.RevokeDocumentAccess(ctx, &documentv1.RevokeDocumentAccessRequest{DocumentId: "doc-1", SubjectId: "reader"}); err != nil || f.subjectKind != "person" || f.subjectID != "reader" {
		t.Fatalf("revoke = %q %q, %v", f.subjectKind, f.subjectID, err)
	}

	// Service envelopes pass through unchanged.
	f.err = envelope.New(envelope.CodeNotFound, "document.unavailable", "the document does not exist or is not visible")
	if _, err := s.ListDocumentAccess(ctx, &documentv1.ListDocumentAccessRequest{DocumentId: "doc-1"}); status.Code(err) != codes.NotFound {
		t.Fatalf("access denial = %v", err)
	}
	f.err = envelope.New(envelope.CodeAlreadyExists, "document.folder_name_taken", "taken")
	if _, err := s.CreateDocumentFolder(ctx, &documentv1.CreateDocumentFolderRequest{Name: "Runbooks"}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("taken name = %v", err)
	}
	for name, call := range map[string]func() error{
		"library": func() error { _, err := s.GetDocumentLibrary(ctx, nil); return err },
		"rename": func() error {
			_, err := s.RenameDocumentFolder(ctx, &documentv1.RenameDocumentFolderRequest{FolderId: "f", Name: "n"})
			return err
		},
		"delete": func() error {
			_, err := s.DeleteDocumentFolder(ctx, &documentv1.DeleteDocumentFolderRequest{FolderId: "f"})
			return err
		},
		"move": func() error {
			_, err := s.MoveDocuments(ctx, &documentv1.MoveDocumentsRequest{DocumentIds: []string{"d"}})
			return err
		},
		"star": func() error {
			_, err := s.SetDocumentStarred(ctx, &documentv1.SetDocumentStarredRequest{DocumentId: "d"})
			return err
		},
		"revoke": func() error {
			_, err := s.RevokeDocumentAccess(ctx, &documentv1.RevokeDocumentAccessRequest{DocumentId: "d", SubjectId: "p"})
			return err
		},
	} {
		if status.Code(call()) != codes.AlreadyExists {
			t.Fatalf("%s dropped the service error", name)
		}
	}
	anonymous := context.Background()
	for name, call := range map[string]func() error{
		"create": func() error { _, err := s.CreateDocumentFolder(anonymous, nil); return err },
		"rename": func() error { _, err := s.RenameDocumentFolder(anonymous, nil); return err },
		"delete": func() error { _, err := s.DeleteDocumentFolder(anonymous, nil); return err },
		"move":   func() error { _, err := s.MoveDocuments(anonymous, nil); return err },
		"star":   func() error { _, err := s.SetDocumentStarred(anonymous, nil); return err },
		"access": func() error { _, err := s.ListDocumentAccess(anonymous, nil); return err },
		"revoke": func() error { _, err := s.RevokeDocumentAccess(anonymous, nil); return err },
	} {
		if status.Code(call()) != codes.Unauthenticated {
			t.Fatalf("anonymous %s admitted", name)
		}
	}
}

func TestDocumentSearchModesAndPages(t *testing.T) {
	now := time.Now().UTC()
	f := &fakeService{rows: []Summary{
		{DocumentID: "doc-a", Title: "Alpha", UpdatedAt: now, Snippet: "…the shift handover…", Match: "text"},
		{DocumentID: "doc-b", Title: "Bravo", UpdatedAt: now},
		{DocumentID: "doc-c", Title: "Charlie", UpdatedAt: now},
	}}
	s := &server{service: f, cursorKey: []byte("test-document-page-key")}
	ctx := documentContext(t)
	got, err := s.ListDocuments(ctx, &documentv1.ListDocumentsRequest{Query: "handov", SearchMode: SearchMeaning, PageSize: 2, Page: 3})
	if err != nil || f.options.Offset != 4 || f.options.Mode != SearchMeaning || got.GetSearchMode() != SearchSmart || got.GetSemanticAvailable() {
		t.Fatalf("meaning without a model = %+v, %v; options %+v", got, err, f.options)
	}
	if got.GetDocuments()[0].GetSnippet() != "…the shift handover…" || got.GetDocuments()[0].GetMatch() != "text" {
		t.Fatalf("snippet lost: %+v", got.GetDocuments()[0])
	}
	f.semantic = true
	got, err = s.ListDocuments(ctx, &documentv1.ListDocumentsRequest{Query: "wages", SearchMode: SearchMeaning, Sort: SortTitleDesc})
	if err != nil || got.GetSearchMode() != SearchMeaning || !got.GetSemanticAvailable() || f.options.Sort != SortTitleDesc {
		t.Fatalf("meaning = %+v, %v", got, err)
	}
	if _, err := s.ListDocuments(ctx, &documentv1.ListDocumentsRequest{Sort: SortRelevance}); err != nil || f.options.Sort != SortUpdated {
		t.Fatalf("relevance without a query = %+v, %v", f.options, err)
	}
	first, err := s.ListDocuments(ctx, &documentv1.ListDocumentsRequest{PageSize: 2, Sort: SortTitleDesc, Page: 2, PageToken: "ignored"})
	if err != nil || f.options.Offset != 2 || f.options.AfterTitle != nil || first.GetNextPageToken() == "" {
		t.Fatalf("page overrides the token: %+v, %v", f.options, err)
	}
	for _, order := range []string{SortUpdatedAsc, SortTitleDesc} {
		page, err := s.ListDocuments(ctx, &documentv1.ListDocumentsRequest{PageSize: 2, Sort: order})
		if err != nil || page.GetNextPageToken() == "" {
			t.Fatalf("%s page = %v", order, err)
		}
		if _, err := s.ListDocuments(ctx, &documentv1.ListDocumentsRequest{PageSize: 2, Sort: order, PageToken: page.GetNextPageToken()}); err != nil || f.options.BeforeID != "doc-b" {
			t.Fatalf("%s keyset = %+v, %v", order, f.options, err)
		}
	}
}

func TestDocumentOwnerSortPagesByOffset(t *testing.T) {
	now := time.Now().UTC()
	f := &fakeService{semantic: true, rows: []Summary{{DocumentID: "doc-a", UpdatedAt: now}, {DocumentID: "doc-b", UpdatedAt: now}, {DocumentID: "doc-c", UpdatedAt: now}}}
	s := &server{service: f, cursorKey: []byte("test-document-page-key")}
	ctx := documentContext(t)
	for _, order := range []string{SortOwner, SortOwnerDesc} {
		first, err := s.ListDocuments(ctx, &documentv1.ListDocumentsRequest{PageSize: 2, Sort: order})
		if err != nil || first.GetNextPageToken() == "" || f.options.Sort != order || first.GetSemanticPending() != 7 {
			t.Fatalf("%s first = %+v, %v", order, first, err)
		}
		if _, err := s.ListDocuments(ctx, &documentv1.ListDocumentsRequest{PageSize: 2, Sort: order, PageToken: first.GetNextPageToken()}); err != nil || f.options.Offset != 2 || f.options.BeforeID != "" {
			t.Fatalf("%s next = %+v, %v", order, f.options, err)
		}
	}
}

func TestDocumentCommentAnchorsRepliesAndResolve(t *testing.T) {
	f := &fakeService{}
	s := &server{service: f}
	ctx := documentContext(t)
	added, err := s.AddDocumentComment(ctx, &documentv1.AddDocumentCommentRequest{DocumentId: "doc-1", VersionId: "docv-1", Body: "Why?",
		Anchor: &documentv1.CommentAnchor{Quote: "six hours", Prefix: "accrue ", Suffix: " per", BlockId: "accrual", Start: 99, End: 999}})
	if err != nil || added.GetComment().GetAnchor().GetQuote() != "six hours" || added.GetComment().GetAnchor().GetStart() != 4 || added.GetComment().GetAnchor().GetEnd() != 13 {
		t.Fatalf("anchored comment = %+v, %v", added, err)
	}
	reply, err := s.AddDocumentComment(ctx, &documentv1.AddDocumentCommentRequest{DocumentId: "doc-1", VersionId: "docv-1", Body: "Because.", ParentCommentId: "docc-1", Anchor: &documentv1.CommentAnchor{}})
	if err != nil || reply.GetComment().GetParentCommentId() != "docc-1" || reply.GetComment().GetAnchor() != nil {
		t.Fatalf("reply = %+v, %v", reply, err)
	}
	for name, a := range map[string]*documentv1.CommentAnchor{
		"blank quote": {Prefix: "context"},
		"long quote":  {Quote: strings.Repeat("q", 501)},
		"long prefix": {Quote: "q", Prefix: strings.Repeat("p", 65)},
		"long suffix": {Quote: "q", Suffix: strings.Repeat("s", 65)},
		"long block":  {Quote: "q", BlockId: strings.Repeat("b", 129)},
	} {
		if _, err := s.AddDocumentComment(ctx, &documentv1.AddDocumentCommentRequest{DocumentId: "doc-1", VersionId: "docv-1", Body: "x", Anchor: a}); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("%s = %v", name, err)
		}
	}
	if _, err := s.AddDocumentComment(ctx, &documentv1.AddDocumentCommentRequest{DocumentId: "doc-1", VersionId: "docv-1", Body: "x", ParentCommentId: "docc-1", Anchor: &documentv1.CommentAnchor{Quote: "q"}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("anchored reply = %v", err)
	}
	if _, err := s.AddDocumentComment(ctx, &documentv1.AddDocumentCommentRequest{DocumentId: "doc-1", VersionId: "docv-1", Body: "x", ParentCommentId: " padded"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("bad parent = %v", err)
	}
	msg := commentMessage(Comment{ID: "c", Anchor: &CommentAnchor{Quote: "q", Start: -1, End: -1}, Orphaned: true, Resolved: true})
	if !msg.GetAnchorOrphaned() || !msg.GetResolved() || msg.GetAnchor().GetStart() != -1 {
		t.Fatalf("orphan message = %+v", msg)
	}
	if _, err := s.ResolveDocumentComment(ctx, &documentv1.ResolveDocumentCommentRequest{DocumentId: "doc-1", CommentId: "docc-1", Resolved: true}); err != nil || f.subjectID != "docc-1" || !f.starred {
		t.Fatalf("resolve = %+v, %v", f, err)
	}
	if _, err := s.ResolveDocumentComment(ctx, &documentv1.ResolveDocumentCommentRequest{DocumentId: "doc-1"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("resolve without comment = %v", err)
	}
	if _, err := s.ResolveDocumentComment(context.Background(), &documentv1.ResolveDocumentCommentRequest{DocumentId: "doc-1", CommentId: "c"}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("anonymous resolve = %v", err)
	}
	f.err = envelope.New(envelope.CodePermissionDenied, "document.comment_resolve_denied", "no")
	if _, err := s.ResolveDocumentComment(ctx, &documentv1.ResolveDocumentCommentRequest{DocumentId: "doc-1", CommentId: "c"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("denied resolve = %v", err)
	}
	if _, err := s.AddDocumentComment(ctx, &documentv1.AddDocumentCommentRequest{DocumentId: "doc-1", VersionId: "docv-1", Body: "x"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("service error dropped = %v", err)
	}
}

func TestDocumentGetReportsLinkTargets(t *testing.T) {
	s := &server{service: &fakeService{}}
	got, err := s.GetDocument(documentContext(t), &documentv1.GetDocumentRequest{DocumentId: "doc-1"})
	if err != nil || len(got.GetLinks()) != 2 {
		t.Fatalf("links = %+v, %v", got.GetLinks(), err)
	}
	if l := got.GetLinks()[0]; l.GetDocumentId() != "doc-2" || l.GetTitle() != "Readable" || !l.GetReadable() {
		t.Fatalf("readable link = %+v", l)
	}
	if l := got.GetLinks()[1]; l.GetDocumentId() != "doc-3" || l.GetTitle() != "" || l.GetReadable() {
		t.Fatalf("unreadable link leaked = %+v", l)
	}
}

package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/documentembed"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

func envelopeIs(err error, code envelope.Code, reason string) bool {
	e, ok := envelope.As(err)
	return ok && e.Code() == code && e.ReasonRef() == reason
}

func TestDocumentLibraryApplication_Integration(t *testing.T) {
	ctx := context.Background()
	svc := documentServiceFixture(t)
	const tenant, owner, reader, stranger = "tenant-library-app", "owner", "reader", "stranger"
	policy, _, err := svc.CreateDocument(ctx, tenant, owner, "Leave policy", "# Leave\nAccrual rules\n")
	if err != nil {
		t.Fatal(err)
	}
	draft, _, err := svc.CreateDocument(ctx, tenant, owner, "Private draft", "# Draft\nNot yet\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ShareDocument(ctx, tenant, owner, policy, reader, transportdocument.RoleViewer); err != nil {
		t.Fatal(err)
	}
	if err := svc.ShareDocument(ctx, tenant, owner, policy, reader, "editor"); !envelopeIs(err, envelope.CodeInvalidArgument, "document.invalid_role") {
		t.Fatalf("unknown role = %v", err)
	}
	if err := svc.ShareDocument(ctx, tenant, reader, policy, stranger, ""); !documentUnavailable(err) {
		t.Fatalf("reader reshared = %v", err)
	}
	viewer, _, _, err := svc.GetDocument(ctx, tenant, reader, policy)
	if err != nil || viewer.CanComment {
		t.Fatalf("viewer projection = %+v, %v", viewer, err)
	}

	folder, err := svc.CreateDocumentFolder(ctx, tenant, reader, "Policies")
	if err != nil || folder.ID == "" || folder.Name != "Policies" {
		t.Fatalf("create folder = %+v, %v", folder, err)
	}
	if _, err := svc.CreateDocumentFolder(ctx, tenant, reader, "POLICIES"); !envelopeIs(err, envelope.CodeAlreadyExists, "document.folder_name_taken") {
		t.Fatalf("duplicate folder = %v", err)
	}
	if _, err := svc.CreateDocumentFolder(ctx, tenant, reader, " "); !envelopeIs(err, envelope.CodeInvalidArgument, "document.invalid_folder_name") {
		t.Fatalf("blank folder = %v", err)
	}
	if err := svc.MoveDocuments(ctx, tenant, reader, []string{policy}, folder.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.MoveDocuments(ctx, tenant, reader, []string{draft}, folder.ID); !documentUnavailable(err) {
		t.Fatalf("filed unreadable document = %v", err)
	}
	if err := svc.MoveDocuments(ctx, tenant, owner, []string{draft}, folder.ID); !envelopeIs(err, envelope.CodeNotFound, "document.folder_unavailable") {
		t.Fatalf("filed into another person's folder = %v", err)
	}
	if err := svc.MoveDocuments(ctx, tenant, reader, make([]string, 101), folder.ID); err != nil {
		t.Fatalf("blank IDs are ignored, got %v", err)
	}
	if err := svc.SetDocumentStarred(ctx, tenant, reader, policy, true); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetDocumentStarred(ctx, tenant, reader, draft, true); !documentUnavailable(err) {
		t.Fatalf("starred unreadable document = %v", err)
	}

	got, _, _, err := svc.GetDocument(ctx, tenant, reader, policy)
	if err != nil || !got.Starred || got.FolderID != folder.ID || got.ReaderCount != 0 {
		t.Fatalf("reader document organization = %+v, %v", got, err)
	}
	ownerView, _, _, err := svc.GetDocument(ctx, tenant, owner, policy)
	if err != nil || ownerView.Starred || ownerView.FolderID != "" || ownerView.ReaderCount != 1 {
		t.Fatalf("owner document organization = %+v, %v", ownerView, err)
	}
	listed, err := svc.ListDocuments(ctx, tenant, reader, transportdocument.ListOptions{Limit: 10, FolderID: folder.ID, Sort: transportdocument.SortTitle})
	if err != nil || listed.Total != 1 || len(listed.Rows) != 1 || listed.Rows[0].DocumentID != policy || !listed.Rows[0].Starred || listed.Rows[0].TitleKey != "leave policy" {
		t.Fatalf("folder list = %+v, %v", listed, err)
	}
	listed, err = svc.ListDocuments(ctx, tenant, owner, transportdocument.ListOptions{Limit: 1, Query: "draft"})
	if err != nil || listed.Total != 1 || len(listed.Rows) != 1 || listed.Rows[0].DocumentID != draft || listed.Rows[0].Match != "title" {
		t.Fatalf("owner draft search = %+v, %v", listed, err)
	}
	lib, err := svc.GetDocumentLibrary(ctx, tenant, reader)
	if err != nil || lib.All != 1 || lib.Shared != 1 || lib.Mine != 0 || lib.Starred != 1 || len(lib.Folders) != 1 || lib.Folders[0].DocumentCount != 1 {
		t.Fatalf("reader library = %+v, %v", lib, err)
	}

	access, err := svc.ListDocumentAccess(ctx, tenant, owner, policy)
	if err != nil || len(access) != 2 || access[1].Role != transportdocument.RoleViewer {
		t.Fatalf("access = %+v, %v", access, err)
	}
	if _, err := svc.ListDocumentAccess(ctx, tenant, reader, policy); !documentUnavailable(err) {
		t.Fatalf("reader listed access = %v", err)
	}
	if _, err := svc.ListDocumentAccess(ctx, tenant, owner, "doc-missing"); !documentUnavailable(err) {
		t.Fatalf("missing document access = %v", err)
	}
	if err := svc.RevokeDocumentAccess(ctx, tenant, owner, policy, "team", reader); !envelopeIs(err, envelope.CodeInvalidArgument, "document.invalid_revoke") {
		t.Fatalf("team revoke = %v", err)
	}
	if err := svc.RevokeDocumentAccess(ctx, tenant, owner, policy, "person", owner); !envelopeIs(err, envelope.CodeFailedPrecondition, "document.owner_access") {
		t.Fatalf("owner revoke = %v", err)
	}
	if err := svc.RevokeDocumentAccess(ctx, tenant, reader, policy, "person", reader); !documentUnavailable(err) {
		t.Fatalf("reader revoke = %v", err)
	}
	if err := svc.RevokeDocumentAccess(ctx, tenant, owner, policy, "person", reader); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := svc.GetDocument(ctx, tenant, reader, policy); !documentUnavailable(err) {
		t.Fatalf("revoked reader opened document = %v", err)
	}

	if err := svc.RenameDocumentFolder(ctx, tenant, reader, folder.ID, "Archive"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RenameDocumentFolder(ctx, tenant, owner, folder.ID, "Mine"); !envelopeIs(err, envelope.CodeNotFound, "document.folder_unavailable") {
		t.Fatalf("renamed another person's folder = %v", err)
	}
	if err := svc.DeleteDocumentFolder(ctx, tenant, reader, folder.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteDocumentFolder(ctx, tenant, reader, folder.ID); !envelopeIs(err, envelope.CodeNotFound, "document.folder_unavailable") {
		t.Fatalf("repeat delete = %v", err)
	}
	lib, err = svc.GetDocumentLibrary(ctx, tenant, owner)
	if err != nil || lib.All != 2 || lib.Mine != 2 || len(lib.Folders) != 0 {
		t.Fatalf("owner library = %+v, %v", lib, err)
	}
	if _, err := svc.GetDocumentLibrary(ctx, tenant, ""); !documentUnavailable(err) {
		t.Fatalf("anonymous library = %v", err)
	}
}

func TestDocumentLibraryErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		err    error
		code   envelope.Code
		reason string
	}{
		{documenthubstore.ErrDenied, envelope.CodeNotFound, "document.unavailable"},
		{documenthubstore.ErrFolderNotFound, envelope.CodeNotFound, "document.folder_unavailable"},
		{documenthubstore.ErrFolderNameInvalid, envelope.CodeInvalidArgument, "document.invalid_folder_name"},
		{documenthubstore.ErrFolderNameTaken, envelope.CodeAlreadyExists, "document.folder_name_taken"},
		{documenthubstore.ErrTooManyDocuments, envelope.CodeInvalidArgument, "document.invalid_move"},
		{documenthubstore.ErrOwnerAccess, envelope.CodeFailedPrecondition, "document.owner_access"},
		{documenthubstore.ErrInvalidRole, envelope.CodeInvalidArgument, "document.invalid_role"},
	} {
		if got := libraryError(tc.err); !envelopeIs(got, tc.code, tc.reason) {
			t.Fatalf("libraryError(%v) = %v", tc.err, got)
		}
	}
	other := errors.New("database down")
	if libraryError(other) != other || libraryError(nil) != nil {
		t.Fatal("unmapped errors must pass through unchanged")
	}
}

// axisEmbedder is a stand-in local model: payroll text on one axis,
// everything else on the other.
type axisEmbedder struct {
	calls int
	fail  bool
}

func (e *axisEmbedder) Model() string { return "test-axis" }
func (e *axisEmbedder) Local() bool   { return true }
func (e *axisEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.calls++
	if e.fail {
		return nil, errors.New("model offline")
	}
	out := make([][]float32, len(texts))
	for i, text := range texts {
		out[i] = []float32{0.05, 1}
		if strings.Contains(strings.ToLower(text), "payroll") || strings.Contains(strings.ToLower(text), "wages") {
			out[i] = []float32{1, 0.05}
		}
	}
	return out, nil
}

func TestDocumentSearchApplication_Integration(t *testing.T) {
	ctx := context.Background()
	svc := documentServiceFixture(t)
	const tenant, owner, reader = "tenant-search-app", "owner", "reader"
	search := func(s documentService, actor, mode, query string) transportdocument.ListResult {
		t.Helper()
		res, err := s.ListDocuments(ctx, tenant, actor, transportdocument.ListOptions{Limit: 10, Query: query, Mode: mode})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := search(svc, reader, "meaning", "wages"); res.Mode != "smart" || res.SemanticAvailable {
		t.Fatalf("meaning without a model = %+v", res)
	}
	embedder := &axisEmbedder{}
	withModel := svc
	withModel.embedder = embedder
	svc.store.SetIndexModel(embedder.Model())
	withModel.indexer = documentembed.NewIndexer(svc.store, embedder, documentembed.IndexerConfig{})
	closeRun, _, err := withModel.CreateDocument(ctx, tenant, owner, "Payroll close runbook", "# Payroll close runbook\n\n## Steps\n\nReconcile the payroll bank file.\n")
	if err != nil {
		t.Fatal(err)
	}
	private, _, err := withModel.CreateDocument(ctx, tenant, owner, "Private payroll draft", "# Private payroll draft\n\nNot for readers.\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := withModel.ShareDocument(ctx, tenant, owner, closeRun, reader, "viewer"); err != nil {
		t.Fatal(err)
	}
	// Writes only enqueue: nothing is embedded until the indexer runs, and
	// meaning search meanwhile reports what is still pending.
	if embedder.calls != 0 {
		t.Fatalf("a request embedded inline: %d calls", embedder.calls)
	}
	if res := search(withModel, reader, "meaning", "wages"); res.SemanticPending != 1 || res.Total != 0 || res.Mode != "meaning" {
		t.Fatalf("before indexing = %+v", res)
	}
	if res := search(withModel, owner, "smart", "wages"); res.SemanticPending != 2 {
		t.Fatalf("owner pending = %+v", res)
	}
	if _, err := withModel.indexer.Drain(ctx, tenant, nil); err != nil {
		t.Fatal(err)
	}
	if res := search(withModel, owner, "smart", "wages"); res.SemanticPending != 0 {
		t.Fatalf("owner pending after drain = %+v", res)
	}
	res := search(withModel, reader, "meaning", "wages")
	if res.Mode != "meaning" || !res.SemanticAvailable || res.Total != 1 || res.Rows[0].DocumentID != closeRun || res.Rows[0].Match != "meaning" || res.Rows[0].Snippet == "" {
		t.Fatalf("reader meaning = %+v", res)
	}
	res = search(withModel, owner, "meaning", "wages")
	if res.Total != 2 {
		t.Fatalf("owner meaning over own draft = %+v", res)
	}
	for _, mode := range []string{"smart", "contains", "fuzzy", "meaning"} {
		for _, row := range search(withModel, reader, mode, "private payroll draft").Rows {
			if row.DocumentID == private {
				t.Fatalf("%s leaked the owner's private draft", mode)
			}
		}
	}
	embedder.fail = true
	if res := search(withModel, reader, "meaning", "payroll"); res.Mode != "smart" || res.Total != 1 {
		t.Fatalf("meaning with a failing model = %+v", res)
	}
	if res, err := withModel.ListDocuments(ctx, tenant, reader, transportdocument.ListOptions{Limit: 10, Mode: "meaning"}); err != nil || res.Mode != "meaning" || res.Total != 1 {
		t.Fatalf("listing with a model = %+v, %v", res, err)
	}
	if _, err := withModel.CreateDocumentVersion(ctx, tenant, owner, private, "missing-base", "x", "# x\n"); err == nil {
		t.Fatal("stale version accepted")
	}
}

func TestComposeDocumentEmbedderFromEnv(t *testing.T) {
	db := pgtest.NewEmpty(t)
	t.Setenv(documentembed.EnvURL, "http://127.0.0.1:1")
	t.Setenv(documentembed.EnvModel, "")
	if _, err := composeDocument(context.Background(), ServeConfig{DatabaseURL: "postgres://core@db/core", DocumentDatabaseURL: db.URL}); err == nil {
		t.Fatal("half-configured embedding accepted")
	}
	t.Setenv(documentembed.EnvModel, "nomic-embed")
	runtime, err := composeDocument(context.Background(), ServeConfig{DatabaseURL: "postgres://core@db/core", DocumentDatabaseURL: db.URL})
	if err != nil || runtime.embedder == nil || runtime.embedder.Model() != "nomic-embed" || documentembed.StoreModel(runtime.embedder).External || runtime.indexer == nil || runtime.store.IndexModel() != "nomic-embed" {
		t.Fatalf("composed embedder = %+v, %v", runtime.embedder, err)
	}
	runtime.close()
	t.Setenv(documentembed.EnvWorkers, "zero")
	if _, err := composeDocument(context.Background(), ServeConfig{DatabaseURL: "postgres://core@db/core", DocumentDatabaseURL: db.URL}); err == nil {
		t.Fatal("bad worker count accepted")
	}
}

func TestOwnerDisplayNames(t *testing.T) {
	for _, tc := range []struct{ preferred, legal, want string }{
		{"Rafa", "Rafael Torres", "Rafa Torres"},
		{"Rafa Torres-Diaz", "Rafael Torres", "Rafa Torres-Diaz"},
		{"", "Rafael Torres", "Rafael Torres"},
		{"Cher", "Cher", "Cher"},
		{"Cher", "", "Cher"},
		{"", "", "hc-050-rafael-torres"},
	} {
		if got := ownerDisplayName("hc-050-rafael-torres", tc.preferred, tc.legal); got != tc.want {
			t.Fatalf("ownerDisplayName(%q, %q) = %q, want %q", tc.preferred, tc.legal, got, tc.want)
		}
	}
	cache := &ownerNameCache{ttl: time.Minute, entries: map[string]ownerNameEntry{}}
	now := time.Unix(1000, 0)
	cache.store("t1", map[string]string{"a": "Ann A"}, now)
	got, missing := cache.lookup("t1", []string{"a", "b"}, now.Add(30*time.Second))
	if got["a"] != "Ann A" || len(missing) != 1 || missing[0] != "b" {
		t.Fatalf("cache hit = %v missing %v", got, missing)
	}
	if _, missing := cache.lookup("t1", []string{"a"}, now.Add(2*time.Minute)); len(missing) != 1 {
		t.Fatal("expired entry served")
	}
	if _, missing := cache.lookup("t2", []string{"a"}, now); len(missing) != 1 {
		t.Fatal("another tenant's name served")
	}
	if documentOwnerNames(nil) != nil {
		t.Fatal("resolver without a pool")
	}
}

func TestDocumentOwnerSortApplication_Integration(t *testing.T) {
	ctx := context.Background()
	svc := documentServiceFixture(t)
	const tenant, viewer = "tenant-owner-app", "viewer"
	for _, owner := range []string{"hc-2-zed", "hc-1-amy", "hc-3-bo"} {
		id, _, err := svc.CreateDocument(ctx, tenant, owner, "Note by "+owner, "# Note\n")
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.ShareDocument(ctx, tenant, owner, id, viewer, "viewer"); err != nil {
			t.Fatal(err)
		}
	}
	var asked []string
	svc.ownerNames = func(_ context.Context, tenantID string, ids []string) (map[string]string, error) {
		asked = ids
		return map[string]string{"hc-1-amy": "Zara Amy", "hc-2-zed": "Adam Zed", "hc-3-bo": "Mia Bo"}, nil
	}
	res, err := svc.ListDocuments(ctx, tenant, viewer, transportdocument.ListOptions{Limit: 10, Sort: "owner"})
	if err != nil || len(res.Rows) != 3 || len(asked) != 3 {
		t.Fatalf("owner sort = %+v, %v (asked %v)", res, err, asked)
	}
	if res.Rows[0].OwnerID != "hc-2-zed" || res.Rows[1].OwnerID != "hc-3-bo" || res.Rows[2].OwnerID != "hc-1-amy" {
		t.Fatalf("owner order = %s, %s, %s", res.Rows[0].OwnerID, res.Rows[1].OwnerID, res.Rows[2].OwnerID)
	}
	svc.ownerNames = func(context.Context, string, []string) (map[string]string, error) {
		return nil, errors.New("core down")
	}
	if _, err := svc.ListDocuments(ctx, tenant, viewer, transportdocument.ListOptions{Limit: 10, Sort: "owner_desc"}); err == nil {
		t.Fatal("name resolution failure hidden")
	}
}

func TestDocumentCommentThreadsApplication_Integration(t *testing.T) {
	ctx := context.Background()
	svc := documentServiceFixture(t)
	const tenant, owner, reader, viewer = "tenant-threads-app", "owner", "reader", "viewer"
	id, v1, err := svc.CreateDocument(ctx, tenant, owner, "Leave", "# Leave\n\n## Accrual\n\nStaff accrue six hours per pay period.\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ShareDocument(ctx, tenant, owner, id, reader, "commenter"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ShareDocument(ctx, tenant, owner, id, viewer, "viewer"); err != nil {
		t.Fatal(err)
	}
	anchor := &transportdocument.CommentAnchor{Quote: "six hours", Prefix: "accrue", BlockID: "accrual"}
	c, err := svc.AddDocumentComment(ctx, tenant, reader, id, v1, "Pro-rated?", anchor, "")
	if err != nil || c.Anchor == nil || c.Anchor.BlockID != "accrual" || c.Anchor.Start < 0 {
		t.Fatalf("anchored = %+v, %v", c, err)
	}
	if _, err := svc.AddDocumentComment(ctx, tenant, reader, id, v1, "x", &transportdocument.CommentAnchor{Quote: "nine hours"}, ""); !envelopeIs(err, envelope.CodeInvalidArgument, "document.invalid_anchor") {
		t.Fatalf("bad anchor = %v", err)
	}
	reply, err := svc.AddDocumentComment(ctx, tenant, owner, id, v1, "Yes.", nil, c.ID)
	if err != nil || reply.ParentID != c.ID {
		t.Fatalf("reply = %+v, %v", reply, err)
	}
	if _, err := svc.AddDocumentComment(ctx, tenant, reader, id, v1, "deep", nil, reply.ID); !envelopeIs(err, envelope.CodeInvalidArgument, "document.invalid_parent") {
		t.Fatalf("nested reply = %v", err)
	}
	if _, err := svc.AddDocumentComment(ctx, tenant, viewer, id, v1, "viewer", nil, ""); !documentUnavailable(err) {
		t.Fatalf("viewer commented = %v", err)
	}
	if err := svc.ResolveDocumentComment(ctx, tenant, viewer, id, c.ID, true); !envelopeIs(err, envelope.CodePermissionDenied, "document.comment_resolve_denied") {
		t.Fatalf("viewer resolve = %v", err)
	}
	if err := svc.ResolveDocumentComment(ctx, tenant, owner, id, "docc-missing", true); !envelopeIs(err, envelope.CodeNotFound, "document.comment_unavailable") {
		t.Fatalf("missing comment = %v", err)
	}
	if err := svc.ResolveDocumentComment(ctx, tenant, owner, id, reply.ID, true); !envelopeIs(err, envelope.CodeInvalidArgument, "document.comment_not_thread") {
		t.Fatalf("resolve reply = %v", err)
	}
	if err := svc.ResolveDocumentComment(ctx, tenant, "stranger", id, c.ID, true); !documentUnavailable(err) {
		t.Fatalf("stranger resolve = %v", err)
	}
	if err := svc.ResolveDocumentComment(ctx, tenant, owner, id, c.ID, true); err != nil {
		t.Fatal(err)
	}
	v2, err := svc.CreateDocumentVersion(ctx, tenant, owner, id, v1, "Leave", "# Leave\n\n## Accrual\n\nNew intro. Staff accrue six hours per pay period.\n")
	if err != nil {
		t.Fatal(err)
	}
	thread, err := svc.ListDocumentComments(ctx, tenant, owner, id, v2)
	if err != nil || len(thread) != 2 || !thread[0].Resolved || thread[0].Anchor.Start <= c.Anchor.Start || thread[1].ParentID != c.ID {
		t.Fatalf("owner thread on v2 = %+v, %v", thread, err)
	}
	if got := commentError(documenthubstore.ErrResolveNotTopic); !envelopeIs(got, envelope.CodeInvalidArgument, "document.comment_not_thread") || commentError(nil) != nil {
		t.Fatal("comment error mapping")
	}
}

func TestDocumentGetLinkTargets_Integration(t *testing.T) {
	ctx := context.Background()
	svc := documentServiceFixture(t)
	const tenant, owner, reader = "tenant-links-app", "owner", "reader"
	open, _, err := svc.CreateDocument(ctx, tenant, owner, "Open runbook", "# Open\n")
	if err != nil {
		t.Fatal(err)
	}
	closed, _, err := svc.CreateDocument(ctx, tenant, owner, "Closed plan", "# Closed\n")
	if err != nil {
		t.Fatal(err)
	}
	hub, _, err := svc.CreateDocument(ctx, tenant, owner, "Hub", "See [open](doc:"+open+") and [closed](doc:"+closed+").\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{open, hub} {
		if err := svc.ShareDocument(ctx, tenant, owner, id, reader, "viewer"); err != nil {
			t.Fatal(err)
		}
	}
	got, _, _, err := svc.GetDocument(ctx, tenant, reader, hub)
	if err != nil || len(got.Links) != 2 {
		t.Fatalf("reader links = %+v, %v", got.Links, err)
	}
	if got.Links[0] != (transportdocument.LinkTarget{DocumentID: open, Title: "Open runbook", Readable: true}) || got.Links[1] != (transportdocument.LinkTarget{DocumentID: closed}) {
		t.Fatalf("reader links = %+v", got.Links)
	}
	mine, _, _, err := svc.GetDocument(ctx, tenant, owner, hub)
	if err != nil || !mine.Links[1].Readable || mine.Links[1].Title != "Closed plan" {
		t.Fatalf("owner links = %+v, %v", mine.Links, err)
	}
}

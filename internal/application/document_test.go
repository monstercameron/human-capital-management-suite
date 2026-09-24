package application

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/pressly/goose/v3"
)

func TestTodo_HUB_001_ComposeDocument(t *testing.T) {
	ctx := context.Background()
	configured, err := ServeConfigFromValues(parseServe(t,
		"-database-url=postgres://core@db/core", "-dev-hmac-key="+testDevKey,
		"-document-database-url=postgres://docs@db/docs"))
	if err != nil || configured.DocumentDatabaseURL != "postgres://docs@db/docs" {
		t.Fatalf("document DSN not parsed: %+v err=%v", configured, err)
	}
	if runtime, err := composeDocument(ctx, ServeConfig{}); err != nil || runtime.store != nil || runtime.close != nil {
		t.Fatalf("unset document database composed: runtime=%+v err=%v", runtime, err)
	}
	core := "postgres://core@db:5432/core"
	if _, err := composeDocument(ctx, ServeConfig{DatabaseURL: core, DocumentDatabaseURL: core}); !errors.Is(err, documenthubstore.ErrIsolatedDatabase) {
		t.Fatalf("core database reused: %v", err)
	}
	chat := "postgres://chat@db:5432/chat"
	if _, err := composeDocument(ctx, ServeConfig{DatabaseURL: core, ChatDatabaseURL: chat, DocumentDatabaseURL: "postgres://docs@db:5432/chat"}); !errors.Is(err, documenthubstore.ErrIsolatedDatabase) {
		t.Fatalf("chat database reused: %v", err)
	}
}

func TestDocumentComments_Integration(t *testing.T) {
	ctx := context.Background()
	svc := documentServiceFixture(t)
	const tenant, owner, reader, readOnly = "tenant-comments", "owner", "reader", "read-only"
	id, versionID, err := svc.CreateDocument(ctx, tenant, owner, "Policy", "# Policy\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddDocumentComment(ctx, tenant, reader, id, versionID, "before share", nil, ""); err == nil {
		t.Fatal("unshared reader commented")
	}
	if err := svc.ShareDocument(ctx, tenant, owner, id, reader, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.ShareDocument(ctx, tenant, id, owner, documenthubstore.GrantInput{SubjectID: readOnly, Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectAllow}); err != nil {
		t.Fatal(err)
	}
	readerSummary, _, _, err := svc.GetDocument(ctx, tenant, reader, id)
	if err != nil || !readerSummary.CanComment || readerSummary.VersionID != versionID {
		t.Fatalf("reader projection = %+v, %v", readerSummary, err)
	}
	readOnlySummary, _, _, err := svc.GetDocument(ctx, tenant, readOnly, id)
	if err != nil || readOnlySummary.CanComment {
		t.Fatalf("read-only projection = %+v, %v", readOnlySummary, err)
	}
	if _, err := svc.AddDocumentComment(ctx, tenant, readOnly, id, versionID, "forged comment", nil, ""); err == nil {
		t.Fatal("read-only actor commented")
	}
	comment, err := svc.AddDocumentComment(ctx, tenant, reader, id, versionID, "Please clarify this rule", nil, "")
	if err != nil || comment.ID == "" || comment.CreatedAt.IsZero() || comment.AuthorID != reader {
		t.Fatalf("add comment = %+v, %v", comment, err)
	}
	thread, err := svc.ListDocumentComments(ctx, tenant, owner, id, versionID)
	if err != nil || len(thread) != 1 || thread[0].ID != comment.ID {
		t.Fatalf("owner thread = %+v, %v", thread, err)
	}
	if _, err := svc.ListDocumentComments(ctx, tenant, reader, id, "forged-version"); !documentUnavailable(err) {
		t.Fatalf("forged version list = %v", err)
	}
	if _, err := svc.AddDocumentComment(ctx, tenant, reader, id, "forged-version", "wrong version", nil, ""); !documentUnavailable(err) {
		t.Fatalf("forged version comment = %v", err)
	}
	if _, err := svc.store.GrantAction(ctx, tenant, documenthubstore.GrantInput{DocumentID: id, SubjectID: reader, Action: documenthubstore.ActionComment, Effect: documenthubstore.EffectDeny, Issuer: owner}); err != nil {
		t.Fatal(err)
	}
	readerSummary, _, _, err = svc.GetDocument(ctx, tenant, reader, id)
	if err != nil || readerSummary.CanComment {
		t.Fatalf("comment-denied reader projection = %+v, %v", readerSummary, err)
	}
	if _, err := svc.AddDocumentComment(ctx, tenant, reader, id, versionID, "after comment deny", nil, ""); !documentUnavailable(err) {
		t.Fatalf("comment-denied reader commented = %v", err)
	}
	thread, err = svc.ListDocumentComments(ctx, tenant, reader, id, versionID)
	if err != nil || len(thread) != 1 {
		t.Fatalf("comment-denied reader lost read access = %+v, %v", thread, err)
	}
	if _, err := svc.store.GrantAction(ctx, tenant, documenthubstore.GrantInput{DocumentID: id, SubjectID: reader, Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectDeny, Issuer: owner}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddDocumentComment(ctx, tenant, reader, id, versionID, "after deny", nil, ""); !documentUnavailable(err) {
		t.Fatalf("denied reader commented = %v", err)
	}
	if _, err := svc.ListDocumentComments(ctx, "other-tenant", owner, id, versionID); !documentUnavailable(err) {
		t.Fatalf("cross-tenant thread = %v", err)
	}
}

func documentServiceFixture(t *testing.T) documentService {
	t.Helper()
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(documenthubstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	documentURL, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	query := documentURL.Query()
	query.Set("search_path", db.Schema)
	documentURL.RawQuery = query.Encode()
	runtime, err := composeDocument(ctx, ServeConfig{DatabaseURL: "postgres://core@db/core", DocumentDatabaseURL: documentURL.String()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.close)
	return documentService{store: runtime.store, recipientAllowed: func(context.Context, string, string) (bool, error) { return true, nil }}
}

func TestDocumentVersionApplication_Integration(t *testing.T) {
	ctx := context.Background()
	svc := documentServiceFixture(t)
	const tenant, owner, reader = "tenant-edits", "owner", "reader"
	id, base, err := svc.CreateDocument(ctx, tenant, owner, "Guide", "# Original\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ShareDocument(ctx, tenant, owner, id, reader, ""); err != nil {
		t.Fatal(err)
	}
	ownerSummary, _, _, err := svc.GetDocument(ctx, tenant, owner, id)
	if err != nil || !ownerSummary.CanEdit {
		t.Fatalf("owner edit capability = %+v, %v", ownerSummary, err)
	}
	readerSummary, _, _, err := svc.GetDocument(ctx, tenant, reader, id)
	if err != nil || readerSummary.CanEdit {
		t.Fatalf("reader edit capability = %+v, %v", readerSummary, err)
	}
	if _, err := svc.CreateDocumentVersion(ctx, tenant, reader, id, base, "Forgery", "# Forged\n"); !documentUnavailable(err) {
		t.Fatalf("reader edited = %v", err)
	}
	created, err := svc.CreateDocumentVersion(ctx, tenant, owner, id, base, "Updated guide", "# Revised\n")
	if err != nil || created == "" || created == base {
		t.Fatalf("owner save = %q, %v", created, err)
	}
	ownerSummary, ownerMarkdown, _, err := svc.GetDocument(ctx, tenant, owner, id)
	if err != nil || ownerSummary.VersionID != created || ownerMarkdown != "# Revised\n" {
		t.Fatalf("owner sees revision = %+v %q, %v", ownerSummary, ownerMarkdown, err)
	}
	readerSummary, readerMarkdown, _, err := svc.GetDocument(ctx, tenant, reader, id)
	if err != nil || readerSummary.VersionID != base || readerMarkdown != "# Original\n" {
		t.Fatalf("reader saw unpublished edit = %+v %q, %v", readerSummary, readerMarkdown, err)
	}
	if _, err := svc.CreateDocumentVersion(ctx, tenant, owner, id, base, "Stale", "# Stale\n"); !documentStale(err) {
		t.Fatalf("stale edit = %v", err)
	}
	if _, err := svc.CreateDocumentVersion(ctx, "other-tenant", owner, id, created, "Cross tenant", "# Cross\n"); !documentUnavailable(err) {
		t.Fatalf("cross-tenant edit = %v", err)
	}
}

func documentStale(err error) bool {
	e, ok := envelope.As(err)
	return ok && e.Code() == envelope.CodeAborted && e.ReasonRef() == "document.stale_version"
}

func documentUnavailable(err error) bool {
	e, ok := envelope.As(err)
	return ok && e.ReasonRef() == "document.unavailable"
}

func TestTodo_HUB_001_ComposeDocument_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	runtime, err := composeDocument(context.Background(), ServeConfig{DatabaseURL: "postgres://core@db/core", DocumentDatabaseURL: db.URL})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.store == nil || runtime.close == nil {
		t.Fatal("document database pool missing")
	}
	runtime.close()
}

func TestTodo_HUB_001_DocumentPoolLifecycle(t *testing.T) {
	db := pgtest.NewEmpty(t)
	cfg := stubServeConfig()
	cfg.DocumentDatabaseURL = db.URL
	app, _, _ := composeStub(t, cfg)
	if node, ok := app.Graph().Component(ComponentDocumentStore); !ok || node.Kind != KindAdapter {
		t.Fatalf("document pool missing from composed graph: %+v %t", node, ok)
	}
	shutdown := app.Runtime().Shutdown
	if len(shutdown) != 5 || shutdown[4].Name != shutdownNameDocument {
		t.Fatalf("document pool has no independent shutdown: %+v", shutdown)
	}
}

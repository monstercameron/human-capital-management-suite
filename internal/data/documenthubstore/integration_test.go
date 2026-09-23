package documenthubstore

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

func documentFixture(t *testing.T) (*Store, string) {
	t.Helper()
	db := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Up(context.Background()); err != nil {
		t.Fatalf("document migrations: %v", err)
	}
	u, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", db.Schema)
	u.RawQuery = q.Encode()
	s, err := New(context.Background(), Config{
		DSN:     u.String(),
		ChatDSN: "postgres://chat:pw@127.0.0.1:1/chat",
		CoreDSN: "postgres://core:pw@127.0.0.1:1/core",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, db.Schema
}

// TestTodo_HUB_001_Integration is the INTEGRATION test for HUB-001: Knowledge
// persists through its own migrated database and outbox, with no
// chat/workflow tables in the same schema and no shared pool.
func TestTodo_HUB_001_Integration(t *testing.T) {
	s, schema := documentFixture(t)
	ctx := context.Background()

	err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO document_outbox(tenant_id,aggregate_id,event_type,payload) VALUES($1,$2,$3,$4)`, "tenant-a", "doc-1", "version.proposed", `{"v":1}`)
		return err
	})
	if err != nil {
		t.Fatalf("document outbox write: %v", err)
	}

	var count int
	err = s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM document_outbox WHERE tenant_id=$1 AND aggregate_id=$2`, "tenant-a", "doc-1").Scan(&count)
	})
	if err != nil || count != 1 {
		t.Fatalf("document outbox readback: count=%d err=%v", count, err)
	}

	var foreign int
	err = s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema=$1 AND table_name LIKE 'chat\_%' ESCAPE '\'`, schema).Scan(&foreign)
	})
	if err != nil || foreign != 0 {
		t.Fatalf("chat tables inside document schema: count=%d err=%v", foreign, err)
	}
}

// TestTodo_HUB_004_Integration is the INTEGRATION test for HUB-004: version
// rows persist normalized Markdown with its digest, and stored versions
// cannot be changed after citation.
func TestTodo_HUB_004_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-1", "PERSONAL")
	if err != nil {
		t.Fatalf("create document: %v", err)
	}
	stored, err := s.InsertVersion(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-1", Title: "Guide", Markdown: "head\r\nbody\n"})
	if err != nil {
		t.Fatalf("insert version: %v", err)
	}
	if stored.Markdown != "head\nbody\n" || stored.Hash != HashContent("head\nbody\n") {
		t.Fatalf("stored bytes not normalized+hashed: %+v", stored)
	}
	var roundtrip string
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT normalized_markdown FROM document_version WHERE tenant_id=$1 AND id=$2`, "tenant-a", stored.ID).Scan(&roundtrip)
	}); err != nil || roundtrip != "head\nbody\n" {
		t.Fatalf("version readback: %q err=%v", roundtrip, err)
	}
	if _, err := s.InsertVersion(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-1", Markdown: "other\n", Hash: stored.Hash}); err == nil {
		t.Fatal("forged version hash accepted")
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE document_version SET title='rewritten' WHERE tenant_id='tenant-a' AND id=$1`, stored.ID)
		return err
	}); err == nil {
		t.Fatal("stored version changed after citation")
	}
}

// TestTodo_HUB_006_Integration is the INTEGRATION test for HUB-006: the
// status column defaults to candidate inside the lifecycle vocabulary, and
// tip resolution follows the parent chain rather than the wall clock.
func TestTodo_HUB_006_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-1", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-1", Markdown: "a\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	var status string
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT status FROM document_version WHERE tenant_id=$1 AND id=$2`, "tenant-a", first.ID).Scan(&status)
	}); err != nil {
		t.Fatal(err)
	}
	// A CHECK violation aborts its own transaction, so each probe below runs
	// separately; a refusal must never poison neighboring writes.
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO document_version(id,tenant_id,document_id,parent_id,creator_id,normalized_markdown,content_hash,status,created_at) VALUES('docv-clash','tenant-a',$1,'x','u-1','m','h','draft',now())`, docID)
		return err
	}); err == nil {
		t.Fatal("status outside the lifecycle vocabulary accepted")
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO document_version(id,tenant_id,document_id,parent_id,creator_id,normalized_markdown,content_hash,created_at) VALUES('docv-older','tenant-a',$1,$2,'u-1','older','h2','2020-01-01T00:00:00Z')`, docID, first.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if status != "candidate" {
		t.Fatalf("submit status %q, want candidate", status)
	}
	// docv-older is older by the clock but newer by the chain: the tip must
	// follow the chain, so basing on first now conflicts with docv-older.
	_, err = s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-1", Markdown: "b\n"}, first.ID)
	var conflict *VersionConflict
	if !errors.As(err, &conflict) || conflict.Current.ID != "docv-older" {
		t.Fatalf("tip followed the clock instead of the chain: %v", err)
	}
	second, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-1", Markdown: "b\n"}, "docv-older")
	if err != nil {
		t.Fatalf("submit on chain tip: %v", err)
	}
	if second.ParentID != "docv-older" {
		t.Fatalf("chain broken: %+v", second)
	}
}

// TestTodo_HUB_007_Integration is the INTEGRATION test for HUB-007: moving
// a pointer appends deployment history instead of rewriting it, and the
// schema refuses deployments of unknown versions.
func TestTodo_HUB_007_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-1", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-1", Markdown: "one\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-1", Markdown: "two\n"}, v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	seedDeployment(t, s, "tenant-a", docID, v1.ID, "default", "")
	seedDeployment(t, s, "tenant-a", docID, v2.ID, "default", "")
	var deployments int
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM document_deployment WHERE tenant_id=$1 AND document_id=$2`, "tenant-a", docID).Scan(&deployments)
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO document_deployment(id,tenant_id,document_id,version_id,deployer_id) VALUES('dep-ghost','tenant-a',$1,'docv-missing','u-1')`, docID)
		return err
	}); err == nil {
		t.Fatal("deployment of unknown version accepted")
	}
	if deployments != 2 {
		t.Fatalf("pointer move rewrote history: %d deployment rows", deployments)
	}
	got, err := s.ResolveDeployment(ctx, "tenant-a", docID, "default", "")
	if err != nil || got.VersionID != v2.ID {
		t.Fatalf("pointer does not name the latest deployment: %+v err=%v", got, err)
	}
}

// TestTodo_HUB_008_Integration is the INTEGRATION test for HUB-008: the
// latest decision wins per version and scope, and the schema refuses
// decisions outside the vocabulary or for unknown versions.
func TestTodo_HUB_008_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "r\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "default", ScopeID: "", ReviewerID: "u-r1", Authority: "team:leads", Decision: "changes_requested"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "default", ScopeID: "", ReviewerID: "u-r2", Authority: "team:leads", Decision: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	latest, err := s.LatestReview(ctx, "tenant-a", docID, v.ID, "default", "")
	if err != nil || latest.ID != second.ID || latest.Decision != "approved" {
		t.Fatalf("re-review did not supersede: %+v err=%v", latest, err)
	}
	var reviews int
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM document_review WHERE tenant_id=$1 AND version_id=$2`, "tenant-a", v.ID).Scan(&reviews)
	}); err != nil || reviews != 2 {
		t.Fatalf("re-review rewrote history: count=%d err=%v", reviews, err)
	}
	if first.Decision != "changes_requested" || first.ID == second.ID {
		t.Fatalf("first decision not preserved: %+v vs %+v", first, second)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO document_review(id,tenant_id,document_id,version_id,version_hash,reviewer_id,authority,decision) VALUES('docr-ghost','tenant-a',$1,'docv-missing','h','u-r1','team:leads','approved')`, docID)
		return err
	}); err == nil {
		t.Fatal("review of unknown version accepted")
	}
}

// TestTodo_HUB_010_Integration is the INTEGRATION test for HUB-010:
// withdrawal deletes only the pointer row, and a version live in another
// scope stays deployed instead of retiring.
func TestTodo_HUB_010_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "one\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range [][2]string{{"default", ""}, {"placement", "chan-A"}} {
		if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v.ID, ScopeKind: scope[0], ScopeID: scope[1], ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, g := range []GrantInput{
		{DocumentID: docID, SubjectKind: "person", SubjectID: "u-deployer", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-owner"},
		{DocumentID: docID, SubjectKind: "person", SubjectID: "u-deployer", Action: ActionRetire, Effect: EffectAllow, Issuer: "u-owner"},
	} {
		if _, err := s.GrantAction(ctx, "tenant-a", g); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Deploy(ctx, "tenant-a", DeployInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "default", ScopeID: "", DeployerID: "u-deployer"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deploy(ctx, "tenant-a", DeployInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "placement", ScopeID: "chan-A", DeployerID: "u-deployer"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Withdraw(ctx, "tenant-a", WithdrawInput{DocumentID: docID, ScopeKind: "default", ScopeID: "", ActorID: "u-deployer", ExpectedLive: v.ID, Reason: "error"}); err != nil {
		t.Fatal(err)
	}
	var pointers, deployments int
	var status string
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_active_pointer WHERE tenant_id=$1 AND document_id=$2`, "tenant-a", docID).Scan(&pointers); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_deployment WHERE tenant_id=$1 AND document_id=$2`, "tenant-a", docID).Scan(&deployments); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT status FROM document_version WHERE tenant_id=$1 AND id=$2`, "tenant-a", v.ID).Scan(&status)
	}); err != nil {
		t.Fatal(err)
	}
	if pointers != 1 || deployments != 2 || status != "deployed" {
		t.Fatalf("withdrawal touched other scopes or history: pointers=%d deployments=%d status=%q", pointers, deployments, status)
	}
	if _, err := s.ResolveDeployment(ctx, "tenant-a", docID, "placement", "chan-A"); err != nil {
		t.Fatalf("surviving placement lost: %v", err)
	}
}

// TestTodo_HUB_012_Integration is the INTEGRATION test for HUB-012: shares
// attribute their issuer, a manager may share and preview, and revocation
// shrinks the audience.
func TestTodo_HUB_012_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-owner", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ShareDocument(ctx, "tenant-a", docID, "u-owner", GrantInput{SubjectKind: "person", SubjectID: "u-manager", Action: ActionManage, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	share, err := s.ShareDocument(ctx, "tenant-a", docID, "u-manager", GrantInput{SubjectKind: "person", SubjectID: "u-friend", Action: ActionRead, Effect: EffectAllow})
	if err != nil {
		t.Fatalf("manager share refused: %v", err)
	}
	if share.Issuer != "u-manager" {
		t.Fatalf("share misattributes issuer: %+v", share)
	}
	before, err := s.Audience(ctx, "tenant-a", docID, "u-manager")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeGrant(ctx, "tenant-a", share.ID, "u-owner"); err != nil {
		t.Fatal(err)
	}
	after, err := s.Audience(ctx, "tenant-a", docID, "u-owner")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before)-1 {
		t.Fatalf("revocation did not shrink audience: %d -> %d", len(before), len(after))
	}
	if err := s.Authorize(ctx, "tenant-a", docID, "person", "u-friend", ActionRead); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked share still authorizes")
	}
}

// TestTodo_HUB_018_Integration is the INTEGRATION test for HUB-018: deploy
// and withdraw cycles never touch discussion, and stored comments cannot
// be edited after posting.
func TestTodo_HUB_018_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "one\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{ActionRead, ActionComment} {
		if _, err := s.ShareDocument(ctx, "tenant-a", docID, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: action, Effect: EffectAllow}); err != nil {
			t.Fatal(err)
		}
	}
	c, err := s.AddComment(ctx, "tenant-a", CommentInput{DocumentID: docID, VersionID: v.ID, AuthorID: "u-anna", Body: "note @u-bob"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "default", ScopeID: "", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	for _, g := range []GrantInput{
		{SubjectKind: "person", SubjectID: "u-deployer", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-owner"},
		{SubjectKind: "person", SubjectID: "u-deployer", Action: ActionRetire, Effect: EffectAllow, Issuer: "u-owner"},
	} {
		g.DocumentID = docID
		if _, err := s.GrantAction(ctx, "tenant-a", g); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "default", ScopeID: "", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deploy(ctx, "tenant-a", DeployInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "default", ScopeID: "", DeployerID: "u-deployer"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Withdraw(ctx, "tenant-a", WithdrawInput{DocumentID: docID, ScopeKind: "default", ScopeID: "", ActorID: "u-deployer", ExpectedLive: v.ID, Reason: "error"}); err != nil {
		t.Fatal(err)
	}
	err = s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, execErr := tx.Exec(ctx, `UPDATE document_comment SET body='edited' WHERE tenant_id='tenant-a' AND id=$1`, c.ID)
		return execErr
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be changed") {
		t.Fatalf("posted comment edited: %v", err)
	}
	thread, err := s.ListComments(ctx, "tenant-a", docID, v.ID, "person", "u-author")
	if err != nil || len(thread) != 1 || thread[0].Body != "note @u-bob" {
		t.Fatalf("lifecycle moved discussion: %+v err=%v", thread, err)
	}
}

// TestTodo_HUB_001_Security is the SECURITY test for HUB-001: every
// document-table statement scopes by tenant, the tenant_isolation RLS policy
// stands as a forced backstop, the outbox is append-only, and writes without
// a tenant are refused without leaking existence.
func TestTodo_HUB_001_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()

	if err := s.RunTenantTx(ctx, "", func(tx dbport.Tx) error { return nil }); err == nil {
		t.Fatal("tenantless document transaction accepted")
	}

	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO document_outbox(tenant_id,aggregate_id,event_type,payload) VALUES($1,$2,$3,$4)`, "tenant-a", "doc-9", "version.proposed", `{}`)
		return err
	}); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	var leaked int
	if err := s.RunTenantTx(ctx, "tenant-b", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM document_outbox WHERE tenant_id=$1 AND aggregate_id=$2`, "tenant-b", "doc-9").Scan(&leaked)
	}); err != nil || leaked != 0 {
		t.Fatalf("cross-tenant document read: count=%d err=%v", leaked, err)
	}

	var policies, forced int
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM pg_policies WHERE schemaname=current_schema() AND tablename IN ('document_outbox','document_outbox_receipt') AND policyname='tenant_isolation'`).Scan(&policies); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM pg_class WHERE relnamespace=current_schema()::regnamespace AND relname IN ('document_outbox','document_outbox_receipt') AND relrowsecurity AND relforcerowsecurity`).Scan(&forced)
	}); err != nil || policies != 2 || forced != 2 {
		t.Fatalf("tenant_isolation backstop missing: policies=%d forced=%d err=%v", policies, forced, err)
	}

	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE document_outbox SET event_type='forged' WHERE tenant_id='tenant-a' AND aggregate_id='doc-9'`)
		return err
	}); err == nil || !strings.Contains(err.Error(), "cannot be changed") {
		t.Fatalf("outbox rewrite accepted: err=%v", err)
	}
}

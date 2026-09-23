package documenthubstore

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func linkDoc(t *testing.T, s *Store, ctx context.Context, tenant, markdown string) (string, Version) {
	t.Helper()
	docID, err := s.CreateDocument(ctx, tenant, "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.SubmitCandidate(ctx, tenant, Version{DocumentID: docID, CreatorID: "u-author", Title: "Doc " + docID, Markdown: markdown}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.StoreLinks(ctx, tenant, docID, v.ID, ExtractLinks(markdown)); err != nil {
		t.Fatal(err)
	}
	return docID, v
}

func grantRead(t *testing.T, s *Store, ctx context.Context, tenant, docID, subject string) {
	t.Helper()
	if _, err := s.ShareDocument(ctx, tenant, docID, "u-author", GrantInput{SubjectKind: "person", SubjectID: subject, Action: ActionRead, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
}

func outboxCount(t *testing.T, s *Store, ctx context.Context, tenant, aggregate, event string) int {
	t.Helper()
	var n int
	if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM document_outbox WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type=$3`, tenant, aggregate, event).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

func deployVersion(t *testing.T, s *Store, ctx context.Context, tenant, docID, versionID, live string) {
	t.Helper()
	if _, err := s.RecordReview(ctx, tenant, ReviewInput{DocumentID: docID, VersionID: versionID, ScopeKind: "default", ScopeID: "", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, tenant, GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: "u-deployer", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	in := DeployInput{DocumentID: docID, VersionID: versionID, ScopeKind: "default", ScopeID: "", DeployerID: "u-deployer", ExpectedLive: live}
	if _, err := s.Deploy(ctx, tenant, in); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_HUB_022 is the PRIMARY test for HUB-022: backlinks show only
// jointly readable sources, the checker marks broken and stale targets,
// and owner alerts fire once per transition.
func TestTodo_HUB_022(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docB, _ := linkDoc(t, s, ctx, "tenant-a", "# Bee\n\nHoney.\n")
	md := "See [bee](doc:" + docB + ").\n"
	docA, vA := linkDoc(t, s, ctx, "tenant-a", md)
	grantRead(t, s, ctx, "tenant-a", docA, "u-anna")
	grantRead(t, s, ctx, "tenant-a", docB, "u-anna")
	grantRead(t, s, ctx, "tenant-a", docB, "u-bob")
	back, err := s.Backlinks(ctx, "tenant-a", docB, "person", "u-anna")
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].SourceDocID != docA || back[0].SourceTitle == "" || back[0].Label != "bee" {
		t.Fatalf("backlink wrong: %+v", back)
	}
	hidden, err := s.Backlinks(ctx, "tenant-a", docB, "person", "u-bob")
	if err != nil {
		t.Fatal(err)
	}
	if len(hidden) != 0 {
		t.Fatalf("restricted source title disclosed: %+v", hidden)
	}
	report, err := s.CheckLinks(ctx, "tenant-a", docA, vA.ID, "u-author")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("healthy links flagged: %+v", report)
	}
	docC, vC := linkDoc(t, s, ctx, "tenant-a", "See [ghost](doc:doc-missing).\n")
	report, err = s.CheckLinks(ctx, "tenant-a", docC, vC.ID, "u-author")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 1 || report.Findings[0].Code != LinkUnknownTarget {
		t.Fatalf("broken target not marked: %+v", report)
	}
	if n := outboxCount(t, s, ctx, "tenant-a", docC, "link.broken"); n != 1 {
		t.Fatalf("owner alert missing: %d", n)
	}
	if _, err := s.CheckLinks(ctx, "tenant-a", docC, vC.ID, "u-author"); err != nil {
		t.Fatal(err)
	}
	if n := outboxCount(t, s, ctx, "tenant-a", docC, "link.broken"); n != 1 {
		t.Fatalf("repeat check re-alerted: %d", n)
	}
}

// TestTodo_HUB_022_Security is the SECURITY test for HUB-022: backlinks
// need target read, checking needs authority, and tenants stay separate.
func TestTodo_HUB_022_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docB, _ := linkDoc(t, s, ctx, "tenant-a", "# Bee\n\nHoney.\n")
	docA, vA := linkDoc(t, s, ctx, "tenant-a", "See [bee](doc:"+docB+").\n")
	if _, err := s.Backlinks(ctx, "tenant-a", docB, "person", "u-stranger"); err == nil {
		t.Fatal("backlinks without target read")
	}
	if _, err := s.CheckLinks(ctx, "tenant-a", docA, vA.ID, "u-stranger"); err == nil {
		t.Fatal("check without authority")
	}
	if _, err := s.Backlinks(ctx, "tenant-b", docB, "person", "u-anna"); err == nil {
		t.Fatal("cross-tenant backlinks")
	}
}

// TestTodo_HUB_022_Integration is the INTEGRATION test for HUB-022: the
// backlink roundtrip reaches the real store, renamed anchors read stale,
// and retired sources leave the backlink set.
func TestTodo_HUB_022_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docB, vB := linkDoc(t, s, ctx, "tenant-a", "# Intro\n\nHoney.\n")
	deployVersion(t, s, ctx, "tenant-a", docB, vB.ID, "")
	if err := s.StoreBlocks(ctx, "tenant-a", docB, vB.ID, DeriveBlocks("# Intro\n\nHoney.\n")); err != nil {
		t.Fatal(err)
	}
	docA, vA := linkDoc(t, s, ctx, "tenant-a", "See [part](doc:"+docB+"#intro).\n")
	grantRead(t, s, ctx, "tenant-a", docA, "u-anna")
	grantRead(t, s, ctx, "tenant-a", docB, "u-anna")
	grantRead(t, s, ctx, "tenant-a", docB, "u-deployer")
	report, err := s.CheckLinks(ctx, "tenant-a", docA, vA.ID, "u-author")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("live anchor flagged: %+v", report)
	}
	deployVersion(t, s, ctx, "tenant-a", docA, vA.ID, "")
	vB2, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docB, CreatorID: "u-author", Title: "T", Markdown: "# Opening\n\nHoney.\n"}, vB.ID)
	if err != nil {
		t.Fatal(err)
	}
	deployVersion(t, s, ctx, "tenant-a", docB, vB2.ID, vB.ID)
	if err := s.StoreBlocks(ctx, "tenant-a", docB, vB2.ID, DeriveBlocks("# Opening\n\nHoney.\n")); err != nil {
		t.Fatal(err)
	}
	report, err = s.CheckLinks(ctx, "tenant-a", docA, vA.ID, "u-author")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 1 || report.Findings[0].Code != LinkStaleAnchor {
		t.Fatalf("renamed anchor not stale: %+v", report)
	}
	back, err := s.Backlinks(ctx, "tenant-a", docB, "person", "u-anna")
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].State != LinkStale {
		t.Fatalf("backlink not current: %+v", back)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docA, SubjectKind: "person", SubjectID: "u-author", Action: ActionRetire, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Withdraw(ctx, "tenant-a", WithdrawInput{DocumentID: docA, ScopeKind: "default", ScopeID: "", ActorID: "u-author", ExpectedLive: vA.ID, Reason: "expiry"}); err != nil {
		t.Fatal(err)
	}
	back, err = s.Backlinks(ctx, "tenant-a", docB, "person", "u-anna")
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 0 {
		t.Fatalf("retired source still backlinked: %+v", back)
	}
}

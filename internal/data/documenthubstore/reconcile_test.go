package documenthubstore

import (
	"context"
	"errors"
	"testing"
)

// TestTodo_HUB_029 is the PRIMARY test for HUB-029: the outbox consumer
// rebuilds live versions, deletes chunks of dead versions, and records a
// watermark that makes replay a no-op.
func TestTodo_HUB_029(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1 := searchFixture(t, s, ctx, "tenant-a", docA, "# Cats\n\nA guide to cats.\n")
	if _, err := s.EmbedSections(ctx, "tenant-a", docA, v1.ID, testEmbeddingPolicy(), "hub-internal-v1", stubEmbedder); err != nil {
		t.Fatal(err)
	}
	grantRead(t, s, ctx, "tenant-a", docA, "u-anna")
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docA, SubjectKind: "person", SubjectID: "u-author", Action: ActionRetire, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Withdraw(ctx, "tenant-a", WithdrawInput{DocumentID: docA, ScopeKind: "default", ScopeID: "", ActorID: "u-author", ExpectedLive: v1.ID, Reason: "expiry"}); err != nil {
		t.Fatal(err)
	}
	first, err := s.ReconcileDocument(ctx, "tenant-a", docA, "u-author", ReconcileOptions{})
	if err != nil {
		t.Fatalf("reconcile refused: %v", err)
	}
	if first.Watermark == 0 || first.Events == 0 {
		t.Fatalf("watermark missing: %+v", first)
	}
	hits, err := s.SearchLexical(ctx, "tenant-a", "cats", "person", "u-anna")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("withdrawn chunks survived: %+v", hits)
	}
	vecs, err := s.SectionVectors(ctx, "tenant-a", docA, v1.ID, "hub-internal-v1")
	if err != nil || len(vecs) != 0 {
		t.Fatalf("withdrawn vectors survived: %+v err=%v", vecs, err)
	}
	second, err := s.ReconcileDocument(ctx, "tenant-a", docA, "u-author", ReconcileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Watermark != first.Watermark || second.Deleted != 0 {
		t.Fatalf("replay not idempotent: %+v vs %+v", first, second)
	}
}

// TestTodo_HUB_029_Recovery is the RECOVERY test for HUB-029: events
// landing after a reconcile are picked up incrementally, and unknown
// event types never wedge the watermark.
func TestTodo_HUB_029_Recovery(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1 := searchFixture(t, s, ctx, "tenant-a", docA, "# Cats\n\nA guide to cats.\n")
	grantRead(t, s, ctx, "tenant-a", docA, "u-anna")
	first, err := s.ReconcileDocument(ctx, "tenant-a", docA, "u-author", ReconcileOptions{
		Embed: stubEmbedder, Policy: testEmbeddingPolicy(), ModelID: "hub-internal-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Rebuilt == 0 {
		t.Fatalf("live version not rebuilt: %+v", first)
	}
	v2, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docA, CreatorID: "u-author", Title: "Guide", Markdown: "# Dogs\n\nA guide to dogs.\n\nSee [ghost](doc:doc-missing).\n"}, v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Broken links are warnings in placement scopes and errors in the
	// official default scope. Deploy here so the recovery test can exercise
	// indexing and link.broken outbox handling without bypassing HUB-020.
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docA, VersionID: v2.ID, ScopeKind: "placement", ScopeID: "chan-A", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deploy(ctx, "tenant-a", DeployInput{DocumentID: docA, VersionID: v2.ID, ScopeKind: "placement", ScopeID: "chan-A", DeployerID: "u-deployer", ExpectedLive: ""}); err != nil {
		t.Fatal(err)
	}
	second, err := s.ReconcileDocument(ctx, "tenant-a", docA, "u-author", ReconcileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Watermark <= first.Watermark {
		t.Fatalf("watermark stalled: %+v vs %+v", first, second)
	}
	hits, err := s.SearchLexical(ctx, "tenant-a", "dogs", "person", "u-anna")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].VersionID != v2.ID {
		t.Fatalf("new deployment not indexed: %+v", hits)
	}
	// link.broken is not index-bearing: the consumer passes over it while
	// still advancing the watermark.
	report, err := s.CheckLinks(ctx, "tenant-a", docA, v2.ID, "u-author")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 1 {
		t.Fatalf("ghost link not flagged: %+v", report)
	}
	third, err := s.ReconcileDocument(ctx, "tenant-a", docA, "u-author", ReconcileOptions{RetireModels: []string{"hub-internal-v1"}})
	if err != nil {
		t.Fatal(err)
	}
	if third.Events != 1 || third.Watermark <= second.Watermark {
		t.Fatalf("unknown event wedged the watermark: %+v vs %+v", second, third)
	}
	vecs, err := s.SectionVectors(ctx, "tenant-a", docA, v2.ID, "hub-internal-v1")
	if err != nil || len(vecs) != 0 {
		t.Fatalf("retired model vectors survived: %+v err=%v", vecs, err)
	}
}

// TestTodo_HUB_029_Security is the SECURITY test for HUB-029: reconciling
// needs authority, and revocation needs no reindex to take effect.
func TestTodo_HUB_029_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	searchFixture(t, s, ctx, "tenant-a", docA, "# Cats\n\nA guide to cats.\n")
	grantRead(t, s, ctx, "tenant-a", docA, "u-anna")
	if _, err := s.ReconcileDocument(ctx, "tenant-a", docA, "u-stranger", ReconcileOptions{}); !errors.Is(err, ErrDenied) {
		t.Fatalf("stranger reconciled: %v", err)
	}
	if _, err := s.ReconcileDocument(ctx, "tenant-b", docA, "u-author", ReconcileOptions{}); !errors.Is(err, ErrDenied) {
		t.Fatalf("cross-tenant reconcile: %v", err)
	}
	g, err := s.ShareDocument(ctx, "tenant-a", docA, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-mallory", Action: ActionRead, Effect: EffectAllow})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReconcileDocument(ctx, "tenant-a", docA, "u-author", ReconcileOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeGrant(ctx, "tenant-a", g.ID, "u-author"); err != nil {
		t.Fatal(err)
	}
	hits, err := s.SearchLexical(ctx, "tenant-a", "cats", "person", "u-mallory")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("revoked reader searched without reindex: %+v", hits)
	}
}

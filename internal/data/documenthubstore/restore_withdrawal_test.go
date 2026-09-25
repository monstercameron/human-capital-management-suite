package documenthubstore

import (
	"context"
	"errors"
	"testing"
)

// TestTodo_DOCS_07_RestoreWithdrawal proves RestoreWithdrawal's split
// rule: undoing a withdrawal on the personal-document default scope needs
// no fresh review, because that scope never needed one to go live in the
// first place (HUB-012's share-without-review path) — restoring is Undo,
// not a new publication, and the document comes back visible with the
// same content and the same grants. (The withdrawn row itself cannot be
// reused once retired — migration 00008's version lifecycle is
// append-only and terminal there — so the live version id after restore
// is a content-identical clone; TestTodo_HUB_010 already covers the
// still-live "stale" case where the same row comes back unchanged.) A
// team/channel placement (HUB-014) did need review to go live, so
// HUB-010's fresh-evidence rule still gates its restore.
func TestTodo_DOCS_07_RestoreWithdrawal(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()

	// --- Personal document: withdraw, then restore with no new review. ---
	docID, v, err := s.CreatePersonalDocument(ctx, "tenant-a", "u-owner", "Handbook", "# Handbook\n")
	if err != nil {
		t.Fatal(err)
	}
	// Sharing (not a formal review/deploy) is what actually publishes a
	// personal document's default-scope pointer.
	if err := s.SharePersonalDocumentRole(ctx, "tenant-a", docID, "u-owner", "u-reader", RoleViewer); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Withdraw(ctx, "tenant-a", WithdrawInput{
		DocumentID: docID, ScopeKind: "default", ScopeID: "", ActorID: "u-owner", ExpectedLive: v.ID, Reason: "test",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveDeployment(ctx, "tenant-a", docID, "default", ""); !errors.Is(err, ErrNoDeployment) {
		t.Fatalf("withdrawn scope still resolves: %v", err)
	}

	restored, err := s.RestoreWithdrawal(ctx, "tenant-a", RestoreInput{
		DocumentID: docID, ScopeKind: "default", ScopeID: "", ActorID: "u-owner", VersionID: v.ID,
	})
	if err != nil {
		t.Fatalf("personal-document restore needed a review it was never supposed to: %v", err)
	}
	if restored.ScopeKind != "default" {
		t.Fatalf("restore put back the wrong deployment: %+v", restored)
	}
	live, err := s.ResolveDeployment(ctx, "tenant-a", docID, "default", "")
	if err != nil || live.VersionID != restored.VersionID {
		t.Fatalf("document not visible again after restore: %+v err=%v", live, err)
	}
	// The version withdraw took down had already been retired (nothing
	// else kept it live), so restore's live version is a content-identical
	// clone of it, not the same immutable row — but the title and bytes
	// are exactly what was withdrawn.
	restoredVersion, err := s.ReadVersion(ctx, "tenant-a", docID, restored.VersionID, "person", "u-owner")
	if err != nil || restoredVersion.Title != "Handbook" || restoredVersion.Markdown != v.Markdown {
		t.Fatalf("restored content does not match what was withdrawn: %+v err=%v", restoredVersion, err)
	}
	// Withdraw/restore never touched the grant table: the reader's share
	// is exactly as it was.
	if err := s.Authorize(ctx, "tenant-a", docID, "person", "u-reader", ActionRead); err != nil {
		t.Fatalf("reader lost their read grant across withdraw/restore: %v", err)
	}
	readerVersion, err := s.ReadVersion(ctx, "tenant-a", docID, restored.VersionID, "person", "u-reader")
	if err != nil || readerVersion.ID != restored.VersionID {
		t.Fatalf("reader cannot read the restored version: %+v err=%v", readerVersion, err)
	}

	// A second withdraw/restore round trip must keep working: restoring is
	// not a one-shot escape hatch.
	if _, err := s.Withdraw(ctx, "tenant-a", WithdrawInput{
		DocumentID: docID, ScopeKind: "default", ScopeID: "", ActorID: "u-owner", ExpectedLive: restored.VersionID, Reason: "test again",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RestoreWithdrawal(ctx, "tenant-a", RestoreInput{
		DocumentID: docID, ScopeKind: "default", ScopeID: "", ActorID: "u-owner", VersionID: restored.VersionID,
	}); err != nil {
		t.Fatalf("second restore refused: %v", err)
	}

	// --- Official team/channel placement: HUB-010's fresh-evidence rule
	// still applies; RestoreWithdrawal only bypasses it for scope=default.
	officialDoc, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	pv1, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: officialDoc, CreatorID: "u-author", Markdown: "one\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{
		DocumentID: officialDoc, VersionID: pv1.ID, ScopeKind: "placement", ScopeID: "team-eng",
		ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved",
	}); err != nil {
		t.Fatal(err)
	}
	for _, g := range []GrantInput{
		{DocumentID: officialDoc, SubjectKind: "person", SubjectID: "u-deployer", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-author"},
		{DocumentID: officialDoc, SubjectKind: "person", SubjectID: "u-deployer", Action: ActionRetire, Effect: EffectAllow, Issuer: "u-author"},
	} {
		if _, err := s.GrantAction(ctx, "tenant-a", g); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Deploy(ctx, "tenant-a", DeployInput{
		DocumentID: officialDoc, VersionID: pv1.ID, ScopeKind: "placement", ScopeID: "team-eng", DeployerID: "u-deployer", ExpectedLive: "",
	}); err != nil {
		t.Fatal(err)
	}
	// Also deploy pv1 to a second placement, so withdrawing the first
	// leaves pv1 still live (and so 'deployed', not 'retired') elsewhere —
	// Deploy's own append-only version lifecycle (migration 00008) never
	// lets a *retired* row move back to 'deployed' regardless of review,
	// so keeping pv1 live in a second scope isolates the fresh-evidence
	// rule this asserts from that unrelated, pre-existing constraint.
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{
		DocumentID: officialDoc, VersionID: pv1.ID, ScopeKind: "placement", ScopeID: "team-other",
		ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deploy(ctx, "tenant-a", DeployInput{
		DocumentID: officialDoc, VersionID: pv1.ID, ScopeKind: "placement", ScopeID: "team-other", DeployerID: "u-deployer", ExpectedLive: "",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Withdraw(ctx, "tenant-a", WithdrawInput{
		DocumentID: officialDoc, ScopeKind: "placement", ScopeID: "team-eng", ActorID: "u-deployer", ExpectedLive: pv1.ID, Reason: "test",
	}); err != nil {
		t.Fatal(err)
	}

	// A second, never-reviewed version cannot be restored into team-eng:
	// the evidence rule still gates it.
	pv2, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: officialDoc, CreatorID: "u-author", Markdown: "two\n"}, pv1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RestoreWithdrawal(ctx, "tenant-a", RestoreInput{
		DocumentID: officialDoc, ScopeKind: "placement", ScopeID: "team-eng", ActorID: "u-deployer", VersionID: pv2.ID,
	}); !errors.Is(err, ErrNoApprovedReview) {
		t.Fatalf("placement restore bypassed HUB-010's evidence rule: %v", err)
	}

	// pv1 itself, already reviewed for team-eng, restores fine: the rule
	// is "needs evidence", not "placements can never come back".
	restoredOfficial, err := s.RestoreWithdrawal(ctx, "tenant-a", RestoreInput{
		DocumentID: officialDoc, ScopeKind: "placement", ScopeID: "team-eng", ActorID: "u-deployer", VersionID: pv1.ID,
	})
	if err != nil || restoredOfficial.VersionID != pv1.ID {
		t.Fatalf("placement restore with existing evidence refused: %+v err=%v", restoredOfficial, err)
	}
}

// TestTodo_DOCS_07_RestoreWithdrawal_Security proves RestoreWithdrawal
// needs the same retire capability Withdraw itself needs, refuses to
// clobber a live pointer, and never leaks across tenants.
func TestTodo_DOCS_07_RestoreWithdrawal_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, v, err := s.CreatePersonalDocument(ctx, "tenant-a", "u-owner", "Handbook", "# Handbook\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SharePersonalDocumentRole(ctx, "tenant-a", docID, "u-owner", "u-reader", RoleViewer); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Withdraw(ctx, "tenant-a", WithdrawInput{
		DocumentID: docID, ScopeKind: "default", ScopeID: "", ActorID: "u-owner", ExpectedLive: v.ID, Reason: "test",
	}); err != nil {
		t.Fatal(err)
	}

	// The reader holds read, not retire: they cannot restore either.
	if _, err := s.RestoreWithdrawal(ctx, "tenant-a", RestoreInput{
		DocumentID: docID, ScopeKind: "default", ScopeID: "", ActorID: "u-reader", VersionID: v.ID,
	}); !errors.Is(err, ErrDenied) {
		t.Fatalf("reader restored without retire capability: %v", err)
	}

	// Cross-tenant restore is refused too.
	if _, err := s.RestoreWithdrawal(ctx, "tenant-b", RestoreInput{
		DocumentID: docID, ScopeKind: "default", ScopeID: "", ActorID: "u-owner", VersionID: v.ID,
	}); err == nil {
		t.Fatal("cross-tenant restore succeeded")
	}

	restored, err := s.RestoreWithdrawal(ctx, "tenant-a", RestoreInput{
		DocumentID: docID, ScopeKind: "default", ScopeID: "", ActorID: "u-owner", VersionID: v.ID,
	})
	if err != nil {
		t.Fatalf("authorized restore refused: %v", err)
	}

	// Something is already live: a second restore must refuse rather than
	// clobber it.
	if _, err := s.RestoreWithdrawal(ctx, "tenant-a", RestoreInput{
		DocumentID: docID, ScopeKind: "default", ScopeID: "", ActorID: "u-owner", VersionID: restored.VersionID,
	}); !errors.Is(err, ErrStalePointer) {
		t.Fatalf("restore onto an already-live scope did not refuse: %v", err)
	}
}

package documenthubstore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// fixedReviewDue is the review due date fixture shared by the HUB-013 and
// HUB-014 tests.
var fixedReviewDue = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

// TestTodo_HUB_014_Golden is the GOLDEN test for HUB-014: the canonical
// placement evidence bytes are pinned for audit consumers.
func TestTodo_HUB_014_Golden(t *testing.T) {
	canonical, err := json.Marshal(map[string]string{
		"custodian_id": "u-custodian", "deployer_id": "u-manager", "document_id": "doc-fixed",
		"review_due_at": fixedReviewDue.Format(time.RFC3339), "scope_id": "chan-A",
		"scope_kind": "placement", "version_id": "docv-fixed",
	})
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/hub014_placement.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(append(canonical, '\n'), want) {
		t.Fatalf("placement evidence bytes changed:\n got %s\nwant %s", canonical, want)
	}
}

// TestTodo_HUB_014 is the PRIMARY test for HUB-014: an authorized manager
// links a reviewed deployment to a team or channel scope with a custodian
// and review due date, and the placement is a fresh, separately versioned
// deployment rather than an edit of the prior one.
func TestTodo_HUB_014(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "one\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v1.ID, ScopeKind: "placement", ScopeID: "chan-A", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	for _, g := range []GrantInput{
		{SubjectKind: "person", SubjectID: "u-manager", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-author"},
		{SubjectKind: "person", SubjectID: "u-manager", Action: ActionManage, Effect: EffectAllow, Issuer: "u-author"},
	} {
		g.DocumentID = docID
		if _, err := s.GrantAction(ctx, "tenant-a", g); err != nil {
			t.Fatal(err)
		}
	}
	placed, err := s.PlaceDocument(ctx, "tenant-a", PlaceInput{
		DocumentID: docID, VersionID: v1.ID, ScopeKind: "placement", ScopeID: "chan-A",
		ActorID: "u-manager", CustodianID: "u-custodian", ReviewDueAt: fixedReviewDue,
	})
	if err != nil {
		t.Fatalf("authorized placement refused: %v", err)
	}
	if !placed.IsOfficialPlacement() || placed.CustodianID != "u-custodian" || !placed.ReviewDueAt.Equal(fixedReviewDue) {
		t.Fatalf("placement did not bind custodian and review due date: %+v", placed)
	}
	got, err := s.CurrentPlacement(ctx, "tenant-a", docID, "placement", "chan-A")
	if err != nil || got.ID != placed.ID {
		t.Fatalf("current placement mismatch: %+v err=%v", got, err)
	}

	v2, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "two\n"}, v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v2.ID, ScopeKind: "placement", ScopeID: "chan-A", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	reviewDue2 := fixedReviewDue.Add(30 * 24 * time.Hour)
	replaced, err := s.PlaceDocument(ctx, "tenant-a", PlaceInput{
		DocumentID: docID, VersionID: v2.ID, ScopeKind: "placement", ScopeID: "chan-A",
		ActorID: "u-manager", ExpectedLive: v1.ID, CustodianID: "u-custodian-2", ReviewDueAt: reviewDue2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if replaced.ID == placed.ID {
		t.Fatal("re-placement reused the prior deployment row instead of versioning")
	}
	if replaced.CustodianID != "u-custodian-2" || !replaced.ReviewDueAt.Equal(reviewDue2) {
		t.Fatalf("re-placement did not rebind custodian and review due date: %+v", replaced)
	}
	var deployments int
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM document_deployment WHERE tenant_id=$1 AND document_id=$2 AND scope_kind='placement' AND scope_id='chan-A'`, "tenant-a", docID).Scan(&deployments)
	}); err != nil {
		t.Fatal(err)
	}
	if deployments != 2 {
		t.Fatalf("placement history rewritten: %d deployment rows", deployments)
	}
}

// TestTodo_HUB_014_Security is the SECURITY test for HUB-014: placing
// without MANAGE, without a custodian, or without a review due date is
// refused, and a plain deploy grant alone never suffices to pin an
// official placement.
func TestTodo_HUB_014_Security(t *testing.T) {
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
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "placement", ScopeID: "chan-A", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: "u-deployer-only", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-author"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PlaceDocument(ctx, "tenant-a", PlaceInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "placement", ScopeID: "chan-A", ActorID: "u-deployer-only", CustodianID: "u-custodian", ReviewDueAt: fixedReviewDue}); !errors.Is(err, ErrDenied) {
		t.Fatalf("deploy-only grant placed a document: %v", err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: "u-deployer-only", Action: ActionManage, Effect: EffectAllow, Issuer: "u-author"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PlaceDocument(ctx, "tenant-a", PlaceInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "placement", ScopeID: "chan-A", ActorID: "u-deployer-only", ReviewDueAt: fixedReviewDue}); !errors.Is(err, ErrPlacementInput) {
		t.Fatalf("missing custodian accepted: %v", err)
	}
	if _, err := s.PlaceDocument(ctx, "tenant-a", PlaceInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "placement", ScopeID: "chan-A", ActorID: "u-deployer-only", CustodianID: "u-custodian"}); !errors.Is(err, ErrPlacementInput) {
		t.Fatalf("missing review due date accepted: %v", err)
	}
	if _, err := s.PlaceDocument(ctx, "tenant-a", PlaceInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "default", ScopeID: "", ActorID: "u-deployer-only", CustodianID: "u-custodian", ReviewDueAt: fixedReviewDue}); !errors.Is(err, ErrPlacementInput) {
		t.Fatalf("default scope masqueraded as placement: %v", err)
	}
}

// TestTodo_HUB_014_Integration is the INTEGRATION test for HUB-014: a bare
// scope_kind='placement' deployment made outside PlaceDocument resolves
// but is not an official placement, distinguishing a pinned link from
// governed policy.
func TestTodo_HUB_014_Integration(t *testing.T) {
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
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "placement", ScopeID: "chan-A", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: "u-deployer", Action: ActionDeploy, Effect: EffectAllow, Issuer: "u-author"}); err != nil {
		t.Fatal(err)
	}
	bare, err := s.Deploy(ctx, "tenant-a", DeployInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "placement", ScopeID: "chan-A", DeployerID: "u-deployer"})
	if err != nil {
		t.Fatal(err)
	}
	if bare.IsOfficialPlacement() {
		t.Fatalf("bare placement deploy reported as official: %+v", bare)
	}
	got, err := s.CurrentPlacement(ctx, "tenant-a", docID, "placement", "chan-A")
	if err != nil || got.ID != bare.ID || got.IsOfficialPlacement() {
		t.Fatalf("current placement wrongly reports official: %+v err=%v", got, err)
	}
}

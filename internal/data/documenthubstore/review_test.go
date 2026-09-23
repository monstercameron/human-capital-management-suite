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

// TestTodo_HUB_008_Golden is the GOLDEN test for HUB-008: the canonical
// evidence bytes of a review decision are pinned, so downstream deploy and
// audit consumers can rely on the shape.
func TestTodo_HUB_008_Golden(t *testing.T) {
	fixed := Review{
		ID: "docr-fixed", DocumentID: "doc-fixed", VersionID: "docv-fixed",
		VersionHash: "9e1e2a8ea86d8c8dcfa4b4a1a0b8e1e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5",
		ScopeKind:   "placement", ScopeID: "chan-A", ReviewerID: "u-reviewer",
		Authority: "team:leads", Decision: "approved", Note: "accurate",
		DecidedAt: time.Date(2026, 9, 22, 14, 0, 0, 0, time.UTC),
	}
	canonical, err := json.Marshal(map[string]string{
		"id": fixed.ID, "document_id": fixed.DocumentID, "version_id": fixed.VersionID,
		"version_hash": fixed.VersionHash, "scope_kind": fixed.ScopeKind, "scope_id": fixed.ScopeID,
		"reviewer_id": fixed.ReviewerID, "authority": fixed.Authority, "decision": fixed.Decision,
		"note": fixed.Note, "decided_at": fixed.DecidedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/hub008_review.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(append(canonical, '\n'), want) {
		t.Fatalf("review evidence bytes changed:\n got %s\nwant %s", canonical, want)
	}
}

// TestTodo_HUB_008 is the PRIMARY test for HUB-008: a review decision binds
// the exact version hash, scope, reviewer and authority, and the author
// cannot review their own candidate.
func TestTodo_HUB_008(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "policy\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "placement", ScopeID: "chan-A", ReviewerID: "u-author", Authority: "team:leads", Decision: "approved"}); !errors.Is(err, ErrSelfApproval) {
		t.Fatalf("self-approval accepted: %v", err)
	}
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "placement", ScopeID: "chan-A", ReviewerID: "u-reviewer", Authority: "", Decision: "approved"}); err == nil {
		t.Fatal("review without authority accepted")
	}
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "placement", ScopeID: "chan-A", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "maybe"}); err == nil {
		t.Fatal("review with unknown decision accepted")
	}
	review, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v.ID, ScopeKind: "placement", ScopeID: "chan-A", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved", Note: "accurate"})
	if err != nil {
		t.Fatalf("independent review refused: %v", err)
	}
	if review.VersionHash != v.Hash || review.VersionHash != HashContent("policy\n") {
		t.Fatalf("review does not bind the exact bytes: %+v", review)
	}
	if review.ScopeKind != "placement" || review.ScopeID != "chan-A" || review.ReviewerID != "u-reviewer" || review.Authority != "team:leads" {
		t.Fatalf("review dropped binding fields: %+v", review)
	}
	latest, err := s.LatestReview(ctx, "tenant-a", docID, v.ID, "placement", "chan-A")
	if err != nil || latest.ID != review.ID || latest.Decision != "approved" {
		t.Fatalf("latest review not returned: %+v err=%v", latest, err)
	}
	if _, err := s.LatestReview(ctx, "tenant-a", docID, v.ID, "placement", "chan-B"); !errors.Is(err, ErrNoReview) {
		t.Fatalf("review leaked across scopes: %v", err)
	}
	if _, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: "docv-missing", ScopeKind: "default", ScopeID: "", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"}); err == nil {
		t.Fatal("review of unknown version accepted")
	}
}

// TestTodo_HUB_008_Security is the SECURITY test for HUB-008: approving
// version A can never authorize version B, because the decision pins A's
// hash and the bytes are immutable.
func TestTodo_HUB_008_Security(t *testing.T) {
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
	v2, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "two\n"}, v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	review, err := s.RecordReview(ctx, "tenant-a", ReviewInput{DocumentID: docID, VersionID: v1.ID, ScopeKind: "default", ScopeID: "", ReviewerID: "u-reviewer", Authority: "team:leads", Decision: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	if review.VersionHash == v2.Hash {
		t.Fatal("review of A authorizes B")
	}
	stored, err := s.LatestReview(ctx, "tenant-a", docID, v1.ID, "default", "")
	if err != nil || stored.VersionHash != HashContent("one\n") {
		t.Fatalf("stored review does not pin A's bytes: %+v err=%v", stored, err)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE document_review SET version_hash='forged' WHERE tenant_id='tenant-a' AND id=$1`, review.ID)
		return err
	}); err == nil {
		t.Fatal("review decision rewritten after recording")
	}
}

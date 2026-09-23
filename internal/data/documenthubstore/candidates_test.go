package documenthubstore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func versionCount(t *testing.T, s *Store, docID string) int {
	t.Helper()
	var count int
	if err := s.RunTenantTx(context.Background(), "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM document_version WHERE tenant_id=$1 AND document_id=$2`, "tenant-a", docID).Scan(&count)
	}); err != nil {
		t.Fatal(err)
	}
	return count
}

// TestTodo_HUB_006 is the PRIMARY test for HUB-006: every submit appends an
// immutable candidate against an expected base, and a stale base returns
// both versions for explicit merge without changing stored bytes.
func TestTodo_HUB_006(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-1", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	base, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-1", Title: "v1", Markdown: "one\n"}, "")
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	if base.ParentID != "" {
		t.Fatalf("first candidate has parent %q", base.ParentID)
	}
	second, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-2", Title: "v2", Markdown: "two\n"}, base.ID)
	if err != nil {
		t.Fatalf("on-tip submit: %v", err)
	}
	if second.ParentID != base.ID {
		t.Fatalf("chain broken: parent %q, want %q", second.ParentID, base.ID)
	}
	_, err = s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-1", Title: "stale", Markdown: "stale\n"}, base.ID)
	var conflict *VersionConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("stale submit is not a conflict: %v", err)
	}
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("conflict missing sentinel: %v", err)
	}
	if conflict.Expected.ID != base.ID || conflict.Current.ID != second.ID {
		t.Fatalf("conflict carries wrong versions: %+v", conflict)
	}
	if conflict.Expected.Markdown != "one\n" || conflict.Current.Markdown != "two\n" {
		t.Fatal("conflict omits merge bytes")
	}
	if count := versionCount(t, s, docID); count != 2 {
		t.Fatalf("failed submit changed storage: count=%d", count)
	}
	if _, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: "doc-missing", Markdown: "x\n"}, ""); err == nil {
		t.Fatal("submit to unknown document accepted")
	}
	if _, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, Markdown: "x\n"}, "docv-missing"); err == nil {
		t.Fatal("submit against unknown base accepted")
	}
}

// TestTodo_HUB_006_Race is the RACE test for HUB-006: concurrent proposers
// never overwrite each other; exactly one wins and every loser gets the
// winner for explicit merge.
func TestTodo_HUB_006_Race(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-1", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	base, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-1", Markdown: "base\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	const proposers = 8
	var wg sync.WaitGroup
	won := make([]Version, proposers)
	failed := make([]error, proposers)
	for i := range proposers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			won[i], failed[i] = s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: fmt.Sprintf("u-%d", i), Markdown: fmt.Sprintf("prop-%d\n", i)}, base.ID)
		}(i)
	}
	wg.Wait()
	wins, winner := 0, Version{}
	for i := range proposers {
		if failed[i] == nil {
			wins++
			winner = won[i]
			continue
		}
		var conflict *VersionConflict
		if !errors.As(failed[i], &conflict) {
			t.Fatalf("loser %d did not get a conflict: %v", i, failed[i])
		}
	}
	if wins != 1 {
		t.Fatalf("proposers won %d times, want exactly 1", wins)
	}
	if winner.ParentID != base.ID {
		t.Fatalf("winner does not extend the base: %+v", winner)
	}
	if count := versionCount(t, s, docID); count != 2 {
		t.Fatalf("concurrent submits stored %d versions, want 2", count)
	}
}

func TestDocumentOwnerCreatesImmutableVersion_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	const tenant, owner, reader = "tenant-a", "owner", "reader"
	id, first, err := s.CreatePersonalDocument(ctx, tenant, owner, "First title", "# First\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SharePersonalDocument(ctx, tenant, id, owner, reader); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePersonalDocumentVersion(ctx, tenant, id, reader, first.ID, "Reader edit", "# Forged\n"); !errors.Is(err, ErrDenied) {
		t.Fatalf("reader edited document: %v", err)
	}
	if _, err := s.CreatePersonalDocumentVersion(ctx, tenant, id, owner, first.ID, "Empty body", "  \n"); err == nil {
		t.Fatal("blank version body accepted")
	}
	second, err := s.CreatePersonalDocumentVersion(ctx, tenant, id, owner, first.ID, "Second title", "# Second\n")
	if err != nil || second.ID == first.ID || second.ParentID != first.ID || second.CreatorID != owner {
		t.Fatalf("new version = %+v, %v", second, err)
	}
	prior, err := s.ReadVersion(ctx, tenant, id, first.ID, "person", owner)
	if err != nil || prior.Title != "First title" || prior.Markdown != "# First\n" {
		t.Fatalf("original version mutated = %+v, %v", prior, err)
	}
	ownerSummary, ownerVersion, err := s.ReadPersonalDocument(ctx, tenant, owner, id)
	if err != nil || ownerSummary.VersionID != second.ID || ownerVersion.Markdown != "# Second\n" || !ownerSummary.CanEdit {
		t.Fatalf("owner view = %+v %+v, %v", ownerSummary, ownerVersion, err)
	}
	readerSummary, readerVersion, err := s.ReadPersonalDocument(ctx, tenant, reader, id)
	if err != nil || readerSummary.VersionID != first.ID || readerVersion.Markdown != "# First\n" || readerSummary.CanEdit {
		t.Fatalf("shared reader saw private revision = %+v %+v, %v", readerSummary, readerVersion, err)
	}
	ownerList, err := s.ListPersonalDocuments(ctx, tenant, owner, 10)
	if err != nil || len(ownerList) != 1 || !ownerList[0].CanEdit {
		t.Fatalf("owner list edit permission = %+v, %v", ownerList, err)
	}
	readerList, err := s.ListPersonalDocuments(ctx, tenant, reader, 10)
	if err != nil || len(readerList) != 1 || readerList[0].CanEdit {
		t.Fatalf("reader list edit permission = %+v, %v", readerList, err)
	}
	if _, err := s.CreatePersonalDocumentVersion(ctx, tenant, id, owner, first.ID, "Stale edit", "# Stale\n"); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale save accepted: %v", err)
	}
	if count := versionCount(t, s, id); count != 2 {
		t.Fatalf("stale save inserted version: %d", count)
	}
	if _, err := s.GrantAction(ctx, tenant, GrantInput{DocumentID: id, SubjectID: owner, Action: ActionPropose, Effect: EffectDeny, Issuer: owner}); err != nil {
		t.Fatal(err)
	}
	ownerSummary, _, err = s.ReadPersonalDocument(ctx, tenant, owner, id)
	if err != nil || ownerSummary.CanEdit {
		t.Fatalf("propose deny did not hide editing: %+v, %v", ownerSummary, err)
	}
	if _, err := s.CreatePersonalDocumentVersion(ctx, tenant, id, owner, second.ID, "After deny", "# Denied\n"); !errors.Is(err, ErrDenied) {
		t.Fatalf("propose-denied owner edited: %v", err)
	}
}

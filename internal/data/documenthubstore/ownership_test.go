package documenthubstore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// TestTodo_HUB_040_Golden is the GOLDEN test for HUB-040: the canonical
// ownership-transfer evidence bytes are pinned for audit consumers.
func TestTodo_HUB_040_Golden(t *testing.T) {
	canonical, err := json.Marshal(map[string]string{
		"document_id": "doc-fixed", "prior_owner_id": "u-departed", "reason": "role change",
		"successor_owner_id": "u-successor", "transferred_by": "u-manager",
	})
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/hub040_ownership_transfer.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(append(canonical, '\n'), want) {
		t.Fatalf("ownership transfer evidence changed:\n got %s\nwant %s", canonical, want)
	}
}

// TestTodo_HUB_040 is the PRIMARY test for HUB-040: transfer records the
// successor as custodian, preserves the departed owner's and any other
// subject's existing grants untouched, and installs the successor with a
// working owner grant set rather than leaving the document unreviewable.
func TestTodo_HUB_040(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-departed", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ShareDocument(ctx, "tenant-a", docID, "u-departed", GrantInput{SubjectKind: "person", SubjectID: "u-reader", Action: ActionRead, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	before, err := s.Audience(ctx, "tenant-a", docID, "u-departed")
	if err != nil {
		t.Fatal(err)
	}
	transfer, err := s.TransferOwnership(ctx, "tenant-a", TransferInput{DocumentID: docID, SuccessorOwnerID: "u-successor", ActorID: "u-departed", Reason: "role change"})
	if err != nil {
		t.Fatalf("authorized transfer refused: %v", err)
	}
	if transfer.PriorOwnerID != "u-departed" || transfer.SuccessorOwnerID != "u-successor" {
		t.Fatalf("transfer record wrong: %+v", transfer)
	}
	custodian, err := s.CurrentCustodian(ctx, "tenant-a", docID)
	if err != nil || custodian != "u-successor" {
		t.Fatalf("custodian not updated: %q err=%v", custodian, err)
	}
	if err := s.Authorize(ctx, "tenant-a", docID, "person", "u-successor", ActionDeploy); err != nil {
		t.Fatalf("successor cannot manage transferred document: %v", err)
	}
	// The prior owner's and reader's grants must be preserved exactly, not
	// widened or shrunk by the transfer itself.
	after, err := s.Audience(ctx, "tenant-a", docID, "u-successor")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) <= len(before) {
		t.Fatalf("transfer did not add successor grants: before=%d after=%d", len(before), len(after))
	}
	if err := s.Authorize(ctx, "tenant-a", docID, "person", "u-reader", ActionRead); err != nil {
		t.Fatalf("reader access lost across transfer: %v", err)
	}
	if err := s.Authorize(ctx, "tenant-a", docID, "person", "u-departed", ActionRead); err != nil {
		t.Fatalf("departed owner's prior grant silently revoked: %v", err)
	}
	history, err := s.TransferHistory(ctx, "tenant-a", docID)
	if err != nil || len(history) != 1 || history[0].ID != transfer.ID {
		t.Fatalf("transfer history wrong: %+v err=%v", history, err)
	}
}

// TestTodo_HUB_040_Security is the SECURITY test for HUB-040: an actor who
// is neither the current owner nor a manager cannot transfer custody, a
// disposed document refuses transfer, and a no-op transfer to the current
// owner is refused rather than silently accepted.
func TestTodo_HUB_040_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-owner", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.TransferOwnership(ctx, "tenant-a", TransferInput{DocumentID: docID, SuccessorOwnerID: "u-successor", ActorID: "u-outsider", Reason: "grab"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("unauthorized actor transferred ownership: %v", err)
	}
	if _, err := s.TransferOwnership(ctx, "tenant-a", TransferInput{DocumentID: docID, SuccessorOwnerID: "u-owner", ActorID: "u-owner", Reason: "noop"}); !errors.Is(err, ErrTransferSameOwner) {
		t.Fatalf("no-op transfer accepted: %v", err)
	}
	if _, err := s.TransferOwnership(ctx, "tenant-a", TransferInput{DocumentID: docID, SuccessorOwnerID: "", ActorID: "u-owner", Reason: "x"}); !errors.Is(err, ErrTransferInput) {
		t.Fatalf("blank successor accepted: %v", err)
	}
	if _, err := s.TransferOwnership(ctx, "tenant-a", TransferInput{DocumentID: docID, SuccessorOwnerID: "u-successor", ActorID: "u-owner", Reason: ""}); !errors.Is(err, ErrTransferInput) {
		t.Fatalf("blank reason accepted: %v", err)
	}
	if _, err := s.DisposeDocument(ctx, "tenant-a", docID, "u-owner", "cleanup"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TransferOwnership(ctx, "tenant-a", TransferInput{DocumentID: docID, SuccessorOwnerID: "u-successor", ActorID: "u-owner", Reason: "too late"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("disposed document accepted a transfer: %v", err)
	}
}

// TestTodo_HUB_040_Integration is the INTEGRATION test for HUB-040: a
// manager (not the owner) can transfer custody, a second transfer chains
// onto the new owner rather than the original, and history accumulates
// without rewriting earlier rows.
func TestTodo_HUB_040_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-owner", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ShareDocument(ctx, "tenant-a", docID, "u-owner", GrantInput{SubjectKind: "person", SubjectID: "u-manager", Action: ActionManage, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	first, err := s.TransferOwnership(ctx, "tenant-a", TransferInput{DocumentID: docID, SuccessorOwnerID: "u-successor-1", ActorID: "u-manager", Reason: "departure"})
	if err != nil {
		t.Fatalf("manager transfer refused: %v", err)
	}
	if _, err := s.ShareDocument(ctx, "tenant-a", docID, "u-successor-1", GrantInput{SubjectKind: "person", SubjectID: "u-manager", Action: ActionManage, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	second, err := s.TransferOwnership(ctx, "tenant-a", TransferInput{DocumentID: docID, SuccessorOwnerID: "u-successor-2", ActorID: "u-manager", Reason: "reorg"})
	if err != nil {
		t.Fatal(err)
	}
	if second.PriorOwnerID != "u-successor-1" {
		t.Fatalf("second transfer did not chain onto the interim owner: %+v", second)
	}
	custodian, err := s.CurrentCustodian(ctx, "tenant-a", docID)
	if err != nil || custodian != "u-successor-2" {
		t.Fatalf("final custodian wrong: %q err=%v", custodian, err)
	}
	history, err := s.TransferHistory(ctx, "tenant-a", docID)
	if err != nil || len(history) != 2 || history[0].ID != second.ID || history[1].ID != first.ID {
		t.Fatalf("transfer history not chained in order: %+v err=%v", history, err)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, execErr := tx.Exec(ctx, `UPDATE document_ownership_transfer SET reason='rewritten' WHERE tenant_id='tenant-a' AND id=$1`, first.ID)
		return execErr
	}); err == nil {
		t.Fatal("ownership transfer history rewritten")
	}
}

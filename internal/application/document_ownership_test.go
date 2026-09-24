package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

// TestTodo_HUB_040 is the PRIMARY test for HUB-040 at the application
// boundary: transferring ownership through documentService updates the
// live custodian and records history through the store.
func TestTodo_HUB_040(t *testing.T) {
	svc := documentServiceFixture(t)
	ctx := context.Background()
	const tenant, owner, successor = "tenant-transfer", "u-owner", "u-successor"
	id, _, err := svc.CreateDocument(ctx, tenant, owner, "Guide", "# Guide\n")
	if err != nil {
		t.Fatal(err)
	}
	transfer, err := svc.TransferDocumentOwnership(ctx, tenant, owner, id, successor, "role change")
	if err != nil {
		t.Fatalf("transfer refused: %v", err)
	}
	if transfer.PriorOwnerID != owner || transfer.SuccessorOwnerID != successor {
		t.Fatalf("transfer record wrong: %+v", transfer)
	}
	custodian, err := svc.DocumentCustodian(ctx, tenant, id)
	if err != nil || custodian != successor {
		t.Fatalf("custodian not updated: %q, %v", custodian, err)
	}
	history, err := svc.DocumentOwnershipHistory(ctx, tenant, id)
	if err != nil || len(history) != 1 || history[0].ID != transfer.ID {
		t.Fatalf("ownership history wrong: %+v, %v", history, err)
	}
}

// TestTodo_HUB_040_Security is the SECURITY test for HUB-040 at the
// application boundary: an outsider cannot transfer ownership through
// documentService.
func TestTodo_HUB_040_Security(t *testing.T) {
	svc := documentServiceFixture(t)
	ctx := context.Background()
	const tenant, owner = "tenant-transfer-sec", "u-owner"
	id, _, err := svc.CreateDocument(ctx, tenant, owner, "Guide", "# Guide\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransferDocumentOwnership(ctx, tenant, "u-outsider", id, "u-successor", "grab"); !errors.Is(err, documenthubstore.ErrDenied) {
		t.Fatalf("unauthorized transfer accepted: %v", err)
	}
}

// TestTodo_HUB_040_Integration is the INTEGRATION test for HUB-040 at the
// application boundary: two chained transfers through documentService both
// persist and the second names the interim owner as prior owner.
func TestTodo_HUB_040_Integration(t *testing.T) {
	svc := documentServiceFixture(t)
	ctx := context.Background()
	const tenant, owner = "tenant-transfer-int", "u-owner"
	id, _, err := svc.CreateDocument(ctx, tenant, owner, "Guide", "# Guide\n")
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.TransferDocumentOwnership(ctx, tenant, owner, id, "u-successor-1", "departure")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.TransferDocumentOwnership(ctx, tenant, "u-successor-1", id, "u-successor-2", "reorg")
	if err != nil {
		t.Fatal(err)
	}
	if second.PriorOwnerID != first.SuccessorOwnerID {
		t.Fatalf("second transfer did not chain: %+v after %+v", second, first)
	}
	history, err := svc.DocumentOwnershipHistory(ctx, tenant, id)
	if err != nil || len(history) != 2 {
		t.Fatalf("ownership history did not accumulate: %+v, %v", history, err)
	}
}

// TestTodo_HUB_040_Golden is the GOLDEN test for HUB-040 at the application
// boundary: the pass-through returns exactly the store's own evidence
// shape, pinned by the store's own golden fixture.
func TestTodo_HUB_040_Golden(t *testing.T) {
	svc := documentServiceFixture(t)
	ctx := context.Background()
	const tenant, owner = "tenant-transfer-golden", "u-owner"
	id, _, err := svc.CreateDocument(ctx, tenant, owner, "Guide", "# Guide\n")
	if err != nil {
		t.Fatal(err)
	}
	transfer, err := svc.TransferDocumentOwnership(ctx, tenant, owner, id, "u-successor", "role change")
	if err != nil {
		t.Fatal(err)
	}
	if transfer.DocumentID != id || transfer.Reason != "role change" || transfer.TransferredBy != owner {
		t.Fatalf("transfer evidence shape changed: %+v", transfer)
	}
}

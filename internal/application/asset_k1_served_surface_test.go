package application_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func servedAssetRef(kind, id string) values.EntityRef {
	sum := sha256.Sum256([]byte(id))
	raw := hex.EncodeToString(sum[:16])
	return values.EntityRef{
		Tenant: "tenant-asset-served",
		Kind:   values.Kind(kind),
		Id:     raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:],
	}
}

func servedAssetRevision(t *testing.T, stream string, sequence uint64) values.RevisionToken {
	t.Helper()
	revision, err := values.NewSequenceRevision(stream, sequence)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

func servedAssetReceipt(t *testing.T, id string, verified bool) asset.Receipt {
	t.Helper()
	return asset.Receipt{
		ID:       servedAssetRef("receipt", id),
		Issuer:   servedAssetRef("system", "issuer"),
		IssuedAt: time.Unix(1, 0).UTC(),
		Verified: verified,
		Evidence: "served-evidence",
	}
}

// TestTodo_ASSET_001_Served proves the asset contract is reachable through
// the application package composed by cmd/hcmnext, not only by direct domain
// package imports.
func TestTodo_ASSET_001_Served(t *testing.T) {
	var app application.App
	surface := app.Asset()
	if surface.Version == nil || surface.NewCustodyStore == nil || surface.RegisterInventory == nil || surface.Assign == nil || surface.BeginReturn == nil || surface.CompleteReturn == nil || surface.Explain == nil {
		t.Fatal("served asset surface is incomplete")
	}

	at := time.Unix(100, 0).UTC()
	inventory := asset.InventoryRevision{
		InventoryID:    servedAssetRef("asset", "laptop-1"),
		Owner:          servedAssetRef("organization", "org-1"),
		Classification: "LAPTOP",
		SerialNumber:   "SN-1",
		Revision:       servedAssetRevision(t, "inventory", 1),
		EffectiveAt:    at,
		Status:         asset.Available,
	}
	store := surface.NewCustodyStore()
	if surface.Version() != 1 || store == nil {
		t.Fatalf("served asset construction version=%d store=%v", surface.Version(), store)
	}
	if err := surface.RegisterInventory(store, inventory); err != nil {
		t.Fatalf("served inventory registration: %v", err)
	}
	worker := servedAssetRef("worker", "worker-1")
	if err := surface.Assign(store, inventory.InventoryID, worker, "HQ", "GOOD", servedAssetReceipt(t, "assignee", true), servedAssetReceipt(t, "issuer", true), values.UnspecifiedRevision(), servedAssetRevision(t, "custody", 1), at.Add(time.Minute)); err != nil {
		t.Fatalf("served assignment: %v", err)
	}

	current, ok, err := store.CurrentCustody(inventory.InventoryID)
	if err != nil || !ok || current.Status != asset.Assigned || current.Worker != worker {
		t.Fatalf("served custody current=%+v ok=%v", current, ok)
	}
	if err := surface.CompleteReturn(store, inventory.InventoryID, "HQ", "GOOD", servedAssetReceipt(t, "return-assignee", false), servedAssetReceipt(t, "return-issuer", true), current.Revision, servedAssetRevision(t, "custody", 2), at.Add(2*time.Minute)); !errors.Is(err, asset.ErrUnverifiedReturn) {
		t.Fatalf("served unverified return=%v, want ErrUnverifiedReturn", err)
	}

	history, err := store.HistoryCustody(inventory.InventoryID)
	if err != nil {
		t.Fatalf("served custody history: %v", err)
	}
	explanation, err := surface.Explain(inventory, history)
	if err != nil || explanation.CurrentStatus != asset.Assigned || explanation.CustodyEvents != 1 || explanation.Digest == "" {
		t.Fatalf("served asset explanation=%+v err=%v", explanation, err)
	}

	var nilApp *application.App
	if exposed := nilApp.Asset(); exposed.Version != nil || exposed.NewCustodyStore != nil {
		t.Fatal("nil application exposed asset capabilities")
	}
}

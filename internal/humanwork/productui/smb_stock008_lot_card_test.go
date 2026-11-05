package productui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/stock"
)

type smbStock008Allocations struct {
	opens map[string][]stock.OpenAllocation
}

func (f *smbStock008Allocations) OpenAllocations(_ context.Context, _, _, itemID string) ([]stock.OpenAllocation, error) {
	return f.opens[itemID], nil
}

func smbStock008TraceFixture(t *testing.T) (stock.LotTrace, stock.OperationContext) {
	t.Helper()
	ctx := context.Background()
	op := stock.OperationContext{TenantID: "tenant-a", CompanyID: "company-a", ActorID: "operator-1", Origin: "human"}
	catalog := stock.NewCatalog(nil)
	item, err := catalog.CreateItem(ctx, op, stock.ItemDraft{SKU: "SERUM-10", Name: "Serum vials", BaseUnit: "each", Scale: 0, Tracking: stock.TrackingEach}, "card-seed")
	if err != nil {
		t.Fatal(err)
	}
	allocs := &smbStock008Allocations{opens: map[string][]stock.OpenAllocation{
		item.ItemID: {{AllocationID: "alloc-1", Qty: "8", HolderRef: "job-1"}},
	}}
	registry := stock.NewLotRegistry(catalog, allocs)
	lot, err := registry.RegisterLot(ctx, op, stock.RegisterLotInput{
		ItemID: item.ItemID, SiteID: "site-1", SupplierLot: "SUP-77",
		ReceivedFrom: "supplier-acme", ReceivedDoc: "recv-1",
		ReceivedAt:     time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC),
		Expiry:         stock.ExpiryPolicy{LocalDate: "2026-12-01", Timezone: "America/New_York", Rule: stock.ExpiryNextLocalDayStart},
		PolicyRevision: "exp-v3", Scale: 0, OnHand: "10",
	}, "card-lot")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.HoldLot(ctx, op, lot.LotID, "suspect temperature excursion", "", "card-hold"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.RecallItem(ctx, op, item.ItemID, "supplier flagged contamination", "lab-report-9", "card-recall"); err != nil {
		t.Fatal(err)
	}
	trace, err := registry.TraceLot(op, lot.LotID)
	if err != nil {
		t.Fatal(err)
	}
	return trace, op
}

// TestTodo_SMB_STOCK_008_Browser renders the lot card the way the Inventory
// lots surface serves it: status, pinned expiry with source zone, on-hand
// and held quantities, recall obligations, affected-work links and the
// immutable event timeline, localized across en-US, de-DE and ar RTL.
func TestTodo_SMB_STOCK_008_Browser(t *testing.T) {
	trace, _ := smbStock008TraceFixture(t)

	markup, err := ui.RenderToString(SMBStockLotCard(trace, "8", 3, "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"SUP-77", "SERUM-10", "Status: Recalled", "2026-12-01 (America/New_York)",
		"2026-12-02 05:00 UTC", "Affected work: 3.", "view=work",
		"Recall: supplier flagged contamination", "Blocked demand: 8", "Shortage: 8",
		"REGISTERED", "HELD", "RECALLED", "suspect temperature excursion",
		"data-lot-id=\"" + trace.Lot.LotID + "\"", "tabIndex=\"-1\"",
		"Lot events are immutable",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("lot card missing %q: %s", want, markup)
		}
	}

	german, err := ui.RenderToString(SMBStockLotCard(trace, "8", 0, "de-DE"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Status: Zurückgerufen", "Rückruf: supplier flagged contamination", "Keine betroffene Arbeit."} {
		if !strings.Contains(german, want) {
			t.Errorf("German lot card missing %q: %s", want, german)
		}
	}

	rtl, err := ui.RenderToString(SMBStockLotCard(trace, "8", 1, "ar"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dir=\"rtl\"", "lang=\"ar\"", "الحالة: مستدعى", "الأعمال المتأثرة"} {
		if !strings.Contains(rtl, want) {
			t.Errorf("Arabic lot card missing %q: %s", want, rtl)
		}
	}
}

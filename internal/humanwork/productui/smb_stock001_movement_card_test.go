package productui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/inventory"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/stock"
)

// TestTodo_SMB_STOCK_001_Browser renders the movement card the way the
// Inventory surfaces serve it: a posted receipt with canonical quantities
// and derived balances, a typed refusal with guidance and no raw
// internals, the serialized-hold note while serial actions stay hidden,
// and localized right-to-left content with a focusable heading.
func TestTodo_SMB_STOCK_001_Browser(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	op := stock.OperationContext{TenantID: "tenant-a", CompanyID: "company-a", SiteID: "site-1", ActorID: "operator-1", Origin: stock.OriginHuman}
	catalog := stock.NewCatalog(nil)
	item, err := catalog.CreateItem(ctx, op, stock.ItemDraft{
		SKU: "CARD-1", Name: "Card widget", BaseUnit: "each", Scale: 0, Tracking: stock.TrackingEach,
	}, "card-item")
	if err != nil {
		t.Fatal(err)
	}
	journal := inventory.NewJournal(catalog, inventory.JournalConfig{})
	posted, err := journal.Post(ctx, op, inventory.MovementRequest{
		ItemID: item.ItemID, ExpectedItemRevision: item.Revision, Kind: inventory.MovementReceive,
		QuantityText: "10", SiteID: "site-1", Reason: "PO-1", At: at,
	}, "card-op-1")
	if err != nil {
		t.Fatal(err)
	}
	balance, err := journal.ReadBalance(ctx, op, item.ItemID, "site-1")
	if err != nil {
		t.Fatal(err)
	}
	markup, err := ui.RenderToString(SMBStockMovementCard(posted, nil, balance, "CARD-1", "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Movement receipt", "RECEIVE", ">10<", "CARD-1", "site-1", "card-op-1",
		"data-operation-id=\"card-op-1\"", "data-sequence=\"1\"", "tabIndex=\"-1\"",
		"Balance after posting", "Movements are immutable",
		"Serialized stock is not enabled yet",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("receipt card missing %q: %s", want, markup)
		}
	}

	_, refuseErr := journal.Post(ctx, op, inventory.MovementRequest{
		ItemID: item.ItemID, ExpectedItemRevision: item.Revision, Kind: inventory.MovementIssue,
		QuantityText: "99", SiteID: "site-1", Reason: "greedy pick", At: at,
	}, "card-op-2")
	if refuseErr == nil {
		t.Fatal("oversell posted, want refusal")
	}
	denied, err := ui.RenderToString(SMBStockMovementCard(inventory.Movement{OperationID: "card-op-2"}, refuseErr, balance, "CARD-1", "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Movement refused", "Not enough stock is available."} {
		if !strings.Contains(denied, want) {
			t.Errorf("refusal card missing %q: %s", want, denied)
		}
	}
	if strings.Contains(denied, "INSUFFICIENT") || strings.Contains(denied, "inventory:") {
		t.Errorf("refusal card leaks internals: %s", denied)
	}

	german, err := ui.RenderToString(SMBStockMovementCard(posted, nil, balance, "CARD-1", "de-DE"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(german, "Buchungsbeleg") || !strings.Contains(german, ">10<") {
		t.Errorf("german card keeps no canonical quantity: %s", german)
	}
	arabic, err := ui.RenderToString(SMBStockMovementCard(posted, nil, balance, "CARD-1", "ar"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"إيصال الحركة", "dir=\"rtl\"", "lang=\"ar\"", ">10<"} {
		if !strings.Contains(arabic, want) {
			t.Errorf("arabic card missing %q: %s", want, arabic)
		}
	}
}

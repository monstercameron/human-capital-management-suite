package productui

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/stock"
)

func smbStock002CardFixture(t *testing.T) (*stock.Catalog, stock.CatalogItem, stock.OperationContext) {
	t.Helper()
	ctx := context.Background()
	op := stock.OperationContext{TenantID: "tenant-a", CompanyID: "company-a", ActorID: "operator-1", Origin: "human"}
	catalog := stock.NewCatalog(nil)
	item, err := catalog.CreateItem(ctx, op, stock.ItemDraft{
		SKU: "WIDGET-12", Name: "Widget 12-pack box", BaseUnit: "each", Scale: 0, Tracking: stock.TrackingEach,
		Conversions: []stock.PackConversion{{PackUnit: "box", Numerator: 12, Denominator: 1}},
		Aliases:     []stock.BarcodeAlias{{Symbology: "EAN-13", Code: "000123"}},
	}, "card-create")
	if err != nil {
		t.Fatal(err)
	}
	return catalog, item, op
}

// TestTodo_SMB_STOCK_002_Browser renders the catalog card the way the
// Inventory items surface serves it: identity facts, the exact conversion
// preview or its refusal, leading-zero barcodes, retirement obligations and
// localized right-to-left content, with a focusable heading after save.
func TestTodo_SMB_STOCK_002_Browser(t *testing.T) {
	ctx := context.Background()
	catalog, _, op := smbStock002CardFixture(t)
	stored := catalog.ListItems(op, stock.ItemFilter{Search: "WIDGET-12"})
	if len(stored) != 1 {
		t.Fatalf("card item lookup = %d items", len(stored))
	}
	boxPreview, err := catalog.ValidateConversion(op, stored[0].ItemID, "2", "box", false)
	if err != nil {
		t.Fatal(err)
	}
	markup, err := ui.RenderToString(SMBStockItemCard(stored[0], &boxPreview, nil, 0, "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Widget 12-pack box", "WIDGET-12", "000123", "EAN-13",
		"2 × box (12 each) = 24 each", "Status: Active",
		`data-item-id="` + stored[0].ItemID + `"`, `data-revision="1"`,
		"tabIndex=\"-1\"", "Retirement releases the SKU",
		"Revisions never rewrite history",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("item card missing %q: %s", want, markup)
		}
	}

	retired, err := catalog.RetireItem(ctx, op, stored[0].ItemID, "card-retire")
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := ui.RenderToString(SMBStockItemCard(retired, nil, nil, 2, "de-DE"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Status: Stillgelegt", "Stilllegung blockiert: 2.", "Verpflichtungen ansehen", "view=obligations"} {
		if !strings.Contains(blocked, want) {
			t.Errorf("retired German card missing %q: %s", want, blocked)
		}
	}

	arabic, err := catalog.CreateItem(ctx, op, stock.ItemDraft{
		SKU: "AR-1", Name: "مضخة هيدروليكية صناعية طويلة الاسم للاختبار في أسوأ الحالات", BaseUnit: "each", Scale: 0, Tracking: stock.TrackingSerialized,
	}, "card-arabic")
	if err != nil {
		t.Fatal(err)
	}
	rtl, err := ui.RenderToString(SMBStockItemCard(arabic, nil, nil, 0, "ar"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`dir="rtl"`, `lang="ar"`, "مضخة هيدروليكية", "لا توجد رموز شريطية مسجلة"} {
		if !strings.Contains(rtl, want) {
			t.Errorf("Arabic card missing %q: %s", want, rtl)
		}
	}

	thirds, err := catalog.CreateItem(ctx, op, stock.ItemDraft{
		SKU: "THIRD-1", Name: "Third packs", BaseUnit: "each", Scale: 2, Tracking: stock.TrackingEach,
		Conversions: []stock.PackConversion{{PackUnit: "third-pack", Numerator: 1, Denominator: 3}},
	}, "card-thirds")
	if err != nil {
		t.Fatal(err)
	}
	_, convErr := catalog.ValidateConversion(op, thirds.ItemID, "1", "third-pack", false)
	if convErr == nil {
		t.Fatal("single third-pack preview succeeded, want refusal")
	}
	refused, err := ui.RenderToString(SMBStockItemCard(thirds, nil, convErr, 0, "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`role="alert"`, "Conversion cannot post", "INEXACT_CONVERSION"} {
		if !strings.Contains(refused, want) {
			t.Errorf("refused-preview card missing %q: %s", want, refused)
		}
	}
}

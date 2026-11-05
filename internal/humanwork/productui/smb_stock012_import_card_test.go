package productui

import (
	"bytes"
	"context"
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops/importing"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/stock"
)

type smbStock012Sites struct{}

func (smbStock012Sites) IsKnownSite(_ string, siteID string) bool {
	return siteID == "site-1" || siteID == "site-2"
}

var smbStock012Header = []string{
	"record_kind", "external_key", "source_system", "source_version", "sku", "name",
	"base_unit", "scale", "tracking", "barcode_symbology", "barcode_code",
	"pack_unit", "pack_num", "pack_den", "item_key", "site", "quantity", "unit",
	"physical_bucket", "quality_reason", "policy_version", "lot_supplier",
	"lot_expiry_date", "lot_expiry_tz", "serial_namespace", "serial", "asset_ref",
	"as_of", "source_authority", "origin_receipt_ref", "source_dispatch_ref",
	"transfer_ref", "from_location", "to_location",
}

func smbStock012CSV(t *testing.T, rows ...map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write(smbStock012Header); err != nil {
		t.Fatal(err)
	}
	for _, cells := range rows {
		record := make([]string, len(smbStock012Header))
		for i, column := range smbStock012Header {
			record[i] = cells[column]
		}
		if err := writer.Write(record); err != nil {
			t.Fatal(err)
		}
	}
	writer.Flush()
	return buf.String()
}

func smbStock012ResultFixture(t *testing.T) (stock.ImportCommitResult, []SMBStockImportBucket) {
	t.Helper()
	ctx := context.Background()
	op := stock.OperationContext{TenantID: "tenant-a", CompanyID: "company-a", ActorID: "operator-1", Origin: "human"}
	catalog := stock.NewCatalog(nil)
	lots := stock.NewLotRegistry(catalog, nil)
	importer := stock.NewInventoryImporter(catalog, lots, importing.NewMemoryCheckpoints(), smbStock012Sites{})
	file := smbStock012CSV(t,
		map[string]string{"record_kind": "ITEM", "external_key": "item-card", "source_system": "acme-erp", "source_version": "v1", "sku": "CARD-1", "name": "Card item", "base_unit": "each", "scale": "0", "tracking": "EACH"},
		map[string]string{"record_kind": "OPENING", "external_key": "open-card-1", "source_system": "acme-erp", "source_version": "v1", "item_key": "item-card", "site": "site-1", "quantity": "6", "unit": "each", "physical_bucket": "USABLE", "as_of": "2026-10-01T00:00:00Z", "source_authority": "legacy-erp", "origin_receipt_ref": "recv-1"},
		map[string]string{"record_kind": "OPENING", "external_key": "open-card-2", "source_system": "acme-erp", "source_version": "v1", "item_key": "item-card", "site": "site-1", "quantity": "2", "unit": "each", "physical_bucket": "QUARANTINE", "quality_reason": "awaiting inspection", "policy_version": "qp-v1", "as_of": "2026-10-01T00:00:00Z", "source_authority": "legacy-erp", "origin_receipt_ref": "recv-2"},
		map[string]string{"record_kind": "OPENING", "external_key": "open-card-bad", "source_system": "acme-erp", "source_version": "v1", "item_key": "item-card", "site": "site-9", "quantity": "-4", "unit": "each", "physical_bucket": "USABLE", "as_of": "2026-10-01T00:00:00Z", "source_authority": "legacy-erp", "origin_receipt_ref": "recv-3"},
	)
	opts := stock.ImportOptions{SourceAuthorityAck: true, RetrievedAt: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)}
	dryRun, err := importer.ValidateInventoryImport(ctx, op, "card.csv", file, opts)
	if err != nil {
		t.Fatal(err)
	}
	result, err := importer.CommitInventoryImport(ctx, op, dryRun, file, opts)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied != 3 || result.Failed != 1 {
		t.Fatalf("card fixture = %+v, want 3 applied and 1 failed", result)
	}
	balances, err := importer.OpeningBalances(op, "site-1", "CARD-1")
	if err != nil {
		t.Fatal(err)
	}
	buckets := []SMBStockImportBucket{{Site: "site-1", SKU: "CARD-1", Usable: balances.Usable, Quarantine: balances.Quarantine, Transit: balances.Transit}}
	return result, buckets
}

// TestTodo_SMB_STOCK_012_Browser renders the import results card the way the
// shared setup Import inventory step serves it: exact applied counts, receipt
// links, line-specific row errors with a safe download note, resume and the
// opening buckets, localized across en-US, de-DE and ar RTL.
func TestTodo_SMB_STOCK_012_Browser(t *testing.T) {
	result, buckets := smbStock012ResultFixture(t)

	markup, err := ui.RenderToString(SMBStockImportCard(result, buckets, "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Inventory import results", "3 of 4 rows applied.", "CARD-1",
		"imports?receipt=" + result.Receipts[0].ReceiptID, "Row errors",
		"Line 5 site:", "Download row errors (CSV)",
		"site-1", "Usable", "Quarantine", "In transit",
		"In-transit stock is not available until received.",
		"Receipts are immutable",
		"data-import-id=\"" + result.ImportID + "\"", "tabIndex=\"-1\"",
		"Opening buckets",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("import card missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "Resume import") {
		t.Errorf("completed import card offers resume: %s", markup)
	}

	german, err := ui.RenderToString(SMBStockImportCard(result, buckets, "de-DE"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Ergebnisse des Bestandsimports", "3 von 4 Zeilen übernommen.", "Zeilenfehler herunterladen (CSV)"} {
		if !strings.Contains(german, want) {
			t.Errorf("German import card missing %q: %s", want, german)
		}
	}

	rtl, err := ui.RenderToString(SMBStockImportCard(result, buckets, "ar"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dir=\"rtl\"", "lang=\"ar\"", "نتائج استيراد المخزون", "3 من 4 صفوف مطبقة."} {
		if !strings.Contains(rtl, want) {
			t.Errorf("Arabic import card missing %q: %s", want, rtl)
		}
	}

	partial := result
	partial.Complete = false
	partial.Unattempted = 2
	resume, err := ui.RenderToString(SMBStockImportCard(partial, nil, "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resume, "Resume import") || !strings.Contains(resume, "view=resume") {
		t.Errorf("incomplete import card missing resume: %s", resume)
	}
}

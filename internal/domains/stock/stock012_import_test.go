package stock

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/importdraft"
)

var stock012Header = []string{
	"record_kind", "external_key", "source_system", "source_version", "sku", "name",
	"base_unit", "scale", "tracking", "barcode_symbology", "barcode_code",
	"pack_unit", "pack_num", "pack_den", "item_key", "site", "quantity", "unit",
	"physical_bucket", "quality_reason", "policy_version", "lot_supplier",
	"lot_expiry_date", "lot_expiry_tz", "serial_namespace", "serial", "asset_ref",
	"as_of", "source_authority", "origin_receipt_ref", "source_dispatch_ref",
	"transfer_ref", "from_location", "to_location",
}

func stock012Row(cells map[string]string) []string {
	row := make([]string, len(stock012Header))
	for i, col := range stock012Header {
		row[i] = cells[col]
	}
	return row
}

func stock012CSV(rows ...[]string) string {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	_ = writer.Write(stock012Header)
	for _, row := range rows {
		_ = writer.Write(row)
	}
	writer.Flush()
	return buf.String()
}

func stock012ItemRow(key, sku, name string, extra map[string]string) []string {
	cells := map[string]string{
		"record_kind": "ITEM", "external_key": key, "source_system": "acme-erp",
		"source_version": "v1", "sku": sku, "name": name, "base_unit": "each",
		"scale": "0", "tracking": "EACH",
	}
	for k, v := range extra {
		cells[k] = v
	}
	return stock012Row(cells)
}

func stock012OpeningRow(key, itemKey, site, qty, unit, bucket string, extra map[string]string) []string {
	cells := map[string]string{
		"record_kind": "OPENING", "external_key": key, "source_system": "acme-erp",
		"source_version": "v1", "item_key": itemKey, "site": site, "quantity": qty,
		"unit": unit, "physical_bucket": bucket, "as_of": "2026-10-01T00:00:00Z",
		"source_authority": "legacy-erp", "origin_receipt_ref": "recv-" + key,
	}
	for k, v := range extra {
		cells[k] = v
	}
	return stock012Row(cells)
}

type stock012Sites struct {
	known map[string]bool
}

func (s stock012Sites) IsKnownSite(_ string, siteID string) bool {
	return s.known[siteID]
}

func stock012Context() OperationContext {
	return OperationContext{TenantID: "tenant-a", CompanyID: "company-a", ActorID: "operator-1", Origin: "human"}
}

func stock012Importer() (*InventoryImporter, *Catalog, *LotRegistry) {
	catalog := NewCatalog(nil)
	lots := NewLotRegistry(catalog, nil)
	return NewInventoryImporter(catalog, lots, importdraft.NewMemoryCheckpoints(), stock012Sites{known: map[string]bool{"site-1": true, "site-2": true}}), catalog, lots
}

func stock012Options() ImportOptions {
	return ImportOptions{SourceAuthorityAck: true, RetrievedAt: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)}
}

// TestTodo_SMB_STOCK_012 proves staged opening import: dry-run previews
// starting and resulting quantities, commit posts catalog rows and bucketed
// opening balances with receipts, transit and quarantine release exactly
// once, and live locations refuse new opening balances pending reconcile.
func TestTodo_SMB_STOCK_012(t *testing.T) {
	ctx := context.Background()
	op := stock012Context()
	importer, _, _ := stock012Importer()
	opts := stock012Options()

	file := stock012CSV(
		stock012ItemRow("item-widget", "WIDGET-12", "Widget 12-pack", map[string]string{
			"barcode_symbology": "EAN-13", "barcode_code": "000123",
			"pack_unit": "box", "pack_num": "12", "pack_den": "1",
		}),
		stock012ItemRow("item-serum", "SERUM-10", "Serum vials", nil),
		stock012OpeningRow("open-w usable", "item-widget", "site-1", "6", "each", "USABLE", nil),
		stock012OpeningRow("open-serum-q", "item-serum", "site-1", "2", "each", "QUARANTINE", map[string]string{
			"quality_reason": "awaiting inspection", "policy_version": "qp-v1",
			"lot_supplier": "SUP-1", "lot_expiry_date": "2027-01-01", "lot_expiry_tz": "America/New_York",
		}),
		stock012OpeningRow("open-w-transit", "item-widget", "site-2", "3", "each", "IN_TRANSIT", map[string]string{
			"source_dispatch_ref": "disp-1", "transfer_ref": "tr-1",
			"from_location": "site-1", "to_location": "site-2",
		}),
	)

	dryRun, err := importer.ValidateInventoryImport(ctx, op, "upload-1.csv", file, opts)
	if err != nil {
		t.Fatal(err)
	}
	if dryRun.TotalRows != 5 || dryRun.ValidRows != 5 || len(dryRun.Errors) != 0 {
		t.Fatalf("dry run = %+v, want 5 valid rows", dryRun)
	}
	preview, err := importer.PreviewOpeningMovements(op, dryRun)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview) != 3 {
		t.Fatalf("preview has %d movements, want 3 opening rows", len(preview))
	}
	for _, move := range preview {
		if move.StartingQty != "0" {
			t.Fatalf("preview starts at %q, want 0: %+v", move.StartingQty, move)
		}
	}

	committed, err := importer.CommitInventoryImport(ctx, op, dryRun, file, opts)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Applied != 5 || committed.Failed != 0 || !committed.Complete || len(committed.Receipts) != 5 {
		t.Fatalf("commit = %+v, want 5 applied receipts", committed)
	}
	widget, err := importer.OpeningBalances(op, "site-1", "WIDGET-12")
	if err != nil {
		t.Fatal(err)
	}
	if widget.Available != "6" || widget.OnHandTotal != "6" {
		t.Fatalf("widget site-1 balances = %+v, want available6/total6", widget)
	}
	serum, err := importer.OpeningBalances(op, "site-1", "SERUM-10")
	if err != nil {
		t.Fatal(err)
	}
	if serum.Quarantine != "2" || serum.OnHandTotal != "2" {
		t.Fatalf("serum balances = %+v, want quarantine2/total2", serum)
	}
	transit, err := importer.OpeningBalances(op, "site-2", "WIDGET-12")
	if err != nil {
		t.Fatal(err)
	}
	if transit.Transit != "3" || transit.OnHandTotal != "0" || transit.Available != "0" {
		t.Fatalf("widget site-2 balances = %+v, want transit3 only", transit)
	}

	dupTransit := stock012CSV(
		stock012OpeningRow("open-w-transit-2", "item-widget", "site-2", "3", "each", "IN_TRANSIT", map[string]string{
			"source_dispatch_ref": "disp-1", "transfer_ref": "tr-1",
			"from_location": "site-1", "to_location": "site-2",
		}),
	)
	dupDry, err := importer.ValidateInventoryImport(ctx, op, "upload-2.csv", dupTransit, opts)
	if err != nil {
		t.Fatal(err)
	}
	if dupDry.ValidRows != 0 || len(dupDry.Errors) != 1 || !strings.Contains(dupDry.Errors[0].Message, "transfer_ref") {
		t.Fatalf("duplicate transit dry run = %+v, want one transfer_ref error", dupDry)
	}

	received, err := importer.CompleteTransitReceive(ctx, op, "tr-1", "receive-tr-1")
	if err != nil {
		t.Fatal(err)
	}
	if received.Status != TransitReceived {
		t.Fatalf("transit status = %q", received.Status)
	}
	afterReceive, err := importer.OpeningBalances(op, "site-2", "WIDGET-12")
	if err != nil {
		t.Fatal(err)
	}
	if afterReceive.Transit != "0" || afterReceive.Available != "3" {
		t.Fatalf("post-receive balances = %+v, want transit moved to available once", afterReceive)
	}
	if _, err := importer.CompleteTransitReceive(ctx, op, "tr-1", "receive-tr-1"); err != nil {
		t.Fatalf("transit replay err=%v, want idempotent same result", err)
	}
	if again, err := importer.OpeningBalances(op, "site-2", "WIDGET-12"); err != nil || again.Available != "3" {
		t.Fatalf("transit replay moved twice: %+v err=%v", again, err)
	}

	var quarantineLot string
	for _, receipt := range committed.Receipts {
		if receipt.ExternalKey == "open-serum-q" {
			quarantineLot = receipt.LotID
		}
	}
	if quarantineLot == "" {
		t.Fatal("quarantine opening left no lot")
	}
	if _, err := importer.ReleaseOpeningQuarantine(ctx, op, quarantineLot, "reviewer-2", "inspect-9", time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC), "release-q-1"); err != nil {
		t.Fatal(err)
	}
	released, err := importer.OpeningBalances(op, "site-1", "SERUM-10")
	if err != nil {
		t.Fatal(err)
	}
	if released.Quarantine != "0" || released.Available != "2" {
		t.Fatalf("post-release balances = %+v, want quarantine moved to usable once", released)
	}

	if err := importer.MarkLocationLive(ctx, op, "site-1", "WIDGET-12"); err != nil {
		t.Fatal(err)
	}
	liveFile := stock012CSV(
		stock012OpeningRow("open-w-late", "item-widget", "site-1", "5", "each", "USABLE", nil),
	)
	liveDry, err := importer.ValidateInventoryImport(ctx, op, "upload-3.csv", liveFile, opts)
	if err != nil {
		t.Fatal(err)
	}
	if liveDry.ValidRows != 0 || !strings.Contains(liveDry.Errors[0].Message, "reconcile") {
		t.Fatalf("live-location dry run = %+v, want reconcile refusal", liveDry)
	}
	liveCommit, err := importer.CommitInventoryImport(ctx, op, liveDry, liveFile, opts)
	if err != nil {
		t.Fatal(err)
	}
	if liveCommit.Applied != 0 || liveCommit.Failed != 1 {
		t.Fatalf("live-location commit = %+v, want zero movements", liveCommit)
	}
	unchanged, err := importer.OpeningBalances(op, "site-1", "WIDGET-12")
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Available != "6" {
		t.Fatalf("live-location import moved balances: %+v", unchanged)
	}
}

// TestTodo_SMB_STOCK_012_Integration proves resumable bounded-group commit,
// line-specific dry-run errors without movement, correction of rejected rows
// in a new artifact, and the explicit-correction path for new versions.
func TestTodo_SMB_STOCK_012_Integration(t *testing.T) {
	ctx := context.Background()
	op := stock012Context()
	importer, _, lots := stock012Importer()
	opts := stock012Options()

	records := [][]string{stock012ItemRow("item-bulk", "BULK-1", "Bulk screws", nil)}
	for i := 0; i < 99; i++ {
		records = append(records, stock012OpeningRow(fmt.Sprintf("open-bulk-%d", i), "item-bulk", "site-1", "1", "each", "USABLE", nil))
	}
	file := stock012CSV(records...)
	dryRun, err := importer.ValidateInventoryImport(ctx, op, "bulk.csv", file, opts)
	if err != nil {
		t.Fatal(err)
	}
	if dryRun.TotalRows != 100 || dryRun.ValidRows != 100 {
		t.Fatalf("bulk dry run = %+v, want 100 valid rows", dryRun)
	}
	first, err := importer.CommitInventoryImport(ctx, op, dryRun, file, ImportOptions{SourceAuthorityAck: true, RetrievedAt: opts.RetrievedAt, GroupSize: 78})
	if err != nil {
		t.Fatal(err)
	}
	if first.Applied != 78 || first.Unattempted != 22 || first.Complete {
		t.Fatalf("interrupted commit = %+v, want 78 applied and 22 unattempted", first)
	}
	resumed, err := importer.CommitInventoryImport(ctx, op, dryRun, file, opts)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Applied != 100 || !resumed.Complete || len(resumed.Receipts) != 100 {
		t.Fatalf("resumed commit = %+v, want exactly 100 receipts", resumed)
	}
	for i := 0; i < 78; i++ {
		if resumed.Receipts[i].ReceiptID != first.Receipts[i].ReceiptID {
			t.Fatal("resume changed already-committed receipts")
		}
	}
	balances, err := importer.OpeningBalances(op, "site-1", "BULK-1")
	if err != nil {
		t.Fatal(err)
	}
	if balances.Available != "99" {
		t.Fatalf("bulk available = %q, want 99", balances.Available)
	}

	badFile := stock012CSV(
		stock012ItemRow("item-bad", "BAD-1", "Bad item", map[string]string{"pack_unit": "box", "pack_num": "12", "pack_den": "0"}),
		stock012ItemRow("item-dup-alias", "DUP-1", "Dup alias", map[string]string{"barcode_symbology": "EAN-13", "barcode_code": "999001"}),
		stock012ItemRow("item-dup-alias-2", "DUP-2", "Dup alias 2", map[string]string{"barcode_symbology": "EAN-13", "barcode_code": "999001"}),
		stock012OpeningRow("open-neg", "item-bad", "site-1", "-3", "each", "USABLE", nil),
		stock012OpeningRow("open-prec", "item-bad", "site-1", "1.5", "each", "USABLE", nil),
		stock012OpeningRow("open-unit", "item-bad", "site-1", "2", "stone", "USABLE", nil),
		stock012OpeningRow("open-site", "item-bad", "site-9", "2", "each", "USABLE", nil),
		stock012OpeningRow("open-lot", "item-bad", "site-1", "2", "each", "USABLE", map[string]string{"lot_supplier": "SUP-x", "lot_expiry_date": "2027-01-01"}),
	)
	badDry, err := importer.ValidateInventoryImport(ctx, op, "bad.csv", badFile, opts)
	if err != nil {
		t.Fatal(err)
	}
	if badDry.ValidRows != 0 || len(badDry.Errors) < 8 {
		t.Fatalf("bad dry run = %+v, want every row rejected with line errors", badDry)
	}
	lines := map[int]bool{}
	fields := map[string]bool{}
	for _, rowErr := range badDry.Errors {
		lines[rowErr.CSVLine] = true
		fields[rowErr.Field] = true
	}
	for line := 2; line <= 9; line++ {
		if !lines[line] {
			t.Fatalf("row at CSV line %d has no error: %+v", line, badDry.Errors)
		}
	}
	for _, field := range []string{"pack_den", "barcode_code", "quantity", "unit", "site"} {
		if !fields[field] {
			t.Fatalf("no error names field %q: %+v", field, badDry.Errors)
		}
	}
	badCommit, err := importer.CommitInventoryImport(ctx, op, badDry, badFile, opts)
	if err != nil {
		t.Fatal(err)
	}
	if badCommit.Applied != 0 {
		t.Fatalf("invalid batch applied %d rows, want zero movement", badCommit.Applied)
	}

	mixed := [][]string{stock012ItemRow("item-g", "GADGET-1", "Gadget", nil)}
	for i := 0; i < 77; i++ {
		mixed = append(mixed, stock012OpeningRow(fmt.Sprintf("open-g-%d", i), "item-g", "site-1", "1", "each", "USABLE", nil))
	}
	for i := 0; i < 22; i++ {
		mixed = append(mixed, stock012OpeningRow(fmt.Sprintf("open-g-bad-%d", i), "item-g", "site-1", "-1", "each", "USABLE", nil))
	}
	mixedFile := stock012CSV(mixed...)
	mixedDry, err := importer.ValidateInventoryImport(ctx, op, "mixed.csv", mixedFile, opts)
	if err != nil {
		t.Fatal(err)
	}
	mixedCommit, err := importer.CommitInventoryImport(ctx, op, mixedDry, mixedFile, opts)
	if err != nil {
		t.Fatal(err)
	}
	if mixedCommit.Applied != 78 || mixedCommit.Failed != 22 {
		t.Fatalf("mixed commit = %+v, want 78 applied and 22 failed", mixedCommit)
	}
	fixed := [][]string{}
	for i := 0; i < 22; i++ {
		fixed = append(fixed, stock012OpeningRow(fmt.Sprintf("open-g-bad-%d", i), "item-g", "site-1", "1", "each", "USABLE", nil))
	}
	fixedFile := stock012CSV(fixed...)
	fixedDry, err := importer.ValidateInventoryImport(ctx, op, "mixed-fix.csv", fixedFile, opts)
	if err != nil {
		t.Fatal(err)
	}
	if fixedDry.ValidRows != 22 {
		t.Fatalf("fixed dry run = %+v, want 22 valid rows in the new artifact", fixedDry)
	}
	fixedCommit, err := importer.CommitInventoryImport(ctx, op, fixedDry, fixedFile, opts)
	if err != nil {
		t.Fatal(err)
	}
	if fixedCommit.Applied != 22 {
		t.Fatalf("fixed commit = %+v, want 22 applied", fixedCommit)
	}
	seen := map[string]bool{}
	for _, receipt := range mixedCommit.Receipts {
		seen[receipt.ReceiptID] = true
	}
	for _, receipt := range fixedCommit.Receipts {
		if seen[receipt.ReceiptID] {
			t.Fatalf("fixed artifact reused receipt %s", receipt.ReceiptID)
		}
		seen[receipt.ReceiptID] = true
	}
	if len(seen) != 100 {
		t.Fatalf("total receipts = %d, want exactly 100", len(seen))
	}

	changedFile := stock012CSV(
		stock012OpeningRow("open-g-0", "item-g", "site-1", "7", "each", "USABLE", nil),
	)
	changedDry, err := importer.ValidateInventoryImport(ctx, op, "changed.csv", changedFile, opts)
	if err != nil {
		t.Fatal(err)
	}
	if changedDry.ValidRows != 0 || !strings.Contains(changedDry.Errors[0].Code, "CONFLICT") {
		t.Fatalf("changed same-version row = %+v, want conflict", changedDry)
	}

	var original ImportReceipt
	for _, receipt := range mixedCommit.Receipts {
		if receipt.ExternalKey == "open-g-1" {
			original = receipt
		}
	}
	corrected, err := importer.CorrectOpeningRow(ctx, op, ImportCorrection{ReceiptID: original.ReceiptID, SourceVersion: "v2", Quantity: "4", Unit: "each"}, "correct-1")
	if err != nil {
		t.Fatal(err)
	}
	if corrected.CorrectsReceiptID != original.ReceiptID || corrected.SourceVersion != "v2" {
		t.Fatalf("correction = %+v, want explicit link to original receipt", corrected)
	}
	superseded, err := importer.ImportReceipt(op, original.ReceiptID)
	if err != nil {
		t.Fatal(err)
	}
	if superseded.Status != ReceiptSuperseded {
		t.Fatalf("original receipt status = %q, want superseded", superseded.Status)
	}
	afterCorrect, err := importer.OpeningBalances(op, "site-1", "GADGET-1")
	if err != nil {
		t.Fatal(err)
	}
	if afterCorrect.Available != "102" {
		t.Fatalf("corrected available = %q, want 99+3 delta applied once", afterCorrect.Available)
	}
	if _, err := importer.CorrectOpeningRow(ctx, op, ImportCorrection{ReceiptID: original.ReceiptID, SourceVersion: "v2", Quantity: "4", Unit: "each"}, "correct-1"); err != nil {
		t.Fatalf("correction replay err=%v, want idempotent same result", err)
	}
	if again, err := importer.OpeningBalances(op, "site-1", "GADGET-1"); err != nil || again.Available != "102" {
		t.Fatalf("correction replay moved twice: %+v err=%v", again, err)
	}
	dupKeyFile := stock012CSV(
		stock012ItemRow("dup-key", "KA-1", "Key A", nil),
		stock012ItemRow("dup-key", "KA-2", "Key B", nil),
	)
	dupKeyDry, err := importer.ValidateInventoryImport(ctx, op, "dupkey.csv", dupKeyFile, opts)
	if err != nil {
		t.Fatal(err)
	}
	if dupKeyDry.ValidRows != 0 || !strings.Contains(dupKeyDry.Errors[0].Code, "DUPLICATE") {
		t.Fatalf("duplicate external_key = %+v, want duplicate errors on both rows", dupKeyDry)
	}

	newVersionFile := stock012CSV(
		stock012OpeningRow("open-g-2", "item-g", "site-1", "1", "each", "USABLE", map[string]string{"source_version": "v9"}),
	)
	newVersionDry, err := importer.ValidateInventoryImport(ctx, op, "newver.csv", newVersionFile, opts)
	if err != nil {
		t.Fatal(err)
	}
	if newVersionDry.ValidRows != 0 || !strings.Contains(newVersionDry.Errors[0].Code, "CORRECTION") {
		t.Fatalf("new-version row = %+v, want explicit-correction refusal", newVersionDry)
	}

	serialFile := stock012CSV(
		stock012ItemRow("item-pump", "PUMP-1", "Pump", map[string]string{"tracking": "SERIALIZED"}),
		stock012OpeningRow("open-pump-1", "item-pump", "site-1", "1", "each", "USABLE", map[string]string{
			"serial_namespace": "PUMP", "serial": "PX-1",
		}),
	)
	serialDry, err := importer.ValidateInventoryImport(ctx, op, "serial.csv", serialFile, opts)
	if err != nil {
		t.Fatal(err)
	}
	if serialDry.ValidRows != 2 {
		t.Fatalf("serial dry run = %+v, want rows valid pending asset linkage", serialDry)
	}
	serialCommit, err := importer.CommitInventoryImport(ctx, op, serialDry, serialFile, opts)
	if err != nil {
		t.Fatal(err)
	}
	if serialCommit.Applied != 1 || serialCommit.Pending != 1 || len(serialCommit.PendingRows) != 1 {
		t.Fatalf("unlinked serial commit = %+v, want item applied and serial row pending", serialCommit)
	}
	if err := importer.AttachSerialAsset(ctx, op, serialDry.ImportID, serialCommit.PendingRows[0].RowID, "asset-px-1"); err != nil {
		t.Fatal(err)
	}
	serialResume, err := importer.CommitInventoryImport(ctx, op, serialDry, serialFile, opts)
	if err != nil {
		t.Fatal(err)
	}
	if serialResume.Applied != 2 || !serialResume.Complete {
		t.Fatalf("linked serial resume = %+v, want both rows applied", serialResume)
	}
	linked, err := lots.SerialTrace(op, "PUMP", "PX-1")
	if err != nil || linked.Unit.AssetRef != "asset-px-1" {
		t.Fatalf("linked serial = %+v err=%v, want asset-px-1 bound", linked.Unit, err)
	}

	freshFile := stock012CSV(stock012ItemRow("item-cancel", "CANCEL-1", "Cancel me", nil))
	freshDry, err := importer.ValidateInventoryImport(ctx, op, "cancel.csv", freshFile, opts)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	cancelledCommit, err := importer.CommitInventoryImport(cancelled, op, freshDry, freshFile, opts)
	if err != nil {
		t.Fatal(err)
	}
	if cancelledCommit.Applied != 0 || cancelledCommit.Unattempted != 1 || cancelledCommit.Complete {
		t.Fatalf("cancelled commit = %+v, want nothing attempted", cancelledCommit)
	}
}

// TestTodo_SMB_STOCK_012_Race proves concurrent resumes converge on one
// receipt per row and that identical identities across files return the
// original result instead of duplicating it.
func TestTodo_SMB_STOCK_012_Race(t *testing.T) {
	ctx := context.Background()
	op := stock012Context()
	importer, _, _ := stock012Importer()
	opts := stock012Options()

	records := [][]string{stock012ItemRow("item-race", "RACE-1", "Race item", nil)}
	for i := 0; i < 20; i++ {
		records = append(records, stock012OpeningRow(fmt.Sprintf("open-race-%d", i), "item-race", "site-1", "1", "each", "USABLE", nil))
	}
	file := stock012CSV(records...)
	dryRun, err := importer.ValidateInventoryImport(ctx, op, "race.csv", file, opts)
	if err != nil {
		t.Fatal(err)
	}

	const racers = 4
	var wg sync.WaitGroup
	results := make([]ImportCommitResult, racers)
	errs := make([]error, racers)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := importer.CommitInventoryImport(ctx, op, dryRun, file, opts)
			results[i], errs[i] = res, err
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("concurrent resume err=%v", err)
		}
	}
	for _, res := range results {
		if res.Applied != 21 || !res.Complete {
			t.Fatalf("concurrent resume = %+v, want 21 applied", res)
		}
	}
	byRow := map[string]string{}
	for _, res := range results {
		for _, receipt := range res.Receipts {
			if prior, ok := byRow[receipt.RowID]; ok && prior != receipt.ReceiptID {
				t.Fatal("concurrent resumes diverged on receipt identity")
			}
			byRow[receipt.RowID] = receipt.ReceiptID
		}
	}
	if len(byRow) != 21 {
		t.Fatalf("%d distinct receipted rows, want 21", len(byRow))
	}
	balances, err := importer.OpeningBalances(op, "site-1", "RACE-1")
	if err != nil {
		t.Fatal(err)
	}
	if balances.Available != "20" {
		t.Fatalf("raced available = %q, want exactly 20", balances.Available)
	}

	again, err := importer.ValidateInventoryImport(ctx, op, "race-copy.csv", file, opts)
	if err != nil {
		t.Fatal(err)
	}
	second, err := importer.CommitInventoryImport(ctx, op, again, file, opts)
	if err != nil {
		t.Fatal(err)
	}
	if second.Applied != 21 {
		t.Fatalf("same-identity second file applied %d, want 21 original results", second.Applied)
	}
	for _, receipt := range second.Receipts {
		if byRow[receipt.RowID] != receipt.ReceiptID {
			t.Fatal("same identity and payload across files did not return the original result")
		}
	}
	if same, err := importer.OpeningBalances(op, "site-1", "RACE-1"); err != nil || same.Available != "20" {
		t.Fatalf("second file doubled balances: %+v err=%v", same, err)
	}
}

// TestTodo_SMB_STOCK_012_Security proves tenant/company isolation on import,
// context authentication, source-authority acknowledgement, and that row
// errors never leak inaccessible records.
func TestTodo_SMB_STOCK_012_Security(t *testing.T) {
	ctx := context.Background()
	op := stock012Context()
	importer, _, _ := stock012Importer()
	opts := stock012Options()

	seed := stock012CSV(stock012ItemRow("item-seed", "SEED-1", "Seeded", nil))
	seedDry, err := importer.ValidateInventoryImport(ctx, op, "seed.csv", seed, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := importer.CommitInventoryImport(ctx, op, seedDry, seed, opts); err != nil {
		t.Fatal(err)
	}

	foreignTenant := op
	foreignTenant.TenantID = "tenant-evil"
	probe := stock012CSV(stock012OpeningRow("open-probe", "item-seed", "site-1", "1", "each", "USABLE", nil))
	probeDry, err := importer.ValidateInventoryImport(ctx, foreignTenant, "probe.csv", probe, opts)
	if err != nil {
		t.Fatal(err)
	}
	if probeDry.ValidRows != 0 {
		t.Fatalf("cross-tenant import validated %d rows against foreign records", probeDry.ValidRows)
	}
	for _, rowErr := range probeDry.Errors {
		if strings.Contains(rowErr.Message, "SEED-1") || strings.Contains(rowErr.Message, "Seeded") {
			t.Fatalf("cross-tenant error leaks record details: %+v", rowErr)
		}
	}
	foreignCompany := op
	foreignCompany.CompanyID = "company-evil"
	companyDry, err := importer.ValidateInventoryImport(ctx, foreignCompany, "probe.csv", probe, opts)
	if err != nil {
		t.Fatal(err)
	}
	if companyDry.ValidRows != 0 {
		t.Fatalf("cross-company import validated %d rows against foreign records", companyDry.ValidRows)
	}

	badOrigin := op
	badOrigin.Origin = "scan-beep"
	if _, err := importer.ValidateInventoryImport(ctx, badOrigin, "bad.csv", seed, opts); !errors.Is(err, ErrStockContext) {
		t.Fatalf("bad origin err=%v, want context refusal", err)
	}

	noAck := opts
	noAck.SourceAuthorityAck = false
	if _, err := importer.ValidateInventoryImport(ctx, op, "noack.csv", seed, noAck); !errors.Is(err, ErrImportAck) {
		t.Fatalf("unacknowledged dry run err=%v, want ack refusal", err)
	}
	acked, err := importer.ValidateInventoryImport(ctx, op, "ack.csv", seed, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := importer.CommitInventoryImport(ctx, op, acked, seed, noAck); !errors.Is(err, ErrImportAck) {
		t.Fatalf("unacknowledged commit err=%v, want ack refusal", err)
	}

	if _, err := importer.OpeningBalances(foreignTenant, "site-1", "SEED-1"); !errors.Is(err, ErrStockScope) {
		t.Fatalf("cross-tenant balances err=%v, want scope refusal", err)
	}
}

// TestTodo_SMB_STOCK_012_Recovery proves a snapshot round-trip preserves
// receipts, ledger buckets, transit records and identity admissions so a
// resumed import continues exactly where it stopped.
func TestTodo_SMB_STOCK_012_Recovery(t *testing.T) {
	ctx := context.Background()
	op := stock012Context()
	opts := stock012Options()
	catalog := NewCatalog(nil)
	lots := NewLotRegistry(catalog, nil)
	importer := NewInventoryImporter(catalog, lots, importdraft.NewMemoryCheckpoints(), stock012Sites{known: map[string]bool{"site-1": true, "site-2": true}})

	records := [][]string{stock012ItemRow("item-rec", "REC-1", "Recovery item", nil)}
	for i := 0; i < 10; i++ {
		records = append(records, stock012OpeningRow(fmt.Sprintf("open-rec-%d", i), "item-rec", "site-1", "1", "each", "USABLE", nil))
	}
	records = append(records, stock012OpeningRow("open-rec-t", "item-rec", "site-2", "5", "each", "IN_TRANSIT", map[string]string{
		"source_dispatch_ref": "disp-r", "transfer_ref": "tr-r",
		"from_location": "site-1", "to_location": "site-2",
	}))
	file := stock012CSV(records...)
	dryRun, err := importer.ValidateInventoryImport(ctx, op, "recovery.csv", file, opts)
	if err != nil {
		t.Fatal(err)
	}
	partial, err := importer.CommitInventoryImport(ctx, op, dryRun, file, ImportOptions{SourceAuthorityAck: true, RetrievedAt: opts.RetrievedAt, GroupSize: 6})
	if err != nil {
		t.Fatal(err)
	}
	if partial.Applied != 6 || partial.Unattempted != 6 {
		t.Fatalf("partial commit = %+v, want 6 applied and 6 unattempted", partial)
	}

	impBytes, err := importer.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	catBytes, err := catalog.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	lotsBytes, err := lots.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	catalog2 := NewCatalog(nil)
	if err := catalog2.Restore(catBytes); err != nil {
		t.Fatal(err)
	}
	lots2 := NewLotRegistry(catalog2, nil)
	if err := lots2.Restore(lotsBytes); err != nil {
		t.Fatal(err)
	}
	restarted := NewInventoryImporter(catalog2, lots2, importdraft.NewMemoryCheckpoints(), stock012Sites{known: map[string]bool{"site-1": true, "site-2": true}})
	if err := restarted.Restore(impBytes); err != nil {
		t.Fatal(err)
	}
	finished, err := restarted.CommitInventoryImport(ctx, op, dryRun, file, opts)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Applied != 12 || !finished.Complete || len(finished.Receipts) != 12 {
		t.Fatalf("post-restart resume = %+v, want 12 receipts", finished)
	}
	for i := 0; i < 6; i++ {
		if finished.Receipts[i].ReceiptID != partial.Receipts[i].ReceiptID {
			t.Fatal("restart changed already-committed receipts")
		}
	}
	balances, err := restarted.OpeningBalances(op, "site-1", "REC-1")
	if err != nil {
		t.Fatal(err)
	}
	if balances.Available != "10" {
		t.Fatalf("post-restart available = %q, want 10", balances.Available)
	}
	if _, err := restarted.CompleteTransitReceive(ctx, op, "tr-r", "receive-r"); err != nil {
		t.Fatalf("post-restart transit receive err=%v", err)
	}
	received, err := restarted.OpeningBalances(op, "site-2", "REC-1")
	if err != nil {
		t.Fatal(err)
	}
	if received.Available != "5" || received.Transit != "0" {
		t.Fatalf("post-restart transit balances = %+v", received)
	}
	if err := restarted.Restore(impBytes); err == nil {
		t.Fatal("restore into a populated importer succeeded, want refusal")
	}
	fresh := NewInventoryImporter(NewCatalog(nil), NewLotRegistry(nil, nil), importdraft.NewMemoryCheckpoints(), stock012Sites{})
	if err := fresh.Restore([]byte("{broken")); err == nil {
		t.Fatal("restore of corrupt bytes succeeded, want refusal")
	}
}

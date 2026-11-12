// TestTodo_SMB_ASSET_002 covers SMB-ASSET-002: the equipment register that
// binds each physical unit to purchase, location and history without
// double-counting stock. Receipts create exactly one asset per serial;
// stock linkage stays optional and pending until reconciled; staged
// imports enforce domain source-key uniqueness with replay, conflict and
// explicit-correction semantics.
package asset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/importdraft"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func asset002UUID(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	raw := hex.EncodeToString(sum[:16])
	return raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:]
}

func asset002Ref(tenant, kind, id string) values.EntityRef {
	return values.EntityRef{Tenant: values.TenantId(tenant), Kind: values.Kind(kind), Id: asset002UUID(kind + ":" + id)}
}

const (
	asset002Worker1 = "4c102969-7ee3-5871-5d38-1b03416ec15f"
	asset002Worker2 = "6ec10296-7ee3-5871-5d38-1b03416ec15f"
)

func asset002Owner() values.EntityRef {
	return asset002Ref("tenant-a", "organization", "acme-llc")
}

func asset002At() time.Time {
	return time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
}

func asset002Workers(tenant values.TenantId, worker values.EntityRef) bool {
	if worker.Tenant != tenant || worker.Kind != "worker" {
		return false
	}
	return worker.Id == asset002Worker1 || worker.Id == asset002Worker2
}

func asset002Commit(t *testing.T, committer *EquipmentCommitter, simDigest string, row EquipmentImportRow) importdraft.ItemLineage {
	t.Helper()
	lineage, err := committer.CommitItem(context.Background(), importdraft.ItemRequest{
		Tenant: "tenant-a", ImportID: "import-1", SimulationDigest: simDigest,
		IdempotencyKey: importdraft.IdempotencyKey(simDigest, row.RowID),
		Draft:          importdraft.BusinessIntentDraft{RowID: row.RowID, IntentType: "equipment.register", Status: "CREATE", Writes: row.DraftWrites()},
	})
	if err != nil {
		t.Fatal(err)
	}
	return lineage
}

func asset002CommitInline(t *testing.T, committer *EquipmentCommitter, simDigest string, row EquipmentImportRow) importdraft.ItemLineage {
	t.Helper()
	return asset002Commit(t, committer, simDigest, row)
}

func asset002CommitErr(committer *EquipmentCommitter, simDigest string, row EquipmentImportRow) (importdraft.ItemLineage, error) {
	return committer.CommitItem(context.Background(), importdraft.ItemRequest{
		Tenant: "tenant-a", ImportID: "import-1", SimulationDigest: simDigest,
		IdempotencyKey: importdraft.IdempotencyKey(simDigest, row.RowID),
		Draft:          importdraft.BusinessIntentDraft{RowID: row.RowID, IntentType: "equipment.register", Status: "CREATE", Writes: row.DraftWrites()},
	})
}

func asset002Reg() *EquipmentRegister {
	reg := NewEquipmentRegister(asset002Workers)
	if err := reg.RegisterSerialScheme(SerialScheme{ID: "VENDOR_SN", Description: "Vendor serial plate"}); err != nil {
		panic(err)
	}
	return reg
}

func asset002Receipt(serials ...string) ReceiptInput {
	return ReceiptInput{
		Key: "receipt-r1", ReceiptID: "rcpt-1", Tenant: "tenant-a", Company: "acme",
		Owner: asset002Owner(), Classification: "PUMP", SchemeID: "VENDOR_SN", Serials: serials,
		SiteRef: "SITE-A", Condition: "NEW", AcquiredAt: asset002At(), Evidence: "po-77",
	}
}

func asset002Row(rowID, externalKey, version, serial, worker string) EquipmentImportRow {
	return EquipmentImportRow{
		RowID:         rowID,
		Source:        ImportSourceKey{SourceSystem: "legacy-erp", Company: "acme", RecordKind: "equipment", ExternalKey: externalKey},
		SourceVersion: version,
		Serial:        serial, Scheme: "VENDOR_SN", Classification: "PUMP", Site: "SITE-A", Condition: "NEW",
		Worker: worker, CustodyEvidence: "custody-1",
	}
}

func TestTodo_SMB_ASSET_002(t *testing.T) {
	reg := asset002Reg()
	if err := reg.RegisterSerialScheme(SerialScheme{ID: "VENDOR_SN"}); err == nil {
		t.Fatal("duplicate scheme registration succeeded, want refusal")
	}
	records, err := reg.RegisterEquipmentFromReceipt(asset002Receipt("SN-1", "SN-2", "SN-3"))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("receipt created %d assets, want exactly 3", len(records))
	}
	ids := map[string]bool{}
	for _, record := range records {
		if record.LinkState != StockLinkNone || record.Verification != VerificationVerified {
			t.Fatalf("receipt record = %+v, want link NONE and verified", record)
		}
		ids[record.Inventory.InventoryID.String()] = true
	}
	if len(ids) != 3 {
		t.Fatalf("receipt asset ids = %v, want 3 distinct", ids)
	}

	snapshot, err := reg.Export()
	if err != nil {
		t.Fatal(err)
	}
	restored := NewEquipmentRegister(asset002Workers)
	if err := restored.RegisterSerialScheme(SerialScheme{ID: "VENDOR_SN"}); err != nil {
		t.Fatal(err)
	}
	if err := restored.Import(snapshot); err != nil {
		t.Fatal(err)
	}
	if got := restored.ListEquipment("tenant-a", "acme", EquipmentFilter{}); len(got) != 3 {
		t.Fatalf("restarted register holds %d assets, want 3", len(got))
	}
	replay, err := restored.RegisterEquipmentFromReceipt(asset002Receipt("SN-1", "SN-2", "SN-3"))
	if err != nil || len(replay) != 3 {
		t.Fatalf("receipt retry = %d records, err=%v; want the original 3", len(replay), err)
	}
	if got := restored.ListEquipment("tenant-a", "acme", EquipmentFilter{}); len(got) != 3 {
		t.Fatalf("receipt retry left %d assets, want 3 with none extra", len(got))
	}

	dupes := asset002Receipt("SN-9", "SN-9")
	dupes.Key = "receipt-dupes"
	if _, err := reg.RegisterEquipmentFromReceipt(dupes); err == nil {
		t.Fatal("duplicate-serial receipt succeeded, want refusal with zero records")
	}
	if got := reg.ListEquipment("tenant-a", "acme", EquipmentFilter{Search: "SN-9"}); len(got) != 0 {
		t.Fatalf("refused receipt left %d records, want zero", len(got))
	}
	trimmed := asset002Receipt("  SN-1  ")
	trimmed.Key = "receipt-trimmed"
	if _, err := reg.RegisterEquipmentFromReceipt(trimmed); !errors.Is(err, ErrEquipmentConflict) {
		t.Fatalf("recased serial err=%v, want conflict with the canonical SN-1", err)
	}
}
func TestTodo_SMB_ASSET_002_Integration(t *testing.T) {
	reg := asset002Reg()
	manual, err := reg.RegisterEquipmentManual(ManualInput{
		Key: "manual-i1", Tenant: "tenant-a", Company: "acme", Owner: asset002Owner(),
		Classification: "LAPTOP", SchemeID: "VENDOR_SN", Serial: "LT-1",
		SiteRef: "SITE-A", Condition: "NEW", AcquiredSource: "po-78", AcquiredAt: asset002At(), Evidence: "po-78",
	})
	if err != nil {
		t.Fatal(err)
	}
	if manual.LinkState != StockLinkNone || manual.TrackInStock {
		t.Fatalf("asset-only manual = %+v, want untracked NONE", manual)
	}
	trackedBad := ManualInput{
		Key: "manual-i2", Tenant: "tenant-a", Company: "acme", Owner: asset002Owner(),
		Classification: "LAPTOP", SchemeID: "VENDOR_SN", Serial: "LT-2",
		SiteRef: "SITE-A", Condition: "NEW", AcquiredSource: "po-78", AcquiredAt: asset002At(), Evidence: "po-78",
		TrackInStock: true,
	}
	if _, err := reg.RegisterEquipmentManual(trackedBad); !errors.Is(err, ErrEquipmentValidation) {
		t.Fatalf("tracked manual without item/location err=%v, want validation refusal", err)
	}
	trackedBad.StockItemRef, trackedBad.StockLocationRef = "item-laptop", "loc-wh-1"
	tracked, err := reg.RegisterEquipmentManual(trackedBad)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := reg.RequestStockLink(LinkRequestInput{
		Key: "link-i1", Asset: tracked.Inventory.InventoryID, ExpectedRevision: tracked.Revision,
		Evidence: "link-req-1", Actor: "clerk-1", At: asset002At(),
	})
	if err != nil || pending.LinkState != StockLinkPending {
		t.Fatalf("link request = %+v, err=%v; want PENDING", pending, err)
	}
	if reg.EquipmentAvailability(pending).Assignable != true {
		t.Fatal("pending link blocked assignment; linking must not block lawful asset operations")
	}
	// Failure after registration before the stock link: only the snapshot
	// survives. Repair must complete the original IDs, never duplicates.
	snapshot, err := reg.Export()
	if err != nil {
		t.Fatal(err)
	}
	repaired := NewEquipmentRegister(asset002Workers)
	if err := repaired.RegisterSerialScheme(SerialScheme{ID: "VENDOR_SN"}); err != nil {
		t.Fatal(err)
	}
	if err := repaired.Import(snapshot); err != nil {
		t.Fatal(err)
	}
	linked, err := repaired.ReconcileStockLink(LinkReconcileInput{
		Key: "reconcile-i1", Asset: tracked.Inventory.InventoryID, ExpectedRevision: pending.Revision,
		SourceReceipt: "po-78", CustodyEvidence: "custody-1", Actor: "clerk-1", At: asset002At(),
	})
	if err != nil || linked.LinkState != StockLinkLinked {
		t.Fatalf("link repair = %+v, err=%v; want LINKED on original ids", linked, err)
	}
	if linked.Inventory.InventoryID != tracked.Inventory.InventoryID {
		t.Fatal("link repair minted a new asset id, want the original")
	}
	if got := repaired.ListEquipment("tenant-a", "acme", EquipmentFilter{}); len(got) != 2 {
		t.Fatalf("repair left %d assets, want 2 with no duplicates", len(got))
	}

}

func TestTodo_SMB_ASSET_002_ImportCorrection(t *testing.T) {
	reg := asset002Reg()
	var rows []EquipmentImportRow
	for i := 0; i < 100; i++ {
		external := "eq-" + string(rune(48+i/10)) + string(rune(48+i%10))
		serial := "IMP-" + string(rune(48+i/10)) + string(rune(48+i%10))
		worker := asset002Worker1
		if i >= 78 {
			worker = "not a worker ref"
		}
		rows = append(rows, asset002Row("row-"+external, external, "1", serial, worker))
	}
	verdicts := reg.ValidateEquipmentImport("tenant-a", rows)
	var accepted, rejected int
	for _, verdict := range verdicts {
		switch verdict.Status {
		case VerdictAccept:
			accepted++
		case VerdictReject:
			rejected++
		default:
			t.Fatalf("row %s verdict = %+v, want accept or reject", verdict.RowID, verdict)
		}
	}
	if accepted != 78 || rejected != 22 {
		t.Fatalf("import verdicts = %d accepted %d rejected, want 78/22", accepted, rejected)
	}
	committer := NewEquipmentCommitter(reg)
	committedIDs := map[string]bool{}
	for _, row := range rows[:78] {
		lineage := asset002CommitInline(t, committer, "sim-artifact-1", row)
		committedIDs[lineage.TransactionRef] = true
	}
	if len(committedIDs) != 78 {
		t.Fatalf("committed %d distinct lineage refs, want 78", len(committedIDs))
	}
	// Correct only the 22 into a different artifact: original 78 keep IDs.
	for i := 78; i < 100; i++ {
		rows[i].Worker = asset002Worker2
		asset002CommitInline(t, committer, "sim-artifact-2", rows[i])
	}
	if got := reg.ListEquipment("tenant-a", "acme", EquipmentFilter{}); len(got) != 100 {
		t.Fatalf("corrected import holds %d assets, want exactly 100", len(got))
	}
	changed := rows[0]
	changed.Condition = "REFURBISHED"
	if _, err := asset002CommitErr(committer, "sim-artifact-1", changed); !errors.Is(err, ErrEquipmentConflict) {
		t.Fatalf("changed payload at same version err=%v, want conflict", err)
	}
	corrected, err := reg.CorrectEquipmentImport(CorrectionInput{
		Source: rows[0].Source, NewVersion: "2", OriginalReceipt: "legacy-erp:eq-00",
		Evidence: "recount-1", Actor: "clerk-1", Row: changed, At: asset002At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if corrected.SourceVersion != "2" {
		t.Fatalf("correction = %+v, want admitted version 2", corrected)
	}
	if got := reg.ListEquipment("tenant-a", "acme", EquipmentFilter{}); len(got) != 100 {
		t.Fatalf("correction left %d assets, want 100 with no duplicate", len(got))
	}
}
func TestTodo_SMB_ASSET_002_Race(t *testing.T) {
	reg := asset002Reg()
	var wg sync.WaitGroup
	var wins atomic.Int64
	var conflicts atomic.Int64
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			input := asset002Receipt("SN-R1", "SN-R2", "SN-R3")
			input.Key = "receipt-race-1"
			_, err := reg.RegisterEquipmentFromReceipt(input)
			_ = slot
			if err == nil {
				wins.Add(1)
			} else if errors.Is(err, ErrEquipmentConflict) {
				conflicts.Add(1)
			} else {
				t.Errorf("contended receipt err=%v, want success or conflict", err)
			}
		}(i)
	}
	wg.Wait()
	if wins.Load() != 8 || conflicts.Load() != 0 {
		t.Fatalf("same-key contended receipt wins=%d conflicts=%d, want 8 idempotent wins", wins.Load(), conflicts.Load())
	}
	if got := reg.ListEquipment("tenant-a", "acme", EquipmentFilter{}); len(got) != 3 {
		t.Fatalf("contended receipt left %d assets, want exactly 3", len(got))
	}

	var dupWins atomic.Int64
	var dupConflicts atomic.Int64
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			input := asset002Receipt("SN-R1", "SN-R4")
			input.Key = "receipt-race-dup"
			input.ReceiptID = "rcpt-race-dup"
			_, err := reg.RegisterEquipmentFromReceipt(input)
			_ = slot
			if err == nil {
				dupWins.Add(1)
			} else if errors.Is(err, ErrEquipmentConflict) {
				dupConflicts.Add(1)
			} else {
				t.Errorf("duplicate race err=%v, want success or conflict", err)
			}
		}(i)
	}
	wg.Wait()
	if dupWins.Load() != 0 || dupConflicts.Load() != 8 {
		t.Fatalf("duplicate race wins=%d conflicts=%d, want 0 wins 8 conflicts", dupWins.Load(), dupConflicts.Load())
	}
	if got := reg.ListEquipment("tenant-a", "acme", EquipmentFilter{Search: "SN-R4"}); len(got) != 0 {
		t.Fatalf("refused duplicate receipt left %d SN-R4 records, want zero", len(got))
	}

	committer := NewEquipmentCommitter(reg)
	row := asset002Row("row-race-1", "eq-race-1", "1", "IMP-R1", "")
	row.CustodyEvidence = ""
	var commitWins atomic.Int64
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := asset002CommitErr(committer, "sim-race-1", row); err != nil {
				t.Errorf("concurrent duplicate import row: %v", err)
				return
			}
			commitWins.Add(1)
		}()
	}
	wg.Wait()
	if commitWins.Load() != 8 {
		t.Fatalf("concurrent duplicate rows won %d, want 8 idempotent wins", commitWins.Load())
	}
	if got := reg.ListEquipment("tenant-a", "acme", EquipmentFilter{Search: "IMP-R1"}); len(got) != 1 {
		t.Fatalf("concurrent duplicates left %d IMP-R1 assets, want 1", len(got))
	}
}

func TestTodo_SMB_ASSET_002_Security(t *testing.T) {
	reg := asset002Reg()
	records, err := reg.RegisterEquipmentFromReceipt(asset002Receipt("SN-X1"))
	if err != nil {
		t.Fatal(err)
	}
	asset := records[0].Inventory.InventoryID
	if _, err := reg.GetEquipment(values.TenantId("tenant-b"), "acme", asset); !errors.Is(err, ErrTenantBoundary) {
		t.Fatalf("cross-tenant read err=%v, want tenant boundary refusal", err)
	}
	if got := reg.ListEquipment("tenant-b", "acme", EquipmentFilter{}); len(got) != 0 {
		t.Fatalf("cross-tenant list disclosed %d assets", len(got))
	}
	otherCompany := asset002Receipt("SN-X1")
	otherCompany.Key = "receipt-x1-other"
	otherCompany.Company = "globex"
	otherCompany.ReceiptID = "rcpt-x1-other"
	if _, err := reg.RegisterEquipmentFromReceipt(otherCompany); err != nil {
		t.Fatalf("same serial in another company refused: %v", err)
	}

	pending := asset002Row("row-x-pend", "eq-x-pend", "1", "IMP-XP", asset002Worker1)
	pending.CustodyEvidence = ""
	committer := NewEquipmentCommitter(reg)
	if _, err := asset002CommitErr(committer, "sim-x-1", pending); err != nil {
		t.Fatalf("evidence-free assigned row commit: %v", err)
	}
	stored, err := reg.GetEquipmentBySource("tenant-a", ImportSourceKey{SourceSystem: "legacy-erp", Company: "acme", RecordKind: "equipment", ExternalKey: "eq-x-pend"})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Verification != VerificationPending {
		t.Fatalf("evidence-free assignment verification = %q, want PENDING", stored.Verification)
	}
	if _, ok := reg.CustodyOf(stored.Inventory.InventoryID); ok {
		t.Fatal("pending assignment presents custody; unverified import must not assert custody")
	}
	badReceipt := Receipt{ID: asset002Ref("tenant-a", "receipt", "bad-1"), Issuer: asset002Owner(), IssuedAt: asset002At(), Verified: false, Evidence: "x"}
	worker1 := values.EntityRef{Tenant: "tenant-a", Kind: "worker", Id: asset002Worker1}
	if err := reg.VerifyImportedAssignment(stored.Inventory.InventoryID, 1, worker1, badReceipt, badReceipt); !errors.Is(err, ErrEquipmentValidation) {
		t.Fatalf("unverified assignment receipts err=%v, want validation refusal", err)
	}

	tracked, err := reg.RegisterEquipmentManual(ManualInput{
		Key: "manual-x1", Tenant: "tenant-a", Company: "acme", Owner: asset002Owner(),
		Classification: "LAPTOP", SchemeID: "VENDOR_SN", Serial: "LT-X1",
		SiteRef: "SITE-A", Condition: "NEW", AcquiredSource: "po-1", AcquiredAt: asset002At(), Evidence: "po-1",
		TrackInStock: true, StockItemRef: "item-1", StockLocationRef: "loc-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.RequestStockLink(LinkRequestInput{
		Key: "link-x-stale", Asset: tracked.Inventory.InventoryID, ExpectedRevision: tracked.Revision + 5,
		Evidence: "e", Actor: "clerk-1", At: asset002At(),
	}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale revision link err=%v, want revision conflict", err)
	}
}
func TestTodo_SMB_ASSET_002_Recovery(t *testing.T) {
	reg := asset002Reg()
	records, err := reg.RegisterEquipmentFromReceipt(asset002Receipt("SN-V1"))
	if err != nil {
		t.Fatal(err)
	}
	asset := records[0].Inventory.InventoryID
	worker := values.EntityRef{Tenant: "tenant-a", Kind: "worker", Id: asset002Worker1}
	assignee := Receipt{ID: asset002Ref("tenant-a", "receipt", "assign-v1"), Issuer: asset002Owner(), IssuedAt: asset002At(), Verified: true, Evidence: "hand-off"}
	issuer := Receipt{ID: asset002Ref("tenant-a", "receipt", "issue-v1"), Issuer: asset002Owner(), IssuedAt: asset002At(), Verified: true, Evidence: "hand-off"}
	rev, err := values.NewSequenceRevision("asset-custody", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.AssignEquipment(asset, worker, "SITE-A", "NEW", assignee, issuer, values.UnspecifiedRevision(), rev, asset002At()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := reg.Export()
	if err != nil {
		t.Fatal(err)
	}
	restored := NewEquipmentRegister(asset002Workers)
	if err := restored.RegisterSerialScheme(SerialScheme{ID: "VENDOR_SN"}); err != nil {
		t.Fatal(err)
	}
	if err := restored.Import(snapshot); err != nil {
		t.Fatal(err)
	}
	custody, ok := restored.CustodyOf(asset)
	if !ok || custody.Worker != worker || custody.Status != Assigned {
		t.Fatalf("restored custody = %+v, ok=%v; want assigned worker custody", custody, ok)
	}
	if got := restored.ListEquipment("tenant-a", "acme", EquipmentFilter{}); len(got) != 1 {
		t.Fatalf("restarted register holds %d assets, want 1", len(got))
	}
	replay, err := restored.RegisterEquipmentFromReceipt(asset002Receipt("SN-V1"))
	if err != nil || len(replay) != 1 || replay[0].Inventory.InventoryID != asset {
		t.Fatalf("post-restart replay = %+v, err=%v; want the original asset", replay, err)
	}
	if err := restored.Import([]byte("{not-json")); err == nil {
		t.Fatal("corrupt snapshot import succeeded, want refusal")
	}
}

func TestTodo_SMB_ASSET_002_SnapshotBindingRefused(t *testing.T) {
	reg := asset002Reg()
	if _, err := reg.RegisterEquipmentFromReceipt(asset002Receipt("SN-N1")); err != nil {
		t.Fatal(err)
	}
	snapshot, err := reg.Export()
	if err != nil {
		t.Fatal(err)
	}
	mutate := func(t *testing.T, change func(decoded map[string]any), want string) {
		t.Helper()
		decoded := map[string]any{}
		if err := json.Unmarshal(snapshot, &decoded); err != nil {
			t.Fatal(err)
		}
		change(decoded)
		raw, err := json.Marshal(decoded)
		if err != nil {
			t.Fatal(err)
		}
		fresh := NewEquipmentRegister(asset002Workers)
		if err := fresh.RegisterSerialScheme(SerialScheme{ID: "VENDOR_SN"}); err != nil {
			t.Fatal(err)
		}
		if err := fresh.Import(raw); err == nil {
			t.Fatalf("%s: tampered snapshot import succeeded, want refusal", want)
		}
	}

	mutate(t, func(decoded map[string]any) {
		records := decoded["records"].([]any)
		records[0].(map[string]any)["inventory"].(map[string]any)["SerialNumber"] = "VENDOR_SN:FORGED"
	}, "serial/index mismatch")
	mutate(t, func(decoded map[string]any) {
		records := decoded["records"].([]any)
		records[0].(map[string]any)["revision"] = float64(99)
	}, "revision tamper")
	mutate(t, func(decoded map[string]any) {
		for key, raw := range decoded["idem"].(map[string]any) {
			decoded["idem"].(map[string]any)[key+"-forged"] = raw
			delete(decoded["idem"].(map[string]any), key)
			break
		}
	}, "idempotency key refile")
	mutate(t, func(decoded map[string]any) {
		decoded["seq"] = float64(0)
	}, "regressed sequence")

	honest := NewEquipmentRegister(asset002Workers)
	if err := honest.RegisterSerialScheme(SerialScheme{ID: "VENDOR_SN"}); err != nil {
		t.Fatal(err)
	}
	if err := honest.Import(snapshot); err != nil {
		t.Fatalf("clean snapshot import refused: %v", err)
	}
}

func TestTodo_SMB_ASSET_002_Browser(t *testing.T) {
	reg := asset002Reg()
	records, err := reg.RegisterEquipmentFromReceipt(asset002Receipt("SN-B1", "SN-B2"))
	if err != nil {
		t.Fatal(err)
	}
	pending := records[0]
	linked, err := reg.RequestStockLink(LinkRequestInput{
		Key: "link-b1", Asset: pending.Inventory.InventoryID, ExpectedRevision: pending.Revision,
		StockItemRef: "item-1", StockLocationRef: "loc-1", Evidence: "link-req-1", Actor: "clerk-1", At: asset002At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = linked
	rows := reg.ProjectEquipmentList("tenant-a", "acme", EquipmentFilter{})
	if len(rows) != 2 {
		t.Fatalf("equipment list = %d rows, want 2", len(rows))
	}
	var pendingRow *EquipmentListRow
	for i := range rows {
		if rows[i].Serial == "VENDOR_SN:SN-B1" {
			pendingRow = &rows[i]
		}
	}
	if pendingRow == nil {
		t.Fatalf("equipment list = %+v, want the SN-B1 row", rows)
	}
	if pendingRow.StockState != "PENDING" || pendingRow.StatusText != "Awaiting reconciliation" || pendingRow.StatusOwner != "clerk-1" {
		t.Fatalf("pending row = %+v, want PENDING with owner-attributed reconciliation text", pendingRow)
	}
	capture := reg.ProjectSerialCapture([]string{"SN-B1", "SN-B2", "SN-B3"}, []string{"SN-B1", "SN-B1"})
	if capture.Expected != 3 || capture.Entered != 2 || len(capture.Duplicates) != 1 || capture.Duplicates[0] != 1 {
		t.Fatalf("capture = %+v, want expected 3 entered 2 duplicate at index 1", capture)
	}
	needs := reg.ProjectEquipmentList("tenant-a", "acme", EquipmentFilter{NeedsVerificationOnly: true})
	if len(needs) != 0 {
		t.Fatalf("needs-verification filter returned %d rows, want 0", len(needs))
	}
	if rows := reg.ProjectEquipmentList("tenant-b", "acme", EquipmentFilter{}); len(rows) != 0 {
		t.Fatalf("foreign tenant list disclosed %d rows", len(rows))
	}
}

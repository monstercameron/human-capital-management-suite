// TestTodo_SMB_STOCK_003 covers SMB-STOCK-003: warehouse, van, site and
// quarantine stock locations on canonical site refs with parent/bin
// topology, plus authorized availability views. Quarantine is one quantity
// classification independent of bin purpose: it is excluded from usable
// availability exactly once, never double-deducted.
package stock

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type stock003Transfers struct {
	open map[string]int
	err  error
}

func (s stock003Transfers) OpenTransfers(_ context.Context, _, _, locationID string) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	return s.open[locationID], nil
}

func stock003Context() OperationContext {
	return OperationContext{TenantID: "tenant-a", CompanyID: "company-a", ActorID: "clerk-1", Origin: OriginHuman}
}

func stock003Dec(t *testing.T, s string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(s, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func stock003Balance(t *testing.T, onHand, allocated, quarantine, inTransit string) BalanceInput {
	t.Helper()
	return BalanceInput{
		OnHand:     stock003Dec(t, onHand),
		Allocated:  stock003Dec(t, allocated),
		Quarantine: stock003Dec(t, quarantine),
		InTransit:  stock003Dec(t, inTransit),
	}
}

func stock003Create(t *testing.T, reg *LocationRegistry, op OperationContext, key, site, parent string, kind LocationKind, code, label string) StockLocation {
	t.Helper()
	loc, err := reg.CreateLocation(op, key, LocationRequest{SiteRef: site, ParentID: parent, Kind: kind, Code: code, Label: label})
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func stock003StoreBin(t *testing.T, reg *LocationRegistry, op OperationContext, site, tag string) (StockLocation, StockLocation) {
	t.Helper()
	store := stock003Create(t, reg, op, "create-store-"+tag, site, "", LocationStore, "WH-"+tag, "Warehouse "+tag)
	bin := stock003Create(t, reg, op, "create-bin-"+tag, site, store.ID, LocationBin, "BIN-"+tag, "Bin "+tag)
	return store, bin
}

func TestTodo_SMB_STOCK_003(t *testing.T) {
	reg := NewLocationRegistry(nil)
	op := stock003Context()
	_, bin := stock003StoreBin(t, reg, op, "SITE-A", "p1")
	if err := reg.ReportBalance(op, "bal-p1", bin.ID, "item-1", stock003Balance(t, "10.00", "3.00", "2.00", "0.00")); err != nil {
		t.Fatal(err)
	}
	view, err := reg.LocationStock(op, bin.ID, "item-1")
	if err != nil {
		t.Fatal(err)
	}
	if view.OnHand.String() != "10.00" || view.Usable.String() != "8.00" || view.Allocated.String() != "3.00" ||
		view.Quarantine.String() != "2.00" || view.Available.String() != "5.00" {
		t.Fatalf("10/3/2 buckets = %+v, want on_hand 10 usable 8 allocated 3 quarantine 2 available 5", view)
	}

	if err := reg.ReportBalance(op, "bal-p1-over", bin.ID, "item-1", stock003Balance(t, "10.00", "9.00", "2.00", "0.00")); !errors.Is(err, ErrLocationValidation) {
		t.Fatalf("allocated above usable err=%v, want validation refusal", err)
	}
	if err := reg.ReportBalance(op, "bal-p1-double", bin.ID, "item-1", stock003Balance(t, "10.00", "0.00", "11.00", "0.00")); !errors.Is(err, ErrLocationValidation) {
		t.Fatalf("quarantine above on-hand err=%v, want validation refusal", err)
	}

	storeX := stock003Create(t, reg, op, "create-store-p1x", "SITE-A", "", LocationStore, "WH-P1X", "Store X")
	storeY := stock003Create(t, reg, op, "create-store-p1y", "SITE-A", storeX.ID, LocationStore, "WH-P1Y", "Store Y")
	if _, err := reg.ReviseLocation(op, "reparent-cycle", storeX.ID, storeX.Revision, ReviseLocationRequest{ParentID: storeY.ID, ReorganizationKey: "reorg-1"}); !errors.Is(err, ErrLocationCycle) {
		t.Fatalf("A>B>A reparent err=%v, want cycle refusal", err)
	}
	if _, err := reg.ReviseLocation(op, "reparent-under-bin", storeX.ID, storeX.Revision, ReviseLocationRequest{ParentID: bin.ID, ReorganizationKey: "reorg-2"}); !errors.Is(err, ErrLocationValidation) {
		t.Fatalf("parenting under a bin err=%v, want validation refusal", err)
	}
	otherSite := stock003Create(t, reg, op, "create-store-p1c", "SITE-B", "", LocationStore, "WH-P1C", "Other site")
	if _, err := reg.ReviseLocation(op, "reparent-cross-site", bin.ID, bin.Revision, ReviseLocationRequest{ParentID: otherSite.ID, ReorganizationKey: "reorg-3"}); !errors.Is(err, ErrLocationValidation) {
		t.Fatalf("cross-site reparent err=%v, want validation refusal", err)
	}

	if _, err := reg.ArchiveLocation(op, "archive-stocked", bin.ID, bin.Revision, ArchiveLocationRequest{RelocationRef: "relocate-1"}); !errors.Is(err, ErrLocationNotEmpty) {
		t.Fatalf("archive stocked bin err=%v, want not-empty refusal", err)
	}
	if err := reg.ReportBalance(op, "bal-p1-zero", bin.ID, "item-1", stock003Balance(t, "0.00", "0.00", "0.00", "0.00")); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.ArchiveLocation(op, "archive-no-receipt", bin.ID, bin.Revision, ArchiveLocationRequest{}); !errors.Is(err, ErrLocationValidation) {
		t.Fatalf("archive without relocation evidence err=%v, want validation refusal", err)
	}
	archived, err := reg.ArchiveLocation(op, "archive-p1", bin.ID, bin.Revision, ArchiveLocationRequest{RelocationRef: "relocate-1"})
	if err != nil || archived.Active {
		t.Fatalf("archive = %+v, err=%v; want inactive record", archived, err)
	}
	preserved, err := reg.GetLocation(op, bin.ID)
	if err != nil || preserved.Active || len(reg.History(op, bin.ID)) == 0 {
		t.Fatalf("archive stranded the record: %+v, err=%v", preserved, err)
	}

	conserve := NewLocationRegistry(nil)
	_, normalBin := stock003StoreBin(t, conserve, op, "SITE-A", "q1")
	quarBin := stock003Create(t, conserve, op, "create-quar-q1", "SITE-A", "", LocationQuarantine, "Q-Q1", "Quarantine cage")
	if err := conserve.ReportBalance(op, "bal-q1-a", normalBin.ID, "item-9", stock003Balance(t, "8.00", "3.00", "0.00", "0.00")); err != nil {
		t.Fatal(err)
	}
	if err := conserve.ReportBalance(op, "bal-q1-b", quarBin.ID, "item-9", stock003Balance(t, "2.00", "0.00", "2.00", "0.00")); err != nil {
		t.Fatal(err)
	}
	aggregate, err := conserve.ItemAvailability(op, "item-9")
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.Total.String() != "10.00" || aggregate.Available.String() != "5.00" || aggregate.Quarantine.String() != "2.00" {
		t.Fatalf("8 usable + 2 quarantine with 3 allocated = %+v, want total 10 available 5", aggregate)
	}
	// Move the quarantined 2 into the normal bin: one classification, same totals.
	if err := conserve.ReportBalance(op, "bal-q1-c", quarBin.ID, "item-9", stock003Balance(t, "0.00", "0.00", "0.00", "0.00")); err != nil {
		t.Fatal(err)
	}
	if err := conserve.ReportBalance(op, "bal-q1-d", normalBin.ID, "item-9", stock003Balance(t, "10.00", "3.00", "2.00", "0.00")); err != nil {
		t.Fatal(err)
	}
	moved, err := conserve.ItemAvailability(op, "item-9")
	if err != nil {
		t.Fatal(err)
	}
	if moved.Total.String() != "10.00" || moved.Available.String() != "5.00" || moved.Quarantine.String() != "2.00" {
		t.Fatalf("quarantine move left %+v, want total 10 available 5 quarantine 2", moved)
	}
}

func TestTodo_SMB_STOCK_003_Integration(t *testing.T) {
	reg := NewLocationRegistry(stock003Transfers{open: map[string]int{}})
	op := stock003Context()
	storeA, binA := stock003StoreBin(t, reg, op, "SITE-A", "i1")
	storeB, binB := stock003StoreBin(t, reg, op, "SITE-B", "i2")
	if err := reg.ReportBalance(op, "bal-i1-a", binA.ID, "item-1", stock003Balance(t, "10.00", "3.00", "2.00", "0.00")); err != nil {
		t.Fatal(err)
	}
	if err := reg.ReportBalance(op, "bal-i1-b", binB.ID, "item-1", stock003Balance(t, "4.00", "1.00", "0.00", "2.00")); err != nil {
		t.Fatal(err)
	}
	aggregate, err := reg.ItemAvailability(op, "item-1")
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.Total.String() != "14.00" || aggregate.Available.String() != "8.00" || aggregate.InTransit.String() != "2.00" || len(aggregate.Locations) != 2 {
		t.Fatalf("cross-site aggregate = %+v, want total 14 available 8 in-transit 2 over 2 locations", aggregate)
	}

	before, err := reg.LocationStock(op, binA.ID, "item-1")
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := reg.ReviseLocation(op, "revise-i1", binA.ID, binA.Revision, ReviseLocationRequest{Label: "Bin A renamed"})
	if err != nil || renamed.Label != "Bin A renamed" || renamed.Revision != binA.Revision+1 {
		t.Fatalf("revise = %+v, err=%v", renamed, err)
	}
	storeA2 := stock003Create(t, reg, op, "create-store-i1b", "SITE-A", "", LocationStore, "WH-I1B", "Warehouse i1b")
	moved, err := reg.ReviseLocation(op, "reparent-i1", binA.ID, renamed.Revision, ReviseLocationRequest{ParentID: storeA2.ID, ReorganizationKey: "reorg-i1"})
	if err != nil {
		t.Fatal(err)
	}
	_ = storeA
	after, err := reg.LocationStock(op, binA.ID, "item-1")
	if err != nil {
		t.Fatal(err)
	}
	if after.Available.String() != before.Available.String() || after.OnHand.String() != before.OnHand.String() || moved.Revision != renamed.Revision+1 {
		t.Fatalf("reparent moved stock physically: before=%+v after=%+v", before, after)
	}
	if _, err := reg.ReviseLocation(op, "reparent-i1-nokey", binB.ID, binB.Revision, ReviseLocationRequest{ParentID: storeA.ID}); !errors.Is(err, ErrLocationValidation) {
		t.Fatalf("nonempty reparent without reorganization key err=%v, want validation refusal", err)
	}

	snapshot, err := reg.Export()
	if err != nil {
		t.Fatal(err)
	}
	restored := NewLocationRegistry(stock003Transfers{open: map[string]int{}})
	if err := restored.Import(snapshot); err != nil {
		t.Fatal(err)
	}
	same, err := restored.ItemAvailability(op, "item-1")
	if err != nil || same.Available.String() != "8.00" || same.Total.String() != "14.00" {
		t.Fatalf("restarted aggregate = %+v, err=%v; want total 14 available 8", same, err)
	}
	replay, err := restored.CreateLocation(op, "create-bin-i2", LocationRequest{SiteRef: "SITE-B", ParentID: storeB.ID, Kind: LocationBin, Code: "BIN-i2", Label: "Bin i2"})
	if err != nil {
		t.Fatalf("post-restart key replay err=%v, want the original result", err)
	}
	if replay.ID != binB.ID {
		t.Fatalf("post-restart replay created %q, want the original %q", replay.ID, binB.ID)
	}
}

func TestTodo_SMB_STOCK_003_Race(t *testing.T) {
	reg := NewLocationRegistry(nil)
	op := stock003Context()
	const racers = 8
	var wg sync.WaitGroup
	errs := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			code := "WH-R" + string(rune('0'+slot))
			_, err := reg.CreateLocation(op, "create-race-"+code, LocationRequest{SiteRef: "SITE-A", Kind: LocationStore, Code: code, Label: code})
			errs[slot] = err
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("distinct create %d: %v", i, err)
		}
	}
	seen := map[string]bool{}
	for _, loc := range reg.ListLocations(op, "SITE-A", "") {
		if seen[loc.ID] {
			t.Fatalf("duplicate location id %q", loc.ID)
		}
		seen[loc.ID] = true
	}
	if len(seen) != racers {
		t.Fatalf("race created %d locations, want %d", len(seen), racers)
	}

	var wins atomic.Int64
	var conflicts atomic.Int64
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			_, err := reg.CreateLocation(op, "create-race-same-"+string(rune('0'+slot)), LocationRequest{SiteRef: "SITE-A", Kind: LocationStore, Code: "WH-SAME", Label: "Same"})
			if err == nil {
				wins.Add(1)
			} else if errors.Is(err, ErrLocationConflict) {
				conflicts.Add(1)
			} else {
				t.Errorf("contended create err=%v, want success or conflict", err)
			}
		}(i)
	}
	wg.Wait()
	if wins.Load() != 1 || conflicts.Load() != racers-1 {
		t.Fatalf("contended code wins=%d conflicts=%d, want 1 win and %d conflicts", wins.Load(), conflicts.Load(), racers-1)
	}

	_, bin := stock003StoreBin(t, reg, op, "SITE-A", "race")
	balance := stock003Balance(t, "10.00", "3.00", "2.00", "0.00")
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := reg.ReportBalance(op, "bal-race-1", bin.ID, "item-1", balance); err != nil {
				t.Errorf("concurrent duplicate balance: %v", err)
			}
		}()
	}
	wg.Wait()
	view, err := reg.LocationStock(op, bin.ID, "item-1")
	if err != nil || view.Available.String() != "5.00" {
		t.Fatalf("concurrent duplicates left %+v, err=%v; want one available-5 effect", view, err)
	}
}

func TestTodo_SMB_STOCK_003_Security(t *testing.T) {
	reg := NewLocationRegistry(nil)
	op := stock003Context()
	_, binA := stock003StoreBin(t, reg, op, "SITE-A", "s1")
	_, binB := stock003StoreBin(t, reg, op, "SITE-B", "s2")
	if err := reg.ReportBalance(op, "bal-s1-a", binA.ID, "item-1", stock003Balance(t, "10.00", "3.00", "2.00", "0.00")); err != nil {
		t.Fatal(err)
	}
	if err := reg.ReportBalance(op, "bal-s1-b", binB.ID, "item-1", stock003Balance(t, "100.00", "0.00", "0.00", "0.00")); err != nil {
		t.Fatal(err)
	}

	foreign := OperationContext{TenantID: "tenant-b", CompanyID: "company-a", ActorID: "intruder", Origin: OriginHuman}
	if _, err := reg.GetLocation(foreign, binA.ID); !errors.Is(err, ErrStockScope) {
		t.Fatalf("cross-tenant read err=%v, want neutral scope refusal", err)
	}
	if _, err := reg.GetLocation(op, "loc-guessed"); !errors.Is(err, ErrStockScope) {
		t.Fatalf("guessed id read err=%v, want the same neutral scope refusal", err)
	}
	if _, err := reg.LocationStock(foreign, binA.ID, "item-1"); !errors.Is(err, ErrStockScope) {
		t.Fatalf("cross-tenant stock read err=%v, want neutral scope refusal", err)
	}
	if got := reg.ListLocations(foreign, "", ""); len(got) != 0 {
		t.Fatalf("cross-tenant list disclosed %d locations", len(got))
	}

	siteA := OperationContext{TenantID: "tenant-a", CompanyID: "company-a", SiteID: "SITE-A", ActorID: "viewer-1", Origin: OriginHuman}
	if _, err := reg.GetLocation(siteA, binB.ID); !errors.Is(err, ErrStockScope) {
		t.Fatalf("denied-site direct read err=%v, want neutral scope refusal", err)
	}
	for _, loc := range reg.ListLocations(siteA, "", "") {
		if loc.SiteRef == "SITE-B" {
			t.Fatalf("denied-site list disclosed %+v", loc)
		}
	}
	for _, loc := range reg.ListLocations(siteA, "", "s2") {
		t.Fatalf("denied-site search suggestion disclosed %+v", loc)
	}
	scoped, err := reg.ItemAvailability(siteA, "item-1")
	if err != nil {
		t.Fatal(err)
	}
	if scoped.Total.String() != "10.00" || scoped.Available.String() != "5.00" {
		t.Fatalf("site-scoped totals = %+v, want 10/5 with no SITE-B inference", scoped)
	}
	if _, err := reg.LocationStock(siteA, binB.ID, "item-1"); !errors.Is(err, ErrStockScope) {
		t.Fatalf("denied-site stock read err=%v, want neutral scope refusal", err)
	}

	full, err := reg.ItemAvailability(op, "item-1")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}
	lowered := strings.ToLower(string(payload))
	if strings.Contains(lowered, "cost") || strings.Contains(lowered, "price") {
		t.Fatalf("availability view leaks cost fields: %s", payload)
	}
}

func TestTodo_SMB_STOCK_003_Recovery(t *testing.T) {
	reg := NewLocationRegistry(nil)
	op := stock003Context()
	_, bin := stock003StoreBin(t, reg, op, "SITE-A", "r1")
	if err := reg.ReportBalance(op, "bal-r1", bin.ID, "item-1", stock003Balance(t, "10.00", "3.00", "2.00", "0.00")); err != nil {
		t.Fatal(err)
	}
	snapshot, err := reg.Export()
	if err != nil {
		t.Fatal(err)
	}
	restored := NewLocationRegistry(nil)
	if err := restored.Import(snapshot); err != nil {
		t.Fatal(err)
	}
	view, err := restored.LocationStock(op, bin.ID, "item-1")
	if err != nil || view.Available.String() != "5.00" || view.Watermark == "" {
		t.Fatalf("restarted balance = %+v, err=%v; want available 5 with watermark", view, err)
	}
	if err := restored.ReportBalance(op, "bal-r1", bin.ID, "item-1", stock003Balance(t, "10.00", "3.00", "2.00", "0.00")); err != nil {
		t.Fatalf("post-restart balance replay err=%v, want the original result", err)
	}
	if err := restored.ReportBalance(op, "bal-r1", bin.ID, "item-1", stock003Balance(t, "11.00", "3.00", "2.00", "0.00")); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("reused key with changed payload err=%v, want idempotency conflict", err)
	}
	if err := restored.Import([]byte("{not-json")); err == nil {
		t.Fatal("corrupt snapshot import succeeded, want refusal")
	}
	raw := string(snapshot)
	cut := strings.Index(raw, `"balances":`)
	if cut < 0 {
		t.Fatal("snapshot carries no balances section")
	}
	forged := raw[:cut] + strings.Replace(raw[cut:], "10.00", "99.00", 1)
	if err := NewLocationRegistry(nil).Import([]byte(forged)); err == nil {
		t.Fatal("tampered snapshot import succeeded, want refusal")
	}
}

func TestTodo_SMB_STOCK_003_Browser(t *testing.T) {
	reg := NewLocationRegistry(nil)
	op := stock003Context()
	_, bin := stock003StoreBin(t, reg, op, "SITE-A", "b1")
	if err := reg.ReportBalance(op, "bal-b1", bin.ID, "item-1", stock003Balance(t, "10.00", "3.00", "2.00", "1.00")); err != nil {
		t.Fatal(err)
	}
	view, err := reg.LocationStock(op, bin.ID, "item-1")
	if err != nil {
		t.Fatal(err)
	}
	rows := reg.ProjectTree(op, "SITE-A", "item-1", map[string]string{bin.ID: view.Watermark})
	if len(rows) != 2 {
		t.Fatalf("tree rows = %d, want store + bin", len(rows))
	}
	var binRow *LocationTreeRow
	for i := range rows {
		if rows[i].LocationID == bin.ID {
			binRow = &rows[i]
		}
	}
	if binRow == nil {
		t.Fatalf("tree rows = %+v, want the bin row", rows)
	}
	if binRow.Breadcrumb != "SITE-A > WH-b1 > BIN-b1" {
		t.Fatalf("breadcrumb = %q, want SITE-A > WH-b1 > BIN-b1", binRow.Breadcrumb)
	}
	cols := binRow.Columns
	if cols.OnHand != "10.00" || cols.Reserved != "3.00" || cols.Available != "5.00" || cols.Quarantine != "2.00" || cols.InTransit != "1.00" {
		t.Fatalf("tree columns = %+v, want 10/3/5/2/1", cols)
	}
	if binRow.Stale || !binRow.ActionsEnabled {
		t.Fatalf("fresh row = %+v, want live actions", binRow)
	}

	stale := reg.ProjectTree(op, "SITE-A", "item-1", map[string]string{bin.ID: "stale-watermark"})
	for _, row := range stale {
		if row.LocationID == bin.ID && (!row.Stale || row.ActionsEnabled) {
			t.Fatalf("stale row = %+v, want Updating with disabled actions", row)
		}
	}

	denied := OperationContext{TenantID: "tenant-a", CompanyID: "company-a", SiteID: "SITE-B", ActorID: "viewer-1", Origin: OriginHuman}
	if rows := reg.ProjectTree(denied, "SITE-A", "item-1", nil); len(rows) != 0 {
		t.Fatalf("denied-site tree disclosed %d rows", len(rows))
	}
}

func stock003SnapshotMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func stock003ImportMap(t *testing.T, reg *LocationRegistry, decoded map[string]any) error {
	t.Helper()
	raw, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	return reg.Import(raw)
}

func TestTodo_SMB_STOCK_003_SnapshotBindingRefused(t *testing.T) {
	reg := NewLocationRegistry(nil)
	op := stock003Context()
	_, bin := stock003StoreBin(t, reg, op, "SITE-A", "snap")
	if err := reg.ReportBalance(op, "bal-snap", bin.ID, "item-1", stock003Balance(t, "10.00", "3.00", "2.00", "0.00")); err != nil {
		t.Fatal(err)
	}
	snapshot, err := reg.Export()
	if err != nil {
		t.Fatal(err)
	}
	mutate := func(t *testing.T, change func(decoded map[string]any), want string) {
		t.Helper()
		decoded := stock003SnapshotMap(t, snapshot)
		change(decoded)
		if err := stock003ImportMap(t, NewLocationRegistry(nil), decoded); err == nil {
			t.Fatalf("%s: tampered snapshot import succeeded, want refusal", want)
		}
	}

	mutate(t, func(decoded map[string]any) {
		decoded["balances"].([]any)[0].(map[string]any)["item_id"] = "item-2"
	}, "balance/record key mismatch")
	mutate(t, func(decoded map[string]any) {
		for key, raw := range decoded["idem"].(map[string]any) {
			entry := raw.(map[string]any)
			entry["Location"].(map[string]any)["tenant_id"] = "tenant-b"
			decoded["idem"].(map[string]any)[key] = entry
			break
		}
	}, "idempotency scope swap")
	mutate(t, func(decoded map[string]any) {
		for key, raw := range decoded["idem"].(map[string]any) {
			entry := raw.(map[string]any)
			entry["Location"].(map[string]any)["label"] = "forged"
			decoded["idem"].(map[string]any)[key] = entry
			break
		}
	}, "idempotency location outside history")
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
	mutate(t, func(decoded map[string]any) {
		decoded["stocked"].(map[string]any)["loc-999999"] = true
	}, "stocked flag for unknown location")

	honest := NewLocationRegistry(nil)
	if err := honest.Import(snapshot); err != nil {
		t.Fatalf("clean snapshot import refused: %v", err)
	}
	next, err := honest.CreateLocation(op, "create-after-restore", LocationRequest{SiteRef: "SITE-A", Kind: LocationStore, Code: "WH-NEXT", Label: "Next"})
	if err != nil {
		t.Fatal(err)
	}
	if next.ID == bin.ID {
		t.Fatalf("restored sequence reused id %q", next.ID)
	}
}
func TestTodo_SMB_STOCK_003_SiteGateRefused(t *testing.T) {
	reg := NewLocationRegistry(nil)
	op := stock003Context()
	siteA := OperationContext{TenantID: "tenant-a", CompanyID: "company-a", SiteID: "SITE-A", ActorID: "clerk-1", Origin: OriginHuman}
	if _, err := reg.CreateLocation(siteA, "create-xsite", LocationRequest{SiteRef: "SITE-B", Kind: LocationStore, Code: "WH-X", Label: "Cross-site"}); !errors.Is(err, ErrStockScope) {
		t.Fatalf("cross-site create err=%v, want neutral scope refusal like every sibling write", err)
	}
	same, err := reg.CreateLocation(siteA, "create-ssite", LocationRequest{SiteRef: "SITE-A", Kind: LocationStore, Code: "WH-S", Label: "Same-site"})
	if err != nil {
		t.Fatalf("same-site scoped create refused: %v", err)
	}
	if _, err := reg.GetLocation(siteA, same.ID); err != nil {
		t.Fatalf("scoped op cannot read its own location: %v", err)
	}
	if got := reg.ListLocations(op, "", ""); len(got) != 1 {
		t.Fatalf("refused cross-site create left %d locations, want exactly 1", len(got))
	}
}

func TestTodo_SMB_STOCK_003_CycleGuard(t *testing.T) {
	reg := NewLocationRegistry(nil)
	op := stock003Context()
	store, bin := stock003StoreBin(t, reg, op, "SITE-A", "cycguard")
	reg.mu.Lock()
	history := reg.locations[store.ID]
	history[len(history)-1].ParentID = bin.ID
	reg.locations[store.ID] = history
	reg.mu.Unlock()
	done := make(chan []LocationTreeRow, 1)
	go func() { done <- reg.ProjectTree(op, "SITE-A", "item-1", map[string]string{}) }()
	select {
	case rows := <-done:
		if len(rows) != 2 {
			t.Fatalf("cyclic tree returned %d rows, want 2 bounded rows", len(rows))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ProjectTree hung on a cyclic parent graph, want a bounded walk")
	}
}
func TestTodo_SMB_STOCK_003_CyclicSnapshotRefused(t *testing.T) {
	reg := NewLocationRegistry(nil)
	op := stock003Context()
	stock003StoreBin(t, reg, op, "SITE-A", "cycsnap")
	snapshot, err := reg.Export()
	if err != nil {
		t.Fatal(err)
	}
	decoded := stock003SnapshotMap(t, snapshot)
	locs := decoded["locations"].(map[string]any)
	var storeID, binID string
	for id, raw := range locs {
		tip := raw.([]any)[len(raw.([]any))-1].(map[string]any)
		if _, hasParent := tip["parent_id"]; hasParent {
			binID = id
		} else {
			storeID = id
		}
	}
	locs[storeID].([]any)[0].(map[string]any)["parent_id"] = binID
	for _, raw := range decoded["idem"].(map[string]any) {
		entry := raw.(map[string]any)
		stored, ok := entry["Location"].(map[string]any)
		if ok && stored["id"] == storeID {
			stored["parent_id"] = binID
		}
	}
	if err := stock003ImportMap(t, NewLocationRegistry(nil), decoded); err == nil {
		t.Fatal("cyclic snapshot import succeeded, want refusal")
	}
	honest := NewLocationRegistry(nil)
	if err := honest.Import(snapshot); err != nil {
		t.Fatalf("clean snapshot import refused: %v", err)
	}
}

package inventory

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/stock"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var smbStock001At = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

func smbStock001Op() stock.OperationContext {
	return stock.OperationContext{TenantID: "tenant-a", CompanyID: "company-a", SiteID: "site-1", ActorID: "operator-1", Origin: stock.OriginHuman}
}

func smbStock001Catalog(t *testing.T) *stock.Catalog {
	t.Helper()
	return stock.NewCatalog(nil)
}

func smbStock001Item(t *testing.T, catalog *stock.Catalog, op stock.OperationContext, sku, unit string, scale int32, tracking stock.ItemTracking) stock.CatalogItem {
	t.Helper()
	item, err := catalog.CreateItem(context.Background(), op, stock.ItemDraft{
		SKU: sku, Name: sku + " item", BaseUnit: unit, Scale: scale, Tracking: tracking,
	}, "cat-"+sku)
	if err != nil {
		t.Fatalf("create item %s: %v", sku, err)
	}
	return item
}

type fakeAllocations struct {
	mu     sync.Mutex
	active map[string]values.Decimal
	cover  map[string]bool
}

func (f *fakeAllocations) ActiveQuantity(_ context.Context, _ stock.OperationContext, itemID, siteID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if qty, ok := f.active[itemID+"\x00"+siteID]; ok {
		return qty.String(), nil
	}
	return "0", nil
}

func (f *fakeAllocations) Cover(_ context.Context, _ stock.OperationContext, ref, _, _ string, _ values.Decimal) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cover[ref], nil
}

func (f *fakeAllocations) setActive(itemID, siteID, qty string, scale int32) {
	d, err := values.NewDecimal(qty, scale, values.RoundingExactRequired)
	if err != nil {
		panic(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.active == nil {
		f.active = make(map[string]values.Decimal)
	}
	f.active[itemID+"\x00"+siteID] = d
}

type fakeAssets struct {
	mu       sync.Mutex
	standing map[string]SerialStanding
}

func (f *fakeAssets) InspectSerial(_ context.Context, _ stock.OperationContext, namespace, serial string) (SerialStanding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.standing[namespace+"\x00"+serial]
	if !ok {
		return SerialStanding{}, nil
	}
	return st, nil
}

func smbStock001Journal(catalog *stock.Catalog, allocs *fakeAllocations, assets *fakeAssets, serialized bool) *Journal {
	caps := map[string]bool{}
	if serialized {
		caps[CapabilitySerialized] = true
	}
	return NewJournal(catalog, JournalConfig{Allocations: allocs, Assets: assets, Capabilities: caps})
}

func smbStock001Post(t *testing.T, journal *Journal, op stock.OperationContext, req MovementRequest, key string) Movement {
	t.Helper()
	movement, err := journal.Post(context.Background(), op, req, key)
	if err != nil {
		t.Fatalf("post %s %s: %v", req.Kind, req.QuantityText, err)
	}
	return movement
}

func smbStock001Balance(t *testing.T, journal *Journal, op stock.OperationContext, itemID, siteID string) BalanceReport {
	t.Helper()
	report, err := journal.ReadBalance(context.Background(), op, itemID, siteID)
	if err != nil {
		t.Fatalf("read balance: %v", err)
	}
	return report
}

func smbStock001Decimal(t *testing.T, text string, scale int32) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, scale, values.RoundingExactRequired)
	if err != nil {
		t.Fatalf("decimal %q: %v", text, err)
	}
	return d
}

// TestTodo_SMB_STOCK_001 proves the consumable posting core: receive 10 and
// issue 3 leaves usable 7 with two immutable movements, replays return the
// original operation, changed payloads conflict, and ATP equations gate
// unreserved issues without ever subtracting quarantine twice.
func TestTodo_SMB_STOCK_001(t *testing.T) {
	ctx := context.Background()
	op := smbStock001Op()
	catalog := smbStock001Catalog(t)
	item := smbStock001Item(t, catalog, op, "WIDGET", "each", 0, stock.TrackingEach)
	allocs := &fakeAllocations{}
	journal := smbStock001Journal(catalog, allocs, nil, false)

	received := smbStock001Post(t, journal, op, MovementRequest{
		ItemID: item.ItemID, ExpectedItemRevision: item.Revision, Kind: MovementReceive,
		QuantityText: "10", SiteID: "site-1", Reason: "PO-1 receipt", At: smbStock001At,
	}, "op-receive-1")
	issued := smbStock001Post(t, journal, op, MovementRequest{
		ItemID: item.ItemID, ExpectedItemRevision: item.Revision, Kind: MovementIssue,
		QuantityText: "3", SiteID: "site-1", Reason: "job-1 pick", At: smbStock001At,
	}, "op-issue-1")
	if received.Sequence == issued.Sequence || received.MovementID == issued.MovementID {
		t.Fatal("two movements share an identity")
	}
	report := smbStock001Balance(t, journal, op, item.ItemID, "site-1")
	if report.Usable.String() != "7" || report.OnHandTotal.String() != "7" {
		t.Fatalf("balance = usable %s total %s, want 7", report.Usable, report.OnHandTotal)
	}
	replay, err := journal.Post(ctx, op, MovementRequest{
		ItemID: item.ItemID, ExpectedItemRevision: item.Revision, Kind: MovementIssue,
		QuantityText: "3", SiteID: "site-1", Reason: "job-1 pick", At: smbStock001At,
	}, "op-issue-1")
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replay.MovementID != issued.MovementID || replay.Sequence != issued.Sequence {
		t.Fatal("replay did not return the original movement")
	}
	if _, err := journal.Post(ctx, op, MovementRequest{
		ItemID: item.ItemID, ExpectedItemRevision: item.Revision, Kind: MovementIssue,
		QuantityText: "4", SiteID: "site-1", Reason: "job-1 pick", At: smbStock001At,
	}, "op-issue-1"); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("changed-quantity replay err = %v, want ErrOperationConflict", err)
	}
	again := smbStock001Balance(t, journal, op, item.ItemID, "site-1")
	if again.Usable.String() != "7" {
		t.Fatalf("balance after replay = %s, want 7", again.Usable)
	}

	reservedItem := smbStock001Item(t, catalog, op, "WIDGET-R", "each", 0, stock.TrackingEach)
	smbStock001Post(t, journal, op, MovementRequest{
		ItemID: reservedItem.ItemID, ExpectedItemRevision: reservedItem.Revision, Kind: MovementReceive,
		QuantityText: "10", SiteID: "site-1", Reason: "receipt", At: smbStock001At,
	}, "op-reserve-recv")
	allocs.setActive(reservedItem.ItemID, "site-1", "8", 0)
	allocs.mu.Lock()
	allocs.cover = map[string]bool{"alloc-1": true}
	allocs.mu.Unlock()
	smbStock001Post(t, journal, op, MovementRequest{
		ItemID: reservedItem.ItemID, ExpectedItemRevision: reservedItem.Revision, Kind: MovementIssue,
		QuantityText: "3", SiteID: "site-1", Reason: "reserved pick",
		AllocationRef: "alloc-1", AllocationQtyText: "3", At: smbStock001At,
	}, "op-reserve-issue")
	if _, err := journal.Post(ctx, op, MovementRequest{
		ItemID: reservedItem.ItemID, ExpectedItemRevision: reservedItem.Revision, Kind: MovementIssue,
		QuantityText: "1", SiteID: "site-1", Reason: "ineligible pick",
		AllocationRef: "alloc-nope", AllocationQtyText: "1", At: smbStock001At,
	}, "op-reserve-bad"); !errors.Is(err, ErrAllocationInvalid) {
		t.Fatalf("ineligible allocation err = %v, want ErrAllocationInvalid", err)
	}
	allocs.setActive(item.ItemID, "site-1", "8", 0)
	smbStock001Post(t, journal, op, MovementRequest{
		ItemID: item.ItemID, ExpectedItemRevision: item.Revision, Kind: MovementReceive,
		QuantityText: "3", SiteID: "site-1", Reason: "top-up", At: smbStock001At,
	}, "op-receive-2")
	if _, err := journal.Post(ctx, op, MovementRequest{
		ItemID: item.ItemID, ExpectedItemRevision: item.Revision, Kind: MovementIssue,
		QuantityText: "3", SiteID: "site-1", Reason: "walk-in", At: smbStock001At,
	}, "op-issue-2"); !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("unreserved issue over ATP err = %v, want ErrInsufficientStock", err)
	}
	allocs.setActive(item.ItemID, "site-1", "3", 0)
	smbStock001Post(t, journal, op, MovementRequest{
		ItemID: item.ItemID, ExpectedItemRevision: item.Revision, Kind: MovementAdjust,
		QuantityText: "2", SiteID: "site-1", FromBucket: BucketUsable, ToBucket: BucketQuarantine,
		Reason: "QA hold", At: smbStock001At,
	}, "op-adjust-1")
	final := smbStock001Balance(t, journal, op, item.ItemID, "site-1")
	if final.OnHandTotal.String() != "10" || final.Available.String() != "5" || final.InTransit.String() != "0" {
		t.Fatalf("balance = total %s available %s transit %s, want 10/5/0",
			final.OnHandTotal, final.Available, final.InTransit)
	}
}

// TestTodo_SMB_STOCK_001_Integration proves restart recovery, compensating
// reversals, site transfers, and serialized batch atomicity: an exported
// journal restores identical balances with ordered provenance, a backdated
// reversal keeps append order, and one reused serial refuses the whole
// batch with zero writes.
func TestTodo_SMB_STOCK_001_Integration(t *testing.T) {
	ctx := context.Background()
	op := smbStock001Op()
	catalog := smbStock001Catalog(t)
	item := smbStock001Item(t, catalog, op, "GADGET", "each", 0, stock.TrackingEach)
	assets := &fakeAssets{standing: map[string]SerialStanding{
		"mfg\x00SN-1": {Mapped: true, Serviceable: true},
		"mfg\x00SN-2": {Mapped: true, Serviceable: true},
	}}
	journal := smbStock001Journal(catalog, &fakeAllocations{}, assets, true)

	smbStock001Post(t, journal, op, MovementRequest{
		ItemID: item.ItemID, ExpectedItemRevision: item.Revision, Kind: MovementReceive,
		QuantityText: "10", SiteID: "site-1", Reason: "receipt", At: smbStock001At,
	}, "i-recv")
	issued := smbStock001Post(t, journal, op, MovementRequest{
		ItemID: item.ItemID, ExpectedItemRevision: item.Revision, Kind: MovementIssue,
		QuantityText: "3", SiteID: "site-1", Reason: "pick", At: smbStock001At,
	}, "i-issue")
	reversed, err := journal.Post(ctx, op, MovementRequest{
		ItemID: item.ItemID, ExpectedItemRevision: item.Revision, Kind: MovementReversal,
		QuantityText: "3", SiteID: "site-1", Reason: "pick cancelled",
		OriginalMovementRef: issued.MovementID, EffectiveAt: smbStock001At.Add(-time.Hour), At: smbStock001At,
	}, "i-rev")
	if err != nil {
		t.Fatalf("reversal: %v", err)
	}
	if reversed.Sequence <= issued.Sequence {
		t.Fatal("backdated reversal did not append in recorded order")
	}
	if _, err := journal.Post(ctx, op, MovementRequest{
		ItemID: item.ItemID, ExpectedItemRevision: item.Revision, Kind: MovementReversal,
		QuantityText: "1", SiteID: "site-1", Reason: "double dip",
		OriginalMovementRef: issued.MovementID, At: smbStock001At,
	}, "i-rev2"); !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("superseded reversal err = %v, want ErrInsufficientStock", err)
	}
	smbStock001Post(t, journal, op, MovementRequest{
		ItemID: item.ItemID, ExpectedItemRevision: item.Revision, Kind: MovementTransfer,
		QuantityText: "4", FromSite: "site-1", ToSite: "site-2", Reason: "rebalance", At: smbStock001At,
	}, "i-xfer")
	from := smbStock001Balance(t, journal, op, item.ItemID, "site-1")
	to := smbStock001Balance(t, journal, op, item.ItemID, "site-2")
	if from.Usable.String() != "6" || to.Usable.String() != "4" {
		t.Fatalf("transfer balances = %s/%s, want 6/4", from.Usable, to.Usable)
	}

	snapshot, err := journal.Export()
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	restarted := smbStock001Journal(catalog, &fakeAllocations{}, assets, true)
	if err := restarted.Import(snapshot); err != nil {
		t.Fatalf("import: %v", err)
	}
	restored := smbStock001Balance(t, restarted, op, item.ItemID, "site-1")
	if restored.Usable.String() != "6" || restored.AsOfSequence != from.AsOfSequence {
		t.Fatalf("restored = %s seq %d, want 6 seq %d", restored.Usable, restored.AsOfSequence, from.AsOfSequence)
	}
	provenance, err := restarted.GetOperation(ctx, op, "i-issue")
	if err != nil {
		t.Fatalf("operation lookup: %v", err)
	}
	if provenance.MovementID != issued.MovementID {
		t.Fatal("operation lookup lost receipt provenance")
	}

	serialItem := smbStock001Item(t, catalog, op, "PUMP", "each", 0, stock.TrackingSerialized)
	if _, err := journal.Post(ctx, op, MovementRequest{
		ItemID: serialItem.ItemID, ExpectedItemRevision: serialItem.Revision, Kind: MovementReceive,
		QuantityText: "2", SiteID: "site-1", Reason: "asset intake",
		Serials: []SerialLine{{Namespace: "mfg", Serial: "SN-1"}, {Namespace: "mfg", Serial: " SN-1 "}},
		At:      smbStock001At,
	}, "i-serdup"); !errors.Is(err, ErrSerialConflict) {
		t.Fatalf("reused-serial batch err = %v, want ErrSerialConflict", err)
	}
	serialBalance := smbStock001Balance(t, journal, op, serialItem.ItemID, "site-1")
	if serialBalance.OnHandTotal.String() != "0" {
		t.Fatalf("refused batch left balance %s, want 0", serialBalance.OnHandTotal)
	}
	authorized := smbStock001Post(t, journal, op, MovementRequest{
		ItemID: serialItem.ItemID, ExpectedItemRevision: serialItem.Revision, Kind: MovementReceive,
		QuantityText: "2", SiteID: "site-1", Reason: "asset intake",
		Serials: []SerialLine{{Namespace: "mfg", Serial: "SN-1"}, {Namespace: "mfg", Serial: "SN-2"}},
		At:      smbStock001At,
	}, "i-serok")
	replayed, err := journal.Post(ctx, op, MovementRequest{
		ItemID: serialItem.ItemID, ExpectedItemRevision: serialItem.Revision, Kind: MovementReceive,
		QuantityText: "2", SiteID: "site-1", Reason: "asset intake",
		Serials: []SerialLine{{Namespace: "mfg", Serial: "SN-1"}, {Namespace: "mfg", Serial: "SN-2"}},
		At:      smbStock001At,
	}, "i-serok")
	if err != nil {
		t.Fatalf("asset-authorized replay: %v", err)
	}
	if replayed.MovementID != authorized.MovementID {
		t.Fatal("replay did not return the single authorized receipt")
	}
	serialAfter := smbStock001Balance(t, journal, op, serialItem.ItemID, "site-1")
	if serialAfter.Usable.String() != "2" {
		t.Fatalf("serialized balance = %s, want 2", serialAfter.Usable)
	}
}

// TestTodo_SMB_STOCK_001_Race proves concurrent issues serialize: two
// issues of 6 from 10 commit exactly one, and the survivor stays
// non-negative across a restart.
func TestTodo_SMB_STOCK_001_Race(t *testing.T) {
	op := smbStock001Op()
	catalog := smbStock001Catalog(t)
	item := smbStock001Item(t, catalog, op, "BOLT", "each", 0, stock.TrackingEach)
	journal := smbStock001Journal(catalog, &fakeAllocations{}, nil, false)

	smbStock001Post(t, journal, op, MovementRequest{
		ItemID: item.ItemID, ExpectedItemRevision: item.Revision, Kind: MovementReceive,
		QuantityText: "10", SiteID: "site-1", Reason: "receipt", At: smbStock001At,
	}, "r-recv")

	const racers = 2
	var wg sync.WaitGroup
	errs := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = journal.Post(context.Background(), op, MovementRequest{
				ItemID: item.ItemID, ExpectedItemRevision: item.Revision, Kind: MovementIssue,
				QuantityText: "6", SiteID: "site-1", Reason: "race pick", At: smbStock001At,
			}, fmt.Sprintf("r-issue-%d", i))
		}(i)
	}
	wg.Wait()
	wins, shorts := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrInsufficientStock):
			shorts++
		default:
			t.Fatalf("racer err = %v", err)
		}
	}
	if wins != 1 || shorts != 1 {
		t.Fatalf("race = %d wins %d short, want exactly one commit", wins, shorts)
	}
	report := smbStock001Balance(t, journal, op, item.ItemID, "site-1")
	if report.Usable.String() != "4" {
		t.Fatalf("raced balance = %s, want 4", report.Usable)
	}
	snapshot, err := journal.Export()
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	restarted := smbStock001Journal(catalog, &fakeAllocations{}, nil, false)
	if err := restarted.Import(snapshot); err != nil {
		t.Fatalf("import: %v", err)
	}
	after := smbStock001Balance(t, restarted, op, item.ItemID, "site-1")
	if after.Usable.Sign() < 0 || after.Usable.String() != "4" {
		t.Fatalf("post-restart balance = %s, want non-negative 4", after.Usable)
	}
}

// TestTodo_SMB_STOCK_001_Security proves tenant, company, state, and
// capability fences: tenant B reads and references nothing, cross-company
// transfers refuse, retired items reject postings, inexact serialized
// quantities refuse, and unmapped/assigned/out-of-service assets post
// zero movements.
func TestTodo_SMB_STOCK_001_Security(t *testing.T) {
	ctx := context.Background()
	op := smbStock001Op()
	catalog := smbStock001Catalog(t)
	item := smbStock001Item(t, catalog, op, "NUT", "each", 0, stock.TrackingEach)
	assets := &fakeAssets{standing: map[string]SerialStanding{
		"mfg\x00OK-1":       {Mapped: true, Serviceable: true},
		"mfg\x00BROKEN-1":   {Mapped: true, Serviceable: false},
		"mfg\x00ASSIGNED-1": {Mapped: true, Serviceable: true, Assigned: true},
	}}
	journal := smbStock001Journal(catalog, &fakeAllocations{}, assets, true)

	smbStock001Post(t, journal, op, MovementRequest{
		ItemID: item.ItemID, ExpectedItemRevision: item.Revision, Kind: MovementReceive,
		QuantityText: "10", SiteID: "site-1", Reason: "receipt", At: smbStock001At,
	}, "s-recv")
	foreign := stock.OperationContext{TenantID: "tenant-b", CompanyID: "company-a", SiteID: "site-1", ActorID: "operator-1", Origin: stock.OriginHuman}
	if _, err := journal.ReadBalance(ctx, foreign, item.ItemID, "site-1"); !errors.Is(err, ErrMovementNotFound) {
		t.Fatalf("foreign-tenant read err = %v, want ErrMovementNotFound", err)
	}
	if _, err := journal.Post(ctx, foreign, MovementRequest{
		ItemID: item.ItemID, ExpectedItemRevision: item.Revision, Kind: MovementIssue,
		QuantityText: "1", SiteID: "site-1", Reason: "theft", At: smbStock001At,
	}, "s-evil"); !errors.Is(err, ErrMovementNotFound) {
		t.Fatalf("foreign-tenant post err = %v, want ErrMovementNotFound", err)
	}
	if _, err := journal.Post(ctx, op, MovementRequest{
		ItemID: item.ItemID, ExpectedItemRevision: item.Revision, Kind: MovementTransfer,
		QuantityText: "1", FromSite: "site-1", ToSite: "site-9", ToCompany: "company-b",
		Reason: "intercompany", At: smbStock001At,
	}, "s-xco"); !errors.Is(err, ErrCrossCompany) {
		t.Fatalf("cross-company transfer err = %v, want ErrCrossCompany", err)
	}
	retired, err := catalog.RetireItem(ctx, op, item.ItemID, "s-retire")
	if err != nil {
		t.Fatalf("retire: %v", err)
	}
	if _, err := journal.Post(ctx, op, MovementRequest{
		ItemID: item.ItemID, ExpectedItemRevision: retired.Revision, Kind: MovementIssue,
		QuantityText: "1", SiteID: "site-1", Reason: "late pick", At: smbStock001At,
	}, "s-retired"); !errors.Is(err, ErrItemNotPostable) {
		t.Fatalf("retired-item post err = %v, want ErrItemNotPostable", err)
	}

	serialItem := smbStock001Item(t, catalog, op, "VALVE", "each", 0, stock.TrackingSerialized)
	if _, err := journal.Post(ctx, op, MovementRequest{
		ItemID: serialItem.ItemID, ExpectedItemRevision: serialItem.Revision, Kind: MovementReceive,
		QuantityText: "0.5", SiteID: "site-1", Reason: "half valve",
		Serials: []SerialLine{{Namespace: "mfg", Serial: "OK-1"}}, At: smbStock001At,
	}, "s-half"); !errors.Is(err, ErrInvalidUnit) {
		t.Fatalf("fractional serialized qty err = %v, want ErrInvalidUnit", err)
	}
	for _, tc := range []struct {
		name   string
		serial string
		key    string
	}{
		{"unmapped", "NOPE-1", "s-unmapped"},
		{"out-of-service", "BROKEN-1", "s-broken"},
		{"assigned", "ASSIGNED-1", "s-assigned"},
	} {
		if _, err := journal.Post(ctx, op, MovementRequest{
			ItemID: serialItem.ItemID, ExpectedItemRevision: serialItem.Revision, Kind: MovementReceive,
			QuantityText: "1", SiteID: "site-1", Reason: tc.name,
			Serials: []SerialLine{{Namespace: "mfg", Serial: tc.serial}}, At: smbStock001At,
		}, tc.key); !errors.Is(err, ErrSerialRefused) {
			t.Fatalf("%s asset err = %v, want ErrSerialRefused", tc.name, err)
		}
	}
	serialBalance := smbStock001Balance(t, journal, op, serialItem.ItemID, "site-1")
	if serialBalance.OnHandTotal.String() != "0" {
		t.Fatalf("refused serials left balance %s, want 0", serialBalance.OnHandTotal)
	}

	gated := smbStock001Journal(catalog, &fakeAllocations{}, assets, false)
	if _, err := gated.Post(ctx, op, MovementRequest{
		ItemID: serialItem.ItemID, ExpectedItemRevision: serialItem.Revision, Kind: MovementReceive,
		QuantityText: "1", SiteID: "site-1", Reason: "early asset",
		Serials: []SerialLine{{Namespace: "mfg", Serial: "OK-1"}}, At: smbStock001At,
	}, "s-gated"); !errors.Is(err, ErrSerialNotReady) {
		t.Fatalf("gated serialized post err = %v, want ErrSerialNotReady", err)
	}
	activeItem := smbStock001Item(t, catalog, op, "WASHER-S", "each", 0, stock.TrackingEach)
	smbStock001Post(t, gated, op, MovementRequest{
		ItemID: activeItem.ItemID, ExpectedItemRevision: activeItem.Revision, Kind: MovementReceive,
		QuantityText: "5", SiteID: "site-2", Reason: "consumable still works", At: smbStock001At,
	}, "s-consumable")
}

// TestTodo_SMB_STOCK_001_Recovery proves crash convergence and journal
// integrity: a lost response replays the recorded movement, tampered
// snapshots fail closed, and a shadowed rebuild reproduces live balances
// exactly.
func TestTodo_SMB_STOCK_001_Recovery(t *testing.T) {
	ctx := context.Background()
	op := smbStock001Op()
	catalog := smbStock001Catalog(t)
	item := smbStock001Item(t, catalog, op, "WASHER", "each", 0, stock.TrackingEach)
	journal := smbStock001Journal(catalog, &fakeAllocations{}, nil, false)

	journal.AfterCommit = func() error { return errStockSimulatedCrash }
	_, err := journal.Post(ctx, op, MovementRequest{
		ItemID: item.ItemID, ExpectedItemRevision: item.Revision, Kind: MovementReceive,
		QuantityText: "10", SiteID: "site-1", Reason: "receipt", At: smbStock001At,
	}, "v-recv")
	if !errors.Is(err, errStockSimulatedCrash) {
		t.Fatalf("crashing post err = %v, want the simulated crash", err)
	}
	journal.AfterCommit = nil
	replayed, err := journal.Post(ctx, op, MovementRequest{
		ItemID: item.ItemID, ExpectedItemRevision: item.Revision, Kind: MovementReceive,
		QuantityText: "10", SiteID: "site-1", Reason: "receipt", At: smbStock001At,
	}, "v-recv")
	if err != nil {
		t.Fatalf("replay after crash: %v", err)
	}
	report := smbStock001Balance(t, journal, op, item.ItemID, "site-1")
	if report.Usable.String() != "10" || replayed.Sequence != 1 {
		t.Fatalf("replayed = seq %d usable %s, want one receipt of 10", replayed.Sequence, report.Usable)
	}
	if err := journal.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	after := smbStock001Balance(t, journal, op, item.ItemID, "site-1")
	if after.Usable.String() != "10" || after.AsOfSequence != report.AsOfSequence {
		t.Fatal("rebuild diverged from live balances")
	}
	if err := journal.Import([]byte(`{"sequence": 9, "movements": [{"sequence": 3}]}`)); err == nil {
		t.Fatal("tampered snapshot imported, want refusal")
	}

	// Conservation property: fixed-seed random postings never break the
	// bucket equation and always rebuild identically.
	rng := rand.New(rand.NewSource(54321))
	prop := smbStock001Journal(catalog, &fakeAllocations{}, nil, false)
	for i := 0; i < 200; i++ {
		kind := MovementReceive
		if i%3 == 2 {
			kind = MovementIssue
		}
		qty := fmt.Sprintf("%d", 1+rng.Intn(5))
		_, _ = prop.Post(ctx, op, MovementRequest{
			ItemID: item.ItemID, ExpectedItemRevision: item.Revision, Kind: kind,
			QuantityText: qty, SiteID: "site-1", Reason: "property", At: smbStock001At,
		}, fmt.Sprintf("p-%d", i))
	}
	conserved := smbStock001Balance(t, prop, op, item.ItemID, "site-1")
	sum, err := conserved.Usable.Add(conserved.Quarantine)
	if err != nil {
		t.Fatal(err)
	}
	sum, err = sum.Add(conserved.InTransit)
	if err != nil {
		t.Fatal(err)
	}
	sum, err = sum.Add(conserved.Lost)
	if err != nil {
		t.Fatal(err)
	}
	sum, err = sum.Add(conserved.Disposed)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Cmp(conserved.Total()) != 0 {
		t.Fatalf("bucket sum %s != total %s", sum, conserved.Total())
	}
	if conserved.Usable.Sign() < 0 {
		t.Fatalf("usable went negative: %s", conserved.Usable)
	}
	if err := prop.Rebuild(); err != nil {
		t.Fatalf("property rebuild: %v", err)
	}
}

// TestTodo_SMB_STOCK_001_Refusals pins the UI-safe refusal vocabulary the
// movement surfaces render, and proves movement lookup hides foreign
// records.
func TestTodo_SMB_STOCK_001_Refusals(t *testing.T) {
	op := smbStock001Op()
	catalog := smbStock001Catalog(t)
	item := smbStock001Item(t, catalog, op, "REFUSAL", "each", 0, stock.TrackingEach)
	journal := smbStock001Journal(catalog, &fakeAllocations{}, nil, false)
	posted := smbStock001Post(t, journal, op, MovementRequest{
		ItemID: item.ItemID, ExpectedItemRevision: item.Revision, Kind: MovementReceive,
		QuantityText: "1", SiteID: "site-1", Reason: "receipt", At: smbStock001At,
	}, "f-recv")
	loaded, err := journal.GetMovement(op, posted.MovementID)
	if err != nil || loaded.OperationID != "f-recv" {
		t.Fatalf("get movement = %+v err %v, want the posted receipt", loaded, err)
	}
	foreign := stock.OperationContext{TenantID: "tenant-b", CompanyID: "company-a", SiteID: "site-1", ActorID: "operator-1", Origin: stock.OriginHuman}
	if _, err := journal.GetMovement(foreign, posted.MovementID); !errors.Is(err, ErrMovementNotFound) {
		t.Fatalf("foreign lookup err = %v, want ErrMovementNotFound", err)
	}
	titles := map[error]string{
		ErrInsufficientStock: "Not enough stock",
		ErrStaleItemRevision: "refetch and retry",
		ErrOperationConflict: "reused for another movement",
		ErrInvalidUnit:       "not exact in the item unit",
		ErrSerialNotReady:    "not enabled yet",
		ErrSerialRefused:     "not available",
		ErrSerialConflict:    "already recorded",
		ErrItemNotPostable:   "does not accept postings",
		ErrCrossCompany:      "never moves across companies",
		ErrAllocationInvalid: "does not cover",
		ErrMovementNotFound:  "not available",
		errors.New("other"):  "was refused",
	}
	for refusal, want := range titles {
		if got := MovementRefusalTitle(refusal); !strings.Contains(got, want) {
			t.Fatalf("title(%v) = %q, want %q", refusal, got, want)
		}
	}
}

var errStockSimulatedCrash = errors.New("inventory: simulated stock crash")

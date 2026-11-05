package stock

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func stock008Context() OperationContext {
	return OperationContext{TenantID: "tenant-a", CompanyID: "company-a", ActorID: "operator-1", Origin: "human"}
}

func stock008Catalog(t *testing.T) *Catalog {
	t.Helper()
	ctx := context.Background()
	op := stock008Context()
	catalog := NewCatalog(nil)
	for _, draft := range []ItemDraft{
		{SKU: "SERUM-10", Name: "Serum vials", BaseUnit: "each", Scale: 0, Tracking: TrackingEach},
		{SKU: "PUMP-S1", Name: "Serialized pump", BaseUnit: "each", Scale: 0, Tracking: TrackingSerialized},
	} {
		if _, err := catalog.CreateItem(ctx, op, draft, "seed-"+draft.SKU); err != nil {
			t.Fatal(err)
		}
	}
	return catalog
}

func stock008ItemID(t *testing.T, catalog *Catalog, sku string) string {
	t.Helper()
	items := catalog.ListItems(stock008Context(), ItemFilter{Search: sku})
	if len(items) != 1 {
		t.Fatalf("seed lookup %q = %d items", sku, len(items))
	}
	return items[0].ItemID
}

type stock008Allocations struct {
	mu    sync.Mutex
	opens map[string][]OpenAllocation
}

func (f *stock008Allocations) OpenReservations(context.Context, string, string, string) (int, error) {
	return 0, nil
}

func (f *stock008Allocations) OpenAllocations(_ context.Context, _, _, itemID string) ([]OpenAllocation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]OpenAllocation(nil), f.opens[itemID]...), nil
}

func (f *stock008Allocations) set(itemID string, allocs []OpenAllocation) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.opens == nil {
		f.opens = make(map[string][]OpenAllocation)
	}
	f.opens[itemID] = allocs
}

func stock008LotInput(itemID string) RegisterLotInput {
	return RegisterLotInput{
		ItemID: itemID, SiteID: "site-1", SupplierLot: "SUP-77",
		ReceivedFrom: "supplier-acme", ReceivedDoc: "recv-1",
		ReceivedAt:     time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC),
		Expiry:         ExpiryPolicy{LocalDate: "2026-12-01", Timezone: "America/New_York", Rule: ExpiryNextLocalDayStart},
		PolicyRevision: "exp-v3", Scale: 0, OnHand: "10",
	}
}

// TestTodo_SMB_STOCK_008 proves tracked-stock eligibility: FEFO picks the
// earliest eligible lot, quarantine/recall/expiry block promising and issue,
// serialized assets refuse generic commands without owner binding, and recall
// math counts every unit once.
func TestTodo_SMB_STOCK_008(t *testing.T) {
	ctx := context.Background()
	op := stock008Context()
	catalog := stock008Catalog(t)
	allocs := &stock008Allocations{}
	registry := NewLotRegistry(catalog, allocs)
	itemID := stock008ItemID(t, catalog, "SERUM-10")

	early := stock008LotInput(itemID)
	early.SupplierLot = "SUP-EARLY"
	early.Expiry = ExpiryPolicy{LocalDate: "2026-11-01", Timezone: "America/New_York", Rule: ExpiryNextLocalDayStart}
	earlyLot, err := registry.RegisterLot(ctx, op, early, "lot-early")
	if err != nil {
		t.Fatal(err)
	}
	late := stock008LotInput(itemID)
	late.SupplierLot = "SUP-LATE"
	late.OnHand = "4"
	lateLot, err := registry.RegisterLot(ctx, op, late, "lot-late")
	if err != nil {
		t.Fatal(err)
	}
	held, err := registry.HoldLot(ctx, op, lateLot.LotID, "suspect temperature excursion", "", "hold-late")
	if err != nil || held.State != LotQuarantined {
		t.Fatalf("hold = %+v, err=%v", held, err)
	}

	asOf := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	preview, err := registry.PreviewEligibility(op, itemID, "12", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Picks) != 1 || preview.Picks[0].LotID != earlyLot.LotID || preview.Picks[0].Take != "10" {
		t.Fatalf("FEFO picks = %+v, want earliest lot only", preview.Picks)
	}
	if preview.Shortfall != "2" {
		t.Fatalf("shortfall = %q, want 2 (quarantined 4 excluded, not promised)", preview.Shortfall)
	}
	excluded := false
	for _, x := range preview.Excluded {
		if x.LotID == lateLot.LotID && strings.Contains(x.Reason, "QUARANTINED") {
			excluded = true
		}
	}
	if !excluded {
		t.Fatalf("quarantined lot missing from exclusions: %+v", preview.Excluded)
	}

	if _, err := registry.IssueFromLot(ctx, op, lateLot.LotID, "1", "move-1", asOf, "issue-held"); !errors.Is(err, ErrLotBlocked) {
		t.Fatalf("issue from quarantined lot err=%v, want blocked", err)
	}
	issued, err := registry.IssueFromLot(ctx, op, earlyLot.LotID, "2", "move-2", asOf, "issue-early")
	if err != nil {
		t.Fatal(err)
	}
	if issued.OnHand != "8" {
		t.Fatalf("on hand after issue = %q, want 8", issued.OnHand)
	}

	allocs.set(itemID, []OpenAllocation{
		{AllocationID: "alloc-1", LotID: earlyLot.LotID, Qty: "5", HolderRef: "job-101"},
		{AllocationID: "alloc-2", Qty: "3", HolderRef: "job-102"},
	})
	report, err := registry.RecallItem(ctx, op, itemID, "supplier flagged contamination", "lab-report-9", "recall-1")
	if err != nil {
		t.Fatal(err)
	}
	if report.Usable != "0" || report.Quarantined != "12" || report.ActiveDemand != "8" || report.Available != "0" || report.Shortage != "8" || report.BlockedQty != "8" {
		t.Fatalf("recall report = %+v, want usable0/quarantine12/demand8/available0/shortage8/blocked8", report)
	}
	if len(report.BlockedAllocations) != 2 {
		t.Fatalf("blocked allocations = %v, want both allocations named", report.BlockedAllocations)
	}
	after, err := registry.PreviewEligibility(op, itemID, "1", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Picks) != 0 || after.Shortfall != "1" {
		t.Fatalf("post-recall preview = %+v, want nothing eligible", after)
	}

	pumpID := stock008ItemID(t, catalog, "PUMP-S1")
	unit, err := registry.RegisterSerial(ctx, op, RegisterSerialInput{
		Namespace: "PUMP", Serial: "  Gus-0007 ", AssetRef: "asset-pump-7", ItemID: pumpID, SiteID: "site-1",
	}, "serial-1")
	if err != nil {
		t.Fatal(err)
	}
	if unit.Serial != "Gus-0007" || unit.State != SerialAvailable {
		t.Fatalf("registered serial = %+v, want trimmed case-preserved AVAILABLE", unit)
	}
	assigned, err := registry.PostSerialCommand(ctx, op, SerialCommand{
		Kind: SerialIssue, Namespace: "PUMP", Serial: "Gus-0007", Reason: "dispatch to job-101",
		Binding: &AssetBinding{AssetID: "asset-pump-7", CustodyRev: "custody-1", ServiceabilityRev: "svc-1", MovementOp: "move-s1"},
	}, "serial-issue-1")
	if err != nil || assigned.State != SerialAssigned {
		t.Fatalf("bound issue = %+v, err=%v", assigned, err)
	}
	before := assigned
	if _, err := registry.PostSerialCommand(ctx, op, SerialCommand{Kind: SerialAdjust, Namespace: "PUMP", Serial: "Gus-0007", Reason: "generic adjust"}, "serial-generic"); !errors.Is(err, ErrAssetBindingRequired) {
		t.Fatalf("generic adjust on assigned asset err=%v, want binding refusal", err)
	}
	current, err := registry.SerialTrace(op, "PUMP", "Gus-0007")
	if err != nil {
		t.Fatal(err)
	}
	if current.Unit.State != before.State || len(current.Events) != 2 {
		t.Fatalf("refused command moved the asset: %+v", current.Unit)
	}
	replay, err := registry.PostSerialCommand(ctx, op, SerialCommand{
		Kind: SerialIssue, Namespace: "PUMP", Serial: "Gus-0007", Reason: "dispatch to job-101",
		Binding: &AssetBinding{AssetID: "asset-pump-7", CustodyRev: "custody-1", ServiceabilityRev: "svc-1", MovementOp: "move-s1"},
	}, "serial-issue-1")
	if err != nil || replay.State != SerialAssigned {
		t.Fatalf("authorized replay = %+v, err=%v", replay, err)
	}
	afterReplay, err := registry.SerialTrace(op, "PUMP", "Gus-0007")
	if err != nil {
		t.Fatal(err)
	}
	if len(afterReplay.Events) != 2 {
		t.Fatalf("replay projected %d transitions, want exactly one", len(afterReplay.Events))
	}
	returned, err := registry.PostSerialCommand(ctx, op, SerialCommand{
		Kind: SerialReturn, Namespace: "PUMP", Serial: "Gus-0007", Reason: "job-101 complete",
		Binding: &AssetBinding{AssetID: "asset-pump-7", CustodyRev: "custody-1", ServiceabilityRev: "svc-1", MovementOp: "move-s2"},
	}, "serial-return-1")
	if err != nil || returned.State != SerialAvailable {
		t.Fatalf("bound return = %+v, err=%v", returned, err)
	}
	parked, err := registry.PostSerialCommand(ctx, op, SerialCommand{
		Kind: SerialAdjust, Namespace: "PUMP", Serial: "Gus-0007", Reason: "failed service check", MarkOutOfService: true,
		Binding: &AssetBinding{AssetID: "asset-pump-7", CustodyRev: "custody-1", ServiceabilityRev: "svc-2", MovementOp: "move-s3"},
	}, "serial-park-1")
	if err != nil || parked.State != SerialOutOfService {
		t.Fatalf("park out of service = %+v, err=%v", parked, err)
	}
	if _, err := registry.PostSerialCommand(ctx, op, SerialCommand{Kind: SerialIssue, Namespace: "PUMP", Serial: "Gus-0007", Reason: "generic issue"}, "serial-generic-oos"); !errors.Is(err, ErrAssetBindingRequired) {
		t.Fatalf("generic issue on out-of-service asset err=%v, want binding refusal", err)
	}
	unparked, err := registry.PostSerialCommand(ctx, op, SerialCommand{
		Kind: SerialAdjust, Namespace: "PUMP", Serial: "Gus-0007", Reason: "verified service release",
		Binding: &AssetBinding{AssetID: "asset-pump-7", CustodyRev: "custody-1", ServiceabilityRev: "svc-3", MovementOp: "move-s4"},
	}, "serial-unpark-1")
	if err != nil || unparked.State != SerialAvailable {
		t.Fatalf("return to service = %+v, err=%v", unparked, err)
	}
	moved, err := registry.PostSerialCommand(ctx, op, SerialCommand{
		Kind: SerialTransfer, Namespace: "PUMP", Serial: "Gus-0007", Reason: "relocate to van", ToSite: "site-2",
		Binding: &AssetBinding{AssetID: "asset-pump-7", CustodyRev: "custody-2", ServiceabilityRev: "svc-3", MovementOp: "move-s5"},
	}, "serial-transfer-1")
	if err != nil || moved.SiteID != "site-2" {
		t.Fatalf("bound transfer = %+v, err=%v", moved, err)
	}
	lost, err := registry.PostSerialCommand(ctx, op, SerialCommand{
		Kind: SerialLoss, Namespace: "PUMP", Serial: "Gus-0007", Reason: "missing after job-102",
		Binding: &AssetBinding{AssetID: "asset-pump-7", CustodyRev: "custody-2", ServiceabilityRev: "svc-3", MovementOp: "move-s6"},
	}, "serial-loss-1")
	if err != nil || lost.State != SerialLost {
		t.Fatalf("bound loss = %+v, err=%v", lost, err)
	}
	if _, err := registry.PostSerialCommand(ctx, op, SerialCommand{
		Kind: SerialReturn, Namespace: "PUMP", Serial: "Gus-0007", Reason: "return lost unit",
		Binding: &AssetBinding{AssetID: "asset-pump-7", CustodyRev: "custody-2", ServiceabilityRev: "svc-3", MovementOp: "move-s7"},
	}, "serial-lost-cmd"); !errors.Is(err, ErrSerialUnavailable) {
		t.Fatalf("command on lost unit err=%v, want unavailable", err)
	}
}

// TestTodo_SMB_STOCK_008_Integration proves idempotent lot writes, pinned
// expiry cutoffs across timezones and DST, legacy serial mapping and the
// physical-serial uniqueness that survives item reclassification.
func TestTodo_SMB_STOCK_008_Integration(t *testing.T) {
	ctx := context.Background()
	op := stock008Context()
	catalog := stock008Catalog(t)
	registry := NewLotRegistry(catalog, &stock008Allocations{})
	itemID := stock008ItemID(t, catalog, "SERUM-10")

	first, err := registry.RegisterLot(ctx, op, stock008LotInput(itemID), "dup-key")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := registry.RegisterLot(ctx, op, stock008LotInput(itemID), "dup-key")
	if err != nil || replay.LotID != first.LotID {
		t.Fatalf("exact lot replay = %+v, err=%v", replay, err)
	}
	changed := stock008LotInput(itemID)
	changed.OnHand = "99"
	if _, err := registry.RegisterLot(ctx, op, changed, "dup-key"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed-payload replay err=%v, want idempotency conflict", err)
	}
	if _, err := registry.HoldLot(ctx, op, first.LotID, "", "", "hold-noreason"); !errors.Is(err, ErrStockValidation) {
		t.Fatalf("reasonless hold err=%v, want validation refusal", err)
	}
	if _, err := registry.ReleaseLot(ctx, op, first.LotID, "reviewer-2", "", time.Now().UTC(), "release-noevidence"); !errors.Is(err, ErrStockValidation) {
		t.Fatalf("evidenceless release err=%v, want validation refusal", err)
	}

	dst := stock008LotInput(itemID)
	dst.SupplierLot = "SUP-DST"
	dst.Expiry = ExpiryPolicy{LocalDate: "2026-03-08", Timezone: "America/New_York", Rule: ExpiryNextLocalDayStart}
	dstLot, err := registry.RegisterLot(ctx, op, dst, "lot-dst")
	if err != nil {
		t.Fatal(err)
	}
	nyc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	cutoff := time.Date(2026, 3, 9, 0, 0, 0, 0, nyc)
	justBefore := cutoff.Add(-time.Nanosecond)
	if ok, err := registry.LotExpired(op, dstLot.LotID, justBefore); err != nil || ok {
		t.Fatalf("lot just before DST cutoff expired=%v err=%v, want eligible", ok, err)
	}
	if ok, err := registry.LotExpired(op, dstLot.LotID, cutoff); err != nil || !ok {
		t.Fatalf("lot at DST cutoff expired=%v err=%v, want expired", ok, err)
	}
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	sameInstantTokyo := cutoff.In(tokyo)
	if ok, err := registry.LotExpired(op, dstLot.LotID, sameInstantTokyo); err != nil || !ok {
		t.Fatalf("cutoff moved under another timezone: expired=%v err=%v", ok, err)
	}
	trace, err := registry.TraceLot(op, dstLot.LotID)
	if err != nil {
		t.Fatal(err)
	}
	if !trace.Cutoff.Equal(cutoff) {
		t.Fatalf("trace cutoff = %v, want pinned %v", trace.Cutoff, cutoff)
	}

	if _, err := registry.HoldLot(ctx, op, first.LotID, "pre-expiry hold", "", "hold-first"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ReleaseLot(ctx, op, first.LotID, "reviewer-2", "recheck-1", cutoff.AddDate(0, 0, 300), "release-expired"); !errors.Is(err, ErrLotExpired) {
		t.Fatalf("release of an expired lot err=%v, want expiry refusal without silent revival", err)
	}

	legacy, err := registry.RegisterSerial(ctx, op, RegisterSerialInput{Serial: "legacy-42", ItemID: itemID, SiteID: "site-1"}, "serial-legacy")
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Namespace != SerialNamespaceLegacy || legacy.State != SerialPendingReview {
		t.Fatalf("legacy serial = %+v, want LEGACY pending review", legacy)
	}
	if _, err := registry.PostSerialCommand(ctx, op, SerialCommand{Kind: SerialIssue, Namespace: SerialNamespaceLegacy, Serial: "legacy-42", Reason: "x"}, "serial-legacy-issue"); !errors.Is(err, ErrSerialUnavailable) {
		t.Fatalf("pending serial issue err=%v, want unavailable", err)
	}
	mapped, err := registry.MapLegacySerial(ctx, op, SerialNamespaceLegacy, "legacy-42", "asset-legacy-42", "map-legacy")
	if err != nil || mapped.State != SerialAvailable || mapped.AssetRef != "asset-legacy-42" {
		t.Fatalf("legacy mapping = %+v, err=%v", mapped, err)
	}

	pumpID := stock008ItemID(t, catalog, "PUMP-S1")
	if _, err := registry.RegisterSerial(ctx, op, RegisterSerialInput{Namespace: "PUMP", Serial: "DUP-1", AssetRef: "asset-a", ItemID: itemID}, "serial-dup-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.RegisterSerial(ctx, op, RegisterSerialInput{Namespace: "PUMP", Serial: "DUP-1", AssetRef: "asset-a", ItemID: pumpID}, "serial-dup-b"); !errors.Is(err, ErrDuplicateSerial) {
		t.Fatalf("reclassified physical serial err=%v, want duplicate refusal", err)
	}

	disposed, err := registry.DisposeLot(ctx, op, dstLot.LotID, "move-dispose-1", "expired at vendor return", "dispose-1")
	if err != nil || disposed.State != LotDisposed || disposed.OnHand != "0" {
		t.Fatalf("dispose = %+v, err=%v", disposed, err)
	}
	if _, err := registry.IssueFromLot(ctx, op, dstLot.LotID, "1", "move-x", cutoff, "issue-disposed"); !errors.Is(err, ErrLotBlocked) {
		t.Fatalf("issue from disposed lot err=%v, want blocked", err)
	}
}

// TestTodo_SMB_STOCK_008_Race proves a hold racing issues serializes: every
// issue either predates the hold with a traceable receipt or is refused, and
// duplicate serials cannot race into the registry.
func TestTodo_SMB_STOCK_008_Race(t *testing.T) {
	ctx := context.Background()
	op := stock008Context()
	catalog := stock008Catalog(t)
	registry := NewLotRegistry(catalog, &stock008Allocations{})
	itemID := stock008ItemID(t, catalog, "SERUM-10")

	input := stock008LotInput(itemID)
	input.OnHand = "10"
	lot, err := registry.RegisterLot(ctx, op, input, "race-lot")
	if err != nil {
		t.Fatal(err)
	}
	asOf := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

	const issuers = 8
	var wg sync.WaitGroup
	issueErrs := make([]error, issuers)
	for i := range issueErrs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := registry.IssueFromLot(ctx, op, lot.LotID, "1", "race-move", asOf, "race-issue-"+stock008Itoa(i))
			issueErrs[i] = err
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = registry.HoldLot(ctx, op, lot.LotID, "race hold", "", "race-hold")
	}()
	wg.Wait()

	trace, err := registry.TraceLot(op, lot.LotID)
	if err != nil {
		t.Fatal(err)
	}
	heldAt := -1
	for i, event := range trace.Events {
		if event.Kind == LotEventHeld {
			heldAt = i
		}
		if event.Kind == LotEventIssued && heldAt >= 0 && i > heldAt {
			t.Fatalf("issue event %d posts after hold event %d", i, heldAt)
		}
	}
	issued := 0
	for _, event := range trace.Events {
		if event.Kind == LotEventIssued {
			issued++
		}
	}
	refused := 0
	for _, err := range issueErrs {
		if errors.Is(err, ErrLotBlocked) {
			refused++
		} else if err != nil {
			t.Fatalf("race issue err=%v, want nil or blocked", err)
		}
	}
	if issued+refused != issuers {
		t.Fatalf("issued %d + refused %d != %d racers", issued, refused, issuers)
	}
	final, err := registry.TraceLot(op, lot.LotID)
	if err != nil {
		t.Fatal(err)
	}
	want := 10 - issued
	if final.Lot.OnHand != stock008Itoa(want) {
		t.Fatalf("on hand = %q after %d raced issues, want %d", final.Lot.OnHand, issued, want)
	}

	var serialWG sync.WaitGroup
	serialErrs := make([]error, issuers)
	for i := range serialErrs {
		serialWG.Add(1)
		go func(i int) {
			defer serialWG.Done()
			_, err := registry.RegisterSerial(ctx, op, RegisterSerialInput{Namespace: "PUMP", Serial: "RACE-S1", AssetRef: "asset-race", ItemID: itemID}, "race-serial-"+stock008Itoa(i))
			serialErrs[i] = err
		}(i)
	}
	serialWG.Wait()
	winners := 0
	for _, err := range serialErrs {
		if err == nil {
			winners++
		} else if !errors.Is(err, ErrDuplicateSerial) {
			t.Fatalf("serial race err=%v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("%d serial race winners, want exactly 1", winners)
	}
}

// TestTodo_SMB_STOCK_008_Security proves tenant/company/site isolation on
// lots and serials, and that recall and trace outputs carry counts and IDs
// without worker or customer names.
func TestTodo_SMB_STOCK_008_Security(t *testing.T) {
	ctx := context.Background()
	op := stock008Context()
	catalog := stock008Catalog(t)
	allocs := &stock008Allocations{}
	registry := NewLotRegistry(catalog, allocs)
	itemID := stock008ItemID(t, catalog, "SERUM-10")

	lot, err := registry.RegisterLot(ctx, op, stock008LotInput(itemID), "sec-lot")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.RegisterSerial(ctx, op, RegisterSerialInput{Namespace: "PUMP", Serial: "SEC-1", AssetRef: "asset-sec", ItemID: itemID, SiteID: "site-1"}, "sec-serial"); err != nil {
		t.Fatal(err)
	}

	foreignTenant := op
	foreignTenant.TenantID = "tenant-evil"
	if _, err := registry.TraceLot(foreignTenant, lot.LotID); !errors.Is(err, ErrItemNotFound) {
		t.Fatalf("cross-tenant trace err=%v, want not-found", err)
	}
	if _, err := registry.SerialTrace(foreignTenant, "PUMP", "SEC-1"); !errors.Is(err, ErrSerialNotFound) {
		t.Fatalf("cross-tenant serial trace err=%v, want not-found", err)
	}
	if _, err := registry.PreviewEligibility(foreignTenant, itemID, "1", time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)); !errors.Is(err, ErrItemNotFound) {
		t.Fatalf("cross-tenant eligibility err=%v, want not-found", err)
	}

	foreignCompany := op
	foreignCompany.CompanyID = "company-evil"
	if _, err := registry.TraceLot(foreignCompany, lot.LotID); !errors.Is(err, ErrItemNotFound) {
		t.Fatalf("cross-company trace err=%v, want not-found", err)
	}

	deniedSite := op
	deniedSite.SiteID = "site-9"
	if _, err := registry.TraceLot(deniedSite, lot.LotID); !errors.Is(err, ErrItemNotFound) {
		t.Fatalf("denied-site trace err=%v, want not-found without disclosure", err)
	}
	if _, err := registry.SerialTrace(deniedSite, "PUMP", "SEC-1"); !errors.Is(err, ErrSerialNotFound) {
		t.Fatalf("denied-site serial trace err=%v, want not-found", err)
	}

	badOrigin := op
	badOrigin.Origin = "scan-beep"
	if _, err := registry.RegisterLot(ctx, badOrigin, stock008LotInput(itemID), "sec-bad"); !errors.Is(err, ErrStockContext) {
		t.Fatalf("bad origin err=%v, want context refusal", err)
	}

	allocs.set(itemID, []OpenAllocation{{AllocationID: "alloc-9", LotID: lot.LotID, Qty: "2", HolderRef: "job-holder-zephyr-9"}})
	report, err := registry.RecallItem(ctx, op, itemID, "quality escape", "evidence-1", "sec-recall")
	if err != nil {
		t.Fatal(err)
	}
	reportJSON, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(reportJSON), "zephyr") {
		t.Fatalf("recall report leaks holder identity: %s", reportJSON)
	}
	traced, err := registry.TraceLot(op, lot.LotID)
	if err != nil {
		t.Fatal(err)
	}
	traceJSON, err := json.Marshal(traced)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(traceJSON), "zephyr") {
		t.Fatalf("lot trace leaks holder identity: %s", traceJSON)
	}
}

// TestTodo_SMB_STOCK_008_Recovery proves a snapshot round-trip preserves
// lots, serials, recall obligations and trace order across restart.
func TestTodo_SMB_STOCK_008_Recovery(t *testing.T) {
	ctx := context.Background()
	op := stock008Context()
	catalog := stock008Catalog(t)
	allocs := &stock008Allocations{}
	registry := NewLotRegistry(catalog, allocs)
	itemID := stock008ItemID(t, catalog, "SERUM-10")

	lot, err := registry.RegisterLot(ctx, op, stock008LotInput(itemID), "rec-lot")
	if err != nil {
		t.Fatal(err)
	}
	allocs.set(itemID, []OpenAllocation{{AllocationID: "alloc-1", LotID: lot.LotID, Qty: "8", HolderRef: "job-1"}})
	before, err := registry.RecallItem(ctx, op, itemID, "contamination", "lab-1", "rec-recall")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.RegisterSerial(ctx, op, RegisterSerialInput{Namespace: "PUMP", Serial: "REC-1", AssetRef: "asset-rec", ItemID: itemID}, "rec-serial"); err != nil {
		t.Fatal(err)
	}

	bytes, err := registry.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewLotRegistry(catalog, allocs)
	if err := restarted.Restore(bytes); err != nil {
		t.Fatal(err)
	}
	after, err := restarted.TraceLot(op, lot.LotID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Events) == 0 || after.Lot.State != LotRecalled {
		t.Fatalf("post-restart trace = %+v, want recalled lot with events", after.Lot)
	}
	if len(after.Recalls) != 1 || after.Recalls[0].RecallID != before.RecallID || after.Recalls[0].BlockedQty != "8" || after.Recalls[0].Shortage != "8" {
		t.Fatalf("post-restart obligations = %+v, want identical recall record", after.Recalls)
	}
	if _, err := restarted.RegisterSerial(ctx, op, RegisterSerialInput{Namespace: "PUMP", Serial: "REC-1", AssetRef: "asset-rec", ItemID: itemID}, "rec-clash"); !errors.Is(err, ErrDuplicateSerial) {
		t.Fatalf("post-restart duplicate serial err=%v, want duplicate refusal", err)
	}
	if err := restarted.Restore(bytes); err == nil {
		t.Fatal("restore into a populated registry succeeded, want refusal")
	}
	if err := NewLotRegistry(catalog, allocs).Restore([]byte("{broken")); err == nil {
		t.Fatal("restore of corrupt bytes succeeded, want refusal")
	}
}

func stock008Itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}

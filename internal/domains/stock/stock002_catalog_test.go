package stock

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func stock002Context() OperationContext {
	return OperationContext{TenantID: "tenant-a", CompanyID: "company-a", ActorID: "operator-1", Origin: "human"}
}

func stock002BoxDraft() ItemDraft {
	return ItemDraft{
		SKU: "WIDGET-12", Name: "Widget 12-pack box", BaseUnit: "each", Scale: 0, Tracking: TrackingEach,
		Conversions: []PackConversion{{PackUnit: "box", Numerator: 12, Denominator: 1}},
		Aliases:     []BarcodeAlias{{Symbology: "EAN-13", Code: "000123"}},
	}
}

type stock002Reservations struct {
	mu     sync.Mutex
	byItem map[string]int
}

func (f *stock002Reservations) OpenReservations(_ context.Context, _, _, itemID string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byItem[itemID], nil
}

func (f *stock002Reservations) set(itemID string, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.byItem == nil {
		f.byItem = make(map[string]int)
	}
	f.byItem[itemID] = n
}

// TestTodo_SMB_STOCK_002 proves the catalog acceptance core: leading-zero
// barcodes resolve deterministically, pack conversions preview exact base
// quantities, fractional serialized eaches and cross-dimension conversions
// fail before posting, revisions never rewrite history, and retirement is
// blocked by open reservations and hidden from pickers.
func TestTodo_SMB_STOCK_002(t *testing.T) {
	ctx := context.Background()
	op := stock002Context()
	reservations := &stock002Reservations{}
	catalog := NewCatalog(reservations)

	created, err := catalog.CreateItem(ctx, op, stock002BoxDraft(), "create-box")
	if err != nil {
		t.Fatal(err)
	}
	if created.Revision != 1 || created.Status != ItemStatusActive || created.ItemID == "" {
		t.Fatalf("created item lost identity fields: %+v", created)
	}
	if created.Aliases[0].Code != "000123" {
		t.Fatalf("alias code = %q, want leading zeros preserved", created.Aliases[0].Code)
	}

	resolved, err := catalog.ResolveBarcode(op, "EAN-13", "000123")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ItemID != created.ItemID || resolved.Unit != "each" || resolved.Revision != 1 {
		t.Fatalf("barcode resolution = %+v, want item %s at revision 1 in each", resolved, created.ItemID)
	}

	preview, err := catalog.ValidateConversion(op, created.ItemID, "2", "box", false)
	if err != nil {
		t.Fatal(err)
	}
	if preview.BaseQtyText != "24" || preview.BaseUnit != "each" {
		t.Fatalf("box receipt preview = %+v, want 24 each", preview)
	}

	serialized, err := catalog.CreateItem(ctx, op, ItemDraft{
		SKU: "SERIAL-1", Name: "Serialized pump", BaseUnit: "each", Scale: 0, Tracking: TrackingSerialized,
	}, "create-serialized")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ValidateConversion(op, serialized.ItemID, "0.5", "each", false); !errors.Is(err, ErrInexactConversion) {
		t.Fatalf("0.5 serialized each err=%v, want INEXACT_CONVERSION", err)
	}
	if _, err := catalog.ValidateConversion(op, serialized.ItemID, "1", "kg", false); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("kg to each err=%v, want unknown unit", err)
	}

	thirds, err := catalog.CreateItem(ctx, op, ItemDraft{
		SKU: "THIRD-1", Name: "Third packs", BaseUnit: "each", Scale: 2, Tracking: TrackingEach,
		Conversions: []PackConversion{{PackUnit: "third-pack", Numerator: 1, Denominator: 3}},
	}, "create-thirds")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ValidateConversion(op, thirds.ItemID, "1", "third-pack", false); !errors.Is(err, ErrInexactConversion) {
		t.Fatalf("single 1/3 pack err=%v, want exact rejection", err)
	}
	if _, err := catalog.ValidateConversion(op, thirds.ItemID, "3", "third-pack", false); !errors.Is(err, ErrInexactConversion) {
		t.Fatalf("unapproved aggregate err=%v, want exact rejection without approval", err)
	}
	aggregate, err := catalog.ValidateConversion(op, thirds.ItemID, "3", "third-pack", true)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.BaseQtyText != "1.00" || !aggregate.Aggregate {
		t.Fatalf("approved aggregate preview = %+v, want 1.00 each aggregate", aggregate)
	}

	if err := catalog.MarkMovementsSeen(op.TenantID, op.CompanyID, created.ItemID); err != nil {
		t.Fatal(err)
	}
	frozen := stock002BoxDraft()
	frozen.Scale = 2
	if _, err := catalog.ReviseItem(ctx, op, created.ItemID, frozen, "rev-scale"); !errors.Is(err, ErrScaleFrozen) {
		t.Fatalf("scale change after movement err=%v, want frozen-scale refusal", err)
	}
	renamed := stock002BoxDraft()
	renamed.Name = "Widget 12-pack box (labeled)"
	revised, err := catalog.ReviseItem(ctx, op, created.ItemID, renamed, "rev-name")
	if err != nil {
		t.Fatal(err)
	}
	if revised.Revision != 2 {
		t.Fatalf("revision = %d, want 2", revised.Revision)
	}
	history, err := catalog.History(op, created.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Name != "Widget 12-pack box" || history[0].BaseUnit != "each" || history[0].Revision != 1 {
		t.Fatalf("revision history rewrote the past: %+v", history)
	}

	reservations.set(created.ItemID, 2)
	if _, err := catalog.RetireItem(ctx, op, created.ItemID, "retire-blocked"); !errors.Is(err, ErrOpenReservations) {
		t.Fatalf("retire with open reservations err=%v, want open-reservation refusal", err)
	}
	reservations.set(created.ItemID, 0)
	retired, err := catalog.RetireItem(ctx, op, created.ItemID, "retire-ok")
	if err != nil {
		t.Fatal(err)
	}
	if retired.Status != ItemStatusRetired {
		t.Fatalf("retired status = %q", retired.Status)
	}
	pickers := catalog.ListItems(op, ItemFilter{PickersOnly: true})
	for _, item := range pickers {
		if item.ItemID == created.ItemID {
			t.Fatalf("retired item %s still appears in pickers", created.ItemID)
		}
	}
	if _, err := catalog.History(op, created.ItemID); err != nil {
		t.Fatalf("retired history is unreadable: %v", err)
	}
	after, err := catalog.ResolveBarcode(op, "EAN-13", "000123")
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != ItemStatusRetired {
		t.Fatalf("retired barcode resolution = %+v, want RETIRED status surfaced", after)
	}
}

// TestTodo_SMB_STOCK_002_Integration proves durable idempotent writes,
// scoped SKU uniqueness, revision-carried alias/conversion registries and
// archive-then-retire lifecycle ordering.
func TestTodo_SMB_STOCK_002_Integration(t *testing.T) {
	ctx := context.Background()
	op := stock002Context()
	catalog := NewCatalog(nil)

	first, err := catalog.CreateItem(ctx, op, stock002BoxDraft(), "box-key")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := catalog.CreateItem(ctx, op, stock002BoxDraft(), "box-key")
	if err != nil || replay.ItemID != first.ItemID || replay.Revision != first.Revision {
		t.Fatalf("exact replay = %+v, err=%v, want durable first result", replay, err)
	}
	changed := stock002BoxDraft()
	changed.Name = "Different box"
	if _, err := catalog.CreateItem(ctx, op, changed, "box-key"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed-payload replay err=%v, want idempotency conflict", err)
	}
	if _, err := catalog.CreateItem(ctx, op, stock002BoxDraft(), "box-key-2"); !errors.Is(err, ErrDuplicateSKU) {
		t.Fatalf("duplicate SKU err=%v, want duplicate-SKU refusal", err)
	}

	otherCompany := op
	otherCompany.CompanyID = "company-b"
	branched, err := catalog.CreateItem(ctx, otherCompany, stock002BoxDraft(), "box-key")
	if err != nil {
		t.Fatalf("same SKU in another company must be allowed: %v", err)
	}
	if branched.ItemID == first.ItemID {
		t.Fatal("cross-company items share identity")
	}

	swap := stock002BoxDraft()
	swap.Aliases = []BarcodeAlias{{Symbology: "CODE128", Code: "BOX-9"}}
	if _, err := catalog.ReviseItem(ctx, op, first.ItemID, swap, "rev-alias"); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ResolveBarcode(op, "EAN-13", "000123"); !errors.Is(err, ErrUnknownCode) {
		t.Fatalf("removed alias err=%v, want UNKNOWN_CODE", err)
	}
	moved, err := catalog.ResolveBarcode(op, "CODE128", "BOX-9")
	if err != nil || moved.ItemID != first.ItemID || moved.Revision != 2 {
		t.Fatalf("revised alias resolution = %+v, err=%v", moved, err)
	}
	if _, err := catalog.RegisterAlias(ctx, otherCompany, branched.ItemID, BarcodeAlias{Symbology: "CODE128", Code: "BOX-9"}, "alias-branch"); err != nil {
		t.Fatalf("same code in another company must be allowed: %v", err)
	}

	second, err := catalog.CreateItem(ctx, op, ItemDraft{SKU: "WIDGET-13", Name: "Other", BaseUnit: "each", Scale: 0, Tracking: TrackingEach}, "second")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.RegisterAlias(ctx, op, second.ItemID, BarcodeAlias{Symbology: "CODE128", Code: "BOX-9"}, "alias-clash"); !errors.Is(err, ErrAliasConflict) {
		t.Fatalf("duplicate alias err=%v, want alias conflict", err)
	}

	archived, err := catalog.ArchiveItem(ctx, op, second.ItemID, "archive-second")
	if err != nil || archived.Status != ItemStatusArchived {
		t.Fatalf("archive = %+v, err=%v", archived, err)
	}
	for _, item := range catalog.ListItems(op, ItemFilter{PickersOnly: true}) {
		if item.ItemID == second.ItemID {
			t.Fatal("archived item still appears in pickers")
		}
	}
	found := false
	for _, item := range catalog.ListItems(op, ItemFilter{}) {
		if item.ItemID == second.ItemID {
			found = true
		}
	}
	if !found {
		t.Fatal("archived item disappeared from readable history")
	}
	retired, err := catalog.RetireItem(ctx, op, second.ItemID, "retire-second")
	if err != nil || retired.Status != ItemStatusRetired {
		t.Fatalf("retire after archive = %+v, err=%v", retired, err)
	}
	if _, err := catalog.ReviseItem(ctx, op, second.ItemID, ItemDraft{SKU: "WIDGET-13", Name: "Late edit", BaseUnit: "each", Scale: 0, Tracking: TrackingEach}, "rev-retired"); !errors.Is(err, ErrItemNotEditable) {
		t.Fatalf("revise retired err=%v, want not-editable refusal", err)
	}
}

// TestTodo_SMB_STOCK_002_Race proves parallel writers cannot register the
// same alias twice and parallel idempotent retries converge on one item.
func TestTodo_SMB_STOCK_002_Race(t *testing.T) {
	ctx := context.Background()
	op := stock002Context()
	catalog := NewCatalog(nil)

	const writers = 8
	items := make([]CatalogItem, writers)
	for i := range items {
		draft := ItemDraft{SKU: fmt.Sprintf("RACE-%d", i), Name: fmt.Sprintf("Race item %d", i), BaseUnit: "each", Scale: 0, Tracking: TrackingEach}
		created, err := catalog.CreateItem(ctx, op, draft, fmt.Sprintf("race-create-%d", i))
		if err != nil {
			t.Fatal(err)
		}
		items[i] = created
	}
	var wg sync.WaitGroup
	won := make([]bool, writers)
	for i := range items {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := catalog.RegisterAlias(ctx, op, items[i].ItemID, BarcodeAlias{Symbology: "EAN-13", Code: "777001"}, fmt.Sprintf("race-alias-%d", i))
			won[i] = err == nil
		}(i)
	}
	wg.Wait()
	winners := 0
	for _, w := range won {
		if w {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("%d writers registered the same alias, want exactly 1", winners)
	}
	resolved, err := catalog.ResolveBarcode(op, "EAN-13", "777001")
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, item := range items {
		if item.ItemID == resolved.ItemID {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("race winner %s is not one of the raced items", resolved.ItemID)
	}

	retryIDs := make([]string, writers)
	var retryWG sync.WaitGroup
	for i := range retryIDs {
		retryWG.Add(1)
		go func(i int) {
			defer retryWG.Done()
			created, err := catalog.CreateItem(ctx, op, ItemDraft{SKU: "RACE-ONE", Name: "Single", BaseUnit: "each", Scale: 0, Tracking: TrackingEach}, "race-shared-key")
			if err != nil {
				t.Errorf("shared-key create err=%v", err)
				return
			}
			retryIDs[i] = created.ItemID
		}(i)
	}
	retryWG.Wait()
	for _, id := range retryIDs {
		if id == "" || id != retryIDs[0] {
			t.Fatalf("parallel idempotent retries diverged: %v", retryIDs)
		}
	}
	if got := catalog.ListItems(op, ItemFilter{Search: "RACE-ONE"}); len(got) != 1 {
		t.Fatalf("shared-key creates stored %d items, want 1", len(got))
	}
}

// TestTodo_SMB_STOCK_002_Security proves tenant/company/site isolation,
// context authentication and conflict disclosure discipline.
func TestTodo_SMB_STOCK_002_Security(t *testing.T) {
	ctx := context.Background()
	op := stock002Context()
	catalog := NewCatalog(nil)

	sited := stock002BoxDraft()
	sited.SKU = "SITE-1"
	sited.Sites = []string{"site-1"}
	created, err := catalog.CreateItem(ctx, op, sited, "site-create")
	if err != nil {
		t.Fatal(err)
	}

	foreignTenant := op
	foreignTenant.TenantID = "tenant-evil"
	if got := catalog.ListItems(foreignTenant, ItemFilter{}); len(got) != 0 {
		t.Fatalf("cross-tenant list disclosed %d items", len(got))
	}
	if _, err := catalog.ResolveBarcode(foreignTenant, "EAN-13", "000123"); !errors.Is(err, ErrUnknownCode) {
		t.Fatalf("cross-tenant resolve err=%v, want UNKNOWN_CODE", err)
	}
	if _, err := catalog.History(foreignTenant, created.ItemID); !errors.Is(err, ErrItemNotFound) {
		t.Fatalf("cross-tenant history err=%v, want not-found", err)
	}

	foreignCompany := op
	foreignCompany.CompanyID = "company-evil"
	if got := catalog.ListItems(foreignCompany, ItemFilter{}); len(got) != 0 {
		t.Fatalf("cross-company list disclosed %d items", len(got))
	}

	deniedSite := op
	deniedSite.SiteID = "site-9"
	if got := catalog.ListItems(deniedSite, ItemFilter{}); len(got) != 0 {
		t.Fatalf("denied-site list disclosed %d items", len(got))
	}
	if _, err := catalog.ResolveBarcode(deniedSite, "EAN-13", "000123"); !errors.Is(err, ErrUnknownCode) {
		t.Fatalf("denied-site resolve err=%v, want UNKNOWN_CODE without disclosure", err)
	}
	allowedSite := op
	allowedSite.SiteID = "site-1"
	if got := catalog.ListItems(allowedSite, ItemFilter{}); len(got) != 1 {
		t.Fatalf("allowed-site list = %d items, want 1", len(got))
	}

	for name, bad := range map[string]OperationContext{
		"empty tenant":  {CompanyID: "company-a", ActorID: "operator-1", Origin: "human"},
		"empty company": {TenantID: "tenant-a", ActorID: "operator-1", Origin: "human"},
		"empty actor":   {TenantID: "tenant-a", CompanyID: "company-a", Origin: "human"},
		"empty origin":  {TenantID: "tenant-a", CompanyID: "company-a", ActorID: "operator-1"},
		"bad origin":    {TenantID: "tenant-a", CompanyID: "company-a", ActorID: "operator-1", Origin: "scan-beep"},
	} {
		if _, err := catalog.CreateItem(ctx, bad, ItemDraft{SKU: "BAD-" + name, Name: "Bad", BaseUnit: "each", Scale: 0, Tracking: TrackingEach}, "bad-"+name); !errors.Is(err, ErrStockContext) {
			t.Fatalf("%s err=%v, want context refusal", name, err)
		}
	}
}

// TestTodo_SMB_STOCK_002_Recovery proves a snapshot round-trip preserves
// identity, aliases, conversions and registry conflicts across restart.
func TestTodo_SMB_STOCK_002_Recovery(t *testing.T) {
	ctx := context.Background()
	op := stock002Context()
	catalog := NewCatalog(nil)

	created, err := catalog.CreateItem(ctx, op, stock002BoxDraft(), "recover-create")
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.MarkMovementsSeen(op.TenantID, op.CompanyID, created.ItemID); err != nil {
		t.Fatal(err)
	}

	bytes, err := catalog.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(bytes) == 0 {
		t.Fatal("snapshot is empty")
	}

	restarted := NewCatalog(nil)
	if err := restarted.Restore(bytes); err != nil {
		t.Fatal(err)
	}
	resolved, err := restarted.ResolveBarcode(op, "EAN-13", "000123")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ItemID != created.ItemID || resolved.Revision != created.Revision {
		t.Fatalf("post-restart resolution = %+v, want item %s", resolved, created.ItemID)
	}
	preview, err := restarted.ValidateConversion(op, created.ItemID, "2", "box", false)
	if err != nil || preview.BaseQtyText != "24" {
		t.Fatalf("post-restart preview = %+v, err=%v", preview, err)
	}
	frozen := stock002BoxDraft()
	frozen.Scale = 2
	if _, err := restarted.ReviseItem(ctx, op, created.ItemID, frozen, "rev-after-restart"); !errors.Is(err, ErrScaleFrozen) {
		t.Fatalf("post-restart scale change err=%v, want frozen-scale refusal", err)
	}
	second, err := restarted.CreateItem(ctx, op, ItemDraft{SKU: "RECOVER-2", Name: "Second", BaseUnit: "each", Scale: 0, Tracking: TrackingEach}, "recover-second")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.RegisterAlias(ctx, op, second.ItemID, BarcodeAlias{Symbology: "EAN-13", Code: "000123"}, "recover-clash"); !errors.Is(err, ErrAliasConflict) {
		t.Fatalf("post-restart duplicate alias err=%v, want alias conflict", err)
	}
	if err := restarted.Restore(bytes); err == nil {
		t.Fatal("restore into a populated catalog succeeded, want refusal")
	}
	if err := NewCatalog(nil).Restore([]byte("{broken")); err == nil {
		t.Fatal("restore of corrupt bytes succeeded, want refusal")
	}
}

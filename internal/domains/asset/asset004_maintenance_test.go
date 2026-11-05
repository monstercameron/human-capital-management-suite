package asset

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func asset004Actor(id string) ServiceActor {
	return ServiceActor{Actor: assetRef("worker", id), Origin: "human"}
}

func asset004Register(t *testing.T, store *CustodyStore, id string) values.EntityRef {
	t.Helper()
	at := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	asset := assetRef("asset", id)
	inv := InventoryRevision{InventoryID: asset, Owner: assetRef("organization", "org-"+id), Classification: "PUMP", SerialNumber: "SN-" + id, Revision: assetRev(t, "inventory-"+id, 1), EffectiveAt: at, Status: Available}
	if err := store.Register(inv); err != nil {
		t.Fatal(err)
	}
	worker := assetRef("worker", "worker-"+id)
	if err := store.Assign(asset, worker, "site-1", "GOOD", assetReceipt(t, "a-"+id, true), assetReceipt(t, "i-"+id, true), values.UnspecifiedRevision(), assetRev(t, "custody-"+id, 1), at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	return asset
}

func asset004Plan(asset values.EntityRef) MaintenancePlanInput {
	return MaintenancePlanInput{
		Asset: asset, CalendarEnabled: true, IntervalDays: 30,
		MeterEnabled: true, MeterThreshold: 1000,
		Baseline: time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC), TemplateRevision: "tmpl-v2",
		BlockMode: BlockHard, BlockWhenOverdue: true,
	}
}

type asset004Parts struct {
	mu     sync.Mutex
	calls  int
	fail   bool
	issued map[string]PartsReceipt
}

func (f *asset004Parts) IssueServiceParts(_ context.Context, _ values.EntityRef, workID string, lines []PartsLine) (PartsReceipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.fail {
		return PartsReceipt{}, errors.New("parts cabinet is locked")
	}
	if receipt, ok := f.issued[workID]; ok {
		return receipt, nil
	}
	if f.issued == nil {
		f.issued = make(map[string]PartsReceipt)
	}
	receipt := PartsReceipt{ReceiptID: "parts-" + workID, Lines: append([]PartsLine(nil), lines...)}
	f.issued[workID] = receipt
	return receipt, nil
}

type asset004Sink struct {
	mu    sync.Mutex
	tasks []ServiceWorkItem
}

func (s *asset004Sink) CreateServiceTask(_ context.Context, item ServiceWorkItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = append(s.tasks, item)
	return nil
}

// TestTodo_SMB_ASSET_004 proves maintenance due creates one task per cycle,
// out-of-service blocks bookings with explicit resolution, parts issue once
// with an exact receipt, and offline completions release only after server
// validation.
func TestTodo_SMB_ASSET_004(t *testing.T) {
	ctx := context.Background()
	store := NewCustodyStore()
	parts := &asset004Parts{}
	sink := &asset004Sink{}
	coordinator := NewMaintenanceCoordinator(store, parts, sink)
	actor := asset004Actor("tech-1")
	asset := asset004Register(t, store, "pump-1")

	plan, err := coordinator.SetMaintenancePlan(actor, asset004Plan(asset), "plan-1")
	if err != nil {
		t.Fatal(err)
	}
	if plan.PlanVersion != 1 || plan.NextDue.Day() != 1 || plan.NextDue.Month() != time.October {
		t.Fatalf("plan = %+v, want version 1 due 2026-10-01", plan)
	}

	owner := assetRef("worker", "owner-1")
	booking, err := coordinator.RequestBooking(actor, asset, owner, "book-1", time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC), time.Date(2026, 10, 5, 17, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if booking.Status != BookingHeld {
		t.Fatalf("booking = %+v, want HELD", booking)
	}
	confirmed, err := coordinator.ConfirmBooking(actor, "book-1")
	if err != nil || confirmed.Status != BookingConfirmed {
		t.Fatalf("confirm on available asset = %+v err=%v, want CONFIRMED", confirmed, err)
	}

	dueAt := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	first, opened, err := coordinator.TickDue(asset, dueAt)
	if err != nil || !opened {
		t.Fatalf("due tick = %+v opened=%v err=%v, want one opened task", first, opened, err)
	}
	if first.Trigger != DueCalendar || first.ProjectRef != "" {
		t.Fatalf("standalone due task = %+v, want calendar trigger without dummy project", first)
	}
	second, openedAgain, err := coordinator.TickDue(asset, dueAt.Add(time.Hour))
	if err != nil || openedAgain || second.WorkID != first.WorkID {
		t.Fatalf("repeated tick = %+v opened=%v err=%v, want same task", second, openedAgain, err)
	}
	if _, err := coordinator.RecordMeterReading(actor, asset, 1000, dueAt.Add(2*time.Hour), "meter-1"); err != nil {
		t.Fatal(err)
	}
	third, openedThird, err := coordinator.TickDue(asset, dueAt.Add(3*time.Hour))
	if err != nil || openedThird || third.WorkID != first.WorkID {
		t.Fatalf("simultaneous meter tick = %+v opened=%v err=%v, want same single task", third, openedThird, err)
	}
	if len(sink.tasks) != 1 || sink.tasks[0].WorkID != first.WorkID {
		t.Fatalf("sink received %d tasks, want exactly one", len(sink.tasks))
	}

	serviceability, err := coordinator.ServiceabilityOf(asset)
	if err != nil {
		t.Fatal(err)
	}
	if serviceability.State != ServiceOutOfService {
		t.Fatalf("overdue hard asset = %+v, want OUT_OF_SERVICE", serviceability)
	}
	bookings := coordinator.Bookings(asset)
	if len(bookings) != 1 || bookings[0].Status != BookingNeedsResolution || bookings[0].Owner != owner {
		t.Fatalf("bookings after block = %+v, want one NEEDS_RESOLUTION with owner", bookings)
	}
	if _, err := coordinator.RequestBooking(actor, asset, owner, "book-2", dueAt.Add(48*time.Hour), dueAt.Add(72*time.Hour)); !errors.Is(err, ErrServiceBlocked) {
		t.Fatalf("booking on out-of-service asset err=%v, want blocked", err)
	}

	completion, err := coordinator.RecordServiceCompletion(ctx, actor, asset, first.WorkID, ServiceCompletionInput{
		WorkPerformed: "Replaced seals", Parts: []PartsLine{{ItemSKU: "SEAL-9", Qty: "4", Unit: "each"}}, Evidence: "photo-1",
	}, "complete-1")
	if err != nil {
		t.Fatal(err)
	}
	if completion.Status != CompletionVerified || completion.PartsReceipt == nil || completion.PartsReceipt.Lines[0].Qty != "4" {
		t.Fatalf("completion = %+v, want verified with exact parts receipt", completion)
	}
	if _, err := coordinator.RecordServiceCompletion(ctx, actor, asset, first.WorkID, ServiceCompletionInput{
		WorkPerformed: "Replaced seals", Parts: []PartsLine{{ItemSKU: "SEAL-9", Qty: "4", Unit: "each"}}, Evidence: "photo-1",
	}, "complete-1"); err != nil {
		t.Fatalf("completion replay err=%v", err)
	}
	if parts.calls != 1 {
		t.Fatalf("parts issued %d times, want exactly once", parts.calls)
	}
	stillBlocked, err := coordinator.ServiceabilityOf(asset)
	if err != nil {
		t.Fatal(err)
	}
	if stillBlocked.State != ServiceOutOfService {
		t.Fatalf("task completion released the asset: %+v", stillBlocked)
	}

	released, err := coordinator.ReleaseToService(actor, asset, "reviewer-2", "inspect-9", dueAt.Add(6*time.Hour), "release-1")
	if err != nil {
		t.Fatal(err)
	}
	if released.State != ServiceAvailable || released.Cycle != 1 {
		t.Fatalf("released = %+v, want AVAILABLE at cycle 1", released)
	}

	if _, err := coordinator.RecordMeterReading(actor, asset, 1500, dueAt.Add(30*24*time.Hour), "meter-2"); err != nil {
		t.Fatal(err)
	}
	offlineWork, openedOffline, err := coordinator.TickDue(asset, dueAt.Add(30*24*time.Hour))
	if err != nil || !openedOffline || offlineWork.Cycle != 1 {
		t.Fatalf("second cycle tick = %+v opened=%v err=%v", offlineWork, openedOffline, err)
	}
	offline, err := coordinator.RecordServiceCompletion(ctx, actor, asset, offlineWork.WorkID, ServiceCompletionInput{
		WorkPerformed: "Offline top-up", Evidence: "note-1", Offline: true,
	}, "complete-offline")
	if err != nil {
		t.Fatal(err)
	}
	if offline.Status != CompletionUnverified {
		t.Fatalf("offline completion = %+v, want UNVERIFIED", offline)
	}
	if _, err := coordinator.ReleaseToService(actor, asset, "reviewer-2", "inspect-10", dueAt.Add(31*24*time.Hour), "release-offline"); !errors.Is(err, ErrServiceRelease) {
		t.Fatalf("offline release err=%v, want release refusal until server validates", err)
	}
	confirmedCompletion, err := coordinator.ConfirmServiceCompletion(actor, asset, offline.ReceiptID, "reviewer-2", "server-check-1", "confirm-1")
	if err != nil || confirmedCompletion.Status != CompletionVerified {
		t.Fatalf("server confirm = %+v err=%v, want VERIFIED", confirmedCompletion, err)
	}
	final, err := coordinator.ReleaseToService(actor, asset, "reviewer-2", "inspect-10", dueAt.Add(31*24*time.Hour), "release-2")
	if err != nil || final.State != ServiceAvailable || final.Cycle != 2 {
		t.Fatalf("final release = %+v err=%v, want AVAILABLE at cycle 2", final, err)
	}
}

// TestTodo_SMB_ASSET_004_Integration proves failed parts keep service
// incomplete and blocked, meter epochs stay monotonic across authorized
// replacement, calendar revisions reschedule only the future, warnings stay
// usable, and stock linkage needs explicit evidence.
func TestTodo_SMB_ASSET_004_Integration(t *testing.T) {
	ctx := context.Background()
	store := NewCustodyStore()
	coordinator := NewMaintenanceCoordinator(store, &asset004Parts{fail: true}, nil)
	actor := asset004Actor("tech-2")
	asset := asset004Register(t, store, "pump-2")

	if _, err := coordinator.SetMaintenancePlan(actor, asset004Plan(asset), "plan-2"); err != nil {
		t.Fatal(err)
	}
	dueAt := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	work, opened, err := coordinator.TickDue(asset, dueAt)
	if err != nil || !opened {
		t.Fatal(err)
	}
	incomplete, err := coordinator.RecordServiceCompletion(ctx, actor, asset, work.WorkID, ServiceCompletionInput{
		WorkPerformed: "Attempted seal swap", Parts: []PartsLine{{ItemSKU: "SEAL-9", Qty: "4", Unit: "each"}}, Evidence: "photo-2",
	}, "complete-fail")
	if err != nil {
		t.Fatal(err)
	}
	if incomplete.Status != CompletionIncomplete || incomplete.PartsAction == "" {
		t.Fatalf("failed-parts completion = %+v, want INCOMPLETE with failed action", incomplete)
	}
	if _, err := coordinator.ReleaseToService(actor, asset, "reviewer-2", "inspect-2", dueAt.Add(time.Hour), "release-fail"); !errors.Is(err, ErrServiceRelease) {
		t.Fatalf("release of incomplete service err=%v, want refusal", err)
	}
	blocked, err := coordinator.ServiceabilityOf(asset)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.State != ServiceOutOfService {
		t.Fatalf("failed release freed the asset: %+v", blocked)
	}
	events, err := coordinator.Trace(actor, asset)
	if err != nil {
		t.Fatal(err)
	}
	var releaseFailed, completionRecorded bool
	for _, event := range events {
		if event.Kind == ServiceEventReleaseFailed && event.Detail != "" {
			releaseFailed = true
		}
		if event.Kind == ServiceEventCompletion {
			completionRecorded = true
		}
	}
	if !releaseFailed || !completionRecorded {
		t.Fatalf("trace lacks explainable release failure: %+v", events)
	}

	if _, err := coordinator.RecordMeterReading(actor, asset, 900, dueAt, "meter-ok"); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.RecordMeterReading(actor, asset, 500, dueAt.Add(30*time.Minute), "meter-low"); !errors.Is(err, ErrMeterRegression) {
		t.Fatalf("lower unapproved reading err=%v, want regression refusal", err)
	}
	if _, err := coordinator.RecordMeterReplacement(actor, asset, 900, 0, "swap-evidence", "reviewer-2", dueAt.Add(time.Hour), "meter-swap"); err != nil {
		t.Fatal(err)
	}
	state, err := coordinator.RecordMeterReading(actor, asset, 50, dueAt.Add(2*time.Hour), "meter-newepoch")
	if err != nil || state.Epoch != 1 || state.LastReading != 50 {
		t.Fatalf("new-epoch reading = %+v err=%v, want epoch 1 at 50", state, err)
	}
	preserved, _, err := coordinator.TickDue(asset, dueAt.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if preserved.WorkID != work.WorkID {
		t.Fatal("authorized meter reset erased the outstanding overdue task")
	}

	revised, err := coordinator.SetMaintenancePlan(actor, MaintenancePlanInput{
		Asset: asset, CalendarEnabled: true, IntervalDays: 60,
		MeterEnabled: true, MeterThreshold: 1000,
		Baseline: time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC), TemplateRevision: "tmpl-v3",
		BlockMode: BlockHard, BlockWhenOverdue: true,
	}, "plan-2b")
	if err != nil {
		t.Fatal(err)
	}
	if revised.PlanVersion != 2 {
		t.Fatalf("revised plan = %+v, want version 2", revised)
	}
	after, err := coordinator.Trace(actor, asset)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) <= len(events) {
		t.Fatal("plan revision rewrote past evidence")
	}

	warnAsset := asset004Register(t, store, "pump-warn")
	warnPlan := asset004Plan(warnAsset)
	warnPlan.BlockMode = BlockWarning
	warnPlan.BlockWhenOverdue = false
	soft := NewMaintenanceCoordinator(store, &asset004Parts{}, nil)
	if _, err := soft.SetMaintenancePlan(actor, warnPlan, "plan-warn"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := soft.TickDue(warnAsset, dueAt); err != nil {
		t.Fatal(err)
	}
	warning, err := soft.ServiceabilityOf(warnAsset)
	if err != nil {
		t.Fatal(err)
	}
	if warning.State != ServiceDueWarning {
		t.Fatalf("warning-mode overdue = %+v, want DUE_WARNING", warning)
	}
	held, err := soft.RequestBooking(actor, warnAsset, assetRef("worker", "owner-w"), "book-w", dueAt.Add(48*time.Hour), dueAt.Add(72*time.Hour))
	if err != nil {
		t.Fatalf("warning-mode booking request err=%v, want allowed", err)
	}
	if _, err := soft.ConfirmBooking(actor, held.BookingID); err != nil {
		t.Fatalf("warning-mode confirm err=%v, want usable", err)
	}
	manual, err := soft.OpenServiceWork(ctx, actor, warnAsset, "technician requested early service", "", "open-warn")
	if err != nil {
		t.Fatal(err)
	}
	existing, openedNow, err := soft.TickDue(warnAsset, dueAt)
	if err != nil || openedNow || existing.WorkID != manual.WorkID {
		t.Fatalf("manual open diverged from due work: %+v opened=%v err=%v", existing, openedNow, err)
	}
	inService, err := soft.ServiceabilityOf(warnAsset)
	if err != nil {
		t.Fatal(err)
	}
	if inService.State != ServiceInService {
		t.Fatalf("opened service = %+v, want IN_SERVICE", inService)
	}
	warnBookings := soft.Bookings(warnAsset)
	if len(warnBookings) != 1 || warnBookings[0].Status != BookingNeedsResolution {
		t.Fatalf("manual open left bookings %+v, want flagged for resolution", warnBookings)
	}

	linked, err := coordinator.SetStockLink(actor, asset, StockLinkPending, "", "", "", "link-1")
	if err != nil || linked.StockLinkState != StockLinkPending {
		t.Fatalf("pending link = %+v err=%v", linked, err)
	}
	if _, err := coordinator.SetStockLink(actor, asset, StockLinkLinked, "", "", "", "link-2"); err == nil {
		t.Fatal("linked without refs and evidence succeeded, want refusal")
	}
	done, err := coordinator.SetStockLink(actor, asset, StockLinkLinked, "PUMP-1", "site-1", "receipt-9", "link-3")
	if err != nil || done.StockLinkState != StockLinkLinked {
		t.Fatalf("explicit link = %+v err=%v", done, err)
	}

	lostAsset := asset004Register(t, store, "pump-lost")
	if err := store.ReportLost(lostAsset, assetRef("worker", "worker-pump-lost"), "site-1", "MISSING", assetRev(t, "custody-pump-lost", 1), assetRev(t, "custody-pump-lost", 2), dueAt); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.SetMaintenancePlan(actor, asset004Plan(lostAsset), "plan-lost"); err != nil {
		t.Fatal(err)
	}
	lostWork, _, err := coordinator.TickDue(lostAsset, dueAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.RecordServiceCompletion(ctx, actor, lostAsset, lostWork.WorkID, ServiceCompletionInput{WorkPerformed: "Checked yard", Evidence: "ev-lost"}, "complete-lost"); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.ReleaseToService(actor, lostAsset, "reviewer-2", "inspect-lost", dueAt.Add(time.Hour), "release-lost"); !errors.Is(err, ErrServiceRelease) {
		t.Fatalf("release of lost asset err=%v, want refusal until recovery", err)
	}
}

// TestTodo_SMB_ASSET_004_Race proves bookings racing the out-of-service
// transition never confirm usable past the blocked revision, and parallel
// due ticks open exactly one task.
func TestTodo_SMB_ASSET_004_Race(t *testing.T) {
	ctx := context.Background()
	_ = ctx
	store := NewCustodyStore()
	sink := &asset004Sink{}
	coordinator := NewMaintenanceCoordinator(store, &asset004Parts{}, sink)
	actor := asset004Actor("tech-3")
	asset := asset004Register(t, store, "pump-3")

	if _, err := coordinator.SetMaintenancePlan(actor, asset004Plan(asset), "plan-3"); err != nil {
		t.Fatal(err)
	}
	const bookings = 8
	held := make([]string, bookings)
	for i := range held {
		booking, err := coordinator.RequestBooking(actor, asset, assetRef("worker", "owner-3"), "race-book-"+string(rune(97+i)), time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC), time.Date(2026, 10, 5, 17, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		held[i] = booking.BookingID
	}
	dueAt := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	errs := make([]error, bookings)
	for i := range held {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := coordinator.ConfirmBooking(actor, held[i])
			errs[i] = err
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _, _ = coordinator.TickDue(asset, dueAt)
	}()
	wg.Wait()
	confirmed, refused := 0, 0
	for _, err := range errs {
		if err == nil {
			confirmed++
		} else if errors.Is(err, ErrBookingConflict) || errors.Is(err, ErrServiceBlocked) {
			refused++
		} else {
			t.Fatalf("race confirm err=%v", err)
		}
	}
	if confirmed+refused != bookings {
		t.Fatalf("confirmed %d + refused %d != %d racers", confirmed, refused, bookings)
	}
	final, err := coordinator.ServiceabilityOf(asset)
	if err != nil {
		t.Fatal(err)
	}
	for _, booking := range coordinator.Bookings(asset) {
		if booking.Status == BookingConfirmed && (booking.EligibilityRev != final.BlockedRev || final.State == ServiceOutOfService) {
			t.Fatalf("usable booking %+v survived blocked revision %d", booking, final.BlockedRev)
		}
	}

	var tickWG sync.WaitGroup
	workIDs := make([]string, bookings)
	for i := range workIDs {
		tickWG.Add(1)
		go func(i int) {
			defer tickWG.Done()
			item, _, err := coordinator.TickDue(asset, dueAt.Add(time.Hour))
			if err != nil {
				t.Errorf("race tick err=%v", err)
				return
			}
			workIDs[i] = item.WorkID
		}(i)
	}
	tickWG.Wait()
	for _, id := range workIDs {
		if id == "" || id != workIDs[0] {
			t.Fatalf("parallel due ticks diverged: %v", workIDs)
		}
	}
	if len(sink.tasks) != 1 {
		t.Fatalf("sink received %d tasks, want exactly one", len(sink.tasks))
	}
}

// TestTodo_SMB_ASSET_004_Security proves tenant isolation and origin
// authentication on every service write and trace.
func TestTodo_SMB_ASSET_004_Security(t *testing.T) {
	ctx := context.Background()
	_ = ctx
	store := NewCustodyStore()
	coordinator := NewMaintenanceCoordinator(store, &asset004Parts{}, nil)
	actor := asset004Actor("tech-4")
	asset := asset004Register(t, store, "pump-4")

	if _, err := coordinator.SetMaintenancePlan(actor, asset004Plan(asset), "plan-4"); err != nil {
		t.Fatal(err)
	}
	foreignActor := ServiceActor{Actor: values.EntityRef{Tenant: "tenant-evil", Kind: "worker", Id: assetRef("worker", "evil").Id}, Origin: "human"}
	if _, err := coordinator.SetMaintenancePlan(foreignActor, asset004Plan(asset), "plan-evil"); !errors.Is(err, ErrTenantBoundary) {
		t.Fatalf("cross-tenant plan err=%v, want tenant boundary", err)
	}
	if _, _, err := coordinator.TickDue(asset, time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Trace(foreignActor, asset); !errors.Is(err, ErrTenantBoundary) {
		t.Fatalf("cross-tenant trace err=%v, want tenant boundary", err)
	}
	badOrigin := ServiceActor{Actor: assetRef("worker", "tech-4"), Origin: "scan-beep"}
	if _, err := coordinator.RecordInspection(badOrigin, asset, "notes", "ev", time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC), "insp-bad"); !errors.Is(err, ErrServiceValidation) {
		t.Fatalf("bad origin err=%v, want validation refusal", err)
	}
	if _, _, err := coordinator.TickDue(assetRef("asset", "missing"), time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)); !errors.Is(err, ErrServiceState) {
		t.Fatalf("tick on unplanned asset err=%v, want state refusal", err)
	}
}

// TestTodo_SMB_ASSET_004_Recovery proves a snapshot round-trip preserves
// plans, cycles, work, bookings, meters and events across restart.
func TestTodo_SMB_ASSET_004_Recovery(t *testing.T) {
	ctx := context.Background()
	store := NewCustodyStore()
	parts := &asset004Parts{}
	coordinator := NewMaintenanceCoordinator(store, parts, nil)
	actor := asset004Actor("tech-5")
	asset := asset004Register(t, store, "pump-5")

	if _, err := coordinator.SetMaintenancePlan(actor, asset004Plan(asset), "plan-5"); err != nil {
		t.Fatal(err)
	}
	owner := assetRef("worker", "owner-5")
	if _, err := coordinator.RequestBooking(actor, asset, owner, "book-5", time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC), time.Date(2026, 10, 5, 17, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.ConfirmBooking(actor, "book-5"); err != nil {
		t.Fatal(err)
	}
	dueAt := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	work, _, err := coordinator.TickDue(asset, dueAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.RecordServiceCompletion(ctx, actor, asset, work.WorkID, ServiceCompletionInput{WorkPerformed: "Seals", Evidence: "photo-5"}, "complete-5"); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.RecordMeterReading(actor, asset, 400, dueAt, "meter-5"); err != nil {
		t.Fatal(err)
	}

	bytes, err := coordinator.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewMaintenanceCoordinator(store, parts, nil)
	if err := restarted.Restore(bytes); err != nil {
		t.Fatal(err)
	}
	serviceability, err := restarted.ServiceabilityOf(asset)
	if err != nil {
		t.Fatal(err)
	}
	if serviceability.State != ServiceOutOfService || serviceability.Cycle != 0 {
		t.Fatalf("post-restart serviceability = %+v", serviceability)
	}
	bookings := restarted.Bookings(asset)
	if len(bookings) != 1 || bookings[0].Status != BookingNeedsResolution {
		t.Fatalf("post-restart bookings = %+v, want flagged booking", bookings)
	}
	events, err := restarted.Trace(actor, asset)
	if err != nil || len(events) == 0 {
		t.Fatalf("post-restart trace = %d events err=%v", len(events), err)
	}
	released, err := restarted.ReleaseToService(actor, asset, "reviewer-2", "inspect-5", dueAt.Add(time.Hour), "release-5")
	if err != nil || released.Cycle != 1 {
		t.Fatalf("post-restart release = %+v err=%v", released, err)
	}
	if err := restarted.Restore(bytes); err == nil {
		t.Fatal("restore into a populated coordinator succeeded, want refusal")
	}
	if err := NewMaintenanceCoordinator(store, parts, nil).Restore([]byte("{broken")); err == nil {
		t.Fatal("restore of corrupt bytes succeeded, want refusal")
	}
}

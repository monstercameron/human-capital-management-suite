package productui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type smbAsset004Parts struct{}

func (smbAsset004Parts) IssueServiceParts(_ context.Context, _ values.EntityRef, workID string, lines []asset.PartsLine) (asset.PartsReceipt, error) {
	return asset.PartsReceipt{ReceiptID: "parts-" + workID, Lines: append([]asset.PartsLine(nil), lines...)}, nil
}

func smbAsset004Ref(t *testing.T, kind, id string) values.EntityRef {
	t.Helper()
	ref := values.EntityRef{Tenant: "tenant-a", Kind: values.Kind(kind), Id: id}
	if err := ref.Validate(); err != nil {
		t.Fatal(err)
	}
	return ref
}

func smbAsset004Rev(t *testing.T, stream string, n uint64) values.RevisionToken {
	t.Helper()
	rev, err := values.NewSequenceRevision(stream, n)
	if err != nil {
		t.Fatal(err)
	}
	return rev
}

func smbAsset004Fixture(t *testing.T) (*asset.MaintenanceCoordinator, asset.MaintenancePlan, values.EntityRef, asset.ServiceActor) {
	t.Helper()
	ctx := context.Background()
	_ = ctx
	store := asset.NewCustodyStore()
	coordinator := asset.NewMaintenanceCoordinator(store, smbAsset004Parts{}, nil)
	equipment := smbAsset004Ref(t, "asset", "123e4567-e89b-12d3-a456-426614174000")
	at := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	inv := asset.InventoryRevision{InventoryID: equipment, Owner: smbAsset004Ref(t, "organization", "123e4567-e89b-12d3-a456-426614174001"), Classification: "PUMP", SerialNumber: "SN-P1", Revision: smbAsset004Rev(t, "inventory-card", 1), EffectiveAt: at, Status: asset.Available}
	if err := store.Register(inv); err != nil {
		t.Fatal(err)
	}
	worker := smbAsset004Ref(t, "worker", "123e4567-e89b-12d3-a456-426614174002")
	receipt := asset.Receipt{ID: smbAsset004Ref(t, "receipt", "123e4567-e89b-12d3-a456-426614174003"), Issuer: smbAsset004Ref(t, "system", "123e4567-e89b-12d3-a456-426614174004"), IssuedAt: at, Verified: true, Evidence: "evidence"}
	if err := store.Assign(equipment, worker, "site-1", "GOOD", receipt, receipt, values.UnspecifiedRevision(), smbAsset004Rev(t, "custody-card", 1), at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	actor := asset.ServiceActor{Actor: worker, Origin: "human"}
	plan, err := coordinator.SetMaintenancePlan(actor, asset.MaintenancePlanInput{
		Asset: equipment, CalendarEnabled: true, IntervalDays: 30,
		MeterEnabled: true, MeterThreshold: 1000,
		Baseline: at, TemplateRevision: "tmpl-v2",
		BlockMode: asset.BlockHard, BlockWhenOverdue: true,
	}, "card-plan")
	if err != nil {
		t.Fatal(err)
	}
	return coordinator, plan, equipment, actor
}

// TestTodo_SMB_ASSET_004_Browser renders the maintenance card the way the
// Equipment Maintenance tab serves it: next due, service status with the
// out-of-service banner, outstanding work, release review and the service
// timeline, localized across en-US, de-DE and ar RTL.
func TestTodo_SMB_ASSET_004_Browser(t *testing.T) {
	ctx := context.Background()
	coordinator, plan, equipment, actor := smbAsset004Fixture(t)

	dueAt := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	work, opened, err := coordinator.TickDue(equipment, dueAt)
	if err != nil || !opened {
		t.Fatal(err)
	}
	completion, err := coordinator.RecordServiceCompletion(ctx, actor, equipment, work.WorkID, asset.ServiceCompletionInput{
		WorkPerformed: "Replaced seals", Parts: []asset.PartsLine{{ItemSKU: "SEAL-9", Qty: "4", Unit: "each"}}, Evidence: "photo-1",
	}, "card-complete")
	if err != nil {
		t.Fatal(err)
	}
	serviceability, err := coordinator.ServiceabilityOf(equipment)
	if err != nil {
		t.Fatal(err)
	}
	events, err := coordinator.Trace(actor, equipment)
	if err != nil {
		t.Fatal(err)
	}
	meter := asset.MeterState{Epoch: 0, LastReading: 0}

	markup, err := ui.RenderToString(SMBAssetMaintenanceCard(plan, serviceability, &work, &completion, events, meter, "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Equipment maintenance", "Status: Out of service",
		"Out of service — bookings are held until verified release.",
		"2026-10-01", "cycle", "epoch 0 reading 0 / threshold 1000", "tmpl-v2",
		"Standalone — no project", "DUE_CALENDAR", "OPEN",
		"VERIFIED", "parts-" + work.WorkID, "photo-1",
		"PLAN_SET", "WORK_OPENED", "COMPLETION_RECORDED",
		"data-service-state=\"OUT_OF_SERVICE\"", "tabIndex=\"-1\"",
		"task completion alone never releases",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("maintenance card missing %q: %s", want, markup)
		}
	}

	german, err := ui.RenderToString(SMBAssetMaintenanceCard(plan, serviceability, &work, &completion, events, meter, "de-DE"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Anlagenwartung", "Status: Außer Betrieb", "Eigenständig — kein Projekt"} {
		if !strings.Contains(german, want) {
			t.Errorf("German maintenance card missing %q: %s", want, german)
		}
	}

	rtl, err := ui.RenderToString(SMBAssetMaintenanceCard(plan, serviceability, &work, &completion, events, meter, "ar"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dir=\"rtl\"", "lang=\"ar\"", "صيانة المعدات", "الحالة: خارج الخدمة"} {
		if !strings.Contains(rtl, want) {
			t.Errorf("Arabic maintenance card missing %q: %s", want, rtl)
		}
	}

	released, err := coordinator.ReleaseToService(actor, equipment, "reviewer-2", "inspect-1", dueAt.Add(6*time.Hour), "card-release")
	if err != nil {
		t.Fatal(err)
	}
	plan, err = coordinator.Plan(equipment)
	if err != nil {
		t.Fatal(err)
	}
	available, err := ui.RenderToString(SMBAssetMaintenanceCard(plan, released, nil, &completion, events, meter, "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Status: Available", "No open service work."} {
		if !strings.Contains(available, want) {
			t.Errorf("released card missing %q: %s", want, available)
		}
	}
	if strings.Contains(available, "bookings are held") {
		t.Errorf("released card still shows the block banner: %s", available)
	}
}

package payable

import (
	"errors"
	"testing"
	"time"
)

// TestTodo_SMB_BILL_012_Reads pins the read-model surface: GetPayable,
// ListPayables and PayableStatus serve the tenant's own payables, filter by
// supplier and status, and deny foreign scopes and unknown keys — never an
// empty success that could mask a leak.
func TestTodo_SMB_BILL_012_Reads(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry, _ := bill012Registry(now)
	op := bill012Op()
	foreign := op
	foreign.TenantID = "tenant-evil"

	admitted, err := registry.AdmitMatchedBill(op, bill012Match(), "reads-admit")
	if err != nil {
		t.Fatal(err)
	}

	got, err := registry.GetPayable(op, admitted.PayableID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PayableID != admitted.PayableID || got.ApprovedToPay != "60.00 USD" {
		t.Fatalf("GetPayable = %+v, want the 60.00 approved obligation", got)
	}
	if _, err := registry.GetPayable(op, "pay-missing"); !errors.Is(err, ErrPayableDenied) {
		t.Fatalf("GetPayable unknown err = %v, want denied", err)
	}
	if _, err := registry.GetPayable(foreign, admitted.PayableID); !errors.Is(err, ErrPayableDenied) {
		t.Fatalf("GetPayable foreign err = %v, want denied", err)
	}

	all, err := registry.ListPayables(op, "supplier-acme", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].PayableID != admitted.PayableID {
		t.Fatalf("ListPayables all = %+v, want the one obligation", all)
	}
	open, err := registry.ListPayables(op, "supplier-acme", PayableOpen)
	if err != nil || len(open) != 1 {
		t.Fatalf("ListPayables open = %+v err=%v, want one", open, err)
	}
	paid, err := registry.ListPayables(op, "supplier-acme", PayablePaid)
	if err != nil || len(paid) != 0 {
		t.Fatalf("ListPayables paid = %+v err=%v, want empty", paid, err)
	}
	other, err := registry.ListPayables(op, "supplier-other", "")
	if err != nil || len(other) != 0 {
		t.Fatalf("ListPayables other = %+v err=%v, want empty", other, err)
	}
	leaked, err := registry.ListPayables(foreign, "supplier-acme", "")
	if err != nil || len(leaked) != 0 {
		t.Fatalf("ListPayables foreign = %+v err=%v, want empty", leaked, err)
	}

	status, err := registry.PayableStatus(op, "reads-admit")
	if err != nil {
		t.Fatal(err)
	}
	if status.PayableID != admitted.PayableID {
		t.Fatalf("PayableStatus = %+v, want %s", status, admitted.PayableID)
	}
	if _, err := registry.PayableStatus(op, "reads-missing"); !errors.Is(err, ErrPayableDenied) {
		t.Fatalf("PayableStatus unknown err = %v, want denied", err)
	}
	if _, err := registry.PayableStatus(foreign, "reads-admit"); !errors.Is(err, ErrPayableDenied) {
		t.Fatalf("PayableStatus foreign err = %v, want denied", err)
	}
}

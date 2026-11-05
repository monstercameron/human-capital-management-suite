package workorderbilling

import (
	"errors"
	"testing"
	"time"
)

// TestBill002MoneySources covers money-dimension consumption, expense views,
// reverse conversions and inconvertible billable units.
func TestBill002MoneySources(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	clock := &bill002Clock{now: now}
	registry := NewSourceClaimRegistry(clock.Now)
	op := bill002Op()
	if err := registry.RegisterRate(op, bill002Rate()); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterRate(op, BillRateInput{Ref: "rate:USD1.50/minute", Currency: "USD", Amount: "1.50", Unit: "minute"}); err != nil {
		t.Fatal(err)
	}
	period := func() (time.Time, time.Time) {
		return time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	}

	material, err := registry.RegisterSource(op, BillSourceInput{
		Owner: "JOB007", RootID: "mat-3", Revision: 1,
		WorkOrderID: "wo-1", JobRef: "job-9",
		Kind: BillKindMaterial, UnitDimension: "money",
		ApprovedAmount: "200.00 USD", RateRef: "rate:USD90/hour",
		PricingMode: PricingTimeMaterial, Category: "LABOR",
		EvidenceRef: "mat-appr-3", ApprovedBy: "supervisor-2", ApprovedAt: "2026-10-21T15:04:05Z",
		Description: "Crew materials", ServiceDate: "2026-10-21",
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := registry.PreviewClaim(op, BillClaimInput{
		FamilyID: material.FamilyID, Revision: 1, Amount: "80.00 USD",
		PolicyID: "contract-tm", PolicyVersion: "v2", Kind: BillClaimIssue,
	})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Amount != "80.00 USD" || preview.RemainingAfterAmount != "120.00 USD" {
		t.Fatalf("money preview = %+v, want 80.00 / 120.00 left", preview)
	}
	receipt, err := registry.Claim(op, BillClaimInput{
		FamilyID: material.FamilyID, Revision: 1, Amount: "80.00 USD",
		PolicyID: "contract-tm", PolicyVersion: "v2", Kind: BillClaimIssue,
	}, "money-1")
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ClaimedAmount != "80.00 USD" || receipt.RemainingAmount != "120.00 USD" {
		t.Fatalf("money receipt = %+v, want 80.00 / 120.00 left", receipt)
	}
	if _, err := registry.Claim(op, BillClaimInput{
		FamilyID: material.FamilyID, Revision: 1, Amount: "120.01 USD",
		PolicyID: "contract-tm", PolicyVersion: "v2", Kind: BillClaimIssue,
	}, "money-over"); !errors.Is(err, ErrBillSourceShortfall) {
		t.Fatalf("money over-claim err = %v, want shortfall", err)
	}
	from, to := period()
	views, err := registry.QuerySources(op, "wo-1", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].ApprovedAmount != "200.00 USD" || views[0].ClaimedAmount != "80.00 USD" || views[0].RemainingAmount != "120.00 USD" || !views[0].Eligible {
		t.Fatalf("money views = %+v, want one eligible 200/80/120 row", views)
	}

	hold, err := registry.Claim(op, BillClaimInput{
		FamilyID: material.FamilyID, Revision: 1, Amount: "20.00 USD",
		PolicyID: "contract-tm", PolicyVersion: "v2", Kind: BillClaimHold, LeaseMinutes: 30,
	}, "money-hold")
	if err != nil {
		t.Fatal(err)
	}
	if hold.RemainingAmount != "100.00 USD" {
		t.Fatalf("money hold = %+v, want 100.00 left", hold)
	}
	if _, err := registry.CommitHold(op, hold.ClaimID, "money-commit"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ReverseIssued(op, hold.ClaimID, "credit", "adj-m", "money-reverse"); err != nil {
		t.Fatal(err)
	}
	correction, err := registry.CorrectSource(op, BillSourceInput{
		Owner: "JOB007", RootID: "mat-3", Revision: 2,
		WorkOrderID: "wo-1", JobRef: "job-9",
		Kind: BillKindMaterial, UnitDimension: "money",
		ApprovedAmount: "50.00 USD", RateRef: "rate:USD90/hour",
		PricingMode: PricingTimeMaterial, Category: "LABOR",
		EvidenceRef: "mat-appr-4", ApprovedBy: "supervisor-2", ApprovedAt: "2026-10-22T08:00:00Z",
		Description: "Crew materials (corrected)", ServiceDate: "2026-10-21",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !correction.AdjustmentRequired || correction.OverIssuedAmount != "30.00 USD" {
		t.Fatalf("money correction = %+v, want 30.00 over-issued", correction)
	}

	expense, err := registry.RegisterExpense(op, BillExpenseInput{
		Owner: "BILL010", RootID: "exp-50", WorkOrderID: "wo-1", JobRef: "job-9",
		CostAmount: "50.00 USD", RechargeRate: "0.20", RechargeEligible: true,
		ReimbursementAmount: "50.00 USD",
		EvidenceRef:         "bill010-appr-4", RevisionBinding: "bill010-rev-4",
		ApprovedBy: "controller-1", ApprovedAt: "2026-10-21T16:00:00Z",
		Description: "Parts run", ServiceDate: "2026-10-21", Category: "recharge",
	})
	if err != nil {
		t.Fatal(err)
	}
	views, err = registry.QuerySources(op, "wo-1", from, to)
	if err != nil {
		t.Fatal(err)
	}
	var expenseView *BillSourceView
	for i := range views {
		if views[i].Kind == BillKindExpense {
			expenseView = &views[i]
		}
	}
	if expenseView == nil || expenseView.ApprovedAmount != "60.00 USD" || expenseView.CostAmount != "50.00 USD" || !expenseView.Eligible {
		t.Fatalf("expense view = %+v, want eligible 60.00 customer / 50.00 cost", expenseView)
	}
	if _, err := registry.ClaimExpense(op, expense.ExpenseID, true, "contract-tm", "v2", "money-exp"); err != nil {
		t.Fatal(err)
	}
	views, err = registry.QuerySources(op, "wo-1", from, to)
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range views {
		if view.Kind == BillKindExpense && (view.Eligible || view.RemainingAmount != "0.00 USD") {
			t.Fatalf("claimed expense view = %+v, want ineligible with 0.00 left", view)
		}
	}
	trace, err := registry.FamilyTrace(op, material.FamilyID)
	if err != nil || len(trace) < 4 {
		t.Fatalf("source trace = %+v err=%v, want register/claim/commit/reverse lineage", trace, err)
	}
	expenseTrace, err := registry.FamilyTrace(op, "BILL010:exp-50")
	if err != nil || len(expenseTrace) != 2 {
		t.Fatalf("expense trace = %+v err=%v, want register/claim lineage", expenseTrace, err)
	}

	hours, err := registry.RegisterSource(op, BillSourceInput{
		Owner: "JOB007", RootID: "alloc-hr", Revision: 1,
		WorkOrderID: "wo-1", JobRef: "job-9",
		Kind: BillKindTime, UnitDimension: "time", Unit: "hour",
		ApprovedQty: "2", RateRef: "rate:USD1.50/minute",
		PricingMode: PricingUnitPrice, BillableUnit: "minute",
		EvidenceRef: "alloc-appr-hr", ApprovedBy: "supervisor-2", ApprovedAt: "2026-10-21T15:04:05Z",
		Description: "Hourly block", ServiceDate: "2026-10-21",
	})
	if err != nil {
		t.Fatal(err)
	}
	reversed, err := registry.PreviewClaim(op, BillClaimInput{
		FamilyID: hours.FamilyID, Revision: 1, Qty: "2",
		PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue,
	})
	if err != nil {
		t.Fatal(err)
	}
	if reversed.BillableQty != "120" || reversed.Amount != "180.00 USD" {
		t.Fatalf("reverse preview = %+v, want 120 minute / 180.00 USD", reversed)
	}

	eaches, err := registry.RegisterSource(op, BillSourceInput{
		Owner: "JOB007", RootID: "alloc-ea", Revision: 1,
		WorkOrderID: "wo-1", JobRef: "job-9",
		Kind: BillKindQuantity, UnitDimension: "quantity", Unit: "each",
		ApprovedQty: "4", RateRef: "rate:USD90/hour",
		PricingMode: PricingUnitPrice, BillableUnit: "hour",
		EvidenceRef: "alloc-appr-ea", ApprovedBy: "supervisor-2", ApprovedAt: "2026-10-21T15:04:05Z",
		Description: "Widgets", ServiceDate: "2026-10-21",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.PreviewClaim(op, BillClaimInput{
		FamilyID: eaches.FamilyID, Revision: 1, Qty: "2",
		PolicyID: "contract-a", PolicyVersion: "v1", Kind: BillClaimIssue,
	}); !errors.Is(err, ErrBillSourceValidation) {
		t.Fatalf("inconvertible preview err = %v, want validation refusal", err)
	}
}

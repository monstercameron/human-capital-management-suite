package payable

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
)

// TestTodo_SMB_BILL_012_Integration composes every admission basis in one
// registry â€” matched purchase bills, approved no-PO bills and approved
// contractor invoices built by the real contractor domain â€” and proves the
// global conservation identity across payments, credits, reversals and a
// versioned correction, durable through snapshot restore.
func TestTodo_SMB_BILL_012_Integration(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry, _ := bill012Registry(now)
	op := bill012Op()
	approver := bill012Approver()

	po, err := registry.AdmitMatchedBill(op, bill012Match(), "int-po")
	if err != nil {
		t.Fatal(err)
	}
	rent, err := registry.AdmitNonPOBill(op, NonPOBillInput{
		ApprovalID: "buy009-appr-3", ApprovalRevision: 2,
		SupplierID: "supplier-rent", SupplierName: "Rent Co", InvoiceNumber: "RENT-800",
		Amount: "800.00 USD", Currency: "USD", Category: "facilities-rent", Terms: "net-15",
		TaxEvidence: "tax-src-rent-1", DueDate: "2026-11-05",
		ApprovedBy: "controller-1", ApprovedAt: "2026-10-21T16:00:00Z",
	}, "int-rent")
	if err != nil {
		t.Fatal(err)
	}
	draft := bill012ContractorDraft(t, "CT42", 600, "50.00")
	contractor, err := registry.AdmitContractor(op, draft, ContractorApproval{
		SupplierID: "supplier-ct", SupplierName: "Contractor LLC",
		ApprovedBy: "controller-1", EvidenceRef: "ct-approve-1", DueDate: "2026-11-05", Terms: "net-15",
	}, "int-ct")
	if err != nil {
		t.Fatal(err)
	}

	obs := bill012Observation(t, "int-obs-1", "tenant-a", operation.ObservationApplied)
	if err := registry.RegisterObservation(op, obs); err != nil {
		t.Fatal(err)
	}
	payment, err := registry.AllocatePayment(op, PaymentInput{
		PayableID: po.PayableID, Amount: "30.00 USD", ObservationID: "int-obs-1",
	}, "int-pay")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ReverseAllocation(op, payment.AllocationID, "bank reversal ref-99", "int-rev"); err != nil {
		t.Fatal(err)
	}
	repaid, err := registry.AllocatePayment(op, PaymentInput{
		PayableID: po.PayableID, Amount: "30.00 USD",
		ManualEvidence: "manual:repay-1", Label: "reissued check",
	}, "int-repay")
	if err != nil || repaid.RemainingDue != "30.00 USD" {
		t.Fatalf("repay = %+v err=%v, want 30 due", repaid, err)
	}
	if _, err := registry.RecordCredit(op, CreditInput{
		PayableID: po.PayableID, Amount: "10.00 USD", SourceRef: "int-cm-1", Reason: "short shipment",
	}, "int-credit"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.AllocatePayment(op, PaymentInput{
		PayableID: rent.PayableID, Amount: "800.00 USD",
		ManualEvidence: "manual:rent-wire", Label: "rent wire",
	}, "int-rent-pay"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.RecordCredit(op, CreditInput{
		PayableID: contractor.PayableID, Amount: "50.00 USD", SourceRef: "int-cm-2", Reason: "rate true-up",
	}, "int-ct-credit"); err != nil {
		t.Fatal(err)
	}
	review, err := registry.AdmitAdjustment(op, po.PayableID, AdjustmentInput{
		Revision: 2, ReceivedQty: "8", BilledQty: "10", BilledTotal: "100.00 USD",
		Reason: "late receipt", EvidenceRef: "recv-8", DecidedBy: "buyer-2",
	}, "int-adj")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ApproveAdjustmentReview(approver, review.ReviewID, "int-adj-ok"); err != nil {
		t.Fatal(err)
	}

	// approved: PO 80 + rent 800 + contractor 500 = 1380.
	// paid: 30 + 800 = 830. applied credits: 10 + 50 = 60.
	// remaining: 1380 - 830 - 60 = 490.
	poExplain, err := registry.Explain(op, po.PayableID)
	if err != nil {
		t.Fatal(err)
	}
	if poExplain.ApprovedToPay != "80.00 USD" || poExplain.Paid != "30.00 USD" || poExplain.Remaining != "40.00 USD" {
		t.Fatalf("po = %+v, want approved 80 / paid 30 / due 40", poExplain)
	}
	rentExplain, err := registry.Explain(op, rent.PayableID)
	if err != nil {
		t.Fatal(err)
	}
	if rentExplain.Remaining != "0.00 USD" || rentExplain.AccountingStatus != "paid" {
		t.Fatalf("rent = %+v, want paid in full", rentExplain)
	}
	ctExplain, err := registry.Explain(op, contractor.PayableID)
	if err != nil {
		t.Fatal(err)
	}
	if ctExplain.Remaining != "450.00 USD" || ctExplain.Credits != "50.00 USD" {
		t.Fatalf("contractor = %+v, want due 450 / credits 50", ctExplain)
	}
	sum := func(texts ...string) int {
		total := 0
		for _, text := range texts {
			n, _ := bill012Split(t, text)
			total += n
		}
		return total
	}
	approved := sum(poExplain.ApprovedToPay, rentExplain.ApprovedToPay, ctExplain.ApprovedToPay)
	paid := sum(poExplain.Paid, rentExplain.Paid, ctExplain.Paid)
	credits := sum(poExplain.Credits, rentExplain.Credits, ctExplain.Credits)
	due := sum(poExplain.Remaining, rentExplain.Remaining, ctExplain.Remaining)
	if approved != 1380 || paid != 830 || credits != 60 || due != 490 || paid+credits+due != approved {
		t.Fatalf("global identity broken: approved=%d paid=%d credits=%d due=%d", approved, paid, credits, due)
	}

	data, err := registry.Export()
	if err != nil {
		t.Fatal(err)
	}
	restored := NewPayableRegistry(func() time.Time { return now })
	if err := restored.Import(data); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{po.PayableID, rent.PayableID, contractor.PayableID} {
		before, err := registry.Explain(op, id)
		if err != nil {
			t.Fatal(err)
		}
		after, err := restored.Explain(op, id)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("restore drift for %s: %+v vs %+v", id, before, after)
		}
	}
	var _ = errors.Is
	var _ = strings.Contains
}

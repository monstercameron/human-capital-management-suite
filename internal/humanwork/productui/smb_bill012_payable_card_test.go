package productui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/payable"
)

func smbBill012CardFixture(t *testing.T) (payable.Payable, payable.PayableProjection) {
	t.Helper()
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry := payable.NewPayableRegistry(func() time.Time { return now })
	op := payable.PayOp{TenantID: "tenant-a", LegalEntityID: "le-1", ActorID: "payer-1", PayableAllowed: true, ApproveAllowed: true}
	admitted, err := registry.AdmitMatchedBill(op, payable.MatchBillInput{
		MatchID: "match-7", SupplierID: "supplier-acme", SupplierName: "Acme Supply",
		InvoiceNumber: "ACME-1001", InvoiceRevision: 1,
		OrderedQty: "10", UnitPrice: "10.00 USD", ReceivedQty: "6", BilledQty: "10",
		BilledTotal: "100.00 USD", DisputedQty: "0", Currency: "USD",
		DueDate: "2026-11-20", Terms: "net-30", Cutoff: "2026-10-21",
		DecidedBy: "buyer-2", DecidedAt: "2026-10-21T15:04:05Z",
	}, "card-admit")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.RecordCredit(op, payable.CreditInput{
		PayableID: admitted.PayableID, Amount: "10.00 USD", SourceRef: "credit-1", Reason: "damaged unit",
	}, "card-credit"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.AllocatePayment(op, payable.PaymentInput{
		PayableID: admitted.PayableID, Amount: "5.00 USD",
		ManualEvidence: "manual:check-221", Label: "manual check",
	}, "card-pay"); err != nil {
		t.Fatal(err)
	}
	obligation, err := registry.GetPayable(op, admitted.PayableID)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := registry.Explain(op, admitted.PayableID)
	if err != nil {
		t.Fatal(err)
	}
	return obligation, projection
}

// TestTodo_SMB_BILL_012_Browser renders the payables drawer the way the
// obligation surface serves it: supplier and invoice identity, admission
// basis with matching status and terms, exact money rows, source links, the
// blocked-state notice and the policy-gated payment form, localized across
// en-US, de-DE and ar RTL.
func TestTodo_SMB_BILL_012_Browser(t *testing.T) {
	obligation, projection := smbBill012CardFixture(t)

	markup, err := ui.RenderToString(SMBPayableObligationCard(obligation, projection, true, "", "/workspace/app/payables/suppliers/supplier-acme", "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Payable obligation " + obligation.Number,
		"Acme Supply", "ACME-1001", "PO_MATCHED", "Three-way matched",
		"2026-11-20", "net-30",
		"Total billed", "100.00 USD", "Approved to pay", "60.00 USD",
		"Unmatched", "40.00 USD", "Credits applied", "10.00 USD",
		"Paid", "5.00 USD", "Remaining due", "45.00 USD",
		"Source links", "BUY007:match-7", "Status: open — due",
		"/workspace/app/payables/suppliers/supplier-acme",
		"Record payment", "Payment evidence (required)", "Record this payment",
		"type=\"submit\"", "tabIndex=\"-1\"",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("payable card missing %q", want)
		}
	}

	blocked := obligation
	blocked.Status = payable.PayableDiscrepancy
	blocked.MatchingStatus = "same number with differing amount"
	denied, err := ui.RenderToString(SMBPayableObligationCard(blocked, payable.PayableProjection{}, false, "settle the discrepancy first", "", "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"No source links.", "Payment blocked: same number with differing amount",
		"Payment blocked: settle the discrepancy first",
	} {
		if !strings.Contains(denied, want) {
			t.Errorf("blocked card missing %q", want)
		}
	}

	german, err := ui.RenderToString(SMBPayableObligationCard(obligation, projection, false, "Freigabe fehlt", "", "de-DE"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Zahlungsverpflichtung", "Lieferant", "Zur Zahlung freigegeben", "Offener Restbetrag", "Herkunftsnachweise", "Zahlung blockiert"} {
		if !strings.Contains(german, want) {
			t.Errorf("German payable card missing %q", want)
		}
	}

	rtl, err := ui.RenderToString(SMBPayableObligationCard(obligation, projection, true, "", "", "ar"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dir=\"rtl\"", "lang=\"ar\"", "التزام الدفع", "المتبقي المستحق", "تسجيل هذه الدفعة"} {
		if !strings.Contains(rtl, want) {
			t.Errorf("Arabic payable card missing %q", want)
		}
	}
}

package productui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderbilling"
)

type bill008CardLedger struct{}

func (bill008CardLedger) Post(workorderbilling.ReceivableEvent) error { return nil }

func (bill008CardLedger) Explain(string, string) (workorderbilling.ReceivableProjection, error) {
	return workorderbilling.ReceivableProjection{}, nil
}

func smbBill008CardFixture(t *testing.T) (workorderbilling.AdjustInvoice, *workorderbilling.AdjustPreview, []workorderbilling.Adjustment) {
	t.Helper()
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry := workorderbilling.NewAdjustmentRegistry(func() time.Time { return now })
	registry.BindLedger("tenant-a", bill008CardLedger{})
	op := workorderbilling.AdjustOp{TenantID: "tenant-a", LegalEntityID: "le-1", ActorID: "adjuster-1", AdjustAllowed: true, ApproveAllowed: true, VoidAllowed: true}
	approver := op
	approver.ActorID = "supervisor-2"
	invoice := workorderbilling.AdjustInvoice{
		InvoiceID: "inv-100", CustomerID: "cust-1", Currency: "USD", Number: "INV-0100",
		Status: "issued", Revision: 3, Digest: "inv-digest-1", IssuedAt: "2026-10-20T10:00:00Z",
		Lines: []workorderbilling.AdjustInvoiceLine{
			{LineID: "l1", Description: "Widget install", Qty: "1", UnitPrice: "90.00 USD", Amount: "90.00 USD", TaxAmount: "10.00 USD"},
		},
		Total: "100.00 USD", TaxTotal: "10.00 USD",
	}
	if err := registry.BindInvoice(op, invoice); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterReceipt(op, workorderbilling.AdjustReceiptInput{ReceiptID: "rcpt-1", InvoiceID: "inv-100", CustomerID: "cust-1", Amount: "100.00 USD", Provenance: "bank-statement-9", At: "2026-10-21T09:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	flow := func(input workorderbilling.AdjustDraftInput, key string) workorderbilling.Adjustment {
		t.Helper()
		draft, err := registry.DraftAdjustment(op, input, key+"-draft")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := registry.SubmitAdjustment(op, draft.AdjustmentID, key+"-submit"); err != nil {
			t.Fatal(err)
		}
		if _, err := registry.ApproveAdjustment(approver, draft.AdjustmentID, key+"-approve"); err != nil {
			t.Fatal(err)
		}
		issued, err := registry.IssueAdjustment(op, draft.AdjustmentID, key+"-issue")
		if err != nil {
			t.Fatal(err)
		}
		return issued
	}
	issued := flow(workorderbilling.AdjustDraftInput{
		InvoiceID: "inv-100", Kind: "credit",
		Lines:  []workorderbilling.AdjustLineInput{{LineID: "l1", Amount: "22.50 USD", TaxAmount: "2.50 USD"}},
		Reason: "short install", EvidenceRef: "return-9",
	}, "card")
	preview, err := registry.PreviewAdjustment(op, workorderbilling.AdjustDraftInput{
		InvoiceID: "inv-100", Kind: "credit",
		Lines:  []workorderbilling.AdjustLineInput{{LineID: "l1", Amount: "5.00 USD", TaxAmount: "0.50 USD"}},
		Reason: "top-up", EvidenceRef: "return-10",
	})
	if err != nil {
		t.Fatal(err)
	}
	return invoice, &preview, []workorderbilling.Adjustment{issued}
}

// TestTodo_SMB_BILL_008_Browser renders the Invoice Actions adjustment drawer
// the way the billing surface serves it: line selection with original,
// previously credited, available and proposed treatment, the required reason,
// the pinned-tax disclosure, the issue review with the original link, the
// adjustment chain and the policy-gated void section, localized across
// en-US, de-DE and ar RTL.
func TestTodo_SMB_BILL_008_Browser(t *testing.T) {
	invoice, preview, chain := smbBill008CardFixture(t)

	markup, err := ui.RenderToString(SMBBillAdjustmentCard(invoice, preview, chain, "/workspace/app/billing/invoices/inv-100", false, "paid invoices credit instead of voiding", "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Create credit or adjustment INV-0100", "Lines",
		"Widget install", "90.00 USD + 10.00 USD", "22.50 USD + 2.50 USD",
		"67.50 USD + 7.50 USD", "5.00 USD + 0.50 USD",
		"Reason (required)", "original treatment", "Review: 5.50 USD",
		"View original", "/workspace/app/billing/invoices/inv-100",
		"Adjustment chain", "CM-0001 credit 25.00 USD issued",
		"Void unavailable: paid invoices credit instead of voiding",
		"type=\"checkbox\"", "tabIndex=\"-1\"",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("adjustment card missing %q", want)
		}
	}

	eligible, err := ui.RenderToString(SMBBillAdjustmentCard(invoice, nil, nil, "", true, "", "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Select lines to preview", "No adjustments yet.", "Void reason (required)", "Void this invoice"} {
		if !strings.Contains(eligible, want) {
			t.Errorf("eligible card missing %q", want)
		}
	}

	german, err := ui.RenderToString(SMBBillAdjustmentCard(invoice, preview, chain, "", false, "unbezahlt erforderlich", "de-DE"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Gutschrift oder Korrektur erstellen", "Bereits gutgeschrieben", "Verfügbar", "Korrekturverlauf", "Storno nicht verfügbar"} {
		if !strings.Contains(german, want) {
			t.Errorf("German adjustment card missing %q", want)
		}
	}

	rtl, err := ui.RenderToString(SMBBillAdjustmentCard(invoice, preview, chain, "", true, "", "ar"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dir=\"rtl\"", "lang=\"ar\"", "إنشاء إشعار دائن أو تسوية", "المتاح", "إلغاء هذه الفاتورة"} {
		if !strings.Contains(rtl, want) {
			t.Errorf("Arabic adjustment card missing %q", want)
		}
	}
}

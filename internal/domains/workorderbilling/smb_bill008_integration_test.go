package workorderbilling

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func bill008IntDec(t *testing.T, text string, scale int32) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, scale, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func bill008IntMoney(t *testing.T, text string) values.Money {
	t.Helper()
	m, err := values.NewMoney(text, "USD", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func bill008StoreInvoice(t *testing.T, store *InvoiceStore, id, amount, issueDate string) CustomerInvoice {
	t.Helper()
	caps := ActorCaps{CanSubmit: true, CanApprove: true, CanIssue: true, CanVoid: true}
	draft, _, err := store.Draft("tenant-a", "clerk-1", "draft-"+id, DraftInvoiceRequest{
		InvoiceID: id, IssuerEntity: "acme-llc", IssuerSeries: "ACME-2026", CustomerID: "cust-1",
		Authority: "local", Currency: "USD",
		Source:       InvoiceSource{Kind: InvoiceSourceManual, Manual: ManualSource{RateRef: "rate-card-2026", PolicyRef: "policy-std", PolicyVersion: "v3", Evidence: "evidence:quote-accepted-9"}},
		Lines:        []InvoiceLine{{ID: "l1", Description: "Widget install", Quantity: bill008IntDec(t, "1", 0), UnitPrice: bill008IntMoney(t, amount), Amount: bill008IntMoney(t, amount), RateRef: "rate-card-2026", PolicyRef: "policy-std"}},
		TaxPolicyRef: "tax:standard-v1", IssueDate: issueDate, DueDate: "2026-11-20",
	})
	if err != nil {
		t.Fatal(err)
	}
	submitted, _, err := store.SubmitForApproval("tenant-a", "clerk-1", caps, draft.ID, draft.Revision, "submit-"+id, issueDate+"T09:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	ready, _, err := store.Approve("tenant-a", "manager-1", caps, draft.ID, submitted.Revision, "approve-"+id, issueDate+"T10:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	issued, _, err := store.Issue("tenant-a", "manager-1", caps, draft.ID, ready.Revision, "issue-"+id, issueDate+"T11:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	return issued
}

// TestTodo_SMB_BILL_008_Integration composes the real owner stack â€” invoice
// store, receivable ledger, adjustment registry and memo documents: a USD 100
// store invoice paid USD 100 and credited USD 25 settles USD 75 with USD 25
// of customer credit on both the registry projection and the ledger
// explanation, while the issued invoice bytes never move.
func TestTodo_SMB_BILL_008_Integration(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	store := NewInvoiceStore()
	ledger := NewReceivableLedger("tenant-a")
	registry := NewAdjustmentRegistry(func() time.Time { return now })
	registry.BindLedger("tenant-a", ledger)
	port := InvoiceStoreAdjustPort{Store: store, Caps: ActorCaps{CanSubmit: true, CanApprove: true, CanIssue: true, CanVoid: true}}
	registry.BindInvoicePort(port)
	op := bill008Op()
	approver := bill008Approver()

	issued := bill008StoreInvoice(t, store, "inv-100", "100.00", "2026-10-20")
	if issued.Status != InvoiceIssued || issued.InvoiceNumber == "" {
		t.Fatalf("store invoice = %+v, want ISSUED with number", issued)
	}
	if err := ledger.Register(issued); err != nil {
		t.Fatal(err)
	}
	snap, err := port.GetInvoice(op, "inv-100")
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.BindInvoice(op, snap); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterReceipt(op, bill008Receipt()); err != nil {
		t.Fatal(err)
	}

	creditInput := AdjustDraftInput{
		InvoiceID: "inv-100", Kind: "credit",
		Lines:  []AdjustLineInput{{LineID: "l1", Amount: "25.00 USD", TaxAmount: "0.00 USD"}},
		Reason: "short install", EvidenceRef: "return-9",
	}
	credit := bill008Issue(t, registry, op, approver, creditInput, "int")
	if credit.Number != "CM-0001" || !strings.HasPrefix(credit.DocumentDigest, "sha256:") {
		t.Fatalf("credit = %+v, want CM-0001 with memo digest", credit)
	}
	allocation, err := registry.AllocateCredit(op, credit.AdjustmentID, "int-alloc")
	if err != nil {
		t.Fatal(err)
	}
	if allocation.SettledAmount != "75.00 USD" || allocation.CustomerCreditAmount != "25.00 USD" {
		t.Fatalf("allocation = %+v, want settled 75 / credit 25", allocation)
	}
	explain, err := ledger.Explain("inv-100", "2026-10-22")
	if err != nil {
		t.Fatal(err)
	}
	if explain.Obligation.String() != "100.00 USD" || explain.Credits.String() != "25.00 USD" ||
		explain.Receipts.String() != "100.00 USD" || explain.Remaining.String() != "0.00 USD" ||
		explain.UnappliedCredit.String() != "25.00 USD" {
		t.Fatalf("ledger = %+v, want 100/25/100/0/25", explain)
	}

	debit := bill008Issue(t, registry, op, approver, AdjustDraftInput{
		InvoiceID: "inv-100", Kind: "debit",
		Lines:  []AdjustLineInput{{LineID: "l1", Amount: "10.00 USD", TaxAmount: "0.00 USD"}},
		Reason: "extra visit", EvidenceRef: "visit-3", TermsRef: "terms-net30-accepted",
	}, "int-debit")
	if debit.Number != "DM-0001" || debit.SettledAmount != "10.00 USD" {
		t.Fatalf("debit = %+v, want DM-0001 settled 10", debit)
	}

	unpaid := bill008StoreInvoice(t, store, "inv-200", "50.00", "2026-10-20")
	if err := ledger.Register(unpaid); err != nil {
		t.Fatal(err)
	}
	unpaidSnap, err := port.GetInvoice(op, "inv-200")
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.BindInvoice(op, unpaidSnap); err != nil {
		t.Fatal(err)
	}
	voided, err := registry.VoidInvoice(op, "inv-200", "duplicate billing", "void-ev-1", "int-void")
	if err != nil {
		t.Fatal(err)
	}
	if voided.Number != unpaid.InvoiceNumber || voided.Reason == "" || voided.At == "" {
		t.Fatalf("void = %+v, want number/reason/time preserved", voided)
	}
	stored, err := store.Get("tenant-a", "inv-200")
	if err != nil || stored.Status != InvoiceVoided {
		t.Fatalf("stored invoice = %+v err=%v, want VOIDED through the owner", stored, err)
	}
	voidExplain, err := ledger.Explain("inv-200", "2026-10-22")
	if err != nil {
		t.Fatal(err)
	}
	if voidExplain.Remaining.String() != "0.00 USD" || voidExplain.Credits.String() != "50.00 USD" {
		t.Fatalf("void ledger = %+v, want extinguished 50.00", voidExplain)
	}
	if _, err := registry.VoidInvoice(op, "inv-100", "paid void", "void-ev-2", "int-void-paid"); !errors.Is(err, ErrAdjustState) {
		t.Fatalf("paid void err = %v, want state refusal", err)
	}
	current, err := store.Get("tenant-a", "inv-100")
	if err != nil || current.Digest != issued.Digest {
		t.Fatalf("original digest moved: %q vs %q err=%v", current.Digest, issued.Digest, err)
	}

	if err := registry.SetAdjustPolicy(op, AdjustPolicy{VoidWindowDays: 1}); err != nil {
		t.Fatal(err)
	}
	old := bill008StoreInvoice(t, store, "inv-old", "20.00", "2026-01-05")
	if err := ledger.Register(old); err != nil {
		t.Fatal(err)
	}
	oldSnap, err := port.GetInvoice(op, "inv-old")
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.BindInvoice(op, oldSnap); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.VoidInvoice(op, "inv-old", "stale", "void-ev-3", "int-void-old"); !errors.Is(err, ErrAdjustState) {
		t.Fatalf("window void err = %v, want state refusal", err)
	}
}

// TestBill008BindIdempotency pins snapshot rebinding, receipt recording
// edges and quantity-based credit issuance.
func TestBill008BindIdempotency(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry, _ := bill008Registry(now)
	op := bill008Op()
	approver := bill008Approver()
	if err := registry.BindInvoice(op, bill008Invoice()); err != nil {
		t.Fatal(err)
	}
	if err := registry.BindInvoice(op, bill008Invoice()); err != nil {
		t.Fatalf("identical rebind err = %v, want idempotent ok", err)
	}
	changed := bill008Invoice()
	changed.Digest = "inv-digest-other"
	if err := registry.BindInvoice(op, changed); !errors.Is(err, ErrAdjustConflict) {
		t.Fatalf("changed rebind err = %v, want conflict", err)
	}
	lower := bill008Invoice()
	lower.Revision = 2
	if err := registry.BindInvoice(op, lower); !errors.Is(err, ErrAdjustStale) {
		t.Fatalf("lower rebind err = %v, want stale", err)
	}

	if err := registry.RegisterReceipt(op, bill008Receipt()); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterReceipt(op, bill008Receipt()); err != nil {
		t.Fatalf("identical receipt err = %v, want idempotent ok", err)
	}
	moved := bill008Receipt()
	moved.Amount = "99.00 USD"
	if err := registry.RegisterReceipt(op, moved); !errors.Is(err, ErrAdjustConflict) {
		t.Fatalf("changed receipt err = %v, want conflict", err)
	}
	foreign := bill008Receipt()
	foreign.ReceiptID = "rcpt-foreign"
	foreign.CustomerID = "cust-9"
	if err := registry.RegisterReceipt(op, foreign); !errors.Is(err, ErrAdjustValidation) {
		t.Fatalf("foreign receipt err = %v, want validation refusal", err)
	}
	missing := bill008Receipt()
	missing.ReceiptID = "rcpt-missing"
	missing.InvoiceID = "inv-missing"
	if err := registry.RegisterReceipt(op, missing); !errors.Is(err, ErrAdjustDenied) {
		t.Fatalf("orphan receipt err = %v, want neutral denied", err)
	}

	multi := bill008Invoice()
	multi.InvoiceID = "inv-qty"
	multi.Number = "INV-QTY"
	multi.Digest = "inv-digest-qty"
	multi.Lines = []AdjustInvoiceLine{
		{LineID: "l1", Description: "Widget batch", Qty: "10", UnitPrice: "9.00 USD", Amount: "90.00 USD", TaxAmount: "10.00 USD"},
	}
	if err := registry.BindInvoice(op, multi); err != nil {
		t.Fatal(err)
	}
	issued := bill008Issue(t, registry, op, approver, AdjustDraftInput{
		InvoiceID: "inv-qty", Kind: "credit",
		Lines:  []AdjustLineInput{{LineID: "l1", Qty: "4", TaxAmount: "4.00 USD"}},
		Reason: "short batch", EvidenceRef: "return-qty",
	}, "qty")
	if issued.TotalAmount != "40.00 USD" {
		t.Fatalf("qty credit = %+v, want 36.00 + 4.00 = 40.00", issued)
	}
	projection, err := registry.Projection(op, "inv-qty")
	if err != nil {
		t.Fatal(err)
	}
	if projection.Credits != "40.00 USD" || projection.Remaining != "60.00 USD" {
		t.Fatalf("qty projection = %+v, want 40 credited / 60 left of unpaid 100", projection)
	}
}

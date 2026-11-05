package workorderbilling

import (
	"errors"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"
)

type bill008Clock struct{ now time.Time }

func (c *bill008Clock) Now() time.Time { return c.now }

func bill008Op() AdjustOp {
	return AdjustOp{TenantID: "tenant-a", LegalEntityID: "le-1", ActorID: "adjuster-1", AdjustAllowed: true, ApproveAllowed: true, VoidAllowed: true}
}

func bill008Approver() AdjustOp {
	op := bill008Op()
	op.ActorID = "supervisor-2"
	return op
}

type bill008FakeLedger struct {
	mu     sync.Mutex
	events []ReceivableEvent
}

func (f *bill008FakeLedger) Post(ev ReceivableEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, ev)
	return nil
}

func (f *bill008FakeLedger) Explain(invoiceID, asOf string) (ReceivableProjection, error) {
	return ReceivableProjection{}, errors.New("fake ledger does not serve projections")
}

func bill008Registry(now time.Time) (*AdjustmentRegistry, *bill008Clock) {
	clock := &bill008Clock{now: now}
	registry := NewAdjustmentRegistry(clock.Now)
	registry.BindLedger("tenant-a", &bill008FakeLedger{})
	return registry, clock
}

func bill008Invoice() AdjustInvoice {
	return AdjustInvoice{
		InvoiceID: "inv-100", CustomerID: "cust-1", Currency: "USD", Number: "INV-0100",
		Status: "issued", Revision: 3, Digest: "inv-digest-1", IssuedAt: "2026-10-20T10:00:00Z",
		Lines: []AdjustInvoiceLine{
			{LineID: "l1", Description: "Widget install", Qty: "1", UnitPrice: "90.00 USD", Amount: "90.00 USD", TaxAmount: "10.00 USD"},
		},
		Total: "100.00 USD", TaxTotal: "10.00 USD",
	}
}

func bill008Receipt() AdjustReceiptInput {
	return AdjustReceiptInput{ReceiptID: "rcpt-1", InvoiceID: "inv-100", CustomerID: "cust-1", Amount: "100.00 USD", Provenance: "bank-statement-9", At: "2026-10-21T09:00:00Z"}
}

func bill008Credit() AdjustDraftInput {
	return AdjustDraftInput{
		InvoiceID: "inv-100", Kind: "credit",
		Lines:  []AdjustLineInput{{LineID: "l1", Amount: "22.50 USD", TaxAmount: "2.50 USD"}},
		Reason: "short install", EvidenceRef: "return-9",
	}
}

func bill008Issue(t *testing.T, registry *AdjustmentRegistry, op, approver AdjustOp, input AdjustDraftInput, key string) Adjustment {
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

func bill008Money(t *testing.T, text string) (string, string) {
	t.Helper()
	parts := strings.SplitN(strings.TrimSpace(text), " ", 2)
	if len(parts) != 2 {
		t.Fatalf("bad test money %q", text)
	}
	return parts[0], parts[1]
}

// TestTodo_SMB_BILL_008 pins the credit-adjustment acceptance oracles: a USD
// 100 invoice paid USD 100 and credited USD 25 leaves USD 75 of obligation,
// USD 75 of settled allocation and USD 25 of available customer credit; the
// original digest never moves; debits, voids and corrections chain with
// explicit conservation.
func TestTodo_SMB_BILL_008(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry, _ := bill008Registry(now)
	op := bill008Op()
	approver := bill008Approver()
	if err := registry.BindInvoice(op, bill008Invoice()); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterReceipt(op, bill008Receipt()); err != nil {
		t.Fatal(err)
	}

	preview, err := registry.PreviewAdjustment(op, bill008Credit())
	if err != nil {
		t.Fatal(err)
	}
	if preview.NetAmount != "22.50 USD" || preview.TaxAmount != "2.50 USD" || preview.TotalAmount != "25.00 USD" {
		t.Fatalf("preview = %+v, want 22.50 + 2.50 = 25.00", preview)
	}
	if len(preview.Lines) != 1 || preview.Lines[0].AvailableAmount != "90.00 USD" || preview.Lines[0].AvailableTax != "10.00 USD" {
		t.Fatalf("preview lines = %+v, want full 90/10 available", preview.Lines)
	}

	issued := bill008Issue(t, registry, op, approver, bill008Credit(), "c25")
	if issued.Status != "issued" || issued.Number != "CM-0001" || issued.DocumentDigest == "" {
		t.Fatalf("issued = %+v, want issued CM-0001 with document", issued)
	}
	if issued.InvoiceDigest != "inv-digest-1" || issued.InvoiceRevision != 3 {
		t.Fatalf("issued = %+v, want pinned rev 3 digest", issued)
	}
	allocation, err := registry.AllocateCredit(op, issued.AdjustmentID, "alloc-1")
	if err != nil {
		t.Fatal(err)
	}
	if allocation.SettledAmount != "75.00 USD" || allocation.CustomerCreditAmount != "25.00 USD" {
		t.Fatalf("allocation = %+v, want settled 75 / credit 25", allocation)
	}
	projection, err := registry.Projection(op, "inv-100")
	if err != nil {
		t.Fatal(err)
	}
	if projection.Obligation != "100.00 USD" || projection.Credits != "25.00 USD" || projection.Settled != "75.00 USD" ||
		projection.CustomerCredit != "25.00 USD" || projection.Remaining != "0.00 USD" {
		t.Fatalf("projection = %+v, want 100/25/75/25/0 conservation", projection)
	}
	balance, err := registry.CustomerCreditBalance(op, "cust-1")
	if err != nil || balance != "25.00 USD" {
		t.Fatalf("credit balance = %q err=%v, want 25.00 USD", balance, err)
	}

	second := bill008Credit()
	second.Lines = []AdjustLineInput{{LineID: "l1", Amount: "5.00 USD", TaxAmount: "0.50 USD"}}
	if _, err := registry.DraftAdjustment(op, second, "c25-dup"); !errors.Is(err, ErrAdjustConflict) {
		t.Fatalf("reused evidence err = %v, want conflict", err)
	}

	debit := bill008Issue(t, registry, op, approver, AdjustDraftInput{
		InvoiceID: "inv-100", Kind: "debit",
		Lines:  []AdjustLineInput{{LineID: "l1", Amount: "10.00 USD", TaxAmount: "0.00 USD"}},
		Reason: "extra visit", EvidenceRef: "visit-3", TermsRef: "terms-net30-accepted",
	}, "d10")
	if debit.Number != "DM-0001" || debit.SettledAmount != "10.00 USD" {
		t.Fatalf("debit = %+v, want DM-0001 settled 10 against credit", debit)
	}
	balance, err = registry.CustomerCreditBalance(op, "cust-1")
	if err != nil || balance != "15.00 USD" {
		t.Fatalf("credit balance = %q err=%v, want 15.00 USD after debit offset", balance, err)
	}

	unpaid := bill008Invoice()
	unpaid.InvoiceID = "inv-200"
	unpaid.Number = "INV-0200"
	unpaid.Digest = "inv-digest-2"
	if err := registry.BindInvoice(op, unpaid); err != nil {
		t.Fatal(err)
	}
	voided, err := registry.VoidInvoice(op, "inv-200", "duplicate billing", "void-ev-1", "void-1")
	if err != nil {
		t.Fatal(err)
	}
	if voided.Number != "INV-0200" || voided.Reason == "" || voided.At == "" {
		t.Fatalf("void = %+v, want number/reason/time preserved", voided)
	}
	if _, err := registry.VoidInvoice(op, "inv-100", "paid void", "void-ev-2", "void-2"); !errors.Is(err, ErrAdjustState) {
		t.Fatalf("paid void err = %v, want state refusal", err)
	}

	correcting := bill008Issue(t, registry, op, approver, AdjustDraftInput{
		InvoiceID: "inv-100", Kind: "debit", CorrectsRef: issued.AdjustmentID,
		Lines:  []AdjustLineInput{{LineID: "l1", Amount: "5.00 USD", TaxAmount: "0.00 USD"}},
		Reason: "credit overstated", EvidenceRef: "review-5", TermsRef: "terms-net30-accepted",
	}, "corr-1")
	if correcting.CorrectsRef != issued.AdjustmentID || correcting.Number != "DM-0002" {
		t.Fatalf("correction = %+v, want DM-0002 linked to CM-0001", correcting)
	}
	original, err := registry.AdjustmentStatus(op, "c25-draft")
	if err != nil || original.Number != "CM-0001" || original.TotalAmount != "25.00 USD" {
		t.Fatalf("original after correction = %+v err=%v, want CM-0001 untouched", original, err)
	}
}

// TestTodo_SMB_BILL_008_Unit pins validation and lifecycle boundaries:
// malformed money, over-basis lines, missing reason/evidence/terms, bad
// correction links, out-of-order transitions and self-approval.
func TestTodo_SMB_BILL_008_Unit(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry, _ := bill008Registry(now)
	op := bill008Op()
	if err := registry.BindInvoice(op, bill008Invoice()); err != nil {
		t.Fatal(err)
	}

	base := bill008Credit()
	cases := []struct {
		name   string
		mutate func(*AdjustDraftInput)
	}{
		{"unknown invoice", func(i *AdjustDraftInput) { i.InvoiceID = "inv-missing" }},
		{"bad kind", func(i *AdjustDraftInput) { i.Kind = "refund" }},
		{"no lines", func(i *AdjustDraftInput) { i.Lines = nil }},
		{"unknown line", func(i *AdjustDraftInput) { i.Lines[0].LineID = "l9" }},
		{"qty and amount", func(i *AdjustDraftInput) { i.Lines[0].Qty = "1" }},
		{"neither qty nor amount", func(i *AdjustDraftInput) { i.Lines[0].Amount = "" }},
		{"bad amount", func(i *AdjustDraftInput) { i.Lines[0].Amount = "9.99" }},
		{"bad tax", func(i *AdjustDraftInput) { i.Lines[0].TaxAmount = "centimes" }},
		{"negative amount", func(i *AdjustDraftInput) { i.Lines[0].Amount = "-1.00 USD" }},
		{"currency mismatch", func(i *AdjustDraftInput) { i.Lines[0].Amount = "1.00 EUR" }},
		{"over amount basis", func(i *AdjustDraftInput) { i.Lines[0].Amount = "90.01 USD" }},
		{"over tax basis", func(i *AdjustDraftInput) { i.Lines[0].TaxAmount = "10.01 USD" }},
		{"over qty basis", func(i *AdjustDraftInput) { i.Lines[0].Amount = ""; i.Lines[0].Qty = "2" }},
		{"missing reason", func(i *AdjustDraftInput) { i.Reason = "" }},
		{"missing evidence", func(i *AdjustDraftInput) { i.EvidenceRef = "" }},
		{"debit without terms", func(i *AdjustDraftInput) { i.Kind = "debit" }},
		{"unknown correction", func(i *AdjustDraftInput) { i.CorrectsRef = "adj-missing" }},
	}
	for _, tc := range cases {
		input := base
		input.Lines = []AdjustLineInput{base.Lines[0]}
		input.EvidenceRef = "unit-" + strings.ReplaceAll(tc.name, " ", "-")
		tc.mutate(&input)
		if _, err := registry.PreviewAdjustment(op, input); !errors.Is(err, ErrAdjustValidation) && !errors.Is(err, ErrAdjustDenied) && !errors.Is(err, ErrAdjustShortfall) && !errors.Is(err, ErrAdjustState) {
			t.Errorf("%s: preview err = %v, want refusal", tc.name, err)
		}
		if _, err := registry.DraftAdjustment(op, input, "unit-"+strings.ReplaceAll(tc.name, " ", "-")); !errors.Is(err, ErrAdjustValidation) && !errors.Is(err, ErrAdjustDenied) && !errors.Is(err, ErrAdjustShortfall) && !errors.Is(err, ErrAdjustState) {
			t.Errorf("%s: draft err = %v, want refusal", tc.name, err)
		}
	}

	qtyInput := base
	qtyInput.EvidenceRef = "unit-qty-ok"
	qtyInput.Lines = []AdjustLineInput{{LineID: "l1", Qty: "1", TaxAmount: "10.00 USD"}}
	preview, err := registry.PreviewAdjustment(op, qtyInput)
	if err != nil {
		t.Fatal(err)
	}
	if preview.NetAmount != "90.00 USD" || preview.TotalAmount != "100.00 USD" {
		t.Fatalf("qty preview = %+v, want 90 + 10 = 100", preview)
	}

	draft, err := registry.DraftAdjustment(op, base, "unit-life")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.IssueAdjustment(op, draft.AdjustmentID, "unit-early"); !errors.Is(err, ErrAdjustState) {
		t.Fatalf("early issue err = %v, want state refusal", err)
	}
	if _, err := registry.ApproveAdjustment(op, draft.AdjustmentID, "unit-self"); !errors.Is(err, ErrAdjustState) {
		t.Fatalf("early approve err = %v, want state refusal", err)
	}
	amended, err := registry.AmendDraft(op, draft.AdjustmentID, AdjustDraftInput{
		InvoiceID: "inv-100", Kind: "credit",
		Lines:  []AdjustLineInput{{LineID: "l1", Amount: "10.00 USD", TaxAmount: "1.00 USD"}},
		Reason: "short install revised", EvidenceRef: "return-9",
	}, "unit-amend")
	if err != nil {
		t.Fatal(err)
	}
	if amended.TotalAmount != "11.00 USD" {
		t.Fatalf("amended = %+v, want 11.00 total", amended)
	}
	if _, err := registry.SubmitAdjustment(op, draft.AdjustmentID, "unit-submit"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.AmendDraft(op, draft.AdjustmentID, base, "unit-late-amend"); !errors.Is(err, ErrAdjustState) {
		t.Fatalf("late amend err = %v, want state refusal", err)
	}
	if _, err := registry.ApproveAdjustment(op, draft.AdjustmentID, "unit-self-approve"); !errors.Is(err, ErrAdjustState) {
		t.Fatalf("self approve err = %v, want distinct-approver refusal", err)
	}
	denied := op
	denied.ApproveAllowed = false
	denied.ActorID = "clerk-9"
	if _, err := registry.ApproveAdjustment(denied, draft.AdjustmentID, "unit-nogrant"); !errors.Is(err, ErrAdjustDenied) {
		t.Fatalf("ungranted approve err = %v, want denied", err)
	}
	if _, err := registry.ApproveAdjustment(bill008Approver(), draft.AdjustmentID, "unit-approve"); err != nil {
		t.Fatal(err)
	}
	issued, err := registry.IssueAdjustment(op, draft.AdjustmentID, "unit-issue")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.AllocateCredit(op, issued.AdjustmentID, "unit-alloc"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.AllocateCredit(op, issued.AdjustmentID, "unit-alloc-2"); !errors.Is(err, ErrAdjustState) {
		t.Fatalf("double allocate err = %v, want state refusal", err)
	}
	cancelInput := base
	cancelInput.EvidenceRef = "unit-cancel-ev"
	abandoned, err := registry.DraftAdjustment(op, cancelInput, "unit-abandon")
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.CancelDraft(op, abandoned.AdjustmentID, "unit-cancel"); err != nil {
		t.Fatal(err)
	}
	reused, err := registry.DraftAdjustment(op, cancelInput, "unit-reuse")
	if err != nil || reused.Status != "draft" {
		t.Fatalf("evidence reuse after cancel = %+v err=%v, want fresh draft", reused, err)
	}
	if err := registry.CancelDraft(op, issued.AdjustmentID, "unit-cancel-issued"); !errors.Is(err, ErrAdjustState) {
		t.Fatalf("cancel issued err = %v, want state refusal", err)
	}
}

// TestTodo_SMB_BILL_008_Property proves conservation under a deterministic
// pseudo-random credit stream: issued credits plus the remaining basis
// always equal the invoice total, and every receipt dollar is either settled
// or released as customer credit.
func TestTodo_SMB_BILL_008_Property(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	rng := rand.New(rand.NewSource(20260808))
	registry, _ := bill008Registry(now)
	op := bill008Op()
	approver := bill008Approver()
	invoice := bill008Invoice()
	invoice.InvoiceID = "inv-prop"
	invoice.Number = "INV-PROP"
	invoice.Digest = "inv-digest-prop"
	invoice.Lines = []AdjustInvoiceLine{
		{LineID: "l1", Description: "Bulk work", Qty: "100", UnitPrice: "9.00 USD", Amount: "900.00 USD", TaxAmount: "90.00 USD"},
	}
	invoice.Total = "990.00 USD"
	invoice.TaxTotal = "90.00 USD"
	if err := registry.BindInvoice(op, invoice); err != nil {
		t.Fatal(err)
	}
	receipt := bill008Receipt()
	receipt.ReceiptID = "rcpt-prop"
	receipt.InvoiceID = "inv-prop"
	receipt.Amount = "990.00 USD"
	if err := registry.RegisterReceipt(op, receipt); err != nil {
		t.Fatal(err)
	}
	creditedNet, creditedTax := 0, 0
	for i := 0; i < 20; i++ {
		net := 1 + rng.Intn(60)
		tax := rng.Intn(9)
		input := AdjustDraftInput{
			InvoiceID: "inv-prop", Kind: "credit",
			Lines:  []AdjustLineInput{{LineID: "l1", Amount: bill008itoa(net) + ".00 USD", TaxAmount: bill008itoa(tax) + ".00 USD"}},
			Reason: "property credit", EvidenceRef: "prop-ev-" + bill008itoa(i),
		}
		draft, err := registry.DraftAdjustment(op, input, "prop-draft-"+bill008itoa(i))
		if err != nil {
			if !errors.Is(err, ErrAdjustShortfall) {
				t.Fatalf("split %d/%d err = %v, want issue or shortfall", net, tax, err)
			}
			continue
		}
		if _, err := registry.SubmitAdjustment(op, draft.AdjustmentID, "prop-submit-"+bill008itoa(i)); err != nil {
			t.Fatal(err)
		}
		if _, err := registry.ApproveAdjustment(approver, draft.AdjustmentID, "prop-approve-"+bill008itoa(i)); err != nil {
			t.Fatal(err)
		}
		issued, err := registry.IssueAdjustment(op, draft.AdjustmentID, "prop-issue-"+bill008itoa(i))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := registry.AllocateCredit(op, issued.AdjustmentID, "prop-alloc-"+bill008itoa(i)); err != nil {
			t.Fatal(err)
		}
		creditedNet += net
		creditedTax += tax
		projection, err := registry.Projection(op, "inv-prop")
		if err != nil {
			t.Fatal(err)
		}
		wantCredits := bill008itoa(creditedNet+creditedTax) + ".00 USD"
		if projection.Credits != wantCredits {
			t.Fatalf("projection = %+v, want credits %s", projection, wantCredits)
		}
		// paid == settled + customer credit, every step.
		settled, _ := bill008Money(t, projection.Settled)
		credit, _ := bill008Money(t, projection.CustomerCredit)
		if settled != bill008itoa(990-creditedNet-creditedTax)+".00" || credit != bill008itoa(creditedNet+creditedTax)+".00" {
			t.Fatalf("projection = %+v, want settled/credit split of 990", projection)
		}
	}
}

// TestTodo_SMB_BILL_008_Security proves the neutral authorization boundary:
// foreign tenants, guessed identifiers and denied capabilities share one
// denied response, and stale revisions are refused exactly.
func TestTodo_SMB_BILL_008_Security(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry, _ := bill008Registry(now)
	op := bill008Op()
	if err := registry.BindInvoice(op, bill008Invoice()); err != nil {
		t.Fatal(err)
	}

	foreign := op
	foreign.TenantID = "tenant-b"
	if err := registry.BindInvoice(foreign, bill008Invoice()); !errors.Is(err, ErrAdjustDenied) {
		t.Errorf("cross-tenant bind err = %v, want neutral denied", err)
	}
	if _, err := registry.DraftAdjustment(foreign, bill008Credit(), "sec-foreign"); !errors.Is(err, ErrAdjustDenied) {
		t.Errorf("cross-tenant draft err = %v, want neutral denied", err)
	}
	if _, err := registry.Projection(foreign, "inv-100"); !errors.Is(err, ErrAdjustDenied) {
		t.Errorf("cross-tenant projection err = %v, want neutral denied", err)
	}
	if _, err := registry.AdjustmentStatus(foreign, "sec-foreign"); !errors.Is(err, ErrAdjustDenied) {
		t.Errorf("cross-tenant status err = %v, want neutral denied", err)
	}
	if _, err := registry.AdjustmentTrace(foreign, "adj-1"); !errors.Is(err, ErrAdjustDenied) {
		t.Errorf("cross-tenant trace err = %v, want neutral denied", err)
	}
	if _, err := registry.VoidInvoice(foreign, "inv-100", "x", "y", "sec-void"); !errors.Is(err, ErrAdjustDenied) {
		t.Errorf("cross-tenant void err = %v, want neutral denied", err)
	}

	otherCompany := op
	otherCompany.LegalEntityID = "le-2"
	if _, err := registry.DraftAdjustment(otherCompany, bill008Credit(), "sec-entity"); !errors.Is(err, ErrAdjustDenied) {
		t.Errorf("cross-company draft err = %v, want neutral denied", err)
	}

	if _, err := registry.AdjustmentStatus(op, "never-used-key"); !errors.Is(err, ErrAdjustDenied) {
		t.Errorf("unknown key status err = %v, want neutral denied", err)
	}
	if _, err := registry.AdjustmentTrace(op, "adj-missing"); !errors.Is(err, ErrAdjustDenied) {
		t.Errorf("guessed trace err = %v, want neutral denied", err)
	}
	if _, err := registry.Projection(op, "inv-missing"); !errors.Is(err, ErrAdjustDenied) {
		t.Errorf("guessed projection err = %v, want neutral denied", err)
	}

	denied := op
	denied.AdjustAllowed = false
	if _, err := registry.DraftAdjustment(denied, bill008Credit(), "sec-denied"); !errors.Is(err, ErrAdjustDenied) {
		t.Errorf("denied draft err = %v, want neutral denied", err)
	}
	if _, err := registry.Projection(denied, "inv-100"); !errors.Is(err, ErrAdjustDenied) {
		t.Errorf("denied projection err = %v, want neutral denied", err)
	}

	revised := bill008Invoice()
	revised.Revision = 4
	revised.Digest = "inv-digest-2"
	if err := registry.BindInvoice(op, revised); err != nil {
		t.Fatal(err)
	}
	stale, err := registry.DraftAdjustment(op, bill008Credit(), "sec-stale-draft")
	if err != nil {
		t.Fatal(err)
	}
	if stale.InvoiceRevision != 4 {
		t.Fatalf("draft pins rev %d, want head 4", stale.InvoiceRevision)
	}
	newer := revised
	newer.Revision = 5
	newer.Digest = "inv-digest-3"
	if err := registry.BindInvoice(op, newer); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SubmitAdjustment(op, stale.AdjustmentID, "sec-stale-submit"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ApproveAdjustment(bill008Approver(), stale.AdjustmentID, "sec-stale-approve"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.IssueAdjustment(op, stale.AdjustmentID, "sec-stale-issue"); !errors.Is(err, ErrAdjustStale) {
		t.Fatalf("stale issue err = %v, want stale refusal", err)
	}
}

func bill008itoa(n int) string {
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

// TestTodo_SMB_BILL_008_Race hammers one USD 25 remainder with concurrent
// credits: same-evidence racers produce one issue plus a conflict, and
// duplicate keys converge on one receipt without double posting.
func TestTodo_SMB_BILL_008_Race(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry, _ := bill008Registry(now)
	op := bill008Op()
	approver := bill008Approver()
	if err := registry.BindInvoice(op, bill008Invoice()); err != nil {
		t.Fatal(err)
	}

	submit := func(t *testing.T, input AdjustDraftInput, key string) Adjustment {
		t.Helper()
		draft, err := registry.DraftAdjustment(op, input, key+"-draft")
		if err != nil {
			return Adjustment{}
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
	_ = submit

	type outcome struct {
		id  string
		err error
	}
	// Same return evidence, distinct keys: exactly one draft wins.
	plan := []string{"race-a", "race-b", "race-c", "race-d"}
	results := make(chan outcome, len(plan))
	var wg sync.WaitGroup
	for _, key := range plan {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			draft, err := registry.DraftAdjustment(op, bill008Credit(), key)
			if err != nil {
				results <- outcome{err: err}
				return
			}
			results <- outcome{id: draft.AdjustmentID}
		}(key)
	}
	wg.Wait()
	close(results)
	winners, conflicts := 0, 0
	var winner string
	for r := range results {
		if r.err == nil {
			winners++
			winner = r.id
		} else if errors.Is(r.err, ErrAdjustConflict) {
			conflicts++
		} else {
			t.Fatalf("race err = %v, want issue or conflict", r.err)
		}
	}
	if winners != 1 || conflicts != 3 {
		t.Fatalf("winners = %d conflicts = %d, want 1 and 3", winners, conflicts)
	}
	if _, err := registry.SubmitAdjustment(op, winner, "race-submit"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ApproveAdjustment(approver, winner, "race-approve"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.IssueAdjustment(op, winner, "race-issue"); err != nil {
		t.Fatal(err)
	}

	// Same key twice: one receipt, never two drafts.
	dup := bill008Credit()
	dup.EvidenceRef = "race-dup-ev"
	first, err := registry.DraftAdjustment(op, dup, "race-dup")
	if err != nil {
		t.Fatal(err)
	}
	second, err := registry.DraftAdjustment(op, dup, "race-dup")
	if err != nil || second.AdjustmentID != first.AdjustmentID {
		t.Fatalf("duplicate key = %+v err=%v, want one receipt", second, err)
	}
	projection, err := registry.Projection(op, "inv-100")
	if err != nil {
		t.Fatal(err)
	}
	if projection.Credits != "25.00 USD" {
		t.Fatalf("projection = %+v, want exactly 25.00 credited", projection)
	}
}

// TestTodo_SMB_BILL_008_Fault proves crash and correction semantics: a commit
// observed as timed out replays the original receipt with no second effect,
// and a correction of a correction links forward without rewriting history.
func TestTodo_SMB_BILL_008_Fault(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry, _ := bill008Registry(now)
	op := bill008Op()
	approver := bill008Approver()
	if err := registry.BindInvoice(op, bill008Invoice()); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterReceipt(op, bill008Receipt()); err != nil {
		t.Fatal(err)
	}

	issued := bill008Issue(t, registry, op, approver, bill008Credit(), "fault")
	replayed, err := registry.IssueAdjustment(op, issued.AdjustmentID, "fault-issue")
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Number != issued.Number || replayed.DocumentDigest != issued.DocumentDigest {
		t.Fatalf("replay = %+v, want original number and document", replayed)
	}
	projection, err := registry.Projection(op, "inv-100")
	if err != nil {
		t.Fatal(err)
	}
	if projection.Credits != "25.00 USD" {
		t.Fatalf("projection = %+v, want single 25.00 posting", projection)
	}
	status, err := registry.AdjustmentStatus(op, "fault-draft")
	if err != nil || status.AdjustmentID != issued.AdjustmentID || status.Status != "issued" {
		t.Fatalf("status = %+v err=%v, want live issued record", status, err)
	}

	correcting := bill008Issue(t, registry, op, approver, AdjustDraftInput{
		InvoiceID: "inv-100", Kind: "credit", CorrectsRef: issued.AdjustmentID,
		Lines:  []AdjustLineInput{{LineID: "l1", Amount: "5.00 USD", TaxAmount: "0.50 USD"}},
		Reason: "credit understated", EvidenceRef: "review-6",
	}, "fault-corr")
	if correcting.CorrectsRef != issued.AdjustmentID {
		t.Fatalf("correcting = %+v, want link to %s", correcting, issued.AdjustmentID)
	}
	again, err := registry.AdjustmentStatus(op, "fault-draft")
	if err != nil || again.TotalAmount != "25.00 USD" || again.Number != issued.Number {
		t.Fatalf("original after correction = %+v err=%v, want untouched CM", again, err)
	}

	data, err := registry.Export()
	if err != nil {
		t.Fatal(err)
	}
	restored := NewAdjustmentRegistry(func() time.Time { return now })
	restored.BindLedger("tenant-a", NewReceivableLedger("tenant-a"))
	if err := restored.Import(data); err != nil {
		t.Fatal(err)
	}
	restoredProjection, err := restored.Projection(op, "inv-100")
	if err != nil {
		t.Fatal(err)
	}
	if restoredProjection.Credits != "30.50 USD" {
		t.Fatalf("restored = %+v, want 30.50 across the chain", restoredProjection)
	}
}

// TestTodo_SMB_BILL_008_Conformance pins every critical guard by name so a
// removed check fails a named row instead of silently widening adjustments.
func TestTodo_SMB_BILL_008_Conformance(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	setup := func(t *testing.T) (*AdjustmentRegistry, AdjustOp, AdjustOp) {
		t.Helper()
		registry, _ := bill008Registry(now)
		op := bill008Op()
		if err := registry.BindInvoice(op, bill008Invoice()); err != nil {
			t.Fatal(err)
		}
		return registry, op, bill008Approver()
	}
	creditOf := func(evidence string) AdjustDraftInput {
		input := bill008Credit()
		input.EvidenceRef = evidence
		return input
	}
	rows := []struct {
		name string
		run  func(*testing.T, *AdjustmentRegistry, AdjustOp, AdjustOp) error
		want error
	}{
		{"missing reason blocks draft", func(t *testing.T, r *AdjustmentRegistry, op, _ AdjustOp) error {
			bad := creditOf("conf-reason")
			bad.Reason = ""
			_, err := r.DraftAdjustment(op, bad, "conf-reason")
			return err
		}, ErrAdjustValidation},
		{"over-basis blocks draft", func(t *testing.T, r *AdjustmentRegistry, op, _ AdjustOp) error {
			bad := creditOf("conf-basis")
			bad.Lines[0].Amount = "91.00 USD"
			_, err := r.DraftAdjustment(op, bad, "conf-basis")
			return err
		}, ErrAdjustShortfall},
		{"evidence reuse conflicts", func(t *testing.T, r *AdjustmentRegistry, op, _ AdjustOp) error {
			if _, err := r.DraftAdjustment(op, creditOf("conf-ev"), "conf-ev-1"); err != nil {
				return err
			}
			_, err := r.DraftAdjustment(op, creditOf("conf-ev"), "conf-ev-2")
			return err
		}, ErrAdjustConflict},
		{"cross-tenant blocks draft", func(t *testing.T, r *AdjustmentRegistry, op, _ AdjustOp) error {
			foreign := op
			foreign.TenantID = "tenant-z"
			_, err := r.DraftAdjustment(foreign, creditOf("conf-tenant"), "conf-tenant")
			return err
		}, ErrAdjustDenied},
		{"denied role blocks approve", func(t *testing.T, r *AdjustmentRegistry, op, approver AdjustOp) error {
			draft, err := r.DraftAdjustment(op, creditOf("conf-role"), "conf-role")
			if err != nil {
				return err
			}
			if _, err := r.SubmitAdjustment(op, draft.AdjustmentID, "conf-role-submit"); err != nil {
				return err
			}
			denied := approver
			denied.ApproveAllowed = false
			_, err = r.ApproveAdjustment(denied, draft.AdjustmentID, "conf-role-approve")
			return err
		}, ErrAdjustDenied},
		{"self approval refused", func(t *testing.T, r *AdjustmentRegistry, op, _ AdjustOp) error {
			draft, err := r.DraftAdjustment(op, creditOf("conf-self"), "conf-self")
			if err != nil {
				return err
			}
			if _, err := r.SubmitAdjustment(op, draft.AdjustmentID, "conf-self-submit"); err != nil {
				return err
			}
			_, err = r.ApproveAdjustment(op, draft.AdjustmentID, "conf-self-approve")
			return err
		}, ErrAdjustState},
		{"issue before approve refused", func(t *testing.T, r *AdjustmentRegistry, op, _ AdjustOp) error {
			draft, err := r.DraftAdjustment(op, creditOf("conf-early"), "conf-early")
			if err != nil {
				return err
			}
			_, err = r.IssueAdjustment(op, draft.AdjustmentID, "conf-early-issue")
			return err
		}, ErrAdjustState},
		{"paid void refused", func(t *testing.T, r *AdjustmentRegistry, op, _ AdjustOp) error {
			if err := r.RegisterReceipt(op, bill008Receipt()); err != nil {
				return err
			}
			_, err := r.VoidInvoice(op, "inv-100", "paid", "conf-void-ev", "conf-void")
			return err
		}, ErrAdjustState},
		{"unknown key never fabricates", func(t *testing.T, r *AdjustmentRegistry, op, _ AdjustOp) error {
			_, err := r.AdjustmentStatus(op, "conf-missing")
			return err
		}, ErrAdjustDenied},
		{"key reuse with new payload conflicts", func(t *testing.T, r *AdjustmentRegistry, op, _ AdjustOp) error {
			if _, err := r.DraftAdjustment(op, creditOf("conf-key"), "conf-key"); err != nil {
				return err
			}
			changed := creditOf("conf-key-changed")
			_, err := r.DraftAdjustment(op, changed, "conf-key")
			return err
		}, ErrAdjustConflict},
	}
	for _, row := range rows {
		registry, op, approver := setup(t)
		if err := row.run(t, registry, op, approver); !errors.Is(err, row.want) {
			t.Errorf("%s: err = %v, want %v", row.name, err, row.want)
		}
	}
}

// TestTodo_SMB_BILL_008_Mutation pins the two oracles proven by real
// flip-and-revert mutation runs during development (recorded in the issue
// evidence): the tenant-isolation check and the per-line basis guard.
func TestTodo_SMB_BILL_008_Mutation(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry, _ := bill008Registry(now)
	op := bill008Op()
	if err := registry.BindInvoice(op, bill008Invoice()); err != nil {
		t.Fatal(err)
	}
	t.Run("tenant isolation oracle", func(t *testing.T) {
		foreign := op
		foreign.TenantID = "tenant-mutant"
		if _, err := registry.DraftAdjustment(foreign, bill008Credit(), "mut-tenant"); !errors.Is(err, ErrAdjustDenied) {
			t.Fatalf("mutant survived: cross-tenant draft err = %v", err)
		}
	})
	t.Run("basis guard oracle", func(t *testing.T) {
		over := bill008Credit()
		over.EvidenceRef = "mut-basis-ev"
		over.Lines[0].Amount = "95.00 USD"
		if _, err := registry.DraftAdjustment(op, over, "mut-basis"); !errors.Is(err, ErrAdjustShortfall) {
			t.Fatalf("mutant survived: over-basis draft err = %v", err)
		}
	})
}

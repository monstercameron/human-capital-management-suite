package payable

import (
	"errors"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/contractortime"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type bill012Clock struct{ now time.Time }

func (c *bill012Clock) Now() time.Time { return c.now }

func bill012Op() PayOp {
	return PayOp{TenantID: "tenant-a", LegalEntityID: "le-1", ActorID: "ap-clerk-1", PayableAllowed: true, ApproveAllowed: true}
}

func bill012Approver() PayOp {
	op := bill012Op()
	op.ActorID = "ap-manager-1"
	return op
}

func bill012Registry(now time.Time) (*PayableRegistry, *bill012Clock) {
	clock := &bill012Clock{now: now}
	return NewPayableRegistry(clock.Now), clock
}

func bill012Match() MatchBillInput {
	return MatchBillInput{
		MatchID: "match-7", SupplierID: "supplier-acme", SupplierName: "Acme Supply",
		InvoiceNumber: "ACME-1001", InvoiceRevision: 1,
		OrderedQty: "10", UnitPrice: "10.00 USD", ReceivedQty: "6", BilledQty: "10",
		BilledTotal: "100.00 USD", DisputedQty: "0", Currency: "USD",
		DueDate: "2026-11-20", Terms: "net-30", Cutoff: "2026-10-21",
		DecidedBy: "buyer-2", DecidedAt: "2026-10-21T15:04:05Z",
	}
}

func bill012Observation(t *testing.T, id, tenant string, verdict operation.ObservationVerdict) operation.Observation {
	t.Helper()
	observationID, err := uuid.Parse("11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatal(err)
	}
	operationID, err := uuid.Parse("22222222-2222-2222-2222-222222222222")
	if err != nil {
		t.Fatal(err)
	}
	return operation.Observation{
		TenantID: tenant, ObservationID: observationID, OperationID: operationID,
		ExternalResourceKey: id, Verdict: verdict, ExternalObjectRef: "settlement-" + id,
		ExternalVersion: "v1", ObservedDigest: "digest-" + id,
		ObservedAt: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC), Authority: "bank-connector",
	}
}

// TestTodo_SMB_BILL_012 pins the payable acceptance oracles: PO 10 units at
// USD 10 with 6 received and 10 billed produces USD 100 billed, USD 60
// approved and USD 40 unmatched; a recorded USD 30 payment leaves USD 30 of
// approved due plus USD 40 blocked.
func TestTodo_SMB_BILL_012(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry, _ := bill012Registry(now)
	op := bill012Op()

	payable, err := registry.AdmitMatchedBill(op, bill012Match(), "admit-1")
	if err != nil {
		t.Fatal(err)
	}
	if payable.TotalBilled != "100.00 USD" || payable.ApprovedToPay != "60.00 USD" || payable.Unmatched != "40.00 USD" {
		t.Fatalf("payable = %+v, want billed 100 / approved 60 / unmatched 40", payable)
	}
	if payable.Basis != "PO_MATCHED" || payable.PORef == "" || payable.Number == "" {
		t.Fatalf("payable = %+v, want PO_MATCHED basis with refs and number", payable)
	}

	again, err := registry.AdmitMatchedBill(op, bill012Match(), "admit-2")
	if err != nil {
		t.Fatal(err)
	}
	if again.PayableID != payable.PayableID {
		t.Fatalf("duplicate intake = %s, want one payable %s", again.PayableID, payable.PayableID)
	}

	obs := bill012Observation(t, "pay-obs-1", "tenant-a", operation.ObservationApplied)
	if err := registry.RegisterObservation(op, obs); err != nil {
		t.Fatal(err)
	}
	payment, err := registry.AllocatePayment(op, PaymentInput{
		PayableID: payable.PayableID, Amount: "30.00 USD", ObservationID: "pay-obs-1",
	}, "pay-1")
	if err != nil {
		t.Fatal(err)
	}
	if payment.Amount != "30.00 USD" || payment.RemainingDue != "30.00 USD" {
		t.Fatalf("payment = %+v, want 30 paid / 30 due", payment)
	}
	explain, err := registry.Explain(op, payable.PayableID)
	if err != nil {
		t.Fatal(err)
	}
	if explain.ApprovedToPay != "60.00 USD" || explain.Paid != "30.00 USD" || explain.Remaining != "30.00 USD" ||
		explain.DisputedUnmatched != "40.00 USD" {
		t.Fatalf("explain = %+v, want approved 60 / paid 30 / due 30 / blocked 40", explain)
	}

	credit, err := registry.RecordCredit(op, CreditInput{
		PayableID: payable.PayableID, Amount: "10.00 USD", SourceRef: "supplier-cm-4", Reason: "short shipment",
	}, "credit-1")
	if err != nil {
		t.Fatal(err)
	}
	if credit.AppliedAmount != "10.00 USD" || credit.UnappliedAmount != "0.00 USD" {
		t.Fatalf("credit = %+v, want 10 applied / 0 unapplied", credit)
	}
	after, err := registry.Explain(op, payable.PayableID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Remaining != "20.00 USD" || after.Credits != "10.00 USD" {
		t.Fatalf("explain = %+v, want due 20 / credits 10", after)
	}
	dup, err := registry.RecordCredit(op, CreditInput{
		PayableID: payable.PayableID, Amount: "10.00 USD", SourceRef: "supplier-cm-4", Reason: "short shipment",
	}, "credit-2")
	if err != nil || dup.CreditID != credit.CreditID {
		t.Fatalf("duplicate credit = %+v err=%v, want one receipt", dup, err)
	}
	reversed, err := registry.ReverseAllocation(op, payment.AllocationID, "bank reversal ref-77", "rev-1")
	if err != nil {
		t.Fatal(err)
	}
	if reversed.Status != "reversed" {
		t.Fatalf("reversal = %+v, want reversed status", reversed)
	}
	restored, err := registry.Explain(op, payable.PayableID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Paid != "0.00 USD" || restored.Remaining != "50.00 USD" {
		t.Fatalf("explain = %+v, want paid 0 / due 50 after reversal", restored)
	}
}

// TestTodo_SMB_BILL_012_Unit pins admission boundaries: malformed match
// facts, footing mismatches, unknown approvals, linkage violations and
// payment-guard edges.
func TestTodo_SMB_BILL_012_Unit(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry, _ := bill012Registry(now)
	op := bill012Op()

	base := bill012Match()
	cases := []struct {
		name   string
		mutate func(*MatchBillInput)
	}{
		{"missing match", func(i *MatchBillInput) { i.MatchID = "" }},
		{"missing supplier", func(i *MatchBillInput) { i.SupplierID = "" }},
		{"missing invoice", func(i *MatchBillInput) { i.InvoiceNumber = "" }},
		{"bad qty", func(i *MatchBillInput) { i.ReceivedQty = "six" }},
		{"negative received", func(i *MatchBillInput) { i.ReceivedQty = "-1" }},
		{"bad total", func(i *MatchBillInput) { i.BilledTotal = "100" }},
		{"total mismatch", func(i *MatchBillInput) { i.BilledTotal = "99.00 USD" }},
		{"received above billed", func(i *MatchBillInput) { i.ReceivedQty = "11" }},
		{"bad decided at", func(i *MatchBillInput) { i.DecidedAt = "yesterday" }},
		{"missing decider", func(i *MatchBillInput) { i.DecidedBy = "" }},
	}
	for idx, tc := range cases {
		input := base
		input.MatchID = "unit-match-" + bill012itoa(idx)
		input.InvoiceNumber = "ACME-UNIT-" + bill012itoa(idx)
		tc.mutate(&input)
		if _, err := registry.AdmitMatchedBill(op, input, "unit-"+bill012itoa(idx)); !errors.Is(err, ErrPayableValidation) {
			t.Errorf("%s: err = %v, want validation refusal", tc.name, err)
		}
	}

	payable, err := registry.AdmitMatchedBill(op, base, "unit-ok")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.AllocatePayment(op, PaymentInput{
		PayableID: payable.PayableID, Amount: "1.00 USD", ObservationID: "obs-missing",
	}, "unit-noobs"); !errors.Is(err, ErrPayableState) {
		t.Errorf("unknown observation err = %v, want state refusal", err)
	}
	pending := bill012Observation(t, "pay-obs-pending", "tenant-a", operation.ObservationUnknown)
	if err := registry.RegisterObservation(op, pending); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.AllocatePayment(op, PaymentInput{
		PayableID: payable.PayableID, Amount: "1.00 USD", ObservationID: "pay-obs-pending",
	}, "unit-pending"); !errors.Is(err, ErrPayableState) {
		t.Errorf("pending observation err = %v, want state refusal, never paid", err)
	}
	manual, err := registry.AllocatePayment(op, PaymentInput{
		PayableID: payable.PayableID, Amount: "5.00 USD",
		ManualEvidence: "manual:check-221", Label: "manual check",
	}, "unit-manual")
	if err != nil {
		t.Fatal(err)
	}
	if manual.RemainingDue != "55.00 USD" {
		t.Fatalf("manual payment = %+v, want 55 due", manual)
	}
	if _, err := registry.AllocatePayment(op, PaymentInput{
		PayableID: payable.PayableID, Amount: "56.00 USD",
		ManualEvidence: "manual:check-222", Label: "manual check",
	}, "unit-over"); !errors.Is(err, ErrPayableShortfall) {
		t.Errorf("overpayment err = %v, want shortfall refusal", err)
	}
	if _, err := registry.RecordCredit(op, CreditInput{
		PayableID: "pay-missing", Amount: "1.00 USD", SourceRef: "cm-x", Reason: "x",
	}, "unit-credit-missing"); !errors.Is(err, ErrPayableDenied) {
		t.Errorf("foreign credit err = %v, want neutral denied", err)
	}
	over, err := registry.RecordCredit(op, CreditInput{
		PayableID: payable.PayableID, Amount: "100.00 USD", SourceRef: "cm-big", Reason: "goodwill",
	}, "unit-credit-big")
	if err != nil {
		t.Fatal(err)
	}
	if over.AppliedAmount != "55.00 USD" || over.UnappliedAmount != "45.00 USD" {
		t.Fatalf("big credit = %+v, want 55 applied / 45 visible unapplied", over)
	}
}

func bill012itoa(n int) string {
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

// TestBill012NonPOAdmission proves the no-PO path: an approved USD 800 rent
// invoice becomes one payable marked Matching not applicable / No-PO
// approved, duplicate retries add nothing, and no purchase or receipt record
// is fabricated.
func TestBill012NonPOAdmission(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry, _ := bill012Registry(now)
	op := bill012Op()

	input := NonPOBillInput{
		ApprovalID: "buy009-appr-3", ApprovalRevision: 2,
		SupplierID: "supplier-rent", SupplierName: "Rent Co", InvoiceNumber: "RENT-800",
		Amount: "800.00 USD", Currency: "USD", Category: "facilities-rent", Terms: "net-15",
		TaxEvidence: "tax-src-rent-1", DueDate: "2026-11-05",
		ApprovedBy: "controller-1", ApprovedAt: "2026-10-21T16:00:00Z",
	}
	payable, err := registry.AdmitNonPOBill(op, input, "rent-1")
	if err != nil {
		t.Fatal(err)
	}
	if payable.TotalBilled != "800.00 USD" || payable.ApprovedToPay != "800.00 USD" || payable.Unmatched != "0.00 USD" {
		t.Fatalf("rent payable = %+v, want 800 billed / 800 approved / 0 unmatched", payable)
	}
	if payable.Basis != "NON_PO_APPROVED" || payable.MatchingStatus != "Matching not applicable" || payable.PORef != "" || payable.ReceiptRef != "" {
		t.Fatalf("rent payable = %+v, want no-PO markers and no fabricated PO/receipt", payable)
	}
	again, err := registry.AdmitNonPOBill(op, input, "rent-2")
	if err != nil || again.PayableID != payable.PayableID {
		t.Fatalf("duplicate retry = %+v err=%v, want one payable", again, err)
	}
	changed := input
	changed.ApprovalID = "buy009-appr-4"
	changed.Amount = "801.00 USD"
	clash, err := registry.AdmitNonPOBill(op, changed, "rent-3")
	if err != nil {
		t.Fatal(err)
	}
	if clash.Status != "discrepancy" || clash.ApprovedToPay != "0.00 USD" {
		t.Fatalf("amount clash = %+v, want blocked discrepancy without obligation", clash)
	}
	unknown := input
	unknown.ApprovalID = ""
	if _, err := registry.AdmitNonPOBill(op, unknown, "rent-4"); !errors.Is(err, ErrPayableValidation) {
		t.Fatalf("missing approval err = %v, want validation refusal", err)
	}
}
func bill012ContractorDraft(t *testing.T, id string, minutes int, rate string) contractortime.InvoiceDraft {
	t.Helper()
	money, err := values.NewMoney(rate, "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	hourly, err := values.NewMoneyRate(money, "HOUR")
	if err != nil {
		t.Fatal(err)
	}
	draft, err := contractortime.BuildInvoice(contractortime.BuildInvoiceRequest{
		ID: id,
		Engagement: contractortime.Engagement{
			ID: "eng-ct", WorkerRef: "worker-ct", SOWRef: "sow-7",
			Pricing: contractortime.PricingHourly, Currency: "USD", AmountScale: 2, Rounding: values.RoundingHalfEven,
			RateCard: map[string]values.Rate{"STD": hourly}, Tax: contractortime.TaxTreatment{Kind: contractortime.TaxNone},
		},
		Lines: []contractortime.ApprovedLine{
			{WorkerRef: "worker-ct", Date: "2026-10-19", Minutes: minutes, Project: "field", CostCode: "ops", RateCode: "STD", RevisionDigest: "rev-ct-1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return draft
}

// TestBill012ContractorAdmission proves contractor evidence stays contractor
// evidence: an approved CT42 invoice becomes one USD 500 payable with its
// contractor purpose retained, the same invoice imported as NON_PO links the
// same payable in either entry order, and unresolved alias identity is
// quarantined instead of merged or paid.
func TestBill012ContractorAdmission(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry, _ := bill012Registry(now)
	op := bill012Op()
	approval := ContractorApproval{SupplierID: "supplier-ct", SupplierName: "Contractor LLC", ApprovedBy: "controller-1", EvidenceRef: "ct-approve-1", DueDate: "2026-11-05", Terms: "net-15"}

	draft := bill012ContractorDraft(t, "CT42", 600, "50.00")
	admitted, err := registry.AdmitContractor(op, draft, approval, "ct-1")
	if err != nil {
		t.Fatal(err)
	}
	if admitted.TotalBilled != "500.00 USD" || admitted.Basis != "CONTRACTOR_APPROVED" || admitted.Purpose == "" {
		t.Fatalf("contractor payable = %+v, want 500 with contractor purpose", admitted)
	}
	if admitted.PORef != "" || !strings.Contains(admitted.Purpose, "worker-ct") {
		t.Fatalf("contractor payable = %+v, want worker purpose and no fake PO", admitted)
	}

	asNonPO, err := registry.AdmitNonPOBill(op, NonPOBillInput{
		ApprovalID: "buy009-ct-1", ApprovalRevision: 1,
		SupplierID: "supplier-ct", SupplierName: "Contractor LLC", InvoiceNumber: "CT42",
		Amount: "500.00 USD", Currency: "USD", Category: "contractor", Terms: "net-15",
		TaxEvidence: "tax-src-ct-1", DueDate: "2026-11-05",
		ApprovedBy: "controller-1", ApprovedAt: "2026-10-21T16:00:00Z",
	}, "ct-nonpo")
	if err != nil {
		t.Fatal(err)
	}
	if asNonPO.PayableID != admitted.PayableID {
		t.Fatalf("cross-entry import = %s, want one payable %s", asNonPO.PayableID, admitted.PayableID)
	}

	reversed, _ := bill012Registry(now)
	first, err := reversed.AdmitNonPOBill(op, NonPOBillInput{
		ApprovalID: "buy009-ct-2", ApprovalRevision: 1,
		SupplierID: "supplier-ct", SupplierName: "Contractor LLC", InvoiceNumber: "CT43",
		Amount: "250.00 USD", Currency: "USD", Category: "contractor", Terms: "net-15",
		TaxEvidence: "tax-src-ct-1", DueDate: "2026-11-05",
		ApprovedBy: "controller-1", ApprovedAt: "2026-10-21T16:00:00Z",
	}, "ct-rev-1")
	if err != nil {
		t.Fatal(err)
	}
	draft43 := bill012ContractorDraft(t, "CT43", 300, "50.00")
	second, err := reversed.AdmitContractor(op, draft43, approval, "ct-rev-2")
	if err != nil {
		t.Fatal(err)
	}
	if second.PayableID != first.PayableID {
		t.Fatalf("reversed entry = %s, want one payable %s", second.PayableID, first.PayableID)
	}

	ambiguous, err := reversed.AdmitNonPOBill(op, NonPOBillInput{
		ApprovalID: "buy009-ct-3", ApprovalRevision: 1,
		SupplierID: "supplier-other", SupplierName: "Other LLC", InvoiceNumber: "CT43",
		Amount: "250.00 USD", Currency: "USD", Category: "contractor", Terms: "net-15",
		TaxEvidence: "tax-src-ct-1", DueDate: "2026-11-05",
		ApprovedBy: "controller-1", ApprovedAt: "2026-10-21T16:00:00Z",
	}, "ct-rev-3")
	if err != nil {
		t.Fatal(err)
	}
	if ambiguous.Status != "quarantined" || ambiguous.ApprovedToPay != "0.00 USD" {
		t.Fatalf("ambiguous alias = %+v, want quarantined without obligation", ambiguous)
	}
	if _, err := reversed.AllocatePayment(op, PaymentInput{
		PayableID: ambiguous.PayableID, Amount: "1.00 USD", ManualEvidence: "manual:q", Label: "q",
	}, "ct-rev-pay"); !errors.Is(err, ErrPayableState) {
		t.Fatalf("quarantined pay err = %v, want state refusal, never auto-paid", err)
	}
	resolved, err := reversed.ResolveQuarantine(op, ambiguous.PayableID, "", "ct-rev-resolve")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != "open" || resolved.ApprovedToPay != "250.00 USD" {
		t.Fatalf("resolved = %+v, want confirmed open 250 payable", resolved)
	}
}

// TestTodo_SMB_BILL_012_Property proves conservation under a deterministic
// pseudo-random payment stream: paid plus remaining always equals approved
// minus applied credits, and every credit dollar is applied or unapplied.
func TestTodo_SMB_BILL_012_Property(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	rng := rand.New(rand.NewSource(20260812))
	registry, _ := bill012Registry(now)
	op := bill012Op()
	match := bill012Match()
	match.MatchID = "match-prop"
	match.InvoiceNumber = "ACME-PROP"
	match.OrderedQty = "100"
	match.ReceivedQty = "80"
	match.BilledQty = "100"
	match.BilledTotal = "1000.00 USD"
	payable, err := registry.AdmitMatchedBill(op, match, "prop-admit")
	if err != nil {
		t.Fatal(err)
	}
	paid, credited := 0, 0
	for i := 0; i < 30; i++ {
		amount := 5 + rng.Intn(60)
		if i%5 == 4 {
			receipt, err := registry.RecordCredit(op, CreditInput{
				PayableID: payable.PayableID, Amount: bill012itoa(amount) + ".00 USD",
				SourceRef: "prop-cm-" + bill012itoa(i), Reason: "property credit",
			}, "prop-credit-"+bill012itoa(i))
			if err != nil {
				t.Fatal(err)
			}
			applied, _ := bill012Split(t, receipt.AppliedAmount)
			unapplied, _ := bill012Split(t, receipt.UnappliedAmount)
			if applied+unapplied != amount {
				t.Fatalf("credit = %+v, want applied+unapplied == %d", receipt, amount)
			}
			credited += applied
			continue
		}
		_, err := registry.AllocatePayment(op, PaymentInput{
			PayableID: payable.PayableID, Amount: bill012itoa(amount) + ".00 USD",
			ManualEvidence: "manual:prop-" + bill012itoa(i), Label: "property",
		}, "prop-pay-"+bill012itoa(i))
		if err != nil {
			if !errors.Is(err, ErrPayableShortfall) {
				t.Fatalf("payment %d err = %v, want paid or shortfall", amount, err)
			}
			continue
		}
		paid += amount
		explain, err := registry.Explain(op, payable.PayableID)
		if err != nil {
			t.Fatal(err)
		}
		due, _ := bill012Split(t, explain.Remaining)
		if paid+due != 800-credited {
			t.Fatalf("explain = %+v, want paid+due == approved-credits", explain)
		}
	}
}

func bill012Split(t *testing.T, text string) (int, string) {
	t.Helper()
	parts := strings.SplitN(strings.TrimSpace(text), " ", 2)
	if len(parts) != 2 {
		t.Fatalf("bad test money %q", text)
	}
	dollars := strings.SplitN(parts[0], ".", 2)[0]
	n := 0
	for i := 0; i < len(dollars); i++ {
		n = n*10 + int(dollars[i]-'0')
	}
	return n, parts[1]
}

// TestTodo_SMB_BILL_012_Security proves the neutral authorization boundary:
// foreign tenants, guessed identifiers and denied capabilities share one
// denied response, and stale correction facts are refused exactly.
func TestTodo_SMB_BILL_012_Security(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry, _ := bill012Registry(now)
	op := bill012Op()
	payable, err := registry.AdmitMatchedBill(op, bill012Match(), "sec-admit")
	if err != nil {
		t.Fatal(err)
	}

	foreign := op
	foreign.TenantID = "tenant-b"
	if _, err := registry.AdmitMatchedBill(foreign, bill012Match(), "sec-foreign"); !errors.Is(err, ErrPayableDenied) {
		t.Errorf("cross-tenant admit err = %v, want neutral denied", err)
	}
	if _, err := registry.Explain(foreign, payable.PayableID); !errors.Is(err, ErrPayableDenied) {
		t.Errorf("cross-tenant explain err = %v, want neutral denied", err)
	}
	if _, err := registry.PayableStatus(foreign, "sec-admit"); !errors.Is(err, ErrPayableDenied) {
		t.Errorf("cross-tenant status err = %v, want neutral denied", err)
	}
	if _, err := registry.AllocatePayment(foreign, PaymentInput{PayableID: payable.PayableID, Amount: "1.00 USD", ManualEvidence: "m", Label: "m"}, "sec-pay"); !errors.Is(err, ErrPayableDenied) {
		t.Errorf("cross-tenant pay err = %v, want neutral denied", err)
	}

	otherCompany := op
	otherCompany.LegalEntityID = "le-2"
	if _, err := registry.AdmitMatchedBill(otherCompany, bill012Match(), "sec-entity"); !errors.Is(err, ErrPayableDenied) {
		t.Errorf("cross-company admit err = %v, want neutral denied", err)
	}

	if _, err := registry.Explain(op, "pay-missing"); !errors.Is(err, ErrPayableDenied) {
		t.Errorf("guessed explain err = %v, want neutral denied", err)
	}
	if _, err := registry.PayableStatus(op, "never-used-key"); !errors.Is(err, ErrPayableDenied) {
		t.Errorf("unknown key status err = %v, want neutral denied", err)
	}

	denied := op
	denied.PayableAllowed = false
	if _, err := registry.AdmitMatchedBill(denied, bill012Match(), "sec-denied"); !errors.Is(err, ErrPayableDenied) {
		t.Errorf("denied admit err = %v, want neutral denied", err)
	}
	if _, err := registry.Explain(denied, payable.PayableID); !errors.Is(err, ErrPayableDenied) {
		t.Errorf("denied explain err = %v, want neutral denied", err)
	}

	review, err := registry.AdmitAdjustment(op, payable.PayableID, AdjustmentInput{
		Revision: 2, ReceivedQty: "7", BilledQty: "10", BilledTotal: "100.00 USD",
		Reason: "late receipt", EvidenceRef: "recv-8", DecidedBy: "buyer-2",
	}, "sec-adj")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ApproveAdjustmentReview(bill012Approver(), review.ReviewID, "sec-adj-ok"); err != nil {
		t.Fatal(err)
	}
	stale, err := registry.AdmitAdjustment(op, payable.PayableID, AdjustmentInput{
		Revision: 1, ReceivedQty: "5", BilledQty: "10", BilledTotal: "100.00 USD",
		Reason: "old facts", EvidenceRef: "recv-1", DecidedBy: "buyer-2",
	}, "sec-adj-stale")
	if err == nil {
		if _, err := registry.ApproveAdjustmentReview(bill012Approver(), stale.ReviewID, "sec-adj-stale-ok"); !errors.Is(err, ErrPayableStale) {
			t.Errorf("stale approve err = %v, want stale refusal", err)
		}
	} else if !errors.Is(err, ErrPayableStale) {
		t.Errorf("stale admit err = %v, want stale refusal", err)
	}
}

// TestTodo_SMB_BILL_012_Race hammers one USD 60 remainder with concurrent
// payments and credits: winners conserve exactly, losers refuse, and
// duplicate observations converge on one recording.
func TestTodo_SMB_BILL_012_Race(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry, _ := bill012Registry(now)
	op := bill012Op()
	payable, err := registry.AdmitMatchedBill(op, bill012Match(), "race-admit")
	if err != nil {
		t.Fatal(err)
	}

	obs := bill012Observation(t, "pay-obs-race", "tenant-a", operation.ObservationApplied)
	if err := registry.RegisterObservation(op, obs); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterObservation(op, obs); err != nil {
		t.Fatalf("duplicate observation err = %v, want idempotent ok", err)
	}
	for i := 0; i < 6; i++ {
		racer := bill012Observation(t, "race-pay-obs-"+bill012itoa(i), "tenant-a", operation.ObservationApplied)
		if err := registry.RegisterObservation(op, racer); err != nil {
			t.Fatal(err)
		}
	}

	type outcome struct {
		kind string
		err  error
	}
	results := make(chan outcome, 12)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := registry.AllocatePayment(op, PaymentInput{
				PayableID: payable.PayableID, Amount: "20.00 USD", ObservationID: "race-pay-obs-" + bill012itoa(i),
			}, "race-pay-"+bill012itoa(i))
			results <- outcome{kind: "pay", err: err}
		}(i)
	}
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := registry.RecordCredit(op, CreditInput{
				PayableID: payable.PayableID, Amount: "20.00 USD",
				SourceRef: "race-cm-" + bill012itoa(i), Reason: "race credit",
			}, "race-credit-"+bill012itoa(i))
			results <- outcome{kind: "credit", err: err}
		}(i)
	}
	wg.Wait()
	close(results)
	for r := range results {
		if r.err != nil && !errors.Is(r.err, ErrPayableShortfall) {
			t.Fatalf("%s err = %v, want paid/applied or shortfall", r.kind, r.err)
		}
	}
	explain, err := registry.Explain(op, payable.PayableID)
	if err != nil {
		t.Fatal(err)
	}
	paid, _ := bill012Split(t, explain.Paid)
	credits, _ := bill012Split(t, explain.Credits)
	due, _ := bill012Split(t, explain.Remaining)
	if paid+credits+due != 60 {
		t.Fatalf("explain = %+v, want paid+credits+due == 60", explain)
	}
}

// TestTodo_SMB_BILL_012_Fault proves crash and correction semantics: timed-out
// commits replay their original receipts, reversals reopen exact amounts, and
// approved corrections supersede facts without rewriting history.
func TestTodo_SMB_BILL_012_Fault(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry, _ := bill012Registry(now)
	op := bill012Op()
	payable, err := registry.AdmitMatchedBill(op, bill012Match(), "fault-admit")
	if err != nil {
		t.Fatal(err)
	}
	obs := bill012Observation(t, "pay-obs-fault", "tenant-a", operation.ObservationApplied)
	if err := registry.RegisterObservation(op, obs); err != nil {
		t.Fatal(err)
	}
	first, err := registry.AllocatePayment(op, PaymentInput{
		PayableID: payable.PayableID, Amount: "30.00 USD", ObservationID: "pay-obs-fault",
	}, "fault-pay")
	if err != nil {
		t.Fatal(err)
	}
	retry, err := registry.AllocatePayment(op, PaymentInput{
		PayableID: payable.PayableID, Amount: "30.00 USD", ObservationID: "pay-obs-fault",
	}, "fault-pay")
	if err != nil || retry.AllocationID != first.AllocationID {
		t.Fatalf("retry = %+v err=%v, want original receipt", retry, err)
	}
	explain, err := registry.Explain(op, payable.PayableID)
	if err != nil {
		t.Fatal(err)
	}
	if explain.Paid != "30.00 USD" {
		t.Fatalf("explain = %+v, want single 30.00 posting", explain)
	}
	if _, err := registry.ReverseAllocation(op, first.AllocationID, "bank reversal ref-78", "fault-rev"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ReverseAllocation(op, first.AllocationID, "again", "fault-rev-2"); !errors.Is(err, ErrPayableState) {
		t.Fatalf("double reversal err = %v, want state refusal", err)
	}
	reopened, err := registry.Explain(op, payable.PayableID)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Paid != "0.00 USD" || reopened.Remaining != "60.00 USD" {
		t.Fatalf("explain = %+v, want paid 0 / due 60", reopened)
	}

	review, err := registry.AdmitAdjustment(op, payable.PayableID, AdjustmentInput{
		Revision: 2, ReceivedQty: "8", BilledQty: "10", BilledTotal: "100.00 USD",
		Reason: "late receipt", EvidenceRef: "recv-8", DecidedBy: "buyer-2",
	}, "fault-adj")
	if err != nil {
		t.Fatal(err)
	}
	if review.Status != "pending" {
		t.Fatalf("review = %+v, want pending status", review)
	}
	superseded, err := registry.ApproveAdjustmentReview(bill012Approver(), review.ReviewID, "fault-adj-ok")
	if err != nil {
		t.Fatal(err)
	}
	if superseded.ApprovedToPay != "80.00 USD" || superseded.Unmatched != "20.00 USD" || superseded.Revision != 2 {
		t.Fatalf("superseded = %+v, want approved 80 / unmatched 20 at rev 2", superseded)
	}

	data, err := registry.Export()
	if err != nil {
		t.Fatal(err)
	}
	restored := NewPayableRegistry(func() time.Time { return now })
	if err := restored.Import(data); err != nil {
		t.Fatal(err)
	}
	restoredExplain, err := restored.Explain(op, payable.PayableID)
	if err != nil {
		t.Fatal(err)
	}
	if restoredExplain.ApprovedToPay != "80.00 USD" || restoredExplain.Paid != "0.00 USD" {
		t.Fatalf("restored = %+v, want approved 80 / paid 0", restoredExplain)
	}
}

// TestTodo_SMB_BILL_012_Conformance pins every critical guard by name.
func TestTodo_SMB_BILL_012_Conformance(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	setup := func(t *testing.T) (*PayableRegistry, PayOp) {
		t.Helper()
		registry, _ := bill012Registry(now)
		op := bill012Op()
		if _, err := registry.AdmitMatchedBill(op, bill012Match(), "conf-admit"); err != nil {
			t.Fatal(err)
		}
		return registry, op
	}
	rows := []struct {
		name string
		run  func(*testing.T, *PayableRegistry, PayOp) error
		want error
	}{
		{"total mismatch blocks admit", func(t *testing.T, r *PayableRegistry, op PayOp) error {
			bad := bill012Match()
			bad.MatchID = "conf-match"
			bad.InvoiceNumber = "ACME-CONF"
			bad.BilledTotal = "101.00 USD"
			_, err := r.AdmitMatchedBill(op, bad, "conf-total")
			return err
		}, ErrPayableValidation},
		{"cross-tenant blocks admit", func(t *testing.T, r *PayableRegistry, op PayOp) error {
			foreign := op
			foreign.TenantID = "tenant-z"
			_, err := r.AdmitMatchedBill(foreign, bill012Match(), "conf-tenant")
			return err
		}, ErrPayableDenied},
		{"denied role blocks explain", func(t *testing.T, r *PayableRegistry, op PayOp) error {
			denied := op
			denied.PayableAllowed = false
			payable, err := r.PayableStatus(op, "conf-admit")
			if err != nil {
				return err
			}
			_, err = r.Explain(denied, payable.PayableID)
			return err
		}, ErrPayableDenied},
		{"guessed explain denied", func(t *testing.T, r *PayableRegistry, op PayOp) error {
			_, err := r.Explain(op, "pay-missing")
			return err
		}, ErrPayableDenied},
		{"pending observation never pays", func(t *testing.T, r *PayableRegistry, op PayOp) error {
			payable, err := r.PayableStatus(op, "conf-admit")
			if err != nil {
				return err
			}
			pending := bill012Observation(t, "pay-obs-conf", "tenant-a", operation.ObservationUnknown)
			if err := r.RegisterObservation(op, pending); err != nil {
				return err
			}
			_, err = r.AllocatePayment(op, PaymentInput{PayableID: payable.PayableID, Amount: "1.00 USD", ObservationID: "pay-obs-conf"}, "conf-pending")
			return err
		}, ErrPayableState},
		{"overpayment refused", func(t *testing.T, r *PayableRegistry, op PayOp) error {
			payable, err := r.PayableStatus(op, "conf-admit")
			if err != nil {
				return err
			}
			_, err = r.AllocatePayment(op, PaymentInput{PayableID: payable.PayableID, Amount: "61.00 USD", ManualEvidence: "manual:c", Label: "c"}, "conf-over")
			return err
		}, ErrPayableShortfall},
		{"quarantined never auto-pays", func(t *testing.T, r *PayableRegistry, op PayOp) error {
			clash := bill012Match()
			clash.MatchID = "conf-clash"
			clash.BilledTotal = "200.00 USD"
			clash.BilledQty = "20"
			blocked, err := r.AdmitMatchedBill(op, clash, "conf-clash")
			if err != nil {
				return err
			}
			_, err = r.AllocatePayment(op, PaymentInput{PayableID: blocked.PayableID, Amount: "1.00 USD", ManualEvidence: "manual:c", Label: "c"}, "conf-clash-pay")
			return err
		}, ErrPayableState},
		{"stale correction refused", func(t *testing.T, r *PayableRegistry, op PayOp) error {
			payable, err := r.PayableStatus(op, "conf-admit")
			if err != nil {
				return err
			}
			_, err = r.AdmitAdjustment(op, payable.PayableID, AdjustmentInput{
				Revision: 1, ReceivedQty: "5", BilledQty: "10", BilledTotal: "100.00 USD",
				Reason: "old", EvidenceRef: "recv-1", DecidedBy: "buyer-2",
			}, "conf-stale")
			return err
		}, ErrPayableStale},
		{"observation reuse conflicts", func(t *testing.T, r *PayableRegistry, op PayOp) error {
			payable, err := r.PayableStatus(op, "conf-admit")
			if err != nil {
				return err
			}
			obs := bill012Observation(t, "pay-obs-conf-reuse", "tenant-a", operation.ObservationApplied)
			if err := r.RegisterObservation(op, obs); err != nil {
				return err
			}
			first := PaymentInput{PayableID: payable.PayableID, Amount: "5.00 USD", ObservationID: "pay-obs-conf-reuse"}
			if _, err := r.AllocatePayment(op, first, "conf-reuse-1"); err != nil {
				return err
			}
			_, err = r.AllocatePayment(op, first, "conf-reuse-2")
			return err
		}, ErrPayableConflict},
		{"unknown key never fabricates", func(t *testing.T, r *PayableRegistry, op PayOp) error {
			_, err := r.PayableStatus(op, "conf-missing")
			return err
		}, ErrPayableDenied},
		{"key reuse with new payload conflicts", func(t *testing.T, r *PayableRegistry, op PayOp) error {
			changed := bill012Match()
			changed.MatchID = "conf-key-changed"
			changed.InvoiceNumber = "ACME-KEY"
			_, err := r.AdmitMatchedBill(op, changed, "conf-admit")
			return err
		}, ErrPayableConflict},
	}
	for _, row := range rows {
		registry, op := setup(t)
		if err := row.run(t, registry, op); !errors.Is(err, row.want) {
			t.Errorf("%s: err = %v, want %v", row.name, err, row.want)
		}
	}
}

// TestTodo_SMB_BILL_012_Mutation pins the two oracles proven by real
// flip-and-revert mutation runs during development (recorded in the issue
// evidence): the tenant-isolation check and the payment-due guard.
func TestTodo_SMB_BILL_012_Mutation(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry, _ := bill012Registry(now)
	op := bill012Op()
	if _, err := registry.AdmitMatchedBill(op, bill012Match(), "mut-admit"); err != nil {
		t.Fatal(err)
	}
	t.Run("tenant isolation oracle", func(t *testing.T) {
		foreign := op
		foreign.TenantID = "tenant-mutant"
		if _, err := registry.AdmitMatchedBill(foreign, bill012Match(), "mut-tenant"); !errors.Is(err, ErrPayableDenied) {
			t.Fatalf("mutant survived: cross-tenant admit err = %v", err)
		}
	})
	t.Run("payment due guard oracle", func(t *testing.T) {
		payable, err := registry.PayableStatus(op, "mut-admit")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := registry.AllocatePayment(op, PaymentInput{
			PayableID: payable.PayableID, Amount: "61.00 USD", ManualEvidence: "manual:mut", Label: "mut",
		}, "mut-over"); !errors.Is(err, ErrPayableShortfall) {
			t.Fatalf("mutant survived: overpayment err = %v", err)
		}
	})
}

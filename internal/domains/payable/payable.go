// Package payable owns supplier payable obligations.
//
// SMB-BILL-012 admits matched purchase bills, approved no-PO bills and
// approved contractor invoices into canonical supplier payables, then
// conserves every dollar through credits, observed payments and versioned
// corrections. Entry path is provenance, never identity: the same supplier
// invoice seen twice links one payable, while amount clashes and unresolved
// alias identity quarantine visibly instead of merging, double-paying or
// fabricating purchase and receipt records. Payments allocate only against
// applied observations or explicitly labeled manual evidence; reversals
// reopen exact amounts with history intact. All money math is exact.
package payable

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/contractortime"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrPayableValidation rejects malformed requests, footing mismatches
	// and unknown evidence references.
	ErrPayableValidation = errors.New("payable: payable request is invalid")
	// ErrPayableDenied is the single neutral response for denied
	// capabilities, foreign tenants and guessed identifiers.
	ErrPayableDenied = errors.New("payable: payable not found or not permitted")
	// ErrPayableConflict reports an idempotency key reused with a changed
	// payload, or a single-use observation or evidence consumed twice.
	ErrPayableConflict = errors.New("payable: payable idempotency conflict")
	// ErrPayableStale reports facts pinned behind the family head revision.
	ErrPayableStale = errors.New("payable: payable facts are stale")
	// ErrPayableShortfall reports a payment above the exact remaining due.
	ErrPayableShortfall = errors.New("payable: remaining due is insufficient")
	// ErrPayableState reports a transition the current state forbids, such
	// as paying a quarantined payable or allocating pending evidence.
	ErrPayableState = errors.New("payable: payable state forbids this transition")
)

// PayOp carries the authorized actor for one registry call.
type PayOp struct {
	TenantID       string
	LegalEntityID  string
	ActorID        string
	PayableAllowed bool
	ApproveAllowed bool
}

func (op PayOp) validate() error {
	if strings.TrimSpace(op.TenantID) == "" || strings.TrimSpace(op.LegalEntityID) == "" || strings.TrimSpace(op.ActorID) == "" {
		return fmt.Errorf("%w: operation needs tenant, legal entity and actor", ErrPayableValidation)
	}
	return nil
}

// Payable lifecycle states.
const (
	PayableOpen         = "open"
	PayableDiscrepancy  = "discrepancy"
	PayableQuarantined  = "quarantined"
	PayableResolvedLink = "resolved-linked"
	PayablePaid         = "paid"
)

// Payable is one canonical supplier obligation.
type Payable struct {
	PayableID      string
	FamilyID       string
	TenantID       string
	LegalEntityID  string
	SupplierID     string
	SupplierName   string
	InvoiceNumber  string
	Basis          string
	Purpose        string
	MatchingStatus string
	PORef          string
	ReceiptRef     string
	MatchID        string
	ApprovalID     string
	ContractorRef  string
	Currency       string
	TotalBilled    string
	ApprovedToPay  string
	Disputed       string
	Unmatched      string
	CreditsApplied string
	Paid           string
	Remaining      string
	DueDate        string
	Terms          string
	Status         string
	Revision       uint64
	Number         string
	LinkedPayable  string
	CreatedAt      string
}

// MatchBillInput admits one three-way-matched purchase bill.
type MatchBillInput struct {
	MatchID         string
	SupplierID      string
	SupplierName    string
	InvoiceNumber   string
	InvoiceRevision uint64
	OrderedQty      string
	UnitPrice       string
	ReceivedQty     string
	BilledQty       string
	BilledTotal     string
	DisputedQty     string
	Currency        string
	DueDate         string
	Terms           string
	Cutoff          string
	DecidedBy       string
	DecidedAt       string
}

// NonPOBillInput admits one approved no-PO bill.
type NonPOBillInput struct {
	ApprovalID       string
	ApprovalRevision uint64
	SupplierID       string
	SupplierName     string
	InvoiceNumber    string
	Amount           string
	Currency         string
	Category         string
	Terms            string
	TaxEvidence      string
	DueDate          string
	ApprovedBy       string
	ApprovedAt       string
}

// ContractorApproval carries the payable-side approval for contractor evidence.
type ContractorApproval struct {
	SupplierID   string
	SupplierName string
	ApprovedBy   string
	EvidenceRef  string
	DueDate      string
	Terms        string
}

// CreditInput records one supplier credit against a payable.
type CreditInput struct {
	PayableID string
	Amount    string
	SourceRef string
	Reason    string
}

// CreditReceipt answers one credit recording with its applied split.
type CreditReceipt struct {
	CreditID        string
	PayableID       string
	AppliedAmount   string
	UnappliedAmount string
	Key             string
}

// PaymentInput allocates one payment against a payable: exactly one of an
// applied observation reference or explicitly labeled manual evidence.
type PaymentInput struct {
	PayableID      string
	Amount         string
	ObservationID  string
	ManualEvidence string
	Label          string
}

// PaymentReceipt answers one allocation with the live remaining due.
type PaymentReceipt struct {
	AllocationID string
	PayableID    string
	Amount       string
	RemainingDue string
	Status       string
	Key          string
}

// AdjustmentInput proposes versioned new facts: match quantities for PO
// families, a revised amount for no-PO and contractor families.
type AdjustmentInput struct {
	Revision    uint64
	ReceivedQty string
	BilledQty   string
	BilledTotal string
	DisputedQty string
	Amount      string
	Reason      string
	EvidenceRef string
	DecidedBy   string
}

// AdjustmentReview is one pending versioned correction.
type AdjustmentReview struct {
	ReviewID          string
	PayableID         string
	Revision          uint64
	Status            string
	ProposedApproved  string
	ProposedUnmatched string
	Reason            string
	EvidenceRef       string
	RequestedBy       string
	At                string
}

// PayableProjection explains one payable with explicit conservation.
type PayableProjection struct {
	PayableID         string
	TotalBilled       string
	ApprovedToPay     string
	DisputedUnmatched string
	Credits           string
	Paid              string
	Remaining         string
	Status            string
	SourceLinks       []string
	AccountingStatus  string
}

// PayEvent is one audit entry on a payable family.
type PayEvent struct {
	Kind   string
	Actor  string
	At     string
	Detail string
}

type payMoneyValue struct {
	amount   values.Decimal
	currency string
}

func (m payMoneyValue) valid() bool { return m.currency != "" }

func (m payMoneyValue) String() string {
	if !m.valid() {
		return ""
	}
	return m.amount.String() + " " + m.currency
}

func payScaleOf(text string) int32 {
	if i := strings.IndexByte(text, '.'); i >= 0 {
		return int32(len(text) - i - 1)
	}
	return 0
}

func payParseDecimal(text string) (values.Decimal, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return values.Decimal{}, fmt.Errorf("decimal is required")
	}
	return values.NewDecimal(trimmed, payScaleOf(trimmed), values.RoundingExactRequired)
}

func payStrip(text string) string {
	dot := strings.IndexByte(text, '.')
	if dot < 0 {
		return text
	}
	end := len(text)
	for end > dot+1 && text[end-1] == '0' {
		end--
	}
	if end == dot+1 {
		return text[:dot]
	}
	return text[:end]
}

func payParseMoney(text string) (payMoneyValue, error) {
	parts := strings.SplitN(strings.TrimSpace(text), " ", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return payMoneyValue{}, fmt.Errorf("money %q must read like 100.00 USD", text)
	}
	amount, err := payParseDecimal(parts[0])
	if err != nil {
		return payMoneyValue{}, fmt.Errorf("money %q: %v", text, err)
	}
	return payMoneyValue{amount: amount, currency: strings.TrimSpace(parts[1])}, nil
}

func payCents(d values.Decimal, currency string) (payMoneyValue, error) {
	canonical := payStrip(d.String())
	if payScaleOf(canonical) > 2 {
		return payMoneyValue{}, fmt.Errorf("amount %s needs more than cents", canonical)
	}
	quantized, err := values.NewDecimal(canonical, 2, values.RoundingExactRequired)
	if err != nil {
		return payMoneyValue{}, err
	}
	return payMoneyValue{amount: quantized, currency: currency}, nil
}

func payAlign(a, b values.Decimal) (values.Decimal, values.Decimal, error) {
	target := a.Scale()
	if b.Scale() > target {
		target = b.Scale()
	}
	qa, err := a.Quantize(target, values.RoundingExactRequired)
	if err != nil {
		return values.Decimal{}, values.Decimal{}, err
	}
	qb, err := b.Quantize(target, values.RoundingExactRequired)
	if err != nil {
		return values.Decimal{}, values.Decimal{}, err
	}
	return qa, qb, nil
}

func payAdd(a, b values.Decimal) (values.Decimal, error) {
	qa, qb, err := payAlign(a, b)
	if err != nil {
		return values.Decimal{}, err
	}
	return qa.Add(qb)
}

func paySub(a, b values.Decimal) (values.Decimal, error) {
	qa, qb, err := payAlign(a, b)
	if err != nil {
		return values.Decimal{}, err
	}
	return qa.Sub(qb)
}

func payZero() values.Decimal {
	zero, _ := values.NewDecimal("0", 0, values.RoundingExactRequired)
	return zero
}

func payZeroMoney(currency string) payMoneyValue {
	zero, _ := payCents(payZero(), currency)
	return zero
}

type payEntry struct {
	Owner    string
	RefID    string
	Revision uint64
	At       string
}

type payCredit struct {
	CreditID  string
	Applied   payMoneyValue
	Unapplied payMoneyValue
	SourceRef string
	Reason    string
	At        string
}

type payPayment struct {
	AllocationID string
	Amount       payMoneyValue
	Observation  string
	Manual       string
	Label        string
	Status       string
	Key          string
	At           string
}

type payReview struct {
	ReviewID   string
	Revision   uint64
	Status     string
	Received   values.Decimal
	Billed     values.Decimal
	BilledHeld payMoneyValue
	Disputed   values.Decimal
	Amount     payMoneyValue
	Reason     string
	Evidence   string
	DecidedBy  string
	At         string
}

type payFamily struct {
	id           string
	tenant       string
	entity       string
	supplier     string
	supplierName string
	extNumber    string
	payable      *Payable
	owner        string
	basis        string
	purpose      string
	total        payMoneyValue
	approved     payMoneyValue
	disputed     payMoneyValue
	unmatched    payMoneyValue
	unitPrice    payMoneyValue
	paid         payMoneyValue
	credits      payMoneyValue
	unapplied    payMoneyValue
	revision     uint64
	entries      []payEntry
	creditList   []*payCredit
	payments     []*payPayment
	reviews      []*payReview
	events       []PayEvent
}

type payIdem struct {
	fingerprint string
	payableID   string
	receipt     string
	tenant      string
	entity      string
}

type payStoredObservation struct {
	verdict   operation.ObservationVerdict
	authority string
	usedBy    string
}

// PayableRegistry guards supplier payables for every tenant.
type PayableRegistry struct {
	mu           sync.Mutex
	clock        func() time.Time
	families     map[string]*payFamily
	payables     map[string]*payFamily
	blocked      map[string]*Payable
	suppliers    map[string]string
	supplierName map[string]string
	observations map[string]*payStoredObservation
	credits      map[string]*payCredit
	payments     map[string]*payPayment
	idem         map[string]payIdem
	seq          uint64
	numSeq       map[string]uint64
}

// NewPayableRegistry returns an empty registry using clock for audit stamps.
func NewPayableRegistry(clock func() time.Time) *PayableRegistry {
	if clock == nil {
		clock = time.Now
	}
	return &PayableRegistry{
		clock:        clock,
		families:     map[string]*payFamily{},
		payables:     map[string]*payFamily{},
		blocked:      map[string]*Payable{},
		suppliers:    map[string]string{},
		supplierName: map[string]string{},
		observations: map[string]*payStoredObservation{},
		credits:      map[string]*payCredit{},
		payments:     map[string]*payPayment{},
		idem:         map[string]payIdem{},
		numSeq:       map[string]uint64{},
	}
}

func payKey(parts ...string) string { return strings.Join(parts, "\x00") }

func payNow(r *PayableRegistry) time.Time {
	if r.clock == nil {
		return time.Now().UTC()
	}
	return r.clock().UTC()
}

func payStamp(r *PayableRegistry) string { return payNow(r).Format(time.RFC3339) }

func payParseTime(raw string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if at, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return at, nil
	}
	if day, err := time.Parse("2006-01-02", trimmed); err == nil {
		return day, nil
	}
	return time.Time{}, fmt.Errorf("timestamp %q must be RFC3339 or YYYY-MM-DD", raw)
}

// resolveSupplier verifies the caller's supplier identity against the
// tenant crosswalk. Unknown suppliers whose invoice number already exists
// under another supplier cannot resolve and must quarantine; conflicting
// names for one supplier id are equally unresolvable.
func (r *PayableRegistry) resolveSupplier(op PayOp, supplierID, supplierName, extNumber string) (string, error) {
	id := strings.TrimSpace(supplierID)
	name := strings.TrimSpace(supplierName)
	if id == "" {
		return "", fmt.Errorf("%w: supplier identity is required", ErrPayableValidation)
	}
	for _, family := range r.families {
		if family.supplier == id && family.extNumber == strings.TrimSpace(extNumber) &&
			(family.tenant != op.TenantID || family.entity != op.LegalEntityID) {
			return "", ErrPayableDenied
		}
	}
	idKey := payKey(op.TenantID, op.LegalEntityID, id)
	if _, ok := r.suppliers[idKey]; !ok {
		for _, family := range r.families {
			if family.tenant == op.TenantID && family.entity == op.LegalEntityID && family.extNumber == strings.TrimSpace(extNumber) {
				return "", ErrPayableQuarantine
			}
		}
		r.suppliers[idKey] = id
		if name != "" {
			r.supplierName[payKey(op.TenantID, op.LegalEntityID, name)] = id
		}
		return id, nil
	}
	if name != "" {
		if owner, taken := r.supplierName[payKey(op.TenantID, op.LegalEntityID, name)]; taken && owner != id {
			return "", ErrPayableQuarantine
		}
		r.supplierName[payKey(op.TenantID, op.LegalEntityID, name)] = id
	}
	return id, nil
}

// ErrPayableQuarantine signals identity that must quarantine visibly instead
// of merging or paying. It is internal: callers translate it into a blocked
// record, never a silent merge or a second obligation.
var ErrPayableQuarantine = errors.New("payable: supplier identity is unresolved")

func (r *PayableRegistry) nextNumber(tenant string) string {
	r.numSeq[tenant]++
	return fmt.Sprintf("AP-%04d", r.numSeq[tenant])
}

func (r *PayableRegistry) refreshPayable(family *payFamily) {
	paid := family.paid.amount
	applied := family.credits.amount
	consumed, _ := payAdd(paid, applied)
	due, _ := paySub(family.approved.amount, consumed)
	if due.Sign() < 0 {
		due = payZero()
	}
	kept, _ := payCents(due, family.approved.currency)
	payable := family.payable
	payable.TotalBilled = family.total.String()
	payable.ApprovedToPay = family.approved.String()
	payable.Disputed = family.disputed.String()
	payable.Unmatched = family.unmatched.String()
	payable.CreditsApplied = family.credits.String()
	payable.Paid = family.paid.String()
	payable.Remaining = kept.String()
	payable.Revision = family.revision
	if kept.amount.Sign() == 0 && (paid.Sign() > 0 || applied.Sign() > 0) {
		payable.Status = PayablePaid
	} else if payable.Status == PayablePaid {
		payable.Status = PayableOpen
	}
}

func (r *PayableRegistry) checkIdem(op PayOp, key, fingerprint string) (*payFamily, *Payable, bool, error) {
	if strings.TrimSpace(key) == "" {
		return nil, nil, false, fmt.Errorf("%w: idempotency key is required", ErrPayableValidation)
	}
	prior, ok := r.idem[payKey(op.TenantID, strings.TrimSpace(key))]
	if !ok {
		return nil, nil, false, nil
	}
	if prior.tenant != op.TenantID || prior.entity != op.LegalEntityID {
		return nil, nil, false, ErrPayableDenied
	}
	if prior.fingerprint != fingerprint {
		return nil, nil, false, fmt.Errorf("%w: key is already used", ErrPayableConflict)
	}
	if family, ok := r.payables[prior.payableID]; ok {
		return family, family.payable, true, nil
	}
	if blocked, ok := r.blocked[prior.payableID]; ok {
		return nil, blocked, true, nil
	}
	return nil, nil, false, ErrPayableDenied
}

func matchFingerprint(input MatchBillInput) string {
	return strings.Join([]string{"match", input.MatchID, input.SupplierID, input.SupplierName, input.InvoiceNumber,
		fmt.Sprintf("%d", input.InvoiceRevision), input.OrderedQty, input.UnitPrice, input.ReceivedQty, input.BilledQty,
		input.BilledTotal, input.DisputedQty, input.Currency, input.DueDate, input.Terms, input.Cutoff, input.DecidedBy, input.DecidedAt}, "\x00")
}

// AdmitMatchedBill admits one three-way-matched purchase bill into its
// canonical family. Repeat intake links the same payable; same-number
// different-amount intake quarantines as a discrepancy without obligation.
func (r *PayableRegistry) AdmitMatchedBill(op PayOp, input MatchBillInput, key string) (Payable, error) {
	if err := op.validate(); err != nil {
		return Payable{}, err
	}
	if !op.PayableAllowed {
		return Payable{}, ErrPayableDenied
	}
	if strings.TrimSpace(input.MatchID) == "" || strings.TrimSpace(input.InvoiceNumber) == "" {
		return Payable{}, fmt.Errorf("%w: match needs identity and invoice number", ErrPayableValidation)
	}
	if strings.TrimSpace(input.DecidedBy) == "" {
		return Payable{}, fmt.Errorf("%w: match needs its decider", ErrPayableValidation)
	}
	if _, err := payParseTime(input.DecidedAt); err != nil {
		return Payable{}, fmt.Errorf("%w: %v", ErrPayableValidation, err)
	}
	if _, err := payParseTime(input.Cutoff); err != nil {
		return Payable{}, fmt.Errorf("%w: cutoff %v", ErrPayableValidation, err)
	}
	if _, err := payParseTime(input.DueDate); err != nil {
		return Payable{}, fmt.Errorf("%w: due date %v", ErrPayableValidation, err)
	}
	ordered, err := payParseDecimal(input.OrderedQty)
	if err != nil || ordered.Sign() <= 0 {
		return Payable{}, fmt.Errorf("%w: ordered quantity must be positive", ErrPayableValidation)
	}
	received, err := payParseDecimal(input.ReceivedQty)
	if err != nil || received.Sign() < 0 {
		return Payable{}, fmt.Errorf("%w: received quantity cannot be negative", ErrPayableValidation)
	}
	billed, err := payParseDecimal(input.BilledQty)
	if err != nil || billed.Sign() <= 0 {
		return Payable{}, fmt.Errorf("%w: billed quantity must be positive", ErrPayableValidation)
	}
	disputed, err := payParseDecimal(input.DisputedQty)
	if err != nil || disputed.Sign() < 0 {
		return Payable{}, fmt.Errorf("%w: disputed quantity cannot be negative", ErrPayableValidation)
	}
	if received.Cmp(billed) > 0 {
		return Payable{}, fmt.Errorf("%w: received exceeds billed", ErrPayableValidation)
	}
	price, err := payParseMoney(input.UnitPrice)
	if err != nil {
		return Payable{}, fmt.Errorf("%w: %v", ErrPayableValidation, err)
	}
	total, err := payParseMoney(input.BilledTotal)
	if err != nil {
		return Payable{}, fmt.Errorf("%w: %v", ErrPayableValidation, err)
	}
	if price.currency != total.currency || total.currency != strings.TrimSpace(input.Currency) {
		return Payable{}, fmt.Errorf("%w: match currencies must agree", ErrPayableValidation)
	}
	if price.amount.Sign() <= 0 || total.amount.Sign() <= 0 {
		return Payable{}, fmt.Errorf("%w: match money must be positive", ErrPayableValidation)
	}
	footed, err := billed.Mul(price.amount, billed.Scale()+price.amount.Scale(), values.RoundingExactRequired)
	if err != nil {
		return Payable{}, fmt.Errorf("%w: footing: %v", ErrPayableValidation, err)
	}
	if footed.Cmp(total.amount) != 0 {
		return Payable{}, fmt.Errorf("%w: billed total does not foot to quantity and price", ErrPayableValidation)
	}
	approved, err := received.Mul(price.amount, received.Scale()+price.amount.Scale(), values.RoundingExactRequired)
	if err != nil {
		return Payable{}, fmt.Errorf("%w: approval: %v", ErrPayableValidation, err)
	}
	approvedMoney, err := payCents(approved, total.currency)
	if err != nil {
		return Payable{}, fmt.Errorf("%w: %v", ErrPayableValidation, err)
	}
	disputedMoney, err := payCents(mustPayMul(disputed, price.amount), total.currency)
	if err != nil {
		return Payable{}, fmt.Errorf("%w: %v", ErrPayableValidation, err)
	}
	open, err := paySub(total.amount, approvedMoney.amount)
	if err != nil {
		return Payable{}, err
	}
	open, err = paySub(open, disputedMoney.amount)
	if err != nil {
		return Payable{}, err
	}
	unmatched, err := payCents(open, total.currency)
	if err != nil {
		return Payable{}, fmt.Errorf("%w: %v", ErrPayableValidation, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	_, replay, ok, err := r.checkIdem(op, key, matchFingerprint(input))
	if err != nil || ok {
		if err != nil {
			return Payable{}, err
		}
		return *replay, nil
	}
	supplier, err := r.resolveSupplier(op, input.SupplierID, input.SupplierName, input.InvoiceNumber)
	if err != nil {
		if errors.Is(err, ErrPayableQuarantine) {
			return r.quarantine(op, key, matchFingerprint(input), input.SupplierID, input.SupplierName, input.InvoiceNumber, total, "unresolved supplier identity")
		}
		return Payable{}, err
	}
	famKey := payKey(op.TenantID, op.LegalEntityID, supplier, strings.TrimSpace(input.InvoiceNumber))
	if family, ok := r.families[famKey]; ok {
		if family.total.amount.Cmp(total.amount) != 0 || family.total.currency != total.currency {
			return r.discrepancy(op, key, matchFingerprint(input), family, input.InvoiceNumber, total, "same number with differing amount")
		}
		family.entries = append(family.entries, payEntry{Owner: "BUY007", RefID: strings.TrimSpace(input.MatchID), Revision: input.InvoiceRevision, At: payStamp(r)})
		r.idem[payKey(op.TenantID, strings.TrimSpace(key))] = payIdem{fingerprint: matchFingerprint(input), payableID: family.payable.PayableID, tenant: op.TenantID, entity: op.LegalEntityID}
		return *family.payable, nil
	}
	r.seq++
	payable := &Payable{
		PayableID: fmt.Sprintf("pay-%d", r.seq), FamilyID: famKey,
		TenantID: op.TenantID, LegalEntityID: op.LegalEntityID,
		SupplierID: supplier, SupplierName: strings.TrimSpace(input.SupplierName), InvoiceNumber: strings.TrimSpace(input.InvoiceNumber),
		Basis: "PO_MATCHED", Purpose: "purchase:" + strings.TrimSpace(input.MatchID), MatchingStatus: "Three-way matched",
		PORef: strings.TrimSpace(input.MatchID), ReceiptRef: "cutoff:" + strings.TrimSpace(input.Cutoff),
		MatchID: strings.TrimSpace(input.MatchID), Currency: total.currency,
		DueDate: strings.TrimSpace(input.DueDate), Terms: strings.TrimSpace(input.Terms),
		Status: PayableOpen, Revision: input.InvoiceRevision, Number: r.nextNumber(op.TenantID), CreatedAt: payStamp(r),
	}
	family := &payFamily{
		id: famKey, tenant: op.TenantID, entity: op.LegalEntityID, supplier: supplier,
		supplierName: strings.TrimSpace(input.SupplierName), extNumber: strings.TrimSpace(input.InvoiceNumber),
		payable: payable, owner: "BUY007", basis: "PO_MATCHED", purpose: payable.Purpose,
		total: total, approved: approvedMoney, disputed: disputedMoney, unmatched: unmatched,
		unitPrice: price, paid: payZeroMoney(total.currency), credits: payZeroMoney(total.currency),
		unapplied: payZeroMoney(total.currency), revision: input.InvoiceRevision,
		entries: []payEntry{{Owner: "BUY007", RefID: strings.TrimSpace(input.MatchID), Revision: input.InvoiceRevision, At: payable.CreatedAt}},
	}
	family.events = append(family.events, PayEvent{Kind: "ADMITTED", Actor: op.ActorID, At: payable.CreatedAt, Detail: "PO_MATCHED " + total.String()})
	r.families[famKey] = family
	r.payables[payable.PayableID] = family
	r.idem[payKey(op.TenantID, strings.TrimSpace(key))] = payIdem{fingerprint: matchFingerprint(input), payableID: payable.PayableID, tenant: op.TenantID, entity: op.LegalEntityID}
	r.refreshPayable(family)
	return *payable, nil
}

func mustPayMul(a, b values.Decimal) values.Decimal {
	product, err := a.Mul(b, a.Scale()+b.Scale(), values.RoundingExactRequired)
	if err != nil {
		return payZero()
	}
	return product
}

func (r *PayableRegistry) quarantine(op PayOp, key, fingerprint, supplier, name, number string, total payMoneyValue, reason string) (Payable, error) {
	r.seq++
	blocked := &Payable{
		PayableID: fmt.Sprintf("pay-%d", r.seq), TenantID: op.TenantID, LegalEntityID: op.LegalEntityID,
		SupplierID: strings.TrimSpace(supplier), SupplierName: strings.TrimSpace(name), InvoiceNumber: strings.TrimSpace(number),
		Basis: "QUARANTINE", Currency: total.currency, TotalBilled: total.String(),
		ApprovedToPay: payZeroMoney(total.currency).String(), Disputed: payZeroMoney(total.currency).String(),
		Unmatched: total.String(), CreditsApplied: payZeroMoney(total.currency).String(), Paid: payZeroMoney(total.currency).String(),
		Remaining: payZeroMoney(total.currency).String(), Status: PayableQuarantined, Revision: 1,
		Number: r.nextNumber(op.TenantID), CreatedAt: payStamp(r),
	}
	_ = reason
	r.blocked[blocked.PayableID] = blocked
	r.idem[payKey(op.TenantID, strings.TrimSpace(key))] = payIdem{fingerprint: fingerprint, payableID: blocked.PayableID, tenant: op.TenantID, entity: op.LegalEntityID}
	return *blocked, nil
}

func (r *PayableRegistry) discrepancy(op PayOp, key, fingerprint string, family *payFamily, number string, total payMoneyValue, reason string) (Payable, error) {
	r.seq++
	blocked := &Payable{
		PayableID: fmt.Sprintf("pay-%d", r.seq), TenantID: op.TenantID, LegalEntityID: op.LegalEntityID,
		SupplierID: family.supplier, SupplierName: family.supplierName, InvoiceNumber: strings.TrimSpace(number),
		Basis: family.basis, Currency: total.currency, TotalBilled: total.String(),
		ApprovedToPay: payZeroMoney(total.currency).String(), Disputed: total.String(),
		Unmatched: payZeroMoney(total.currency).String(), CreditsApplied: payZeroMoney(total.currency).String(),
		Paid: payZeroMoney(total.currency).String(), Remaining: payZeroMoney(total.currency).String(),
		Status: PayableDiscrepancy, Revision: 1, Number: r.nextNumber(op.TenantID),
		LinkedPayable: family.payable.PayableID, CreatedAt: payStamp(r),
	}
	_ = reason
	r.blocked[blocked.PayableID] = blocked
	r.idem[payKey(op.TenantID, strings.TrimSpace(key))] = payIdem{fingerprint: fingerprint, payableID: blocked.PayableID, tenant: op.TenantID, entity: op.LegalEntityID}
	family.events = append(family.events, PayEvent{Kind: "DISCREPANCY", Actor: op.ActorID, At: blocked.CreatedAt, Detail: blocked.PayableID})
	return *blocked, nil
}

func nonPOFingerprint(input NonPOBillInput) string {
	return strings.Join([]string{"nonpo", input.ApprovalID, fmt.Sprintf("%d", input.ApprovalRevision), input.SupplierID,
		input.SupplierName, input.InvoiceNumber, input.Amount, input.Currency, input.Category, input.Terms,
		input.TaxEvidence, input.DueDate, input.ApprovedBy, input.ApprovedAt}, "\x00")
}

// AdmitNonPOBill admits one approved no-PO bill. No purchase or receipt
// record is fabricated: the payable carries explicit no-PO markers.
func (r *PayableRegistry) AdmitNonPOBill(op PayOp, input NonPOBillInput, key string) (Payable, error) {
	if err := op.validate(); err != nil {
		return Payable{}, err
	}
	if !op.PayableAllowed {
		return Payable{}, ErrPayableDenied
	}
	if strings.TrimSpace(input.ApprovalID) == "" || strings.TrimSpace(input.InvoiceNumber) == "" {
		return Payable{}, fmt.Errorf("%w: no-PO bill needs approval and invoice number", ErrPayableValidation)
	}
	if strings.TrimSpace(input.ApprovedBy) == "" {
		return Payable{}, fmt.Errorf("%w: no-PO bill needs its approver", ErrPayableValidation)
	}
	if _, err := payParseTime(input.ApprovedAt); err != nil {
		return Payable{}, fmt.Errorf("%w: %v", ErrPayableValidation, err)
	}
	if _, err := payParseTime(input.DueDate); err != nil {
		return Payable{}, fmt.Errorf("%w: due date %v", ErrPayableValidation, err)
	}
	if strings.TrimSpace(input.Category) == "" || strings.TrimSpace(input.TaxEvidence) == "" {
		return Payable{}, fmt.Errorf("%w: no-PO bill needs category and tax evidence", ErrPayableValidation)
	}
	amount, err := payParseMoney(input.Amount)
	if err != nil {
		return Payable{}, fmt.Errorf("%w: %v", ErrPayableValidation, err)
	}
	if amount.amount.Sign() <= 0 || amount.currency != strings.TrimSpace(input.Currency) {
		return Payable{}, fmt.Errorf("%w: no-PO amount must be positive %s money", ErrPayableValidation, input.Currency)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, replay, ok, err := r.checkIdem(op, key, nonPOFingerprint(input))
	if err != nil || ok {
		if err != nil {
			return Payable{}, err
		}
		return *replay, nil
	}
	supplier, err := r.resolveSupplier(op, input.SupplierID, input.SupplierName, input.InvoiceNumber)
	if err != nil {
		if errors.Is(err, ErrPayableQuarantine) {
			return r.quarantine(op, key, nonPOFingerprint(input), input.SupplierID, input.SupplierName, input.InvoiceNumber, amount, "unresolved supplier identity")
		}
		return Payable{}, err
	}
	famKey := payKey(op.TenantID, op.LegalEntityID, supplier, strings.TrimSpace(input.InvoiceNumber))
	if family, ok := r.families[famKey]; ok {
		if family.total.amount.Cmp(amount.amount) != 0 || family.total.currency != amount.currency {
			return r.discrepancy(op, key, nonPOFingerprint(input), family, input.InvoiceNumber, amount, "same number with differing amount")
		}
		family.entries = append(family.entries, payEntry{Owner: "BUY009", RefID: strings.TrimSpace(input.ApprovalID), Revision: input.ApprovalRevision, At: payStamp(r)})
		r.idem[payKey(op.TenantID, strings.TrimSpace(key))] = payIdem{fingerprint: nonPOFingerprint(input), payableID: family.payable.PayableID, tenant: op.TenantID, entity: op.LegalEntityID}
		return *family.payable, nil
	}
	r.seq++
	payable := &Payable{
		PayableID: fmt.Sprintf("pay-%d", r.seq), FamilyID: famKey,
		TenantID: op.TenantID, LegalEntityID: op.LegalEntityID,
		SupplierID: supplier, SupplierName: strings.TrimSpace(input.SupplierName), InvoiceNumber: strings.TrimSpace(input.InvoiceNumber),
		Basis: "NON_PO_APPROVED", Purpose: "nonpo:" + strings.TrimSpace(input.Category), MatchingStatus: "Matching not applicable",
		ApprovalID: strings.TrimSpace(input.ApprovalID), Currency: amount.currency,
		DueDate: strings.TrimSpace(input.DueDate), Terms: strings.TrimSpace(input.Terms),
		Status: PayableOpen, Revision: input.ApprovalRevision, Number: r.nextNumber(op.TenantID), CreatedAt: payStamp(r),
	}
	family := &payFamily{
		id: famKey, tenant: op.TenantID, entity: op.LegalEntityID, supplier: supplier,
		supplierName: strings.TrimSpace(input.SupplierName), extNumber: strings.TrimSpace(input.InvoiceNumber),
		payable: payable, owner: "BUY009", basis: "NON_PO_APPROVED", purpose: payable.Purpose,
		total: amount, approved: amount, disputed: payZeroMoney(amount.currency), unmatched: payZeroMoney(amount.currency),
		paid: payZeroMoney(amount.currency), credits: payZeroMoney(amount.currency), unapplied: payZeroMoney(amount.currency),
		revision: input.ApprovalRevision,
		entries:  []payEntry{{Owner: "BUY009", RefID: strings.TrimSpace(input.ApprovalID), Revision: input.ApprovalRevision, At: payable.CreatedAt}},
	}
	family.events = append(family.events, PayEvent{Kind: "ADMITTED", Actor: op.ActorID, At: payable.CreatedAt, Detail: "NON_PO_APPROVED " + amount.String()})
	r.families[famKey] = family
	r.payables[payable.PayableID] = family
	r.idem[payKey(op.TenantID, strings.TrimSpace(key))] = payIdem{fingerprint: nonPOFingerprint(input), payableID: family.payable.PayableID, tenant: op.TenantID, entity: op.LegalEntityID}
	r.refreshPayable(family)
	return *payable, nil
}

func contractorFingerprint(draft contractortime.InvoiceDraft, approval ContractorApproval) string {
	return strings.Join([]string{"contractor", draft.ID, draft.Digest, approval.SupplierID, approval.SupplierName, approval.ApprovedBy, approval.EvidenceRef, approval.DueDate, approval.Terms}, "\x00")
}

// AdmitContractor admits one approved contractor invoice. Contractor purpose
// (worker and engagement) is retained on the payable; no purchase order is
// fabricated. The same identified invoice links one payable in either entry
// order through the verified alias crosswalk.
func (r *PayableRegistry) AdmitContractor(op PayOp, draft contractortime.InvoiceDraft, approval ContractorApproval, key string) (Payable, error) {
	if err := op.validate(); err != nil {
		return Payable{}, err
	}
	if !op.PayableAllowed {
		return Payable{}, ErrPayableDenied
	}
	if err := draft.Validate(); err != nil {
		return Payable{}, fmt.Errorf("%w: contractor draft: %v", ErrPayableValidation, err)
	}
	if strings.TrimSpace(draft.Digest) == "" {
		return Payable{}, fmt.Errorf("%w: contractor draft needs its digest", ErrPayableValidation)
	}
	if strings.TrimSpace(approval.SupplierID) == "" || strings.TrimSpace(approval.ApprovedBy) == "" || strings.TrimSpace(approval.EvidenceRef) == "" {
		return Payable{}, fmt.Errorf("%w: contractor admission needs supplier, approver and evidence", ErrPayableValidation)
	}
	if _, err := payParseTime(approval.DueDate); err != nil {
		return Payable{}, fmt.Errorf("%w: due date %v", ErrPayableValidation, err)
	}
	total, err := payParseMoney(draft.Total.Amount().String() + " " + draft.Currency)
	if err != nil {
		return Payable{}, fmt.Errorf("%w: contractor total: %v", ErrPayableValidation, err)
	}
	if total.amount.Sign() <= 0 {
		return Payable{}, fmt.Errorf("%w: contractor total must be positive", ErrPayableValidation)
	}
	purpose := "contractor:" + strings.TrimSpace(draft.WorkerRef) + ":" + strings.TrimSpace(draft.EngagementID)
	r.mu.Lock()
	defer r.mu.Unlock()
	_, replay, ok, err := r.checkIdem(op, key, contractorFingerprint(draft, approval))
	if err != nil || ok {
		if err != nil {
			return Payable{}, err
		}
		return *replay, nil
	}
	supplier, err := r.resolveSupplier(op, approval.SupplierID, approval.SupplierName, draft.ID)
	if err != nil {
		if errors.Is(err, ErrPayableQuarantine) {
			return r.quarantine(op, key, contractorFingerprint(draft, approval), approval.SupplierID, approval.SupplierName, draft.ID, total, "unresolved supplier identity")
		}
		return Payable{}, err
	}
	famKey := payKey(op.TenantID, op.LegalEntityID, supplier, strings.TrimSpace(draft.ID))
	if family, ok := r.families[famKey]; ok {
		if family.total.amount.Cmp(total.amount) != 0 || family.total.currency != total.currency {
			return r.discrepancy(op, key, contractorFingerprint(draft, approval), family, draft.ID, total, "same number with differing amount")
		}
		family.entries = append(family.entries, payEntry{Owner: "contractortime", RefID: strings.TrimSpace(draft.ID), Revision: 1, At: payStamp(r)})
		r.idem[payKey(op.TenantID, strings.TrimSpace(key))] = payIdem{fingerprint: contractorFingerprint(draft, approval), payableID: family.payable.PayableID, tenant: op.TenantID, entity: op.LegalEntityID}
		return *family.payable, nil
	}
	r.seq++
	payable := &Payable{
		PayableID: fmt.Sprintf("pay-%d", r.seq), FamilyID: famKey,
		TenantID: op.TenantID, LegalEntityID: op.LegalEntityID,
		SupplierID: supplier, SupplierName: strings.TrimSpace(approval.SupplierName), InvoiceNumber: strings.TrimSpace(draft.ID),
		Basis: "CONTRACTOR_APPROVED", Purpose: purpose, MatchingStatus: "Contractor approved",
		ContractorRef: strings.TrimSpace(draft.ID), Currency: total.currency,
		DueDate: strings.TrimSpace(approval.DueDate), Terms: strings.TrimSpace(approval.Terms),
		Status: PayableOpen, Revision: 1, Number: r.nextNumber(op.TenantID), CreatedAt: payStamp(r),
	}
	family := &payFamily{
		id: famKey, tenant: op.TenantID, entity: op.LegalEntityID, supplier: supplier,
		supplierName: strings.TrimSpace(approval.SupplierName), extNumber: strings.TrimSpace(draft.ID),
		payable: payable, owner: "contractortime", basis: "CONTRACTOR_APPROVED", purpose: purpose,
		total: total, approved: total, disputed: payZeroMoney(total.currency), unmatched: payZeroMoney(total.currency),
		paid: payZeroMoney(total.currency), credits: payZeroMoney(total.currency), unapplied: payZeroMoney(total.currency),
		revision: 1,
		entries:  []payEntry{{Owner: "contractortime", RefID: strings.TrimSpace(draft.ID), Revision: 1, At: payable.CreatedAt}},
	}
	family.events = append(family.events, PayEvent{Kind: "ADMITTED", Actor: op.ActorID, At: payable.CreatedAt, Detail: "CONTRACTOR_APPROVED " + total.String()})
	r.families[famKey] = family
	r.payables[payable.PayableID] = family
	r.idem[payKey(op.TenantID, strings.TrimSpace(key))] = payIdem{fingerprint: contractorFingerprint(draft, approval), payableID: family.payable.PayableID, tenant: op.TenantID, entity: op.LegalEntityID}
	r.refreshPayable(family)
	return *payable, nil
}
func (r *PayableRegistry) familyFor(op PayOp, payableID string) (*payFamily, error) {
	family, ok := r.payables[strings.TrimSpace(payableID)]
	if !ok || family.tenant != op.TenantID || family.entity != op.LegalEntityID {
		if blocked, ok := r.blocked[strings.TrimSpace(payableID)]; ok {
			if blocked.TenantID != op.TenantID || blocked.LegalEntityID != op.LegalEntityID {
				return nil, ErrPayableDenied
			}
			return nil, fmt.Errorf("%w: payable is %s", ErrPayableState, blocked.Status)
		}
		return nil, ErrPayableDenied
	}
	return family, nil
}

func (r *PayableRegistry) remainingDue(family *payFamily) (payMoneyValue, error) {
	consumed, err := payAdd(family.paid.amount, family.credits.amount)
	if err != nil {
		return payMoneyValue{}, err
	}
	due, err := paySub(family.approved.amount, consumed)
	if err != nil {
		return payMoneyValue{}, err
	}
	if due.Sign() < 0 {
		due = payZero()
	}
	return payCents(due, family.approved.currency)
}

// RecordCredit records one supplier credit against an open payable. The
// applied portion can never exceed the remaining due; any excess stays
// visible as unapplied instead of over-reducing the obligation. Recording
// the same source observation twice replays the original receipt.
func (r *PayableRegistry) RecordCredit(op PayOp, input CreditInput, key string) (CreditReceipt, error) {
	if err := op.validate(); err != nil {
		return CreditReceipt{}, err
	}
	if !op.PayableAllowed {
		return CreditReceipt{}, ErrPayableDenied
	}
	if strings.TrimSpace(input.SourceRef) == "" || strings.TrimSpace(input.Reason) == "" {
		return CreditReceipt{}, fmt.Errorf("%w: credit needs source and reason", ErrPayableValidation)
	}
	amount, err := payParseMoney(input.Amount)
	if err != nil {
		return CreditReceipt{}, fmt.Errorf("%w: %v", ErrPayableValidation, err)
	}
	if amount.amount.Sign() <= 0 {
		return CreditReceipt{}, fmt.Errorf("%w: credit amount must be positive", ErrPayableValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	family, err := r.familyFor(op, input.PayableID)
	if err != nil {
		return CreditReceipt{}, err
	}
	if amount.currency != family.approved.currency {
		return CreditReceipt{}, fmt.Errorf("%w: credit currency does not match", ErrPayableValidation)
	}
	for _, existing := range family.creditList {
		if existing.SourceRef == strings.TrimSpace(input.SourceRef) {
			if payTotalOf(existing).amount.Cmp(amount.amount) != 0 {
				return CreditReceipt{}, fmt.Errorf("%w: credit source is already recorded", ErrPayableConflict)
			}
			return CreditReceipt{CreditID: existing.CreditID, PayableID: family.payable.PayableID,
				AppliedAmount: existing.Applied.String(), UnappliedAmount: existing.Unapplied.String(), Key: strings.TrimSpace(key)}, nil
		}
	}
	due, err := r.remainingDue(family)
	if err != nil {
		return CreditReceipt{}, err
	}
	applied := amount.amount
	if applied.Cmp(due.amount) > 0 {
		applied = due.amount
	}
	rest, err := paySub(amount.amount, applied)
	if err != nil {
		return CreditReceipt{}, err
	}
	appliedMoney, err := payCents(applied, family.approved.currency)
	if err != nil {
		return CreditReceipt{}, err
	}
	unappliedMoney, err := payCents(rest, family.approved.currency)
	if err != nil {
		return CreditReceipt{}, err
	}
	r.seq++
	credit := &payCredit{
		CreditID: fmt.Sprintf("cm-%d", r.seq), Applied: appliedMoney, Unapplied: unappliedMoney,
		SourceRef: strings.TrimSpace(input.SourceRef), Reason: strings.TrimSpace(input.Reason), At: payStamp(r),
	}
	grown, err := payAdd(family.credits.amount, applied)
	if err != nil {
		return CreditReceipt{}, err
	}
	kept, err := payCents(grown, family.approved.currency)
	if err != nil {
		return CreditReceipt{}, err
	}
	family.credits = kept
	unappliedGrown, err := payAdd(family.unapplied.amount, rest)
	if err != nil {
		return CreditReceipt{}, err
	}
	keptUnapplied, err := payCents(unappliedGrown, family.approved.currency)
	if err != nil {
		return CreditReceipt{}, err
	}
	family.unapplied = keptUnapplied
	family.creditList = append(family.creditList, credit)
	r.credits[credit.CreditID] = credit
	family.events = append(family.events, PayEvent{Kind: "CREDIT_RECORDED", Actor: op.ActorID, At: credit.At,
		Detail: fmt.Sprintf("applied %s unapplied %s", appliedMoney.String(), unappliedMoney.String())})
	r.refreshPayable(family)
	return CreditReceipt{CreditID: credit.CreditID, PayableID: family.payable.PayableID,
		AppliedAmount: appliedMoney.String(), UnappliedAmount: unappliedMoney.String(), Key: strings.TrimSpace(key)}, nil
}

func payTotalOf(credit *payCredit) payMoneyValue {
	sum, err := payAdd(credit.Applied.amount, credit.Unapplied.amount)
	if err != nil {
		return payZeroMoney(credit.Applied.currency)
	}
	quantized, err := payCents(sum, credit.Applied.currency)
	if err != nil {
		return payZeroMoney(credit.Applied.currency)
	}
	return quantized
}

// RegisterObservation records one settlement observation for later payment
// allocation. Unknown verdicts are refused; re-registering the same external
// resource converges on one recording.
func (r *PayableRegistry) RegisterObservation(op PayOp, obs operation.Observation) error {
	if err := op.validate(); err != nil {
		return err
	}
	if !op.PayableAllowed {
		return ErrPayableDenied
	}
	if obs.TenantID != op.TenantID {
		return ErrPayableDenied
	}
	key := strings.TrimSpace(obs.ExternalResourceKey)
	if key == "" {
		return fmt.Errorf("%w: observation needs its external resource key", ErrPayableValidation)
	}
	switch obs.Verdict {
	case operation.ObservationApplied, operation.ObservationNotApplied, operation.ObservationConflict, operation.ObservationUnknown:
	default:
		return fmt.Errorf("%w: unknown observation verdict", ErrPayableValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	mapKey := payKey(op.TenantID, op.LegalEntityID, key)
	if prior, ok := r.observations[mapKey]; ok {
		if prior.verdict != obs.Verdict || prior.authority != strings.TrimSpace(obs.Authority) {
			return fmt.Errorf("%w: observation %s changed", ErrPayableConflict, key)
		}
		return nil
	}
	r.observations[mapKey] = &payStoredObservation{verdict: obs.Verdict, authority: strings.TrimSpace(obs.Authority)}
	return nil
}

// AllocatePayment allocates one payment against the exact remaining due.
// Observed payments require an applied observation (pending evidence never
// pays); manual payments require explicit labeled evidence and record no
// transfer. Overpayments are refused with the exact remainder.
func (r *PayableRegistry) AllocatePayment(op PayOp, input PaymentInput, key string) (PaymentReceipt, error) {
	if err := op.validate(); err != nil {
		return PaymentReceipt{}, err
	}
	if !op.PayableAllowed {
		return PaymentReceipt{}, ErrPayableDenied
	}
	if strings.TrimSpace(key) == "" {
		return PaymentReceipt{}, fmt.Errorf("%w: payment needs an idempotency key", ErrPayableValidation)
	}
	hasObs := strings.TrimSpace(input.ObservationID) != ""
	hasManual := strings.TrimSpace(input.ManualEvidence) != ""
	if hasObs == hasManual {
		return PaymentReceipt{}, fmt.Errorf("%w: payment takes exactly one of observation or manual evidence", ErrPayableValidation)
	}
	if hasManual && strings.TrimSpace(input.Label) == "" {
		return PaymentReceipt{}, fmt.Errorf("%w: manual payment needs its label", ErrPayableValidation)
	}
	amount, err := payParseMoney(input.Amount)
	if err != nil {
		return PaymentReceipt{}, fmt.Errorf("%w: %v", ErrPayableValidation, err)
	}
	if amount.amount.Sign() <= 0 {
		return PaymentReceipt{}, fmt.Errorf("%w: payment amount must be positive", ErrPayableValidation)
	}
	fingerprint := strings.Join([]string{input.PayableID, input.Amount, input.ObservationID, input.ManualEvidence, input.Label}, "\x00")
	r.mu.Lock()
	defer r.mu.Unlock()
	idemKey := payKey(op.TenantID, "pay", strings.TrimSpace(key))
	if prior, ok := r.idem[idemKey]; ok {
		if prior.fingerprint != fingerprint {
			return PaymentReceipt{}, fmt.Errorf("%w: key is already used", ErrPayableConflict)
		}
		payment, ok := r.payments[prior.receipt]
		if !ok {
			return PaymentReceipt{}, ErrPayableDenied
		}
		return r.paymentReceipt(prior.payableID, payment), nil
	}
	family, err := r.familyFor(op, input.PayableID)
	if err != nil {
		return PaymentReceipt{}, err
	}
	if amount.currency != family.approved.currency {
		return PaymentReceipt{}, fmt.Errorf("%w: payment currency does not match", ErrPayableValidation)
	}
	var obsKey string
	if hasObs {
		stored, ok := r.observations[payKey(op.TenantID, op.LegalEntityID, strings.TrimSpace(input.ObservationID))]
		if !ok {
			return PaymentReceipt{}, fmt.Errorf("%w: observation is not registered", ErrPayableState)
		}
		if stored.verdict != operation.ObservationApplied {
			return PaymentReceipt{}, fmt.Errorf("%w: observation is not applied", ErrPayableState)
		}
		if stored.usedBy != "" {
			return PaymentReceipt{}, fmt.Errorf("%w: observation is already allocated", ErrPayableConflict)
		}
		obsKey = payKey(op.TenantID, op.LegalEntityID, strings.TrimSpace(input.ObservationID))
	}
	due, err := r.remainingDue(family)
	if err != nil {
		return PaymentReceipt{}, err
	}
	if amount.amount.Cmp(due.amount) > 0 {
		return PaymentReceipt{}, fmt.Errorf("%w: %s remains due", ErrPayableShortfall, due.String())
	}
	r.seq++
	payment := &payPayment{
		AllocationID: fmt.Sprintf("pmt-%d", r.seq), Amount: amount,
		Observation: strings.TrimSpace(input.ObservationID), Manual: strings.TrimSpace(input.ManualEvidence),
		Label: strings.TrimSpace(input.Label), Status: "paid", Key: strings.TrimSpace(key), At: payStamp(r),
	}
	grown, err := payAdd(family.paid.amount, amount.amount)
	if err != nil {
		return PaymentReceipt{}, err
	}
	kept, err := payCents(grown, family.approved.currency)
	if err != nil {
		return PaymentReceipt{}, err
	}
	family.paid = kept
	family.payments = append(family.payments, payment)
	r.payments[payment.AllocationID] = payment
	if obsKey != "" {
		r.observations[obsKey].usedBy = payment.AllocationID
	}
	family.events = append(family.events, PayEvent{Kind: "PAYMENT_ALLOCATED", Actor: op.ActorID, At: payment.At,
		Detail: fmt.Sprintf("%s via %s", amount.String(), payment.Observation+payment.Manual)})
	r.idem[idemKey] = payIdem{fingerprint: fingerprint, payableID: family.payable.PayableID, receipt: payment.AllocationID, tenant: op.TenantID, entity: op.LegalEntityID}
	r.refreshPayable(family)
	return r.paymentReceipt(family.payable.PayableID, payment), nil
}

func (r *PayableRegistry) paymentReceipt(payableID string, payment *payPayment) PaymentReceipt {
	receipt := PaymentReceipt{AllocationID: payment.AllocationID, PayableID: payableID,
		Amount: payment.Amount.String(), Status: payment.Status, Key: payment.Key}
	if family, ok := r.payables[payableID]; ok {
		if due, err := r.remainingDue(family); err == nil {
			receipt.RemainingDue = due.String()
		}
	}
	return receipt
}

// ReverseAllocation reverses one paid allocation with reversal evidence,
// reopening its exact amount. History is kept: the allocation reads
// reversed, never deleted.
func (r *PayableRegistry) ReverseAllocation(op PayOp, allocationID, evidence, key string) (PaymentReceipt, error) {
	if err := op.validate(); err != nil {
		return PaymentReceipt{}, err
	}
	if !op.PayableAllowed {
		return PaymentReceipt{}, ErrPayableDenied
	}
	if strings.TrimSpace(evidence) == "" {
		return PaymentReceipt{}, fmt.Errorf("%w: reversal needs evidence", ErrPayableValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	payment, ok := r.payments[strings.TrimSpace(allocationID)]
	if !ok {
		return PaymentReceipt{}, ErrPayableDenied
	}
	var family *payFamily
	for _, candidate := range r.payables {
		for _, owned := range candidate.payments {
			if owned.AllocationID == payment.AllocationID {
				family = candidate
			}
		}
	}
	if family == nil || family.tenant != op.TenantID || family.entity != op.LegalEntityID {
		return PaymentReceipt{}, ErrPayableDenied
	}
	if payment.Status != "paid" {
		return PaymentReceipt{}, fmt.Errorf("%w: only paid allocations reverse", ErrPayableState)
	}
	payment.Status = "reversed"
	shrunk, err := paySub(family.paid.amount, payment.Amount.amount)
	if err != nil {
		return PaymentReceipt{}, err
	}
	kept, err := payCents(shrunk, family.approved.currency)
	if err != nil {
		return PaymentReceipt{}, err
	}
	family.paid = kept
	if payment.Observation != "" {
		if stored, ok := r.observations[payKey(op.TenantID, op.LegalEntityID, payment.Observation)]; ok {
			stored.usedBy = ""
		}
	}
	family.events = append(family.events, PayEvent{Kind: "PAYMENT_REVERSED", Actor: op.ActorID, At: payStamp(r),
		Detail: fmt.Sprintf("%s %s", payment.AllocationID, strings.TrimSpace(evidence))})
	_ = key
	r.refreshPayable(family)
	return r.paymentReceipt(family.payable.PayableID, payment), nil
}

// AdmitAdjustment opens a versioned correction review with new facts. Facts
// behind the family head are stale and refused; nothing changes until an
// authorized approver commits the review.
func (r *PayableRegistry) AdmitAdjustment(op PayOp, payableID string, input AdjustmentInput, key string) (AdjustmentReview, error) {
	if err := op.validate(); err != nil {
		return AdjustmentReview{}, err
	}
	if !op.PayableAllowed {
		return AdjustmentReview{}, ErrPayableDenied
	}
	if strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.EvidenceRef) == "" || strings.TrimSpace(input.DecidedBy) == "" {
		return AdjustmentReview{}, fmt.Errorf("%w: adjustment needs reason, evidence and decider", ErrPayableValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	family, err := r.familyFor(op, payableID)
	if err != nil {
		return AdjustmentReview{}, err
	}
	if input.Revision <= family.revision {
		return AdjustmentReview{}, fmt.Errorf("%w: revision %d is not above head %d", ErrPayableStale, input.Revision, family.revision)
	}
	review := &payReview{
		ReviewID: fmt.Sprintf("rev-%d-%d", family.revision, input.Revision), Revision: input.Revision,
		Status: "pending", Reason: strings.TrimSpace(input.Reason), Evidence: strings.TrimSpace(input.EvidenceRef),
		DecidedBy: strings.TrimSpace(input.DecidedBy), At: payStamp(r),
	}
	if family.basis == "PO_MATCHED" {
		if strings.TrimSpace(input.Amount) != "" {
			return AdjustmentReview{}, fmt.Errorf("%w: matched adjustments restate match facts, not amounts", ErrPayableValidation)
		}
		received, err := payParseDecimal(input.ReceivedQty)
		if err != nil || received.Sign() < 0 {
			return AdjustmentReview{}, fmt.Errorf("%w: received quantity cannot be negative", ErrPayableValidation)
		}
		billed, err := payParseDecimal(input.BilledQty)
		if err != nil || billed.Sign() <= 0 {
			return AdjustmentReview{}, fmt.Errorf("%w: billed quantity must be positive", ErrPayableValidation)
		}
		if received.Cmp(billed) > 0 {
			return AdjustmentReview{}, fmt.Errorf("%w: received exceeds billed", ErrPayableValidation)
		}
		held, err := payParseMoney(input.BilledTotal)
		if err != nil || held.currency != family.total.currency || held.amount.Sign() <= 0 {
			return AdjustmentReview{}, fmt.Errorf("%w: billed total must be positive %s money", ErrPayableValidation, family.total.currency)
		}
		disputed := payZero()
		if strings.TrimSpace(input.DisputedQty) != "" {
			parsed, err := payParseDecimal(input.DisputedQty)
			if err != nil || parsed.Sign() < 0 {
				return AdjustmentReview{}, fmt.Errorf("%w: disputed quantity cannot be negative", ErrPayableValidation)
			}
			disputed = parsed
		}
		approved, err := received.Mul(family.unitPrice.amount, received.Scale()+family.unitPrice.amount.Scale(), values.RoundingExactRequired)
		if err != nil {
			return AdjustmentReview{}, fmt.Errorf("%w: %v", ErrPayableValidation, err)
		}
		approvedMoney, err := payCents(approved, family.total.currency)
		if err != nil {
			return AdjustmentReview{}, fmt.Errorf("%w: %v", ErrPayableValidation, err)
		}
		review.Received, review.Billed, review.BilledHeld, review.Disputed = received, billed, held, disputed
		review.Amount = approvedMoney
	} else {
		if strings.TrimSpace(input.ReceivedQty) != "" || strings.TrimSpace(input.BilledQty) != "" || strings.TrimSpace(input.BilledTotal) != "" || strings.TrimSpace(input.DisputedQty) != "" {
			return AdjustmentReview{}, fmt.Errorf("%w: non-match adjustments restate the amount", ErrPayableValidation)
		}
		amount, err := payParseMoney(input.Amount)
		if err != nil || amount.currency != family.total.currency || amount.amount.Sign() <= 0 {
			return AdjustmentReview{}, fmt.Errorf("%w: revised amount must be positive %s money", ErrPayableValidation, family.total.currency)
		}
		review.Amount = amount
		review.BilledHeld = amount
	}
	proposed, unmatched, err := r.proposeTotals(family, review)
	if err != nil {
		return AdjustmentReview{}, err
	}
	family.reviews = append(family.reviews, review)
	family.events = append(family.events, PayEvent{Kind: "ADJUSTMENT_PROPOSED", Actor: op.ActorID, At: review.At, Detail: review.ReviewID})
	_ = key
	return AdjustmentReview{ReviewID: review.ReviewID, PayableID: family.payable.PayableID, Revision: review.Revision,
		Status: review.Status, ProposedApproved: proposed.String(), ProposedUnmatched: unmatched.String(),
		Reason: review.Reason, EvidenceRef: review.Evidence, RequestedBy: op.ActorID, At: review.At}, nil
}

func (r *PayableRegistry) proposeTotals(family *payFamily, review *payReview) (payMoneyValue, payMoneyValue, error) {
	if family.basis == "PO_MATCHED" {
		disputedMoney, err := payCents(mustPayMul(review.Disputed, family.unitPrice.amount), family.total.currency)
		if err != nil {
			return payMoneyValue{}, payMoneyValue{}, err
		}
		open, err := paySub(review.BilledHeld.amount, review.Amount.amount)
		if err != nil {
			return payMoneyValue{}, payMoneyValue{}, err
		}
		open, err = paySub(open, disputedMoney.amount)
		if err != nil {
			return payMoneyValue{}, payMoneyValue{}, err
		}
		unmatched, err := payCents(open, family.total.currency)
		if err != nil {
			return payMoneyValue{}, payMoneyValue{}, err
		}
		return review.Amount, unmatched, nil
	}
	return review.Amount, payZeroMoney(family.total.currency), nil
}

// ApproveAdjustmentReview commits one pending review after rechecking
// authority at commit time. Approvals cannot strand settled value: revised
// approval below paid plus applied credits is refused with the exact gap.
func (r *PayableRegistry) ApproveAdjustmentReview(op PayOp, reviewID, key string) (Payable, error) {
	if err := op.validate(); err != nil {
		return Payable{}, err
	}
	if !op.ApproveAllowed {
		return Payable{}, ErrPayableDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var family *payFamily
	var review *payReview
	for _, candidate := range r.payables {
		for _, owned := range candidate.reviews {
			if owned.ReviewID == strings.TrimSpace(reviewID) {
				family, review = candidate, owned
			}
		}
	}
	if family == nil || family.tenant != op.TenantID || family.entity != op.LegalEntityID {
		return Payable{}, ErrPayableDenied
	}
	if review.Status != "pending" {
		return Payable{}, fmt.Errorf("%w: review is %s", ErrPayableState, review.Status)
	}
	if review.Revision <= family.revision {
		review.Status = "rejected"
		return Payable{}, fmt.Errorf("%w: revision %d is not above head %d", ErrPayableStale, review.Revision, family.revision)
	}
	settled, err := payAdd(family.paid.amount, family.credits.amount)
	if err != nil {
		return Payable{}, err
	}
	var approved payMoneyValue
	if family.basis == "PO_MATCHED" {
		approved = review.Amount
	} else {
		approved = review.Amount
		family.total = review.BilledHeld
	}
	if approved.amount.Cmp(settled) < 0 {
		gap, _ := paySub(settled, approved.amount)
		over, _ := payCents(gap, family.total.currency)
		return Payable{}, fmt.Errorf("%w: revised approval strands %s of settled value", ErrPayableState, over.String())
	}
	if family.basis == "PO_MATCHED" {
		proposed, unmatched, err := r.proposeTotals(family, review)
		if err != nil {
			return Payable{}, err
		}
		family.approved, family.unmatched = proposed, unmatched
		family.total = review.BilledHeld
		disputedMoney, err := payCents(mustPayMul(review.Disputed, family.unitPrice.amount), family.total.currency)
		if err != nil {
			return Payable{}, err
		}
		family.disputed = disputedMoney
	} else {
		family.approved = approved
	}
	family.revision = review.Revision
	review.Status = "approved"
	family.events = append(family.events, PayEvent{Kind: "ADJUSTMENT_APPROVED", Actor: op.ActorID, At: payStamp(r), Detail: review.ReviewID})
	_ = key
	r.refreshPayable(family)
	return *family.payable, nil
}

// ResolveQuarantine resolves one quarantined or discrepant payable. An empty
// link confirms it as its own new family; a linked payable id merges it as
// verified alias evidence with no new obligation. Either path needs approval.
func (r *PayableRegistry) ResolveQuarantine(op PayOp, quarantineID, linkPayableID, key string) (Payable, error) {
	if err := op.validate(); err != nil {
		return Payable{}, err
	}
	if !op.ApproveAllowed {
		return Payable{}, ErrPayableDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	blocked, ok := r.blocked[strings.TrimSpace(quarantineID)]
	if !ok || blocked.TenantID != op.TenantID || blocked.LegalEntityID != op.LegalEntityID {
		return Payable{}, ErrPayableDenied
	}
	if blocked.Status != PayableQuarantined && blocked.Status != PayableDiscrepancy {
		return Payable{}, fmt.Errorf("%w: record is %s", ErrPayableState, blocked.Status)
	}
	if strings.TrimSpace(linkPayableID) == "" {
		total, err := payParseMoney(blocked.TotalBilled)
		if err != nil {
			return Payable{}, err
		}
		supplier := blocked.SupplierID
		r.suppliers[payKey(op.TenantID, op.LegalEntityID, supplier)] = supplier
		famKey := payKey(op.TenantID, op.LegalEntityID, supplier, blocked.InvoiceNumber)
		if _, taken := r.families[famKey]; taken {
			return Payable{}, fmt.Errorf("%w: family identity is taken", ErrPayableConflict)
		}
		r.seq++
		payable := &Payable{
			PayableID: fmt.Sprintf("pay-%d", r.seq), FamilyID: famKey,
			TenantID: op.TenantID, LegalEntityID: op.LegalEntityID,
			SupplierID: supplier, SupplierName: blocked.SupplierName, InvoiceNumber: blocked.InvoiceNumber,
			Basis: "NON_PO_APPROVED", Purpose: "nonpo:quarantine-resolution", MatchingStatus: "Matching not applicable",
			Currency: total.currency, DueDate: blocked.DueDate, Terms: blocked.Terms,
			Status: PayableOpen, Revision: 1, Number: r.nextNumber(op.TenantID), CreatedAt: payStamp(r),
		}
		family := &payFamily{
			id: famKey, tenant: op.TenantID, entity: op.LegalEntityID, supplier: supplier,
			supplierName: blocked.SupplierName, extNumber: blocked.InvoiceNumber,
			payable: payable, owner: "resolution", basis: "NON_PO_APPROVED", purpose: payable.Purpose,
			total: total, approved: total, disputed: payZeroMoney(total.currency), unmatched: payZeroMoney(total.currency),
			paid: payZeroMoney(total.currency), credits: payZeroMoney(total.currency), unapplied: payZeroMoney(total.currency),
			revision: 1,
			entries:  []payEntry{{Owner: "resolution", RefID: blocked.PayableID, Revision: 1, At: payable.CreatedAt}},
		}
		family.events = append(family.events, PayEvent{Kind: "QUARANTINE_CONFIRMED", Actor: op.ActorID, At: payable.CreatedAt, Detail: blocked.PayableID})
		r.families[famKey] = family
		r.payables[payable.PayableID] = family
		delete(r.blocked, blocked.PayableID)
		_ = key
		r.refreshPayable(family)
		return *payable, nil
	}
	target, ok := r.payables[strings.TrimSpace(linkPayableID)]
	if !ok || target.tenant != op.TenantID || target.entity != op.LegalEntityID {
		return Payable{}, ErrPayableDenied
	}
	blocked.Status = PayableResolvedLink
	blocked.LinkedPayable = target.payable.PayableID
	target.events = append(target.events, PayEvent{Kind: "ALIAS_VERIFIED", Actor: op.ActorID, At: payStamp(r), Detail: blocked.PayableID})
	_ = key
	out := *blocked
	return out, nil
}

// Explain projects one payable with explicit conservation components.
func (r *PayableRegistry) Explain(op PayOp, payableID string) (PayableProjection, error) {
	if err := op.validate(); err != nil {
		return PayableProjection{}, err
	}
	if !op.PayableAllowed {
		return PayableProjection{}, ErrPayableDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	family, ok := r.payables[strings.TrimSpace(payableID)]
	if !ok || family.tenant != op.TenantID || family.entity != op.LegalEntityID {
		return PayableProjection{}, ErrPayableDenied
	}
	blocked, err := payAdd(family.disputed.amount, family.unmatched.amount)
	if err != nil {
		return PayableProjection{}, err
	}
	blockedMoney, err := payCents(blocked, family.total.currency)
	if err != nil {
		return PayableProjection{}, err
	}
	links := make([]string, 0, len(family.entries))
	for _, entry := range family.entries {
		links = append(links, entry.Owner+":"+entry.RefID)
	}
	status := "open"
	if family.paid.amount.Sign() > 0 {
		due, err := r.remainingDue(family)
		if err != nil {
			return PayableProjection{}, err
		}
		if due.amount.Sign() == 0 {
			status = "paid"
		} else {
			status = "due"
		}
	} else if family.payable.Remaining != "" && family.payable.Remaining != payZeroMoney(family.total.currency).String() {
		status = "due"
	}
	due, err := r.remainingDue(family)
	if err != nil {
		return PayableProjection{}, err
	}
	return PayableProjection{
		PayableID: family.payable.PayableID, TotalBilled: family.total.String(),
		ApprovedToPay: family.approved.String(), DisputedUnmatched: blockedMoney.String(),
		Credits: family.credits.String(), Paid: family.paid.String(), Remaining: due.String(),
		Status: family.payable.Status, SourceLinks: links, AccountingStatus: status,
	}, nil
}

// GetPayable returns one live payable by id.
func (r *PayableRegistry) GetPayable(op PayOp, payableID string) (Payable, error) {
	if err := op.validate(); err != nil {
		return Payable{}, err
	}
	if !op.PayableAllowed {
		return Payable{}, ErrPayableDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if family, ok := r.payables[strings.TrimSpace(payableID)]; ok {
		if family.tenant != op.TenantID || family.entity != op.LegalEntityID {
			return Payable{}, ErrPayableDenied
		}
		return *family.payable, nil
	}
	if blocked, ok := r.blocked[strings.TrimSpace(payableID)]; ok {
		if blocked.TenantID != op.TenantID || blocked.LegalEntityID != op.LegalEntityID {
			return Payable{}, ErrPayableDenied
		}
		return *blocked, nil
	}
	return Payable{}, ErrPayableDenied
}

// ListPayables lists live payables for one supplier, optionally filtered by
// status. Foreign scopes read as empty.
func (r *PayableRegistry) ListPayables(op PayOp, supplierID, status string) ([]Payable, error) {
	if err := op.validate(); err != nil {
		return nil, err
	}
	if !op.PayableAllowed {
		return nil, ErrPayableDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Payable{}
	for _, family := range r.payables {
		if family.tenant != op.TenantID || family.entity != op.LegalEntityID {
			continue
		}
		if strings.TrimSpace(supplierID) != "" && family.supplier != strings.TrimSpace(supplierID) {
			continue
		}
		if strings.TrimSpace(status) != "" && family.payable.Status != strings.TrimSpace(status) {
			continue
		}
		out = append(out, *family.payable)
	}
	for _, blocked := range r.blocked {
		if blocked.TenantID != op.TenantID || blocked.LegalEntityID != op.LegalEntityID {
			continue
		}
		if strings.TrimSpace(supplierID) != "" && blocked.SupplierID != strings.TrimSpace(supplierID) {
			continue
		}
		if strings.TrimSpace(status) != "" && blocked.Status != strings.TrimSpace(status) {
			continue
		}
		out = append(out, *blocked)
	}
	return out, nil
}

// PayableStatus returns the live record behind one idempotency key.
func (r *PayableRegistry) PayableStatus(op PayOp, key string) (Payable, error) {
	if err := op.validate(); err != nil {
		return Payable{}, err
	}
	if !op.PayableAllowed {
		return Payable{}, ErrPayableDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, namespace := range []string{payKey(op.TenantID, strings.TrimSpace(key)), payKey(op.TenantID, "pay", strings.TrimSpace(key))} {
		if prior, ok := r.idem[namespace]; ok {
			if prior.tenant != op.TenantID || prior.entity != op.LegalEntityID {
				return Payable{}, ErrPayableDenied
			}
			if family, ok := r.payables[prior.payableID]; ok {
				return *family.payable, nil
			}
			if blocked, ok := r.blocked[prior.payableID]; ok {
				return *blocked, nil
			}
		}
	}
	return Payable{}, ErrPayableDenied
}

type paySnapshotEntry struct {
	Owner    string
	RefID    string
	Revision uint64
	At       string
}

type paySnapshotCredit struct {
	CreditID  string
	Applied   string
	Unapplied string
	SourceRef string
	Reason    string
	At        string
}

type paySnapshotPayment struct {
	AllocationID string
	Amount       string
	Observation  string
	Manual       string
	Label        string
	Status       string
	Key          string
	At           string
}

type paySnapshotReview struct {
	ReviewID  string
	Revision  uint64
	Status    string
	Received  string
	Billed    string
	Held      string
	Disputed  string
	Amount    string
	Reason    string
	Evidence  string
	DecidedBy string
	At        string
}

type paySnapshotFamily struct {
	ID           string
	Tenant       string
	Entity       string
	Supplier     string
	SupplierName string
	ExtNumber    string
	Payable      *Payable
	Owner        string
	Basis        string
	Purpose      string
	Total        string
	Approved     string
	Disputed     string
	Unmatched    string
	UnitPrice    string
	Paid         string
	Credits      string
	Unapplied    string
	Revision     uint64
	Entries      []payEntry
	CreditList   []*paySnapshotCredit
	Payments     []*paySnapshotPayment
	Reviews      []*paySnapshotReview
	Events       []PayEvent
}

type paySnapshotObservation struct {
	Verdict   operation.ObservationVerdict
	Authority string
	UsedBy    string
}

type paySnapshot struct {
	Families     map[string]*paySnapshotFamily
	Blocked      map[string]*Payable
	Suppliers    map[string]string
	SupplierName map[string]string
	Observations map[string]*paySnapshotObservation
	Idem         map[string]payIdem
	Seq          uint64
	NumSeq       map[string]uint64
}

// Export snapshots the registry for durable persistence and restart replay.
func (r *PayableRegistry) Export() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap := paySnapshot{
		Families: map[string]*paySnapshotFamily{}, Blocked: map[string]*Payable{},
		Suppliers: map[string]string{}, SupplierName: map[string]string{},
		Observations: map[string]*paySnapshotObservation{}, Idem: map[string]payIdem{},
		Seq: r.seq, NumSeq: map[string]uint64{},
	}
	for id, family := range r.families {
		saved := &paySnapshotFamily{
			ID: id, Tenant: family.tenant, Entity: family.entity, Supplier: family.supplier,
			SupplierName: family.supplierName, ExtNumber: family.extNumber, Payable: family.payable,
			Owner: family.owner, Basis: family.basis, Purpose: family.purpose,
			Total: family.total.String(), Approved: family.approved.String(), Disputed: family.disputed.String(),
			Unmatched: family.unmatched.String(), UnitPrice: family.unitPrice.String(),
			Paid: family.paid.String(), Credits: family.credits.String(), Unapplied: family.unapplied.String(),
			Revision: family.revision, Entries: family.entries, Events: family.events,
		}
		for _, credit := range family.creditList {
			saved.CreditList = append(saved.CreditList, &paySnapshotCredit{CreditID: credit.CreditID,
				Applied: credit.Applied.String(), Unapplied: credit.Unapplied.String(),
				SourceRef: credit.SourceRef, Reason: credit.Reason, At: credit.At})
		}
		for _, payment := range family.payments {
			saved.Payments = append(saved.Payments, &paySnapshotPayment{AllocationID: payment.AllocationID,
				Amount: payment.Amount.String(), Observation: payment.Observation, Manual: payment.Manual,
				Label: payment.Label, Status: payment.Status, Key: payment.Key, At: payment.At})
		}
		for _, review := range family.reviews {
			saved.Reviews = append(saved.Reviews, &paySnapshotReview{ReviewID: review.ReviewID,
				Revision: review.Revision, Status: review.Status, Received: payStrip(review.Received.String()),
				Billed: payStrip(review.Billed.String()), Held: review.BilledHeld.String(), Disputed: payStrip(review.Disputed.String()),
				Amount: review.Amount.String(), Reason: review.Reason, Evidence: review.Evidence,
				DecidedBy: review.DecidedBy, At: review.At})
		}
		snap.Families[id] = saved
	}
	for id, blocked := range r.blocked {
		snap.Blocked[id] = blocked
	}
	for key, id := range r.suppliers {
		snap.Suppliers[key] = id
	}
	for key, id := range r.supplierName {
		snap.SupplierName[key] = id
	}
	for key, obs := range r.observations {
		snap.Observations[key] = &paySnapshotObservation{Verdict: obs.verdict, Authority: obs.authority, UsedBy: obs.usedBy}
	}
	for key, idem := range r.idem {
		snap.Idem[key] = idem
	}
	for tenant, seq := range r.numSeq {
		snap.NumSeq[tenant] = seq
	}
	return json.Marshal(snap)
}

// Import restores a registry previously saved with Export.
func (r *PayableRegistry) Import(data []byte) error {
	var snap paySnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("%w: snapshot: %v", ErrPayableValidation, err)
	}
	families := map[string]*payFamily{}
	payables := map[string]*payFamily{}
	credits := map[string]*payCredit{}
	payments := map[string]*payPayment{}
	for id, saved := range snap.Families {
		total, err := payParseMoney(saved.Total)
		if err != nil {
			return fmt.Errorf("%w: snapshot family %s: %v", ErrPayableValidation, id, err)
		}
		approved, err := payParseMoney(saved.Approved)
		if err != nil {
			return fmt.Errorf("%w: snapshot family %s: %v", ErrPayableValidation, id, err)
		}
		disputed, err := payParseMoney(saved.Disputed)
		if err != nil {
			return fmt.Errorf("%w: snapshot family %s: %v", ErrPayableValidation, id, err)
		}
		unmatched, err := payParseMoney(saved.Unmatched)
		if err != nil {
			return fmt.Errorf("%w: snapshot family %s: %v", ErrPayableValidation, id, err)
		}
		paid, err := payParseMoney(saved.Paid)
		if err != nil {
			return fmt.Errorf("%w: snapshot family %s: %v", ErrPayableValidation, id, err)
		}
		credited, err := payParseMoney(saved.Credits)
		if err != nil {
			return fmt.Errorf("%w: snapshot family %s: %v", ErrPayableValidation, id, err)
		}
		unapplied, err := payParseMoney(saved.Unapplied)
		if err != nil {
			return fmt.Errorf("%w: snapshot family %s: %v", ErrPayableValidation, id, err)
		}
		var unitPrice payMoneyValue
		if saved.UnitPrice != "" {
			unitPrice, err = payParseMoney(saved.UnitPrice)
			if err != nil {
				return fmt.Errorf("%w: snapshot family %s: %v", ErrPayableValidation, id, err)
			}
		}
		family := &payFamily{
			id: id, tenant: saved.Tenant, entity: saved.Entity, supplier: saved.Supplier,
			supplierName: saved.SupplierName, extNumber: saved.ExtNumber, payable: saved.Payable,
			owner: saved.Owner, basis: saved.Basis, purpose: saved.Purpose,
			total: total, approved: approved, disputed: disputed, unmatched: unmatched,
			unitPrice: unitPrice, paid: paid, credits: credited, unapplied: unapplied,
			revision: saved.Revision, entries: saved.Entries, events: saved.Events,
		}
		for _, savedCredit := range saved.CreditList {
			applied, err := payParseMoney(savedCredit.Applied)
			if err != nil {
				return fmt.Errorf("%w: snapshot credit: %v", ErrPayableValidation, err)
			}
			left, err := payParseMoney(savedCredit.Unapplied)
			if err != nil {
				return fmt.Errorf("%w: snapshot credit: %v", ErrPayableValidation, err)
			}
			credit := &payCredit{CreditID: savedCredit.CreditID, Applied: applied, Unapplied: left,
				SourceRef: savedCredit.SourceRef, Reason: savedCredit.Reason, At: savedCredit.At}
			family.creditList = append(family.creditList, credit)
			credits[credit.CreditID] = credit
		}
		for _, savedPayment := range saved.Payments {
			amount, err := payParseMoney(savedPayment.Amount)
			if err != nil {
				return fmt.Errorf("%w: snapshot payment: %v", ErrPayableValidation, err)
			}
			payment := &payPayment{AllocationID: savedPayment.AllocationID, Amount: amount,
				Observation: savedPayment.Observation, Manual: savedPayment.Manual, Label: savedPayment.Label,
				Status: savedPayment.Status, Key: savedPayment.Key, At: savedPayment.At}
			family.payments = append(family.payments, payment)
			payments[payment.AllocationID] = payment
		}
		for _, savedReview := range saved.Reviews {
			received, err := payParseDecimal(savedReview.Received)
			if err != nil {
				received = payZero()
			}
			billed, err := payParseDecimal(savedReview.Billed)
			if err != nil {
				billed = payZero()
			}
			disputedQty, err := payParseDecimal(savedReview.Disputed)
			if err != nil {
				disputedQty = payZero()
			}
			held, err := payParseMoney(savedReview.Held)
			if err != nil {
				return fmt.Errorf("%w: snapshot review: %v", ErrPayableValidation, err)
			}
			amount, err := payParseMoney(savedReview.Amount)
			if err != nil {
				return fmt.Errorf("%w: snapshot review: %v", ErrPayableValidation, err)
			}
			family.reviews = append(family.reviews, &payReview{ReviewID: savedReview.ReviewID,
				Revision: savedReview.Revision, Status: savedReview.Status, Received: received,
				Billed: billed, BilledHeld: held, Disputed: disputedQty, Amount: amount,
				Reason: savedReview.Reason, Evidence: savedReview.Evidence,
				DecidedBy: savedReview.DecidedBy, At: savedReview.At})
		}
		families[id] = family
		payables[family.payable.PayableID] = family
	}
	observations := map[string]*payStoredObservation{}
	for key, saved := range snap.Observations {
		observations[key] = &payStoredObservation{verdict: saved.Verdict, authority: saved.Authority, usedBy: saved.UsedBy}
	}
	if snap.Blocked == nil {
		snap.Blocked = map[string]*Payable{}
	}
	if snap.Suppliers == nil {
		snap.Suppliers = map[string]string{}
	}
	if snap.SupplierName == nil {
		snap.SupplierName = map[string]string{}
	}
	if snap.Idem == nil {
		snap.Idem = map[string]payIdem{}
	}
	if snap.NumSeq == nil {
		snap.NumSeq = map[string]uint64{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.families = families
	r.payables = payables
	r.blocked = snap.Blocked
	r.suppliers = snap.Suppliers
	r.supplierName = snap.SupplierName
	r.observations = observations
	r.credits = credits
	r.payments = payments
	r.idem = snap.Idem
	r.seq = snap.Seq
	r.numSeq = snap.NumSeq
	return nil
}

// Package workorderbilling issues credit and debit adjustments that conserve
// invoice history.
//
// SMB-BILL-008 adds the AdjustmentRegistry: price mistakes, returns and
// cancellations become typed adjustments with their own numbers, immutable
// documents and ledger postings, while the original invoice bytes never
// move. Credits consume a per-line remaining basis, cannot exceed it across
// concurrent adjustments, and allocate against paid receipts into explicit
// settled and customer-credit splits. Debits create additional obligation
// under explicit accepted terms and auto-offset available customer credit
// first. Voids are policy-gated, unpaid-only, and leave number, reason and
// time. Corrections of corrections link forward as new adjustments. Receipts
// arrive as provenance-carrying observations (the BILL-007 shape) and are
// never invented; refunds stay a separate BILL-009 effect the registry
// cannot trigger.
package workorderbilling

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	doctemplate "github.com/monstercameron/human-capital-management-suite/internal/doctemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrAdjustValidation rejects malformed requests, unknown references and
	// adjustments that violate policy shape (missing reason, evidence or
	// debit terms).
	ErrAdjustValidation = errors.New("workorderbilling: adjustment request is invalid")
	// ErrAdjustDenied is the single neutral response for denied capabilities,
	// foreign tenants and guessed identifiers. It never distinguishes which.
	ErrAdjustDenied = errors.New("workorderbilling: adjustment not found or not permitted")
	// ErrAdjustConflict reports an idempotency key reused with a changed
	// canonical payload, or return/cancellation evidence consumed twice.
	ErrAdjustConflict = errors.New("workorderbilling: adjustment idempotency conflict")
	// ErrAdjustStale reports work pinned to a superseded invoice revision.
	ErrAdjustStale = errors.New("workorderbilling: invoice revision is stale")
	// ErrAdjustShortfall reports an adjustment above the exact remaining
	// per-line credit basis.
	ErrAdjustShortfall = errors.New("workorderbilling: credit basis is insufficient")
	// ErrAdjustState reports a transition the current lifecycle state
	// forbids, including paid-invoice voids and self-approval.
	ErrAdjustState = errors.New("workorderbilling: adjustment state forbids this transition")
)

// AdjustOp carries the authorized actor for one registry call.
type AdjustOp struct {
	TenantID       string
	LegalEntityID  string
	ActorID        string
	AdjustAllowed  bool
	ApproveAllowed bool
	VoidAllowed    bool
}

func (op AdjustOp) validate() error {
	if strings.TrimSpace(op.TenantID) == "" || strings.TrimSpace(op.LegalEntityID) == "" || strings.TrimSpace(op.ActorID) == "" {
		return fmt.Errorf("%w: operation needs tenant, legal entity and actor", ErrAdjustValidation)
	}
	return nil
}

// Adjustment lifecycle states.
const (
	AdjustDraft     = "draft"
	AdjustSubmitted = "submitted"
	AdjustApproved  = "approved"
	AdjustIssued    = "issued"
	AdjustCancelled = "cancelled"
)

// AdjustReceiptInput records one provenance-carrying receipt observation.
type AdjustReceiptInput struct {
	ReceiptID  string
	InvoiceID  string
	CustomerID string
	Amount     string
	Provenance string
	At         string
}

// AdjustLineInput selects one line slice for an adjustment: exactly one of
// Qty (priced from the original unit price) or Amount (explicit net), plus
// an explicit tax component that defaults to zero.
type AdjustLineInput struct {
	LineID    string
	Qty       string
	Amount    string
	TaxAmount string
}

// AdjustDraftInput drafts one credit or debit adjustment.
type AdjustDraftInput struct {
	InvoiceID   string
	Kind        string
	Lines       []AdjustLineInput
	Reason      string
	EvidenceRef string
	TermsRef    string
	CorrectsRef string
}

// AdjustAllocation records how one issued credit split paid receipts into
// settled obligation and released customer credit.
type AdjustAllocation struct {
	AllocationID         string
	AdjustmentID         string
	InvoiceID            string
	CustomerID           string
	SettledAmount        string
	CustomerCreditAmount string
	At                   string
}

// AdjustProjection explains one invoice with explicit conservation:
// Obligation + open debits - credits - settled receipts - customer credit
// movements always reconcile through the shown components.
type AdjustProjection struct {
	InvoiceID      string
	Obligation     string
	Credits        string
	Debits         string
	Receipts       string
	Settled        string
	ReleasedCredit string
	DebitOffsets   string
	CustomerCredit string
	Remaining      string
}

// AdjustVoid preserves a policy-gated void decision.
type AdjustVoid struct {
	VoidID      string
	TenantID    string
	InvoiceID   string
	Number      string
	Reason      string
	EvidenceRef string
	By          string
	At          string
}

// AdjustPolicy configures void behavior per tenant. Paid invoices are never
// voidable; WindowDays bounds unpaid voids (0 is unlimited) and
// RequireApproval adds the approval grant to the void grant.
type AdjustPolicy struct {
	VoidWindowDays      int64
	VoidRequireApproval bool
}

// AdjustLedger is the receivable posting surface adjustments settle into.
// *ReceivableLedger satisfies it.
type AdjustLedger interface {
	Post(ev ReceivableEvent) error
	Explain(invoiceID, asOf string) (ReceivableProjection, error)
}

// AdjustInvoicePort resolves invoice snapshots and issues document voids
// through the invoice owner. The registry never edits foreign tables.
type AdjustInvoicePort interface {
	GetInvoice(op AdjustOp, invoiceID string) (AdjustInvoice, error)
	VoidInvoiceDocument(op AdjustOp, invoiceID string, expected uint64, reason, key, at string) (AdjustInvoice, error)
}

type adjustLineState struct {
	description string
	qty         values.Decimal
	unitPrice   billMoneyValue
	amount      billMoneyValue
	tax         billMoneyValue
	creditedQty values.Decimal
	creditedNet billMoneyValue
	creditedTax billMoneyValue
}

type adjustInvoiceState struct {
	tenant     string
	entity     string
	head       AdjustInvoice
	lines      map[string]*adjustLineState
	total      billMoneyValue
	taxTotal   billMoneyValue
	receipts   map[string]billMoneyValue
	paid       billMoneyValue
	credits    billMoneyValue
	released   billMoneyValue
	openDebits billMoneyValue
	status     string
	revision   uint64
	currency   string
}

type adjustIdem struct {
	fingerprint  string
	adjustmentID string
	tenant       string
	entity       string
}

type adjustBalance struct {
	amount   billMoneyValue
	offsets  billMoneyValue
	currency string
}

// AdjustmentRegistry guards invoice adjustments for every tenant. All money
// math is exact; anything that cannot stay exact is refused with a reason.
type AdjustmentRegistry struct {
	mu          sync.Mutex
	clock       func() time.Time
	invoices    map[string]*adjustInvoiceState
	adjustments map[string]*Adjustment
	evidence    map[string]string
	idem        map[string]adjustIdem
	txidem      map[string]adjustIdem
	balances    map[string]*adjustBalance
	voids       map[string]*AdjustVoid
	allocations map[string]*AdjustAllocation
	policies    map[string]AdjustPolicy
	ledgers     map[string]AdjustLedger
	port        AdjustInvoicePort
	cmSeq       map[string]uint64
	dmSeq       map[string]uint64
	adjSeq      uint64
	allocSeq    uint64
	voidSeq     uint64
	creditTmpl  doctemplate.Template
	debitTmpl   doctemplate.Template
	tmplBuilt   bool
}

// NewAdjustmentRegistry returns an empty registry using clock for audit
// timestamps.
func NewAdjustmentRegistry(clock func() time.Time) *AdjustmentRegistry {
	if clock == nil {
		clock = time.Now
	}
	return &AdjustmentRegistry{
		clock:       clock,
		invoices:    map[string]*adjustInvoiceState{},
		adjustments: map[string]*Adjustment{},
		evidence:    map[string]string{},
		idem:        map[string]adjustIdem{},
		txidem:      map[string]adjustIdem{},
		balances:    map[string]*adjustBalance{},
		voids:       map[string]*AdjustVoid{},
		allocations: map[string]*AdjustAllocation{},
		policies:    map[string]AdjustPolicy{},
		ledgers:     map[string]AdjustLedger{},
		cmSeq:       map[string]uint64{},
		dmSeq:       map[string]uint64{},
	}
}

// BindLedger attaches the receivable ledger adjustments post into.
func (r *AdjustmentRegistry) BindLedger(tenant string, ledger AdjustLedger) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ledgers[strings.TrimSpace(tenant)] = ledger
}

// BindInvoicePort attaches the invoice owner port voids call through.
func (r *AdjustmentRegistry) BindInvoicePort(port AdjustInvoicePort) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.port = port
}

func adjustKey(parts ...string) string { return strings.Join(parts, "\x00") }

func adjustNow(r *AdjustmentRegistry) time.Time {
	if r.clock == nil {
		return time.Now().UTC()
	}
	return r.clock().UTC()
}

func adjustStamp(r *AdjustmentRegistry) string { return adjustNow(r).Format(time.RFC3339) }

func adjustZero(currency string) billMoneyValue {
	zero, _ := billCents(mustBillZero(), currency)
	return zero
}

func adjustParseTime(raw string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if at, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return at, nil
	}
	if day, err := time.Parse("2006-01-02", trimmed); err == nil {
		return day, nil
	}
	return time.Time{}, fmt.Errorf("timestamp %q must be RFC3339 or YYYY-MM-DD", raw)
}

// Bill008MemoTemplate builds the published immutable document template for
// one adjustment kind. Rendering happens at issue; the artifact digest pins
// the issued bytes.
func Bill008MemoTemplate(kind string) (doctemplate.Template, error) {
	id, docType, title := "bill008-credit-memo", "CREDIT_MEMO", "CREDIT MEMO"
	if kind == "debit" {
		id, docType, title = "bill008-debit-memo", "DEBIT_MEMO", "DEBIT MEMO"
	} else if kind != "credit" {
		return doctemplate.Template{}, fmt.Errorf("%w: unknown memo kind %q", ErrAdjustValidation, kind)
	}
	def := doctemplate.Definition{
		ID: "bill008/" + id, Version: "1", Type: docType, Purpose: "Conserved customer adjustment document",
		SourceLocale: "en-US", Locale: "en-US", Jurisdiction: "US", Classification: "customer",
		AccessibilityVersion: "1.0",
		Source:               title + " {{.number}}\nInvoice: {{.invoice_number}} Customer: {{.customer}}\nNet: {{.net_amount}} {{.currency}} Tax: {{.tax_amount}} {{.currency}} Total: {{.total_amount}} {{.currency}}\nReason: {{.reason}} Terms: {{.terms_ref}}\nIssued: {{.issued_at}}\n",
		Placeholders: []doctemplate.Placeholder{
			{Name: "number", Kind: doctemplate.String, Required: true},
			{Name: "invoice_number", Kind: doctemplate.String, Required: true},
			{Name: "customer", Kind: doctemplate.String, Required: true},
			{Name: "currency", Kind: doctemplate.String, Required: true},
			{Name: "net_amount", Kind: doctemplate.String, Required: true},
			{Name: "tax_amount", Kind: doctemplate.String, Required: true},
			{Name: "total_amount", Kind: doctemplate.String, Required: true},
			{Name: "reason", Kind: doctemplate.String, Required: true},
			{Name: "terms_ref", Kind: doctemplate.String},
			{Name: "issued_at", Kind: doctemplate.String, Required: true},
		},
	}
	draft, err := doctemplate.New(def)
	if err != nil {
		return doctemplate.Template{}, err
	}
	binding := func(value string) doctemplate.Binding {
		return doctemplate.Binding{Kind: doctemplate.String, Value: value}
	}
	published, err := draft.Publish(doctemplate.Publication{
		TemplateDigest: draft.Digest(),
		Fixtures: []doctemplate.Fixture{{Name: "sample", Bindings: map[string]doctemplate.Binding{
			"number": binding("CM-0000"), "invoice_number": binding("INV-0000"), "customer": binding("cust-0"),
			"currency": binding("USD"), "net_amount": binding("0.00"), "tax_amount": binding("0.00"),
			"total_amount": binding("0.00"), "reason": binding("sample"), "terms_ref": binding(""),
			"issued_at": binding("2026-01-01T00:00:00Z"),
		}}},
		Approval:      doctemplate.Approval{ApprovedBy: "billing-owner", ApprovalID: "bill008-pub-1"},
		Applicability: doctemplate.Applicability{Locales: []string{"en-US"}, Jurisdictions: []string{"US"}, Classifications: []string{"customer"}},
	})
	if err != nil {
		return doctemplate.Template{}, err
	}
	return published, nil
}

func (r *AdjustmentRegistry) memoTemplate(kind string) (doctemplate.Template, error) {
	if !r.tmplBuilt {
		credit, err := Bill008MemoTemplate("credit")
		if err != nil {
			return doctemplate.Template{}, err
		}
		debit, err := Bill008MemoTemplate("debit")
		if err != nil {
			return doctemplate.Template{}, err
		}
		r.creditTmpl, r.debitTmpl, r.tmplBuilt = credit, debit, true
	}
	if kind == "debit" {
		return r.debitTmpl, nil
	}
	return r.creditTmpl, nil
}

// BindInvoice registers one frozen invoice snapshot. The same revision
// replays when identical and conflicts when changed; a lower revision is
// stale; a higher revision supersedes the head while receipts, credits and
// allocations carry forward on the invoice.
func (r *AdjustmentRegistry) BindInvoice(op AdjustOp, snap AdjustInvoice) error {
	if err := op.validate(); err != nil {
		return err
	}
	if !op.AdjustAllowed {
		return ErrAdjustDenied
	}
	if strings.TrimSpace(snap.InvoiceID) == "" || strings.TrimSpace(snap.CustomerID) == "" {
		return fmt.Errorf("%w: invoice needs identity and customer", ErrAdjustValidation)
	}
	if strings.TrimSpace(snap.Currency) == "" || strings.TrimSpace(snap.Number) == "" {
		return fmt.Errorf("%w: invoice needs currency and number", ErrAdjustValidation)
	}
	if strings.TrimSpace(snap.Digest) == "" {
		return fmt.Errorf("%w: invoice needs its issued digest", ErrAdjustValidation)
	}
	if snap.Revision == 0 {
		return fmt.Errorf("%w: invoice revision starts at 1", ErrAdjustValidation)
	}
	if _, err := adjustParseTime(snap.IssuedAt); err != nil {
		return fmt.Errorf("%w: %v", ErrAdjustValidation, err)
	}
	if len(snap.Lines) == 0 {
		return fmt.Errorf("%w: invoice needs lines", ErrAdjustValidation)
	}
	currency := strings.TrimSpace(snap.Currency)
	lines := map[string]*adjustLineState{}
	netSum := mustBillZero()
	taxSum := mustBillZero()
	for _, line := range snap.Lines {
		if strings.TrimSpace(line.LineID) == "" || strings.TrimSpace(line.Description) == "" {
			return fmt.Errorf("%w: invoice line needs identity and description", ErrAdjustValidation)
		}
		if _, dup := lines[line.LineID]; dup {
			return fmt.Errorf("%w: duplicate invoice line %q", ErrAdjustValidation, line.LineID)
		}
		qty, err := billParseDecimal(line.Qty)
		if err != nil || qty.Sign() <= 0 {
			return fmt.Errorf("%w: line %s quantity must be positive", ErrAdjustValidation, line.LineID)
		}
		unit, err := billParseMoney(line.UnitPrice)
		if err != nil || unit.currency != currency {
			return fmt.Errorf("%w: line %s unit price must be %s money", ErrAdjustValidation, line.LineID, currency)
		}
		amount, err := billParseMoney(line.Amount)
		if err != nil || amount.currency != currency || amount.amount.Sign() < 0 {
			return fmt.Errorf("%w: line %s amount must be %s money", ErrAdjustValidation, line.LineID, currency)
		}
		tax, err := billParseMoney(line.TaxAmount)
		if err != nil || tax.currency != currency || tax.amount.Sign() < 0 {
			return fmt.Errorf("%w: line %s tax must be %s money", ErrAdjustValidation, line.LineID, currency)
		}
		if netSum, err = billAdd(netSum, amount.amount); err != nil {
			return err
		}
		if taxSum, err = billAdd(taxSum, tax.amount); err != nil {
			return err
		}
		lines[line.LineID] = &adjustLineState{description: line.Description, qty: qty, unitPrice: unit, amount: amount, tax: tax,
			creditedQty: mustBillZero(), creditedNet: adjustZero(currency), creditedTax: adjustZero(currency)}
	}
	total, err := billParseMoney(snap.Total)
	if err != nil || total.currency != currency {
		return fmt.Errorf("%w: invoice total must be %s money", ErrAdjustValidation, currency)
	}
	taxTotal, err := billParseMoney(snap.TaxTotal)
	if err != nil || taxTotal.currency != currency {
		return fmt.Errorf("%w: invoice tax total must be %s money", ErrAdjustValidation, currency)
	}
	gross, err := billAdd(netSum, taxSum)
	if err != nil {
		return err
	}
	if gross.Cmp(total.amount) != 0 || taxSum.Cmp(taxTotal.amount) != 0 {
		return fmt.Errorf("%w: invoice totals do not foot to their lines", ErrAdjustValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := adjustKey(op.TenantID, op.LegalEntityID, strings.TrimSpace(snap.InvoiceID))
	for _, state := range r.invoices {
		if state.head.InvoiceID == strings.TrimSpace(snap.InvoiceID) && (state.tenant != op.TenantID || state.entity != op.LegalEntityID) {
			return ErrAdjustDenied
		}
	}
	if state, ok := r.invoices[key]; ok {
		if snap.Revision < state.revision {
			return fmt.Errorf("%w: revision %d is behind head %d", ErrAdjustStale, snap.Revision, state.revision)
		}
		if snap.Revision == state.revision {
			if adjustInvoiceFingerprint(snap) != adjustInvoiceFingerprint(state.head) {
				return fmt.Errorf("%w: revision %d is already bound", ErrAdjustConflict, snap.Revision)
			}
			return nil
		}
		// Supersede the head; money state carries forward on the invoice.
		for id, fresh := range lines {
			if prior, ok := state.lines[id]; ok {
				fresh.creditedQty, fresh.creditedNet, fresh.creditedTax = prior.creditedQty, prior.creditedNet, prior.creditedTax
			}
		}
		state.head = snap
		state.head.InvoiceID = strings.TrimSpace(snap.InvoiceID)
		state.lines = lines
		state.total = total
		state.taxTotal = taxTotal
		state.status = strings.TrimSpace(snap.Status)
		state.revision = snap.Revision
		return nil
	}
	r.invoices[key] = &adjustInvoiceState{
		tenant: op.TenantID, entity: op.LegalEntityID, head: snap,
		lines: lines, total: total, taxTotal: taxTotal,
		receipts: map[string]billMoneyValue{},
		paid:     adjustZero(currency), credits: adjustZero(currency), released: adjustZero(currency),
		openDebits: adjustZero(currency),
		status:     strings.TrimSpace(snap.Status), revision: snap.Revision, currency: currency,
	}
	return nil
}

func adjustInvoiceFingerprint(snap AdjustInvoice) string {
	parts := []string{snap.InvoiceID, snap.CustomerID, snap.Currency, snap.Number, snap.Status, fmt.Sprintf("%d", snap.Revision), snap.Digest, snap.IssuedAt, snap.Total, snap.TaxTotal}
	for _, line := range snap.Lines {
		parts = append(parts, line.LineID, line.Description, line.Qty, line.UnitPrice, line.Amount, line.TaxAmount)
	}
	return strings.Join(parts, "\x00")
}

// RegisterReceipt records one provenance-carrying receipt observation and
// posts its allocation to the bound ledger. Receipts are never invented:
// unknown invoices and foreign scopes share the neutral denial.
func (r *AdjustmentRegistry) RegisterReceipt(op AdjustOp, input AdjustReceiptInput) error {
	if err := op.validate(); err != nil {
		return err
	}
	if !op.AdjustAllowed {
		return ErrAdjustDenied
	}
	if strings.TrimSpace(input.ReceiptID) == "" || strings.TrimSpace(input.InvoiceID) == "" || strings.TrimSpace(input.CustomerID) == "" {
		return fmt.Errorf("%w: receipt needs identity, invoice and customer", ErrAdjustValidation)
	}
	if strings.TrimSpace(input.Provenance) == "" {
		return fmt.Errorf("%w: receipt needs provenance", ErrAdjustValidation)
	}
	if _, err := adjustParseTime(input.At); err != nil {
		return fmt.Errorf("%w: %v", ErrAdjustValidation, err)
	}
	amount, err := billParseMoney(input.Amount)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAdjustValidation, err)
	}
	if amount.amount.Sign() <= 0 {
		return fmt.Errorf("%w: receipt amount must be positive", ErrAdjustValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := adjustKey(op.TenantID, op.LegalEntityID, strings.TrimSpace(input.InvoiceID))
	state, ok := r.invoices[key]
	if !ok {
		return ErrAdjustDenied
	}
	if amount.currency != state.currency || strings.TrimSpace(input.CustomerID) != state.head.CustomerID {
		return fmt.Errorf("%w: receipt does not belong to this invoice", ErrAdjustValidation)
	}
	id := strings.TrimSpace(input.ReceiptID)
	if prior, ok := state.receipts[id]; ok {
		if prior.String() != amount.String() {
			return fmt.Errorf("%w: receipt %s is already recorded", ErrAdjustConflict, id)
		}
		return nil
	}
	ledger, ok := r.ledgers[op.TenantID]
	if !ok || ledger == nil {
		return fmt.Errorf("%w: no ledger is bound for this tenant", ErrAdjustValidation)
	}
	money, err := values.NewMoney(amount.amount.String(), amount.currency, amount.amount.Scale(), values.RoundingExactRequired)
	if err != nil {
		return fmt.Errorf("%w: receipt money: %v", ErrAdjustValidation, err)
	}
	if err := ledger.Post(ReceivableEvent{
		EventID: "receipt:" + id, TenantID: op.TenantID, InvoiceID: state.head.InvoiceID,
		Kind: ReceivableReceiptAllocated, Amount: money, At: strings.TrimSpace(input.At),
		Actor: op.ActorID, Ref: strings.TrimSpace(input.Provenance),
	}); err != nil {
		if errors.Is(err, ErrInvoiceConflict) {
			return fmt.Errorf("%w: receipt %s changed its payload", ErrAdjustConflict, id)
		}
		return err
	}
	state.receipts[id] = amount
	paid, err := billAdd(state.paid.amount, amount.amount)
	if err != nil {
		return err
	}
	quantized, err := billCents(paid, state.currency)
	if err != nil {
		return err
	}
	state.paid = quantized
	return nil
}

// SetAdjustPolicy configures void behavior for the caller's tenant.
func (r *AdjustmentRegistry) SetAdjustPolicy(op AdjustOp, policy AdjustPolicy) error {
	if err := op.validate(); err != nil {
		return err
	}
	if !op.VoidAllowed {
		return ErrAdjustDenied
	}
	if policy.VoidWindowDays < 0 {
		return fmt.Errorf("%w: void window cannot be negative", ErrAdjustValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.policies[op.TenantID] = policy
	return nil
}

type adjustPricedLine struct {
	lineID      string
	description string
	byQty       bool
	qty         values.Decimal
	net         billMoneyValue
	tax         billMoneyValue
	total       billMoneyValue
	origQty     values.Decimal
	origNet     billMoneyValue
	origTax     billMoneyValue
	usedQty     values.Decimal
	usedNet     billMoneyValue
	usedTax     billMoneyValue
}

// validateDraft checks scope, lifecycle, basis and policy without mutating.
// It runs under the registry lock; consume_basis reports whether the caller
// may consume basis (issue) or only test it (preview/draft).
func (r *AdjustmentRegistry) validateDraft(op AdjustOp, input AdjustDraftInput) (*adjustInvoiceState, []adjustPricedLine, billMoneyValue, billMoneyValue, billMoneyValue, error) {
	state, ok := r.invoices[adjustKey(op.TenantID, op.LegalEntityID, strings.TrimSpace(input.InvoiceID))]
	if !ok {
		for _, candidate := range r.invoices {
			if candidate.head.InvoiceID == strings.TrimSpace(input.InvoiceID) {
				return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, ErrAdjustDenied
			}
		}
		return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, ErrAdjustDenied
	}
	if state.status == "voided" || state.status == "VOIDED" {
		return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, fmt.Errorf("%w: invoice is voided", ErrAdjustState)
	}
	if input.Kind != "credit" && input.Kind != "debit" {
		return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, fmt.Errorf("%w: unknown adjustment kind %q", ErrAdjustValidation, input.Kind)
	}
	if len(input.Lines) == 0 {
		return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, fmt.Errorf("%w: adjustment needs lines", ErrAdjustValidation)
	}
	if strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.EvidenceRef) == "" {
		return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, fmt.Errorf("%w: adjustment needs reason and evidence", ErrAdjustValidation)
	}
	if input.Kind == "debit" && strings.TrimSpace(input.TermsRef) == "" {
		return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, fmt.Errorf("%w: debit needs explicit accepted terms", ErrAdjustValidation)
	}
	if strings.TrimSpace(input.CorrectsRef) != "" {
		target, ok := r.adjustments[strings.TrimSpace(input.CorrectsRef)]
		if !ok || target.TenantID != op.TenantID || target.LegalEntityID != op.LegalEntityID || target.Status != AdjustIssued || target.InvoiceID != state.head.InvoiceID {
			return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, fmt.Errorf("%w: correction target is not an issued adjustment on this invoice", ErrAdjustState)
		}
	}
	priced := make([]adjustPricedLine, 0, len(input.Lines))
	seen := map[string]bool{}
	netSum := mustBillZero()
	taxSum := mustBillZero()
	for _, selected := range input.Lines {
		if seen[selected.LineID] {
			return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, fmt.Errorf("%w: duplicate line %q", ErrAdjustValidation, selected.LineID)
		}
		seen[selected.LineID] = true
		line, ok := state.lines[strings.TrimSpace(selected.LineID)]
		if !ok {
			return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, fmt.Errorf("%w: unknown invoice line %q", ErrAdjustValidation, selected.LineID)
		}
		hasQty := strings.TrimSpace(selected.Qty) != ""
		hasAmount := strings.TrimSpace(selected.Amount) != ""
		if hasQty == hasAmount {
			return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, fmt.Errorf("%w: line %s takes exactly one of quantity or amount", ErrAdjustValidation, selected.LineID)
		}
		tax := adjustZero(state.currency)
		if strings.TrimSpace(selected.TaxAmount) != "" {
			parsed, err := billParseMoney(selected.TaxAmount)
			if err != nil || parsed.currency != state.currency {
				return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, fmt.Errorf("%w: line %s tax must be %s money", ErrAdjustValidation, selected.LineID, state.currency)
			}
			if parsed.amount.Sign() < 0 {
				return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, fmt.Errorf("%w: line %s tax cannot be negative", ErrAdjustValidation, selected.LineID)
			}
			tax = parsed
		}
		row := adjustPricedLine{lineID: strings.TrimSpace(selected.LineID), description: line.description,
			origQty: line.qty, origNet: line.amount, origTax: line.tax,
			usedQty: line.creditedQty, usedNet: line.creditedNet, usedTax: line.creditedTax, tax: tax}
		if hasQty {
			qty, err := billParseDecimal(selected.Qty)
			if err != nil || qty.Sign() <= 0 {
				return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, fmt.Errorf("%w: line %s quantity must be positive", ErrAdjustValidation, selected.LineID)
			}
			product, err := line.unitPrice.amount.Mul(qty, line.unitPrice.amount.Scale()+qty.Scale(), values.RoundingExactRequired)
			if err != nil {
				return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, fmt.Errorf("%w: line %s extension: %v", ErrAdjustValidation, selected.LineID, err)
			}
			net, err := billCents(product, state.currency)
			if err != nil {
				return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, fmt.Errorf("%w: line %s net: %v", ErrAdjustValidation, selected.LineID, err)
			}
			row.byQty, row.qty, row.net = true, qty, net
		} else {
			net, err := billParseMoney(selected.Amount)
			if err != nil || net.currency != state.currency || net.amount.Sign() <= 0 {
				return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, fmt.Errorf("%w: line %s amount must be positive %s money", ErrAdjustValidation, selected.LineID, state.currency)
			}
			row.net = net
		}
		total, err := billAdd(row.net.amount, row.tax.amount)
		if err != nil {
			return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, err
		}
		quantized, err := billCents(total, state.currency)
		if err != nil {
			return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, fmt.Errorf("%w: line %s total: %v", ErrAdjustValidation, selected.LineID, err)
		}
		row.total = quantized
		if input.Kind == "credit" {
			if row.byQty {
				avail, err := billSub(line.qty, line.creditedQty)
				if err != nil {
					return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, err
				}
				if row.qty.Cmp(avail) > 0 {
					return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, fmt.Errorf("%w: line %s has %s of %s left", ErrAdjustShortfall, row.lineID, billCanonical(avail), line.description)
				}
			}
			availNet, err := billSub(line.amount.amount, line.creditedNet.amount)
			if err != nil {
				return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, err
			}
			if row.net.amount.Cmp(availNet) > 0 {
				left, _ := billCents(availNet, state.currency)
				return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, fmt.Errorf("%w: line %s has %s net left", ErrAdjustShortfall, row.lineID, left.String())
			}
			availTax, err := billSub(line.tax.amount, line.creditedTax.amount)
			if err != nil {
				return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, err
			}
			if row.tax.amount.Cmp(availTax) > 0 {
				left, _ := billCents(availTax, state.currency)
				return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, fmt.Errorf("%w: line %s has %s tax left", ErrAdjustShortfall, row.lineID, left.String())
			}
		}
		if netSum, err = billAdd(netSum, row.net.amount); err != nil {
			return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, err
		}
		if taxSum, err = billAdd(taxSum, row.tax.amount); err != nil {
			return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, err
		}
		priced = append(priced, row)
	}
	net, err := billCents(netSum, state.currency)
	if err != nil {
		return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, fmt.Errorf("%w: net %s needs more than cents", ErrAdjustValidation, billStrip(netSum.String()))
	}
	tax, err := billCents(taxSum, state.currency)
	if err != nil {
		return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, fmt.Errorf("%w: tax %s needs more than cents", ErrAdjustValidation, billStrip(taxSum.String()))
	}
	gross, err := billAdd(netSum, taxSum)
	if err != nil {
		return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, err
	}
	total, err := billCents(gross, state.currency)
	if err != nil {
		return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, fmt.Errorf("%w: total %s needs more than cents", ErrAdjustValidation, billStrip(gross.String()))
	}
	if strings.TrimSpace(input.CorrectsRef) != "" {
		target := r.adjustments[strings.TrimSpace(input.CorrectsRef)]
		targetTotal, err := billParseMoney(target.TotalAmount)
		if err != nil {
			return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, err
		}
		if total.amount.Cmp(targetTotal.amount) > 0 {
			return nil, nil, billMoneyValue{}, billMoneyValue{}, billMoneyValue{}, fmt.Errorf("%w: correction exceeds its target %s", ErrAdjustShortfall, target.Number)
		}
	}
	return state, priced, net, tax, total, nil
}

// PreviewAdjustment prices one hypothetical adjustment without recording it.
func (r *AdjustmentRegistry) PreviewAdjustment(op AdjustOp, input AdjustDraftInput) (AdjustPreview, error) {
	if err := op.validate(); err != nil {
		return AdjustPreview{}, err
	}
	if !op.AdjustAllowed {
		return AdjustPreview{}, ErrAdjustDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state, priced, net, tax, total, err := r.validateDraft(op, input)
	if err != nil {
		return AdjustPreview{}, err
	}
	preview := AdjustPreview{InvoiceID: state.head.InvoiceID, Kind: input.Kind,
		NetAmount: net.String(), TaxAmount: tax.String(), TotalAmount: total.String(), Currency: state.currency}
	for _, row := range priced {
		line := state.lines[row.lineID]
		availQty, _ := billSub(line.qty, line.creditedQty)
		availNet, _ := billSub(line.amount.amount, line.creditedNet.amount)
		availTax, _ := billSub(line.tax.amount, line.creditedTax.amount)
		leftNet, _ := billCents(availNet, state.currency)
		leftTax, _ := billCents(availTax, state.currency)
		usedNet, _ := billCents(line.creditedNet.amount, state.currency)
		usedTax, _ := billCents(line.creditedTax.amount, state.currency)
		preview.Lines = append(preview.Lines, AdjustPreviewLine{
			LineID: row.lineID, Description: row.description,
			OriginalQty: billCanonical(line.qty), OriginalAmount: line.amount.String(), OriginalTax: line.tax.String(),
			CreditedQty: billCanonical(line.creditedQty), CreditedAmount: usedNet.String(), CreditedTax: usedTax.String(),
			AvailableQty: billCanonical(availQty), AvailableAmount: leftNet.String(), AvailableTax: leftTax.String(),
			ProposedQty: billCanonical(row.qty), ProposedAmount: row.net.String(), ProposedTax: row.tax.String(),
		})
	}
	return preview, nil
}
func adjustDraftFingerprint(input AdjustDraftInput) string {
	parts := []string{input.InvoiceID, input.Kind, input.Reason, input.EvidenceRef, input.TermsRef, input.CorrectsRef}
	for _, line := range input.Lines {
		parts = append(parts, line.LineID, line.Qty, line.Amount, line.TaxAmount)
	}
	return strings.Join(parts, "\x00")
}

func adjustFrozenLines(priced []adjustPricedLine) []AdjustLine {
	lines := make([]AdjustLine, 0, len(priced))
	for _, row := range priced {
		qty := ""
		if row.byQty {
			qty = billCanonical(row.qty)
		}
		lines = append(lines, AdjustLine{
			LineID: row.lineID, Description: row.description, Qty: qty,
			NetAmount: row.net.String(), TaxAmount: row.tax.String(), TotalAmount: row.total.String(),
			OriginalQty: billCanonical(row.origQty), OriginalAmount: row.origNet.String(), OriginalTax: row.origTax.String(),
		})
	}
	return lines
}

// DraftAdjustment records one draft adjustment and reserves its evidence.
// The same key and canonical payload replays the live record; the same key
// with a changed payload, or already-consumed evidence, conflicts.
func (r *AdjustmentRegistry) DraftAdjustment(op AdjustOp, input AdjustDraftInput, key string) (Adjustment, error) {
	if err := op.validate(); err != nil {
		return Adjustment{}, err
	}
	if !op.AdjustAllowed {
		return Adjustment{}, ErrAdjustDenied
	}
	if strings.TrimSpace(key) == "" {
		return Adjustment{}, fmt.Errorf("%w: draft needs an idempotency key", ErrAdjustValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	fingerprint := adjustDraftFingerprint(input)
	idemKey := adjustKey(op.TenantID, strings.TrimSpace(key))
	if prior, ok := r.idem[idemKey]; ok {
		if prior.fingerprint != fingerprint {
			return Adjustment{}, fmt.Errorf("%w: key is already used", ErrAdjustConflict)
		}
		if prior.tenant != op.TenantID || prior.entity != op.LegalEntityID {
			return Adjustment{}, ErrAdjustDenied
		}
		adjustment, ok := r.adjustments[prior.adjustmentID]
		if !ok {
			return Adjustment{}, ErrAdjustDenied
		}
		return *adjustment, nil
	}
	state, priced, net, tax, total, err := r.validateDraft(op, input)
	if err != nil {
		return Adjustment{}, err
	}
	evidenceKey := adjustKey(op.TenantID, strings.TrimSpace(input.EvidenceRef))
	if _, taken := r.evidence[evidenceKey]; taken {
		return Adjustment{}, fmt.Errorf("%w: evidence %q is already consumed", ErrAdjustConflict, input.EvidenceRef)
	}
	r.adjSeq++
	adjustment := &Adjustment{
		AdjustmentID: fmt.Sprintf("adj-%d", r.adjSeq),
		TenantID:     op.TenantID, LegalEntityID: op.LegalEntityID, CustomerID: state.head.CustomerID,
		Currency: state.currency, Kind: input.Kind, InvoiceID: state.head.InvoiceID,
		InvoiceRevision: state.revision, InvoiceDigest: state.head.Digest,
		Lines:     adjustFrozenLines(priced),
		NetAmount: net.String(), TaxAmount: tax.String(), TotalAmount: total.String(),
		Reason: strings.TrimSpace(input.Reason), EvidenceRef: strings.TrimSpace(input.EvidenceRef),
		TermsRef: strings.TrimSpace(input.TermsRef), CorrectsRef: strings.TrimSpace(input.CorrectsRef),
		Status: AdjustDraft, DraftedBy: op.ActorID, CreatedAt: adjustStamp(r),
	}
	adjustment.Events = append(adjustment.Events, AdjustEvent{Kind: "DRAFTED", Actor: op.ActorID, At: adjustment.CreatedAt, Detail: adjustment.TotalAmount})
	r.adjustments[adjustment.AdjustmentID] = adjustment
	r.evidence[evidenceKey] = adjustment.AdjustmentID
	r.idem[idemKey] = adjustIdem{fingerprint: fingerprint, adjustmentID: adjustment.AdjustmentID, tenant: op.TenantID, entity: op.LegalEntityID}
	return *adjustment, nil
}

// AmendDraft rewrites one draft's lines, reason or evidence. Submitted work
// and beyond is immutable; corrections link forward instead.
func (r *AdjustmentRegistry) AmendDraft(op AdjustOp, id string, input AdjustDraftInput, key string) (Adjustment, error) {
	if err := op.validate(); err != nil {
		return Adjustment{}, err
	}
	if !op.AdjustAllowed {
		return Adjustment{}, ErrAdjustDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	adjustment, ok := r.adjustments[strings.TrimSpace(id)]
	if !ok || adjustment.TenantID != op.TenantID || adjustment.LegalEntityID != op.LegalEntityID {
		return Adjustment{}, ErrAdjustDenied
	}
	if adjustment.Status != AdjustDraft {
		return Adjustment{}, fmt.Errorf("%w: only drafts amend", ErrAdjustState)
	}
	if input.Kind != adjustment.Kind || strings.TrimSpace(input.InvoiceID) != adjustment.InvoiceID || strings.TrimSpace(input.CorrectsRef) != adjustment.CorrectsRef {
		return Adjustment{}, fmt.Errorf("%w: amendment cannot change kind, invoice or correction link", ErrAdjustValidation)
	}
	state, priced, net, tax, total, err := r.validateDraft(op, input)
	if err != nil {
		return Adjustment{}, err
	}
	_ = state
	oldEvidence := adjustKey(op.TenantID, adjustment.EvidenceRef)
	newEvidence := adjustKey(op.TenantID, strings.TrimSpace(input.EvidenceRef))
	if newEvidence != oldEvidence {
		if _, taken := r.evidence[newEvidence]; taken {
			return Adjustment{}, fmt.Errorf("%w: evidence %q is already consumed", ErrAdjustConflict, input.EvidenceRef)
		}
		delete(r.evidence, oldEvidence)
		r.evidence[newEvidence] = adjustment.AdjustmentID
	}
	adjustment.Lines = adjustFrozenLines(priced)
	adjustment.NetAmount, adjustment.TaxAmount, adjustment.TotalAmount = net.String(), tax.String(), total.String()
	adjustment.Reason = strings.TrimSpace(input.Reason)
	adjustment.EvidenceRef = strings.TrimSpace(input.EvidenceRef)
	adjustment.TermsRef = strings.TrimSpace(input.TermsRef)
	adjustment.Events = append(adjustment.Events, AdjustEvent{Kind: "AMENDED", Actor: op.ActorID, At: adjustStamp(r), Detail: adjustment.TotalAmount})
	_ = key
	return *adjustment, nil
}

// CancelDraft abandons one draft and releases its evidence for reuse.
func (r *AdjustmentRegistry) CancelDraft(op AdjustOp, id, key string) error {
	if err := op.validate(); err != nil {
		return err
	}
	if !op.AdjustAllowed {
		return ErrAdjustDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	adjustment, ok := r.adjustments[strings.TrimSpace(id)]
	if !ok || adjustment.TenantID != op.TenantID || adjustment.LegalEntityID != op.LegalEntityID {
		return ErrAdjustDenied
	}
	if adjustment.Status != AdjustDraft {
		return fmt.Errorf("%w: only drafts cancel", ErrAdjustState)
	}
	adjustment.Status = AdjustCancelled
	delete(r.evidence, adjustKey(op.TenantID, adjustment.EvidenceRef))
	adjustment.Events = append(adjustment.Events, AdjustEvent{Kind: "CANCELLED", Actor: op.ActorID, At: adjustStamp(r), Detail: ""})
	_ = key
	return nil
}

func (r *AdjustmentRegistry) transition(op AdjustOp, id, key string, from []string, to, event string) (*Adjustment, error) {
	idemKey := adjustKey(op.TenantID, "tx", strings.TrimSpace(key))
	if prior, ok := r.txidem[idemKey]; ok {
		adjustment, ok := r.adjustments[prior.adjustmentID]
		if !ok || prior.tenant != op.TenantID || prior.entity != op.LegalEntityID {
			return nil, ErrAdjustDenied
		}
		return adjustment, nil
	}
	adjustment, ok := r.adjustments[strings.TrimSpace(id)]
	if !ok || adjustment.TenantID != op.TenantID || adjustment.LegalEntityID != op.LegalEntityID {
		return nil, ErrAdjustDenied
	}
	allowed := false
	for _, status := range from {
		if adjustment.Status == status {
			allowed = true
		}
	}
	if !allowed {
		return nil, fmt.Errorf("%w: %s cannot move from %s", ErrAdjustState, adjustment.AdjustmentID, adjustment.Status)
	}
	adjustment.Status = to
	adjustment.Events = append(adjustment.Events, AdjustEvent{Kind: event, Actor: op.ActorID, At: adjustStamp(r), Detail: ""})
	r.txidem[idemKey] = adjustIdem{adjustmentID: adjustment.AdjustmentID, tenant: op.TenantID, entity: op.LegalEntityID}
	return adjustment, nil
}

// SubmitAdjustment locks one draft for approval.
func (r *AdjustmentRegistry) SubmitAdjustment(op AdjustOp, id, key string) (Adjustment, error) {
	if err := op.validate(); err != nil {
		return Adjustment{}, err
	}
	if !op.AdjustAllowed {
		return Adjustment{}, ErrAdjustDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	adjustment, err := r.transition(op, id, key, []string{AdjustDraft}, AdjustSubmitted, "SUBMITTED")
	if err != nil {
		return Adjustment{}, err
	}
	if adjustment.SubmittedBy == "" {
		adjustment.SubmittedBy = op.ActorID
	}
	return *adjustment, nil
}

// ApproveAdjustment records a distinct approver's decision. The approver
// must hold the grant and differ from the drafter.
func (r *AdjustmentRegistry) ApproveAdjustment(op AdjustOp, id, key string) (Adjustment, error) {
	if err := op.validate(); err != nil {
		return Adjustment{}, err
	}
	if !op.ApproveAllowed {
		return Adjustment{}, ErrAdjustDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	adjustment, ok := r.adjustments[strings.TrimSpace(id)]
	if !ok || adjustment.TenantID != op.TenantID || adjustment.LegalEntityID != op.LegalEntityID {
		return Adjustment{}, ErrAdjustDenied
	}
	if adjustment.Status != AdjustSubmitted {
		return Adjustment{}, fmt.Errorf("%w: only submitted work approves", ErrAdjustState)
	}
	if op.ActorID == adjustment.DraftedBy || op.ActorID == adjustment.SubmittedBy {
		return Adjustment{}, fmt.Errorf("%w: approver must differ from the drafter", ErrAdjustState)
	}
	moved, err := r.transition(op, id, key, []string{AdjustSubmitted}, AdjustApproved, "APPROVED")
	if err != nil {
		return Adjustment{}, err
	}
	if moved.ApprovedBy == "" {
		moved.ApprovedBy = op.ActorID
	}
	return *moved, nil
}

// IssueAdjustment numbers one approved adjustment, renders its immutable
// document, consumes the credit basis and settles the ledger effect. Basis
// and revision are rechecked under the lock so concurrent issues conserve.
func (r *AdjustmentRegistry) IssueAdjustment(op AdjustOp, id, key string) (Adjustment, error) {
	if err := op.validate(); err != nil {
		return Adjustment{}, err
	}
	if !op.AdjustAllowed {
		return Adjustment{}, ErrAdjustDenied
	}
	if strings.TrimSpace(key) == "" {
		return Adjustment{}, fmt.Errorf("%w: issue needs an idempotency key", ErrAdjustValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	idemKey := adjustKey(op.TenantID, "tx", strings.TrimSpace(key))
	if prior, ok := r.txidem[idemKey]; ok {
		adjustment, ok := r.adjustments[prior.adjustmentID]
		if !ok || prior.tenant != op.TenantID || prior.entity != op.LegalEntityID {
			return Adjustment{}, ErrAdjustDenied
		}
		return *adjustment, nil
	}
	adjustment, ok := r.adjustments[strings.TrimSpace(id)]
	if !ok || adjustment.TenantID != op.TenantID || adjustment.LegalEntityID != op.LegalEntityID {
		return Adjustment{}, ErrAdjustDenied
	}
	if adjustment.Status != AdjustApproved {
		return Adjustment{}, fmt.Errorf("%w: only approved work issues", ErrAdjustState)
	}
	state, ok := r.invoices[adjustKey(op.TenantID, op.LegalEntityID, adjustment.InvoiceID)]
	if !ok {
		return Adjustment{}, ErrAdjustDenied
	}
	if adjustment.InvoiceRevision != state.revision || adjustment.InvoiceDigest != state.head.Digest {
		return Adjustment{}, fmt.Errorf("%w: adjustment pins rev %d, head is %d", ErrAdjustStale, adjustment.InvoiceRevision, state.revision)
	}
	ledger, ok := r.ledgers[op.TenantID]
	if !ok || ledger == nil {
		return Adjustment{}, fmt.Errorf("%w: no ledger is bound for this tenant", ErrAdjustValidation)
	}
	total, err := billParseMoney(adjustment.TotalAmount)
	if err != nil {
		return Adjustment{}, err
	}
	if adjustment.Kind == "credit" {
		for _, line := range adjustment.Lines {
			live, ok := state.lines[line.LineID]
			if !ok {
				return Adjustment{}, fmt.Errorf("%w: invoice line %q left the head revision", ErrAdjustStale, line.LineID)
			}
			used, err := r.consumeBasis(live, line, state.currency)
			if err != nil {
				return Adjustment{}, err
			}
			_ = used
		}
		next, err := billAdd(state.credits.amount, total.amount)
		if err != nil {
			return Adjustment{}, err
		}
		if next.Cmp(state.total.amount) > 0 {
			left, _ := billSub(state.total.amount, state.credits.amount)
			remaining, _ := billCents(left, state.currency)
			return Adjustment{}, fmt.Errorf("%w: invoice has %s creditable left", ErrAdjustShortfall, remaining.String())
		}
		quantized, err := billCents(next, state.currency)
		if err != nil {
			return Adjustment{}, err
		}
		money, err := values.NewMoney(total.amount.String(), total.currency, total.amount.Scale(), values.RoundingExactRequired)
		if err != nil {
			return Adjustment{}, fmt.Errorf("%w: credit money: %v", ErrAdjustValidation, err)
		}
		if err := ledger.Post(ReceivableEvent{
			EventID: "adj-credit:" + adjustment.AdjustmentID, TenantID: op.TenantID, InvoiceID: state.head.InvoiceID,
			Kind: ReceivableCreditPosted, Amount: money, At: adjustStamp(r), Actor: op.ActorID, Ref: adjustment.AdjustmentID,
		}); err != nil {
			return Adjustment{}, err
		}
		state.credits = quantized
	} else {
		offset, open, err := r.offsetDebit(op.TenantID, state.head.CustomerID, total)
		if err != nil {
			return Adjustment{}, err
		}
		settled, err := billCents(offset, state.currency)
		if err != nil {
			return Adjustment{}, err
		}
		adjustment.SettledAmount = settled.String()
		if open.Sign() > 0 {
			rest, err := billAdd(state.openDebits.amount, open)
			if err != nil {
				return Adjustment{}, err
			}
			kept, err := billCents(rest, state.currency)
			if err != nil {
				return Adjustment{}, err
			}
			state.openDebits = kept
		}
	}
	var number string
	if adjustment.Kind == "credit" {
		r.cmSeq[op.TenantID]++
		number = fmt.Sprintf("CM-%04d", r.cmSeq[op.TenantID])
	} else {
		r.dmSeq[op.TenantID]++
		number = fmt.Sprintf("DM-%04d", r.dmSeq[op.TenantID])
	}
	adjustment.Number = number
	adjustment.IssuedBy = op.ActorID
	adjustment.IssuedAt = adjustStamp(r)
	memo, err := r.memoTemplate(adjustment.Kind)
	if err != nil {
		return Adjustment{}, err
	}
	net, _ := bill008Split(adjustment.NetAmount)
	tax, _ := bill008Split(adjustment.TaxAmount)
	gross, _ := bill008Split(adjustment.TotalAmount)
	artifact, err := memo.Render(map[string]doctemplate.Binding{
		"number": bill008Binding(number), "invoice_number": bill008Binding(state.head.Number),
		"customer": bill008Binding(state.head.CustomerID), "currency": bill008Binding(state.currency),
		"net_amount": bill008Binding(net), "tax_amount": bill008Binding(tax),
		"total_amount": bill008Binding(gross), "reason": bill008Binding(adjustment.Reason),
		"terms_ref": bill008Binding(adjustment.TermsRef), "issued_at": bill008Binding(adjustment.IssuedAt),
	})
	if err != nil {
		return Adjustment{}, fmt.Errorf("%w: memo document: %v", ErrAdjustValidation, err)
	}
	adjustment.DocumentDigest = artifact.Digest
	adjustment.Status = AdjustIssued
	adjustment.Events = append(adjustment.Events, AdjustEvent{Kind: "ISSUED", Actor: op.ActorID, At: adjustment.IssuedAt, Detail: number + " " + artifact.Digest})
	r.txidem[idemKey] = adjustIdem{adjustmentID: adjustment.AdjustmentID, tenant: op.TenantID, entity: op.LegalEntityID}
	return *adjustment, nil
}

func bill008Binding(value string) doctemplate.Binding {
	return doctemplate.Binding{Kind: doctemplate.String, Value: value}
}

func bill008Split(text string) (string, string) {
	parts := strings.SplitN(strings.TrimSpace(text), " ", 2)
	if len(parts) != 2 {
		return text, ""
	}
	return parts[0], parts[1]
}

// consumeBasis moves one issued credit line into the consumed basis,
// refusing anything above the exact remainder.
func (r *AdjustmentRegistry) consumeBasis(live *adjustLineState, line AdjustLine, currency string) (bool, error) {
	if strings.TrimSpace(line.Qty) != "" {
		qty, err := billParseDecimal(line.Qty)
		if err != nil {
			return false, err
		}
		avail, err := billSub(live.qty, live.creditedQty)
		if err != nil {
			return false, err
		}
		if qty.Cmp(avail) > 0 {
			return false, fmt.Errorf("%w: line %s has %s left", ErrAdjustShortfall, line.LineID, billCanonical(avail))
		}
		used, err := billAdd(live.creditedQty, qty)
		if err != nil {
			return false, err
		}
		live.creditedQty = used
	}
	net, err := billParseMoney(line.NetAmount)
	if err != nil {
		return false, err
	}
	availNet, err := billSub(live.amount.amount, live.creditedNet.amount)
	if err != nil {
		return false, err
	}
	if net.amount.Cmp(availNet) > 0 {
		left, _ := billCents(availNet, currency)
		return false, fmt.Errorf("%w: line %s has %s net left", ErrAdjustShortfall, line.LineID, left.String())
	}
	usedNet, err := billAdd(live.creditedNet.amount, net.amount)
	if err != nil {
		return false, err
	}
	keptNet, err := billCents(usedNet, currency)
	if err != nil {
		return false, err
	}
	live.creditedNet = keptNet
	tax, err := billParseMoney(line.TaxAmount)
	if err != nil {
		return false, err
	}
	availTax, err := billSub(live.tax.amount, live.creditedTax.amount)
	if err != nil {
		return false, err
	}
	if tax.amount.Cmp(availTax) > 0 {
		left, _ := billCents(availTax, currency)
		return false, fmt.Errorf("%w: line %s has %s tax left", ErrAdjustShortfall, line.LineID, left.String())
	}
	usedTax, err := billAdd(live.creditedTax.amount, tax.amount)
	if err != nil {
		return false, err
	}
	keptTax, err := billCents(usedTax, currency)
	if err != nil {
		return false, err
	}
	live.creditedTax = keptTax
	return true, nil
}

// offsetDebit settles one issued debit against available customer credit
// first, returning the settled and still-open portions.
func (r *AdjustmentRegistry) offsetDebit(tenant, customer string, total billMoneyValue) (values.Decimal, values.Decimal, error) {
	key := adjustKey(tenant, customer)
	balance, ok := r.balances[key]
	if !ok {
		r.balances[key] = &adjustBalance{amount: adjustZero(total.currency), offsets: adjustZero(total.currency), currency: total.currency}
		balance = r.balances[key]
	}
	available := balance.amount.amount
	settled := total.amount
	if available.Cmp(total.amount) < 0 {
		settled = available
	}
	open, err := billSub(total.amount, settled)
	if err != nil {
		return values.Decimal{}, values.Decimal{}, err
	}
	left, err := billSub(available, settled)
	if err != nil {
		return values.Decimal{}, values.Decimal{}, err
	}
	keptLeft, err := billCents(left, total.currency)
	if err != nil {
		return values.Decimal{}, values.Decimal{}, err
	}
	balance.amount = keptLeft
	grown, err := billAdd(balance.offsets.amount, settled)
	if err != nil {
		return values.Decimal{}, values.Decimal{}, err
	}
	keptGrown, err := billCents(grown, total.currency)
	if err != nil {
		return values.Decimal{}, values.Decimal{}, err
	}
	balance.offsets = keptGrown
	return settled, open, nil
}

// AllocateCredit applies one issued credit to paid receipts, splitting the
// invoice position into settled obligation and released customer credit.
func (r *AdjustmentRegistry) AllocateCredit(op AdjustOp, id, key string) (AdjustAllocation, error) {
	if err := op.validate(); err != nil {
		return AdjustAllocation{}, err
	}
	if !op.AdjustAllowed {
		return AdjustAllocation{}, ErrAdjustDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, ok := r.txidem[adjustKey(op.TenantID, "tx", strings.TrimSpace(key))]; ok {
		adjustment, ok := r.adjustments[prior.adjustmentID]
		if !ok || prior.tenant != op.TenantID || prior.entity != op.LegalEntityID || adjustment.AllocationID == "" {
			return AdjustAllocation{}, ErrAdjustDenied
		}
		return *r.allocations[adjustment.AllocationID], nil
	}
	adjustment, ok := r.adjustments[strings.TrimSpace(id)]
	if !ok || adjustment.TenantID != op.TenantID || adjustment.LegalEntityID != op.LegalEntityID {
		return AdjustAllocation{}, ErrAdjustDenied
	}
	if adjustment.Kind != "credit" {
		return AdjustAllocation{}, fmt.Errorf("%w: only credits allocate", ErrAdjustState)
	}
	if adjustment.Status != AdjustIssued {
		return AdjustAllocation{}, fmt.Errorf("%w: only issued credits allocate", ErrAdjustState)
	}
	if adjustment.AllocationID != "" {
		return AdjustAllocation{}, fmt.Errorf("%w: credit is already allocated", ErrAdjustState)
	}
	state, ok := r.invoices[adjustKey(op.TenantID, op.LegalEntityID, adjustment.InvoiceID)]
	if !ok {
		return AdjustAllocation{}, ErrAdjustDenied
	}
	credit, err := billParseMoney(adjustment.TotalAmount)
	if err != nil {
		return AdjustAllocation{}, err
	}
	netOwed, err := billSub(state.total.amount, state.credits.amount)
	if err != nil {
		return AdjustAllocation{}, err
	}
	netOwed, err = billAdd(netOwed, state.openDebits.amount)
	if err != nil {
		return AdjustAllocation{}, err
	}
	settled := state.paid.amount
	if netOwed.Cmp(state.paid.amount) < 0 {
		settled = netOwed
	}
	if settled.Sign() < 0 {
		settled = mustBillZero()
	}
	releaseTarget, err := billSub(state.paid.amount, settled)
	if err != nil {
		return AdjustAllocation{}, err
	}
	room, err := billSub(releaseTarget, state.released.amount)
	if err != nil {
		return AdjustAllocation{}, err
	}
	release := credit.amount
	if room.Cmp(credit.amount) < 0 {
		release = room
	}
	if release.Sign() < 0 {
		release = mustBillZero()
	}
	grown, err := billAdd(state.released.amount, release)
	if err != nil {
		return AdjustAllocation{}, err
	}
	kept, err := billCents(grown, state.currency)
	if err != nil {
		return AdjustAllocation{}, err
	}
	state.released = kept
	balanceKey := adjustKey(op.TenantID, state.head.CustomerID)
	balance, ok := r.balances[balanceKey]
	if !ok {
		balance = &adjustBalance{amount: adjustZero(state.currency), offsets: adjustZero(state.currency), currency: state.currency}
		r.balances[balanceKey] = balance
	}
	funded, err := billAdd(balance.amount.amount, release)
	if err != nil {
		return AdjustAllocation{}, err
	}
	keptFunded, err := billCents(funded, state.currency)
	if err != nil {
		return AdjustAllocation{}, err
	}
	balance.amount = keptFunded
	r.allocSeq++
	allocation := &AdjustAllocation{
		AllocationID: fmt.Sprintf("alloc-%d", r.allocSeq), AdjustmentID: adjustment.AdjustmentID,
		InvoiceID: state.head.InvoiceID, CustomerID: state.head.CustomerID,
		At: adjustStamp(r),
	}
	settledMoney, err := billCents(settled, state.currency)
	if err != nil {
		return AdjustAllocation{}, err
	}
	releaseMoney, err := billCents(release, state.currency)
	if err != nil {
		return AdjustAllocation{}, err
	}
	allocation.SettledAmount = settledMoney.String()
	allocation.CustomerCreditAmount = releaseMoney.String()
	adjustment.AllocationID = allocation.AllocationID
	adjustment.Events = append(adjustment.Events, AdjustEvent{Kind: "ALLOCATED", Actor: op.ActorID, At: allocation.At,
		Detail: fmt.Sprintf("settled %s credit %s", allocation.SettledAmount, allocation.CustomerCreditAmount)})
	r.allocations[allocation.AllocationID] = allocation
	idemKey := adjustKey(op.TenantID, "tx", strings.TrimSpace(key))
	r.txidem[idemKey] = adjustIdem{adjustmentID: adjustment.AdjustmentID, tenant: op.TenantID, entity: op.LegalEntityID}
	return *allocation, nil
}

// VoidInvoice voids one unpaid invoice under the tenant void policy. Paid
// invoices always refuse (credit them instead); the void keeps number,
// reason and time, calls the bound invoice port first when one is attached,
// and extinguishes the remaining obligation through an explicitly labeled
// ledger credit.
func (r *AdjustmentRegistry) VoidInvoice(op AdjustOp, invoiceID, reason, evidence, key string) (AdjustVoid, error) {
	if err := op.validate(); err != nil {
		return AdjustVoid{}, err
	}
	if !op.VoidAllowed {
		return AdjustVoid{}, ErrAdjustDenied
	}
	if strings.TrimSpace(reason) == "" || strings.TrimSpace(evidence) == "" {
		return AdjustVoid{}, fmt.Errorf("%w: void needs reason and evidence", ErrAdjustValidation)
	}
	if strings.TrimSpace(key) == "" {
		return AdjustVoid{}, fmt.Errorf("%w: void needs an idempotency key", ErrAdjustValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	idemKey := adjustKey(op.TenantID, "void", strings.TrimSpace(key))
	if prior, ok := r.txidem[idemKey]; ok {
		for _, voided := range r.voids {
			if voided.VoidID == prior.adjustmentID && voided.TenantID == op.TenantID {
				return *voided, nil
			}
		}
		return AdjustVoid{}, ErrAdjustDenied
	}
	state, ok := r.invoices[adjustKey(op.TenantID, op.LegalEntityID, strings.TrimSpace(invoiceID))]
	if !ok {
		for _, candidate := range r.invoices {
			if candidate.head.InvoiceID == strings.TrimSpace(invoiceID) {
				return AdjustVoid{}, ErrAdjustDenied
			}
		}
		return AdjustVoid{}, ErrAdjustDenied
	}
	if state.status == "voided" || state.status == "VOIDED" {
		return AdjustVoid{}, fmt.Errorf("%w: invoice is already voided", ErrAdjustState)
	}
	if state.paid.amount.Sign() > 0 {
		return AdjustVoid{}, fmt.Errorf("%w: paid invoices credit instead of voiding", ErrAdjustState)
	}
	policy := r.policies[op.TenantID]
	if policy.VoidRequireApproval && !op.ApproveAllowed {
		return AdjustVoid{}, ErrAdjustDenied
	}
	if policy.VoidWindowDays > 0 {
		issuedAt, err := adjustParseTime(state.head.IssuedAt)
		if err != nil {
			return AdjustVoid{}, err
		}
		if adjustNow(r).Sub(issuedAt) > time.Duration(policy.VoidWindowDays)*24*time.Hour {
			return AdjustVoid{}, fmt.Errorf("%w: void window of %d days has passed", ErrAdjustState, policy.VoidWindowDays)
		}
	}
	if r.port != nil {
		if _, err := r.port.VoidInvoiceDocument(op, state.head.InvoiceID, state.revision, strings.TrimSpace(reason), strings.TrimSpace(key), adjustStamp(r)); err != nil {
			if errors.Is(err, ErrAdjustDenied) || errors.Is(err, ErrInvoiceNotFound) {
				return AdjustVoid{}, ErrAdjustDenied
			}
			return AdjustVoid{}, err
		}
	}
	remaining, err := billSub(state.total.amount, state.credits.amount)
	if err != nil {
		return AdjustVoid{}, err
	}
	if remaining.Sign() > 0 {
		ledger, ok := r.ledgers[op.TenantID]
		if !ok || ledger == nil {
			return AdjustVoid{}, fmt.Errorf("%w: no ledger is bound for this tenant", ErrAdjustValidation)
		}
		money, err := values.NewMoney(remaining.String(), state.currency, remaining.Scale(), values.RoundingExactRequired)
		if err != nil {
			return AdjustVoid{}, fmt.Errorf("%w: void credit money: %v", ErrAdjustValidation, err)
		}
		r.voidSeq++
		if err := ledger.Post(ReceivableEvent{
			EventID:  fmt.Sprintf("void-credit:%s:%d", state.head.InvoiceID, r.voidSeq),
			TenantID: op.TenantID, InvoiceID: state.head.InvoiceID,
			Kind: ReceivableCreditPosted, Amount: money, At: adjustStamp(r), Actor: op.ActorID,
			Ref: "void/" + strings.TrimSpace(reason),
		}); err != nil {
			r.voidSeq--
			return AdjustVoid{}, err
		}
		grown, err := billAdd(state.credits.amount, remaining)
		if err != nil {
			return AdjustVoid{}, err
		}
		kept, err := billCents(grown, state.currency)
		if err != nil {
			return AdjustVoid{}, err
		}
		state.credits = kept
	}
	state.status = "voided"
	r.voidSeq++
	voided := &AdjustVoid{
		VoidID: fmt.Sprintf("void-%d", r.voidSeq), TenantID: op.TenantID,
		InvoiceID: state.head.InvoiceID, Number: state.head.Number,
		Reason: strings.TrimSpace(reason), EvidenceRef: strings.TrimSpace(evidence),
		By: op.ActorID, At: adjustStamp(r),
	}
	r.voids[adjustKey(op.TenantID, op.LegalEntityID, state.head.InvoiceID)] = voided
	r.txidem[idemKey] = adjustIdem{adjustmentID: voided.VoidID, tenant: op.TenantID, entity: op.LegalEntityID}
	return *voided, nil
}

// Projection explains one invoice with explicit conservation components.
func (r *AdjustmentRegistry) Projection(op AdjustOp, invoiceID string) (AdjustProjection, error) {
	if err := op.validate(); err != nil {
		return AdjustProjection{}, err
	}
	if !op.AdjustAllowed {
		return AdjustProjection{}, ErrAdjustDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.invoices[adjustKey(op.TenantID, op.LegalEntityID, strings.TrimSpace(invoiceID))]
	if !ok {
		for _, candidate := range r.invoices {
			if candidate.head.InvoiceID == strings.TrimSpace(invoiceID) {
				return AdjustProjection{}, ErrAdjustDenied
			}
		}
		return AdjustProjection{}, ErrAdjustDenied
	}
	netOwed, err := billSub(state.total.amount, state.credits.amount)
	if err != nil {
		return AdjustProjection{}, err
	}
	netOwed, err = billAdd(netOwed, state.openDebits.amount)
	if err != nil {
		return AdjustProjection{}, err
	}
	settled := state.paid.amount
	if netOwed.Cmp(state.paid.amount) < 0 {
		settled = netOwed
	}
	if settled.Sign() < 0 {
		settled = mustBillZero()
	}
	left, err := billSub(netOwed, settled)
	if err != nil {
		return AdjustProjection{}, err
	}
	if left.Sign() < 0 {
		left = mustBillZero()
	}
	debits := mustBillZero()
	for _, adjustment := range r.adjustments {
		if adjustment.TenantID != op.TenantID || adjustment.LegalEntityID != op.LegalEntityID || adjustment.InvoiceID != state.head.InvoiceID {
			continue
		}
		if adjustment.Kind != "debit" || adjustment.Status != AdjustIssued {
			continue
		}
		parsed, err := billParseMoney(adjustment.TotalAmount)
		if err != nil {
			return AdjustProjection{}, err
		}
		if debits, err = billAdd(debits, parsed.amount); err != nil {
			return AdjustProjection{}, err
		}
	}
	settledMoney, err := billCents(settled, state.currency)
	if err != nil {
		return AdjustProjection{}, err
	}
	leftMoney, err := billCents(left, state.currency)
	if err != nil {
		return AdjustProjection{}, err
	}
	debitMoney, err := billCents(debits, state.currency)
	if err != nil {
		return AdjustProjection{}, err
	}
	balanceKey := adjustKey(op.TenantID, state.head.CustomerID)
	credit, offsets := adjustZero(state.currency), adjustZero(state.currency)
	if balance, ok := r.balances[balanceKey]; ok {
		credit, offsets = balance.amount, balance.offsets
	}
	return AdjustProjection{
		InvoiceID: state.head.InvoiceID, Obligation: state.total.String(), Credits: state.credits.String(),
		Debits: debitMoney.String(), Receipts: state.paid.String(), Settled: settledMoney.String(),
		ReleasedCredit: state.released.String(), DebitOffsets: offsets.String(),
		CustomerCredit: credit.String(), Remaining: leftMoney.String(),
	}, nil
}

// CustomerCreditBalance returns one customer's available credit.
func (r *AdjustmentRegistry) CustomerCreditBalance(op AdjustOp, customerID string) (string, error) {
	if err := op.validate(); err != nil {
		return "", err
	}
	if !op.AdjustAllowed {
		return "", ErrAdjustDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	balance, ok := r.balances[adjustKey(op.TenantID, strings.TrimSpace(customerID))]
	if !ok {
		return "", fmt.Errorf("%w: customer has no credit activity", ErrAdjustValidation)
	}
	return balance.amount.String(), nil
}

// AdjustmentTrace replays one adjustment's audit events.
func (r *AdjustmentRegistry) AdjustmentTrace(op AdjustOp, id string) ([]AdjustEvent, error) {
	if err := op.validate(); err != nil {
		return nil, err
	}
	if !op.AdjustAllowed {
		return nil, ErrAdjustDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	adjustment, ok := r.adjustments[strings.TrimSpace(id)]
	if !ok || adjustment.TenantID != op.TenantID || adjustment.LegalEntityID != op.LegalEntityID {
		return nil, ErrAdjustDenied
	}
	return append([]AdjustEvent(nil), adjustment.Events...), nil
}

// AdjustmentStatus returns the live record behind one idempotency key.
// Unknown keys share the neutral denial: an observed timeout never becomes
// an invented success.
func (r *AdjustmentRegistry) AdjustmentStatus(op AdjustOp, key string) (Adjustment, error) {
	if err := op.validate(); err != nil {
		return Adjustment{}, err
	}
	if !op.AdjustAllowed {
		return Adjustment{}, ErrAdjustDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, namespace := range []string{adjustKey(op.TenantID, strings.TrimSpace(key)), adjustKey(op.TenantID, "tx", strings.TrimSpace(key))} {
		table := r.idem
		if strings.Contains(namespace, "\x00tx\x00") {
			table = r.txidem
		}
		if prior, ok := table[namespace]; ok {
			if prior.tenant != op.TenantID || prior.entity != op.LegalEntityID {
				return Adjustment{}, ErrAdjustDenied
			}
			if adjustment, ok := r.adjustments[prior.adjustmentID]; ok {
				return *adjustment, nil
			}
		}
	}
	return Adjustment{}, ErrAdjustDenied
}

type adjustSnapshotLine struct {
	Description string
	Qty         string
	UnitPrice   string
	Amount      string
	Tax         string
	CreditedQty string
	CreditedNet string
	CreditedTax string
}

type adjustSnapshotInvoice struct {
	Head       AdjustInvoice
	Lines      map[string]*adjustSnapshotLine
	Total      string
	TaxTotal   string
	Receipts   map[string]string
	Paid       string
	Credits    string
	Released   string
	OpenDebits string
	Status     string
	Revision   uint64
	Currency   string
	Tenant     string
	Entity     string
}

type adjustSnapshotBalance struct {
	Amount   string
	Offsets  string
	Currency string
}

type adjustSnapshot struct {
	Invoices    map[string]*adjustSnapshotInvoice
	Adjustments map[string]*Adjustment
	Evidence    map[string]string
	Idem        map[string]adjustIdem
	TxIdem      map[string]adjustIdem
	Balances    map[string]*adjustSnapshotBalance
	Voids       map[string]*AdjustVoid
	Allocations map[string]*AdjustAllocation
	Policies    map[string]AdjustPolicy
	CmSeq       map[string]uint64
	DmSeq       map[string]uint64
	AdjSeq      uint64
	AllocSeq    uint64
	VoidSeq     uint64
}

// Export snapshots the registry for durable persistence and restart replay.
// Ledgers persist through their own owner; re-posting after restore
// reconverges through stable event identities.
func (r *AdjustmentRegistry) Export() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap := adjustSnapshot{
		Invoices: map[string]*adjustSnapshotInvoice{}, Adjustments: map[string]*Adjustment{},
		Evidence: map[string]string{}, Idem: map[string]adjustIdem{}, TxIdem: map[string]adjustIdem{},
		Balances: map[string]*adjustSnapshotBalance{}, Voids: map[string]*AdjustVoid{},
		Allocations: map[string]*AdjustAllocation{}, Policies: map[string]AdjustPolicy{},
		CmSeq: map[string]uint64{}, DmSeq: map[string]uint64{},
		AdjSeq: r.adjSeq, AllocSeq: r.allocSeq, VoidSeq: r.voidSeq,
	}
	for id, state := range r.invoices {
		saved := &adjustSnapshotInvoice{
			Head: state.head, Lines: map[string]*adjustSnapshotLine{},
			Total: state.total.String(), TaxTotal: state.taxTotal.String(),
			Receipts: map[string]string{}, Paid: state.paid.String(), Credits: state.credits.String(),
			Released: state.released.String(), OpenDebits: state.openDebits.String(),
			Status: state.status, Revision: state.revision, Currency: state.currency,
			Tenant: state.tenant, Entity: state.entity,
		}
		for lineID, line := range state.lines {
			saved.Lines[lineID] = &adjustSnapshotLine{
				Description: line.description, Qty: billCanonical(line.qty),
				UnitPrice: line.unitPrice.String(), Amount: line.amount.String(), Tax: line.tax.String(),
				CreditedQty: billCanonical(line.creditedQty), CreditedNet: line.creditedNet.String(), CreditedTax: line.creditedTax.String(),
			}
		}
		for receiptID, amount := range state.receipts {
			saved.Receipts[receiptID] = amount.String()
		}
		snap.Invoices[id] = saved
	}
	for id, adjustment := range r.adjustments {
		snap.Adjustments[id] = adjustment
	}
	for key, id := range r.evidence {
		snap.Evidence[key] = id
	}
	for key, idem := range r.idem {
		snap.Idem[key] = idem
	}
	for key, idem := range r.txidem {
		snap.TxIdem[key] = idem
	}
	for key, balance := range r.balances {
		snap.Balances[key] = &adjustSnapshotBalance{Amount: balance.amount.String(), Offsets: balance.offsets.String(), Currency: balance.currency}
	}
	for key, voided := range r.voids {
		snap.Voids[key] = voided
	}
	for key, allocation := range r.allocations {
		snap.Allocations[key] = allocation
	}
	for tenant, policy := range r.policies {
		snap.Policies[tenant] = policy
	}
	for tenant, seq := range r.cmSeq {
		snap.CmSeq[tenant] = seq
	}
	for tenant, seq := range r.dmSeq {
		snap.DmSeq[tenant] = seq
	}
	return json.Marshal(snap)
}

// Import restores a registry previously saved with Export.
func (r *AdjustmentRegistry) Import(data []byte) error {
	var snap adjustSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("%w: snapshot: %v", ErrAdjustValidation, err)
	}
	invoices := map[string]*adjustInvoiceState{}
	for id, saved := range snap.Invoices {
		total, err := billParseMoney(saved.Total)
		if err != nil {
			return fmt.Errorf("%w: snapshot invoice %s: %v", ErrAdjustValidation, id, err)
		}
		taxTotal, err := billParseMoney(saved.TaxTotal)
		if err != nil {
			return fmt.Errorf("%w: snapshot invoice %s: %v", ErrAdjustValidation, id, err)
		}
		paid, err := billParseMoney(saved.Paid)
		if err != nil {
			return fmt.Errorf("%w: snapshot invoice %s: %v", ErrAdjustValidation, id, err)
		}
		credits, err := billParseMoney(saved.Credits)
		if err != nil {
			return fmt.Errorf("%w: snapshot invoice %s: %v", ErrAdjustValidation, id, err)
		}
		released, err := billParseMoney(saved.Released)
		if err != nil {
			return fmt.Errorf("%w: snapshot invoice %s: %v", ErrAdjustValidation, id, err)
		}
		openDebits, err := billParseMoney(saved.OpenDebits)
		if err != nil {
			return fmt.Errorf("%w: snapshot invoice %s: %v", ErrAdjustValidation, id, err)
		}
		lines := map[string]*adjustLineState{}
		for lineID, savedLine := range saved.Lines {
			qty, err := billParseDecimal(savedLine.Qty)
			if err != nil {
				return fmt.Errorf("%w: snapshot line %s: %v", ErrAdjustValidation, lineID, err)
			}
			unit, err := billParseMoney(savedLine.UnitPrice)
			if err != nil {
				return fmt.Errorf("%w: snapshot line %s: %v", ErrAdjustValidation, lineID, err)
			}
			amount, err := billParseMoney(savedLine.Amount)
			if err != nil {
				return fmt.Errorf("%w: snapshot line %s: %v", ErrAdjustValidation, lineID, err)
			}
			tax, err := billParseMoney(savedLine.Tax)
			if err != nil {
				return fmt.Errorf("%w: snapshot line %s: %v", ErrAdjustValidation, lineID, err)
			}
			creditedQty, err := billParseDecimal(savedLine.CreditedQty)
			if err != nil {
				return fmt.Errorf("%w: snapshot line %s: %v", ErrAdjustValidation, lineID, err)
			}
			creditedNet, err := billParseMoney(savedLine.CreditedNet)
			if err != nil {
				return fmt.Errorf("%w: snapshot line %s: %v", ErrAdjustValidation, lineID, err)
			}
			creditedTax, err := billParseMoney(savedLine.CreditedTax)
			if err != nil {
				return fmt.Errorf("%w: snapshot line %s: %v", ErrAdjustValidation, lineID, err)
			}
			lines[lineID] = &adjustLineState{description: savedLine.Description, qty: qty, unitPrice: unit, amount: amount, tax: tax,
				creditedQty: creditedQty, creditedNet: creditedNet, creditedTax: creditedTax}
		}
		receipts := map[string]billMoneyValue{}
		for receiptID, text := range saved.Receipts {
			parsed, err := billParseMoney(text)
			if err != nil {
				return fmt.Errorf("%w: snapshot receipt %s: %v", ErrAdjustValidation, receiptID, err)
			}
			receipts[receiptID] = parsed
		}
		invoices[id] = &adjustInvoiceState{
			tenant: saved.Tenant, entity: saved.Entity, head: saved.Head, lines: lines,
			total: total, taxTotal: taxTotal, receipts: receipts, paid: paid, credits: credits,
			released: released, openDebits: openDebits, status: saved.Status, revision: saved.Revision, currency: saved.Currency,
		}
	}
	balances := map[string]*adjustBalance{}
	for key, saved := range snap.Balances {
		amount, err := billParseMoney(saved.Amount)
		if err != nil {
			return fmt.Errorf("%w: snapshot balance: %v", ErrAdjustValidation, err)
		}
		offsets, err := billParseMoney(saved.Offsets)
		if err != nil {
			return fmt.Errorf("%w: snapshot balance: %v", ErrAdjustValidation, err)
		}
		balances[key] = &adjustBalance{amount: amount, offsets: offsets, currency: saved.Currency}
	}
	if snap.Adjustments == nil {
		snap.Adjustments = map[string]*Adjustment{}
	}
	if snap.Evidence == nil {
		snap.Evidence = map[string]string{}
	}
	if snap.Idem == nil {
		snap.Idem = map[string]adjustIdem{}
	}
	if snap.TxIdem == nil {
		snap.TxIdem = map[string]adjustIdem{}
	}
	if snap.Voids == nil {
		snap.Voids = map[string]*AdjustVoid{}
	}
	if snap.Allocations == nil {
		snap.Allocations = map[string]*AdjustAllocation{}
	}
	if snap.Policies == nil {
		snap.Policies = map[string]AdjustPolicy{}
	}
	if snap.CmSeq == nil {
		snap.CmSeq = map[string]uint64{}
	}
	if snap.DmSeq == nil {
		snap.DmSeq = map[string]uint64{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.invoices = invoices
	r.adjustments = snap.Adjustments
	r.evidence = snap.Evidence
	r.idem = snap.Idem
	r.txidem = snap.TxIdem
	r.balances = balances
	r.voids = snap.Voids
	r.allocations = snap.Allocations
	r.policies = snap.Policies
	r.cmSeq = snap.CmSeq
	r.dmSeq = snap.DmSeq
	r.adjSeq = snap.AdjSeq
	r.allocSeq = snap.AllocSeq
	r.voidSeq = snap.VoidSeq
	return nil
}

// InvoiceStoreAdjustPort adapts the invoice owner's store to the adjustment
// registry's port. Reads map issued invoices to frozen snapshots; voids go
// through the owner's Void command with the caller's key and clock.
type InvoiceStoreAdjustPort struct {
	Store *InvoiceStore
	Caps  ActorCaps
}

// GetInvoice implements AdjustInvoicePort.
func (p InvoiceStoreAdjustPort) GetInvoice(op AdjustOp, invoiceID string) (AdjustInvoice, error) {
	if p.Store == nil {
		return AdjustInvoice{}, ErrAdjustDenied
	}
	invoice, err := p.Store.Get(op.TenantID, strings.TrimSpace(invoiceID))
	if err != nil {
		return AdjustInvoice{}, ErrAdjustDenied
	}
	return adjustSnapshotOf(invoice), nil
}

// VoidInvoiceDocument implements AdjustInvoicePort.
func (p InvoiceStoreAdjustPort) VoidInvoiceDocument(op AdjustOp, invoiceID string, expected uint64, reason, key, at string) (AdjustInvoice, error) {
	if p.Store == nil {
		return AdjustInvoice{}, ErrAdjustDenied
	}
	invoice, _, err := p.Store.Void(op.TenantID, op.ActorID, p.Caps, strings.TrimSpace(invoiceID), expected, key, at, reason)
	if err != nil {
		return AdjustInvoice{}, err
	}
	return adjustSnapshotOf(invoice), nil
}

func adjustSnapshotOf(invoice CustomerInvoice) AdjustInvoice {
	snap := AdjustInvoice{
		InvoiceID: invoice.ID, CustomerID: invoice.CustomerID, Currency: invoice.Currency,
		Number: invoice.InvoiceNumber, Status: string(invoice.Status), Revision: invoice.Revision,
		Digest: invoice.Digest, IssuedAt: invoice.IssueDate,
		Total:    invoice.Total.Amount().String() + " " + invoice.Currency,
		TaxTotal: "0.00 " + invoice.Currency,
	}
	for _, line := range invoice.Lines {
		snap.Lines = append(snap.Lines, AdjustInvoiceLine{
			LineID: line.ID, Description: line.Description, Qty: line.Quantity.String(),
			UnitPrice: line.UnitPrice.Amount().String() + " " + invoice.Currency,
			Amount:    line.Amount.Amount().String() + " " + invoice.Currency,
			TaxAmount: "0.00 " + invoice.Currency,
		})
	}
	return snap
}

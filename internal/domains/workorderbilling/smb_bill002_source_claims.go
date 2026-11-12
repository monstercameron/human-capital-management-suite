// Package workorderbilling binds approved work to billable consumption.
//
// SMB-BILL-002 adds the SourceClaimRegistry: durable billable-source claims
// that stop the same approved time, material, subcontract, milestone or
// expense from being invoiced twice. Sources register as immutable
// owner/root/revision records carrying their approval evidence; claims
// consume exact quantities or amounts inside one source-version family with
// tenant-scoped idempotency. Draft holds carry an explicit lease and can
// release or expire; issued allocations never expire and leave only through
// an authorized reversal with an adjustment receipt. Corrections supersede
// the family head without resetting already-claimed availability and raise
// an adjustment requirement when issued consumption exceeds the corrected
// approval. Employee cost and pay details never enter a claim: they live in
// restricted cost observations that customer billing cannot read.
package workorderbilling

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrBillSourceValidation rejects malformed requests and sources that
	// lack approval proof, a registered rate or an exact billable conversion.
	ErrBillSourceValidation = errors.New("workorderbilling: billable source request is invalid")
	// ErrBillSourceDenied is the single neutral response for denied roles,
	// foreign tenants and guessed identifiers. It never distinguishes which.
	ErrBillSourceDenied = errors.New("workorderbilling: billable source not found or not permitted")
	// ErrBillSourceConflict reports an idempotency key reused with a changed
	// canonical payload, or a source revision re-registered with changes.
	ErrBillSourceConflict = errors.New("workorderbilling: billable source idempotency conflict")
	// ErrBillSourceStale reports consumption pinned to a superseded revision.
	ErrBillSourceStale = errors.New("workorderbilling: billable source revision is stale")
	// ErrBillSourceShortfall reports a claim above the exact family remainder.
	ErrBillSourceShortfall = errors.New("workorderbilling: billable source remainder is insufficient")
	// ErrBillSourceState reports a claim transition the current state forbids,
	// such as issuing an expired hold or silently releasing an issued claim.
	ErrBillSourceState = errors.New("workorderbilling: billable claim state forbids this transition")
)

// BillOp carries the authorized actor for one registry call. BillingAllowed
// gates every read that discloses billable data and every mutation;
// CostVisible gates restricted cost observations only.
type BillOp struct {
	TenantID       string
	LegalEntityID  string
	ActorID        string
	BillingAllowed bool
	CostVisible    bool
}

func (op BillOp) validate() error {
	if strings.TrimSpace(op.TenantID) == "" || strings.TrimSpace(op.LegalEntityID) == "" || strings.TrimSpace(op.ActorID) == "" {
		return fmt.Errorf("%w: operation needs tenant, legal entity and actor", ErrBillSourceValidation)
	}
	return nil
}

// BillClaimKind selects a leased draft hold or a permanent issued allocation.
type BillClaimKind string

const (
	BillClaimHold  BillClaimKind = "draft-hold"
	BillClaimIssue BillClaimKind = "issued"
)

// Claim lifecycle states carried on BillClaim.Status and BillReceipt.Status.
const (
	BillStatusHeld     = "held"
	BillStatusIssued   = "issued"
	BillStatusReleased = "released"
	BillStatusExpired  = "expired"
	BillStatusReversed = "reversed"
	BillStatusExcluded = "excluded"
)

// BillRateInput registers one sell rate or multiplier the registry may price.
type BillRateInput struct {
	Ref      string
	Currency string
	Amount   string
	Unit     string
}

// BillSourceInput registers one immutable approved source revision.
type BillSourceInput struct {
	Owner          string
	RootID         string
	Revision       uint64
	WorkOrderID    string
	JobRef         string
	Kind           BillSourceKind
	UnitDimension  string
	Unit           string
	ApprovedQty    string
	ApprovedAmount string
	RateRef        string
	PricingMode    PricingMode
	BillableUnit   string
	Category       string
	MilestoneRef   string
	EvidenceRef    string
	ApprovedBy     string
	ApprovedAt     string
	Description    string
	ServiceDate    string
}

// BillSource is one immutable approved source revision.
type BillSource struct {
	FamilyID       string
	TenantID       string
	LegalEntityID  string
	Owner          string
	RootID         string
	Revision       uint64
	WorkOrderID    string
	JobRef         string
	Kind           BillSourceKind
	UnitDimension  string
	Unit           string
	ApprovedQty    string
	ApprovedAmount string
	RateRef        string
	PricingMode    PricingMode
	BillableUnit   string
	Category       string
	MilestoneRef   string
	EvidenceRef    string
	ApprovedBy     string
	ApprovedAt     string
	Description    string
	ServiceDate    string
	Superseded     bool
	RegisteredAt   string
}

// BillClaimInput consumes part of one family head revision.
type BillClaimInput struct {
	FamilyID      string
	Revision      uint64
	Qty           string
	Amount        string
	PolicyID      string
	PolicyVersion string
	Kind          BillClaimKind
	LeaseMinutes  int64
	DraftRef      string
	InvoiceRef    string
}

// BillClaim is one durable consumption record.
type BillClaim struct {
	ClaimID        string
	TenantID       string
	LegalEntityID  string
	FamilyID       string
	Revision       uint64
	Qty            string
	Amount         string
	MoneyAmount    string
	PolicyID       string
	PolicyVersion  string
	Kind           BillClaimKind
	Status         string
	LeaseExpiry    string
	DraftRef       string
	InvoiceRef     string
	ReversalRef    string
	ReversalReason string
	Key            string
	CreatedAt      string
}

// BillReceipt answers one claim, hold commit, release or reversal with the
// immutable claim identity plus the live family remainder.
type BillReceipt struct {
	ClaimID         string
	FamilyID        string
	Revision        uint64
	ClaimedQty      string
	ClaimedAmount   string
	Amount          string
	RemainingQty    string
	RemainingAmount string
	Status          string
	LeaseExpiry     string
	Key             string
}

// BillCorrection answers a family correction with its adjustment position.
type BillCorrection struct {
	Source               BillSource
	AdjustmentRequired   bool
	OverIssuedQty        string
	OverIssuedAmount     string
	CarriedClaimedQty    string
	CarriedClaimedAmount string
}

// BillExpenseInput registers one BILL010-shaped expense and recharge decision.
type BillExpenseInput struct {
	Owner               string
	RootID              string
	WorkOrderID         string
	JobRef              string
	CostAmount          string
	RechargeRate        string
	RechargeEligible    bool
	ReimbursementAmount string
	EvidenceRef         string
	RevisionBinding     string
	ApprovedBy          string
	ApprovedAt          string
	Description         string
	ServiceDate         string
	Category            string
}

// BillExpenseEligibility is one registered expense awaiting a customer claim.
type BillExpenseEligibility struct {
	ExpenseID           string
	TenantID            string
	LegalEntityID       string
	Owner               string
	RootID              string
	WorkOrderID         string
	JobRef              string
	CostAmount          string
	RechargeRate        string
	RechargeEligible    bool
	ReimbursementAmount string
	CustomerAmount      string
	EvidenceRef         string
	RevisionBinding     string
	ApprovedBy          string
	ApprovedAt          string
	Description         string
	ServiceDate         string
	Category            string
	Status              string
	ClaimedAmount       string
	RegisteredAt        string
}

// BillCostInput records one restricted cost observation. Cost observations
// are never billable and never reachable without the cost grant.
type BillCostInput struct {
	Owner       string
	RootID      string
	WorkOrderID string
	Detail      string
	Amount      string
	EvidenceRef string
}

// BillCostObservation is one stored cost observation.
type BillCostObservation struct {
	CostID        string
	TenantID      string
	LegalEntityID string
	Owner         string
	RootID        string
	WorkOrderID   string
	Detail        string
	Amount        string
	EvidenceRef   string
	RegisteredAt  string
}

// BillEvent is one audit entry on a source family.
type BillEvent struct {
	Kind   string
	Actor  string
	At     string
	Detail string
}

type billRate struct {
	currency string
	amount   values.Decimal
	unit     string
}

type billFamily struct {
	id          string
	tenant      string
	entity      string
	owner       string
	root        string
	workOrder   string
	jobRef      string
	kind        BillSourceKind
	dim         string
	unit        string
	rateRef     string
	mode        PricingMode
	billUnit    string
	category    string
	milestone   string
	head        uint64
	revs        map[uint64]*BillSource
	approvedQty values.Decimal
	approvedAmt billMoneyValue
	claims      []*BillClaim
	events      []BillEvent
}

type billMoneyValue struct {
	amount   values.Decimal
	currency string
}

func (m billMoneyValue) valid() bool { return m.currency != "" }

func (m billMoneyValue) String() string {
	if !m.valid() {
		return ""
	}
	return m.amount.String() + " " + m.currency
}

type billIdem struct {
	fingerprint string
	claimID     string
	tenant      string
	entity      string
	receipt     BillReceipt
}

type billExpense struct {
	record      BillExpenseEligibility
	cost        billMoneyValue
	fingerprint string
	events      []BillEvent
}

// SourceClaimRegistry guards billable-source consumption for every tenant.
// All money and quantity math is exact; anything that cannot stay exact is
// refused with a reason instead of rounded.
type SourceClaimRegistry struct {
	mu          sync.Mutex
	clock       func() time.Time
	rates       map[string]billRate
	families    map[string]*billFamily
	claims      map[string]*BillClaim
	idem        map[string]billIdem
	expenses    map[string]*billExpense
	expenseIdem map[string]billExpenseIdem
	costs       map[string]*BillCostObservation
	seq         uint64
}

// NewSourceClaimRegistry returns an empty registry using clock for lease and
// audit timestamps.
func NewSourceClaimRegistry(clock func() time.Time) *SourceClaimRegistry {
	if clock == nil {
		clock = time.Now
	}
	return &SourceClaimRegistry{
		clock:       clock,
		rates:       map[string]billRate{},
		families:    map[string]*billFamily{},
		claims:      map[string]*BillClaim{},
		idem:        map[string]billIdem{},
		expenses:    map[string]*billExpense{},
		expenseIdem: map[string]billExpenseIdem{},
		costs:       map[string]*BillCostObservation{},
	}
}

func billScaleOf(text string) int32 {
	if i := strings.IndexByte(text, '.'); i >= 0 {
		return int32(len(text) - i - 1)
	}
	return 0
}

func billParseDecimal(text string) (values.Decimal, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return values.Decimal{}, fmt.Errorf("decimal is required")
	}
	return values.NewDecimal(trimmed, billScaleOf(trimmed), values.RoundingExactRequired)
}

// billStrip drops redundant fraction zeros so canonical quantities read
// "1.5" and "2" instead of "1.5000" and "2.0000".
func billStrip(text string) string {
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

func billCanonical(d values.Decimal) string { return billStrip(d.String()) }

func billParseMoney(text string) (billMoneyValue, error) {
	parts := strings.SplitN(strings.TrimSpace(text), " ", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return billMoneyValue{}, fmt.Errorf("money %q must read like 50.00 USD", text)
	}
	amount, err := billParseDecimal(parts[0])
	if err != nil {
		return billMoneyValue{}, fmt.Errorf("money %q: %v", text, err)
	}
	return billMoneyValue{amount: amount, currency: strings.TrimSpace(parts[1])}, nil
}

// billCents quantizes an exact decimal to billable cents. Anything needing
// more than two fraction digits is refused instead of rounded.
func billCents(d values.Decimal, currency string) (billMoneyValue, error) {
	canonical := billStrip(d.String())
	if billScaleOf(canonical) > 2 {
		return billMoneyValue{}, fmt.Errorf("amount %s needs more than cents", canonical)
	}
	quantized, err := values.NewDecimal(canonical, 2, values.RoundingExactRequired)
	if err != nil {
		return billMoneyValue{}, err
	}
	return billMoneyValue{amount: quantized, currency: currency}, nil
}

func billAlign(a, b values.Decimal) (values.Decimal, values.Decimal, error) {
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

func billAdd(a, b values.Decimal) (values.Decimal, error) {
	qa, qb, err := billAlign(a, b)
	if err != nil {
		return values.Decimal{}, err
	}
	return qa.Add(qb)
}

func billSub(a, b values.Decimal) (values.Decimal, error) {
	qa, qb, err := billAlign(a, b)
	if err != nil {
		return values.Decimal{}, err
	}
	return qa.Sub(qb)
}

// billConvert moves a native quantity into the billable unit with exact math.
// Anything without an exact conversion fails so callers surface the reason.
func billConvert(qty values.Decimal, from, to string) (values.Decimal, error) {
	if from == to {
		return qty, nil
	}
	switch from + "\x00" + to {
	case "minute\x00hour":
		sixty, err := values.NewDecimal("60", 0, values.RoundingExactRequired)
		if err != nil {
			return values.Decimal{}, err
		}
		return qty.Div(sixty, 4, values.RoundingExactRequired)
	case "hour\x00minute":
		sixty, err := values.NewDecimal("60", 0, values.RoundingExactRequired)
		if err != nil {
			return values.Decimal{}, err
		}
		return qty.Mul(sixty, qty.Scale(), values.RoundingExactRequired)
	default:
		return values.Decimal{}, fmt.Errorf("no exact billable conversion from %s to %s", from, to)
	}
}

func billMoneyMul(m billMoneyValue, factor values.Decimal) (billMoneyValue, error) {
	product, err := m.amount.Mul(factor, m.amount.Scale()+factor.Scale(), values.RoundingExactRequired)
	if err != nil {
		return billMoneyValue{}, err
	}
	return billCents(product, m.currency)
}

func billFamilyID(owner, root string) string { return owner + ":" + root }

func billIdemKey(tenant, key string) string { return tenant + "\x00" + key }

func billNow(r *SourceClaimRegistry) time.Time {
	if r.clock == nil {
		return time.Now().UTC()
	}
	return r.clock().UTC()
}

func billStamp(r *SourceClaimRegistry) string { return billNow(r).Format(time.RFC3339) }

func billUnitOK(dim, unit string) bool {
	switch dim {
	case "time":
		return unit == "minute" || unit == "hour"
	case "quantity":
		return unit == "each"
	case "money":
		return unit == ""
	default:
		return false
	}
}

func billValidKind(kind BillSourceKind) bool {
	switch kind {
	case BillKindTime, BillKindQuantity, BillKindMaterial, BillKindSubcontract, BillKindMilestone, BillKindExpense:
		return true
	default:
		return false
	}
}

func billValidMode(mode PricingMode) bool {
	return mode == PricingUnitPrice || mode == PricingTimeMaterial || mode == PricingFixedMilestone
}

// billApprovedAt parses an approval timestamp and refuses zero or
// non-credible values instead of substituting freshness (IMPL-GAP-003).
func billApprovedAt(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("approval proof is required")
	}
	at, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return fmt.Errorf("approval timestamp: %v", err)
	}
	if at.IsZero() || at.Year() < 2000 {
		return fmt.Errorf("approval timestamp is not credible")
	}
	return nil
}

func billServiceDate(raw string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	day, err := time.Parse("2006-01-02", trimmed)
	if err != nil || day.Format("2006-01-02") != trimmed {
		return time.Time{}, fmt.Errorf("service date must read like 2026-10-21")
	}
	return day, nil
}

// RegisterRate stores one sell rate or per-unit price the registry may use.
// Re-registering a ref with a changed payload is a conflict.
func (r *SourceClaimRegistry) RegisterRate(op BillOp, input BillRateInput) error {
	if err := op.validate(); err != nil {
		return err
	}
	if !op.BillingAllowed {
		return ErrBillSourceDenied
	}
	ref := strings.TrimSpace(input.Ref)
	currency := strings.TrimSpace(input.Currency)
	unit := strings.TrimSpace(input.Unit)
	amount, err := billParseDecimal(input.Amount)
	if err != nil {
		return fmt.Errorf("%w: rate amount: %v", ErrBillSourceValidation, err)
	}
	if ref == "" || currency == "" || unit == "" {
		return fmt.Errorf("%w: rate needs ref, currency and unit", ErrBillSourceValidation)
	}
	if amount.Sign() < 0 {
		return fmt.Errorf("%w: rate amount cannot be negative", ErrBillSourceValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, ok := r.rates[ref]; ok {
		if prior.currency != currency || prior.unit != unit || !prior.amount.Equal(amount) {
			return fmt.Errorf("%w: rate %s is already registered", ErrBillSourceConflict, ref)
		}
		return nil
	}
	r.rates[ref] = billRate{currency: currency, amount: amount, unit: unit}
	return nil
}

func billSourceFingerprint(input BillSourceInput) string {
	parts := []string{
		input.Owner, input.RootID, fmt.Sprintf("%d", input.Revision),
		input.WorkOrderID, input.JobRef, string(input.Kind), input.UnitDimension, input.Unit,
		input.ApprovedQty, input.ApprovedAmount, input.RateRef, string(input.PricingMode),
		input.BillableUnit, input.Category, input.MilestoneRef, input.EvidenceRef,
		input.ApprovedBy, input.ApprovedAt, input.Description, input.ServiceDate,
	}
	return strings.Join(parts, "\x00")
}

func (r *SourceClaimRegistry) validateSourceInput(input BillSourceInput) (values.Decimal, billMoneyValue, error) {
	if strings.TrimSpace(input.Owner) == "" || strings.TrimSpace(input.RootID) == "" {
		return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: source needs owner and root", ErrBillSourceValidation)
	}
	if strings.TrimSpace(input.WorkOrderID) == "" {
		return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: source needs a work order", ErrBillSourceValidation)
	}
	if input.Revision == 0 {
		return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: source revision starts at 1", ErrBillSourceValidation)
	}
	if !billValidKind(input.Kind) {
		return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: unknown source kind %q", ErrBillSourceValidation, input.Kind)
	}
	if input.UnitDimension != "time" && input.UnitDimension != "quantity" && input.UnitDimension != "money" {
		return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: unknown unit dimension %q", ErrBillSourceValidation, input.UnitDimension)
	}
	if !billUnitOK(input.UnitDimension, strings.TrimSpace(input.Unit)) {
		return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: unit %q does not fit dimension %q", ErrBillSourceValidation, input.Unit, input.UnitDimension)
	}
	if !billValidMode(input.PricingMode) {
		return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: unknown pricing mode %q", ErrBillSourceValidation, input.PricingMode)
	}
	if input.PricingMode == PricingUnitPrice && input.UnitDimension == "money" {
		return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: unit price needs a measured quantity, not money", ErrBillSourceValidation)
	}
	if input.PricingMode == PricingTimeMaterial && input.UnitDimension != "money" {
		return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: time/material needs a money cost basis", ErrBillSourceValidation)
	}
	if strings.TrimSpace(input.RateRef) == "" {
		return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: source is missing its rate", ErrBillSourceValidation)
	}
	if _, ok := r.rates[strings.TrimSpace(input.RateRef)]; !ok {
		return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: rate %q is not registered", ErrBillSourceValidation, input.RateRef)
	}
	if input.PricingMode == PricingUnitPrice && strings.TrimSpace(input.BillableUnit) == "" {
		return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: unit price needs a billable unit", ErrBillSourceValidation)
	}
	if input.PricingMode == PricingTimeMaterial && strings.TrimSpace(input.Category) == "" {
		return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: time/material needs a multiplier category", ErrBillSourceValidation)
	}
	if input.Kind == BillKindMilestone && strings.TrimSpace(input.MilestoneRef) == "" {
		return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: milestone source needs a milestone ref", ErrBillSourceValidation)
	}
	if strings.TrimSpace(input.EvidenceRef) == "" || strings.TrimSpace(input.ApprovedBy) == "" {
		return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: source approval is unknown", ErrBillSourceValidation)
	}
	if err := billApprovedAt(input.ApprovedAt); err != nil {
		return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: %v", ErrBillSourceValidation, err)
	}
	if _, err := billServiceDate(input.ServiceDate); err != nil {
		return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: %v", ErrBillSourceValidation, err)
	}
	if strings.TrimSpace(input.Description) == "" {
		return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: source needs a customer-safe description", ErrBillSourceValidation)
	}
	if input.UnitDimension == "money" {
		if strings.TrimSpace(input.ApprovedQty) != "" {
			return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: money source takes an amount, not a quantity", ErrBillSourceValidation)
		}
		amount, err := billParseMoney(input.ApprovedAmount)
		if err != nil {
			return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: %v", ErrBillSourceValidation, err)
		}
		if amount.amount.Sign() <= 0 {
			return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: approved amount must be positive", ErrBillSourceValidation)
		}
		return values.Decimal{}, amount, nil
	}
	if strings.TrimSpace(input.ApprovedAmount) != "" {
		return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: measured source takes a quantity, not an amount", ErrBillSourceValidation)
	}
	qty, err := billParseDecimal(input.ApprovedQty)
	if err != nil {
		return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: approved quantity: %v", ErrBillSourceValidation, err)
	}
	if qty.Sign() <= 0 {
		return values.Decimal{}, billMoneyValue{}, fmt.Errorf("%w: approved quantity must be positive", ErrBillSourceValidation)
	}
	return qty, billMoneyValue{}, nil
}

// RegisterSource stores one immutable approved source revision. The first
// registration opens the family; re-registering the head revision with the
// same payload returns the same source, with a changed payload conflicts,
// and below the head is stale. New revisions must arrive via CorrectSource
// so lineage stays inside the family.
func (r *SourceClaimRegistry) RegisterSource(op BillOp, input BillSourceInput) (BillSource, error) {
	if err := op.validate(); err != nil {
		return BillSource{}, err
	}
	if !op.BillingAllowed {
		return BillSource{}, ErrBillSourceDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	qty, amount, err := r.validateSourceInput(input)
	if err != nil {
		return BillSource{}, err
	}
	id := billFamilyID(strings.TrimSpace(input.Owner), strings.TrimSpace(input.RootID))
	family, ok := r.families[id]
	if !ok {
		family = &billFamily{
			id: id, tenant: op.TenantID, entity: op.LegalEntityID,
			owner: strings.TrimSpace(input.Owner), root: strings.TrimSpace(input.RootID),
			workOrder: strings.TrimSpace(input.WorkOrderID), jobRef: strings.TrimSpace(input.JobRef),
			kind: input.Kind, dim: input.UnitDimension, unit: strings.TrimSpace(input.Unit),
			rateRef: strings.TrimSpace(input.RateRef), mode: input.PricingMode,
			billUnit: strings.TrimSpace(input.BillableUnit), category: strings.TrimSpace(input.Category),
			milestone: strings.TrimSpace(input.MilestoneRef),
			revs:      map[uint64]*BillSource{},
		}
		r.families[id] = family
	}
	if family.tenant != op.TenantID || family.entity != op.LegalEntityID {
		return BillSource{}, ErrBillSourceDenied
	}
	if existing, ok := family.revs[input.Revision]; ok {
		if billSourceFingerprint(input) != existing.fingerprint() {
			return BillSource{}, fmt.Errorf("%w: revision %d of %s is already registered", ErrBillSourceConflict, input.Revision, id)
		}
		return *existing, nil
	}
	if input.Revision < family.head || (family.head != 0 && input.Revision == family.head) {
		return BillSource{}, fmt.Errorf("%w: revision %d of %s is behind head %d", ErrBillSourceStale, input.Revision, id, family.head)
	}
	if family.head != 0 && input.Revision > family.head {
		return BillSource{}, fmt.Errorf("%w: register revision %d above head %d through a correction", ErrBillSourceValidation, input.Revision, family.head)
	}
	source := &BillSource{
		FamilyID: id, TenantID: op.TenantID, LegalEntityID: op.LegalEntityID,
		Owner: family.owner, RootID: family.root, Revision: input.Revision,
		WorkOrderID: family.workOrder, JobRef: family.jobRef,
		Kind: family.kind, UnitDimension: family.dim, Unit: family.unit,
		ApprovedQty: billCanonical(qty), ApprovedAmount: amount.String(),
		RateRef: family.rateRef, PricingMode: family.mode, BillableUnit: family.billUnit,
		Category: family.category, MilestoneRef: family.milestone,
		EvidenceRef: strings.TrimSpace(input.EvidenceRef), ApprovedBy: strings.TrimSpace(input.ApprovedBy),
		ApprovedAt:  strings.TrimSpace(input.ApprovedAt),
		Description: strings.TrimSpace(input.Description), ServiceDate: strings.TrimSpace(input.ServiceDate),
		RegisteredAt: billStamp(r),
	}
	family.revs[input.Revision] = source
	family.head = input.Revision
	family.approvedQty = qty
	family.approvedAmt = amount
	family.events = append(family.events, BillEvent{Kind: "SOURCE_REGISTERED", Actor: op.ActorID, At: source.RegisteredAt, Detail: fmt.Sprintf("rev %d approved %s", input.Revision, source.ApprovedQty+source.ApprovedAmount)})
	return *source, nil
}

func (s *BillSource) fingerprint() string {
	return billSourceFingerprint(BillSourceInput{
		Owner: s.Owner, RootID: s.RootID, Revision: s.Revision,
		WorkOrderID: s.WorkOrderID, JobRef: s.JobRef, Kind: s.Kind,
		UnitDimension: s.UnitDimension, Unit: s.Unit,
		ApprovedQty: s.ApprovedQty, ApprovedAmount: s.ApprovedAmount, RateRef: s.RateRef,
		PricingMode: s.PricingMode, BillableUnit: s.BillableUnit, Category: s.Category,
		MilestoneRef: s.MilestoneRef, EvidenceRef: s.EvidenceRef, ApprovedBy: s.ApprovedBy,
		ApprovedAt: s.ApprovedAt, Description: s.Description, ServiceDate: s.ServiceDate,
	})
}

// CorrectSource supersedes the family head with a corrected revision. Claimed
// consumption carries forward inside the family, so a revised source cannot
// reset its already-billed availability; when issued consumption exceeds the
// corrected approval the correction raises an adjustment requirement while
// every original receipt stays unchanged.
func (r *SourceClaimRegistry) CorrectSource(op BillOp, input BillSourceInput) (BillCorrection, error) {
	if err := op.validate(); err != nil {
		return BillCorrection{}, err
	}
	if !op.BillingAllowed {
		return BillCorrection{}, ErrBillSourceDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	qty, amount, err := r.validateSourceInput(input)
	if err != nil {
		return BillCorrection{}, err
	}
	id := billFamilyID(strings.TrimSpace(input.Owner), strings.TrimSpace(input.RootID))
	family, ok := r.families[id]
	if !ok || family.tenant != op.TenantID || family.entity != op.LegalEntityID {
		return BillCorrection{}, ErrBillSourceDenied
	}
	if input.Revision <= family.head {
		if existing, ok := family.revs[input.Revision]; ok && billSourceFingerprint(input) == existing.fingerprint() {
			carried, carriedAmt := r.activeTotals(family)
			return BillCorrection{Source: *existing, CarriedClaimedQty: billCanonical(carried), CarriedClaimedAmount: carriedAmt.String()}, nil
		}
		return BillCorrection{}, fmt.Errorf("%w: revision %d of %s is not above head %d", ErrBillSourceStale, input.Revision, id, family.head)
	}
	if input.Kind != family.kind || input.UnitDimension != family.dim || strings.TrimSpace(input.Unit) != family.unit ||
		input.PricingMode != family.mode || strings.TrimSpace(input.BillableUnit) != family.billUnit ||
		strings.TrimSpace(input.Category) != family.category || strings.TrimSpace(input.WorkOrderID) != family.workOrder {
		return BillCorrection{}, fmt.Errorf("%w: correction cannot change source identity, units or pricing shape", ErrBillSourceValidation)
	}
	if head, ok := family.revs[family.head]; ok {
		head.Superseded = true
	}
	source := &BillSource{
		FamilyID: id, TenantID: op.TenantID, LegalEntityID: op.LegalEntityID,
		Owner: family.owner, RootID: family.root, Revision: input.Revision,
		WorkOrderID: family.workOrder, JobRef: strings.TrimSpace(input.JobRef),
		Kind: family.kind, UnitDimension: family.dim, Unit: family.unit,
		ApprovedQty: billCanonical(qty), ApprovedAmount: amount.String(),
		RateRef: strings.TrimSpace(input.RateRef), PricingMode: family.mode, BillableUnit: family.billUnit,
		Category: family.category, MilestoneRef: family.milestone,
		EvidenceRef: strings.TrimSpace(input.EvidenceRef), ApprovedBy: strings.TrimSpace(input.ApprovedBy),
		ApprovedAt:  strings.TrimSpace(input.ApprovedAt),
		Description: strings.TrimSpace(input.Description), ServiceDate: strings.TrimSpace(input.ServiceDate),
		RegisteredAt: billStamp(r),
	}
	family.revs[input.Revision] = source
	family.head = input.Revision
	family.rateRef = source.RateRef
	family.approvedQty = qty
	family.approvedAmt = amount
	correction := BillCorrection{Source: *source}
	issued, issuedAmt := r.issuedTotals(family)
	carried, carriedAmt := r.activeTotals(family)
	correction.CarriedClaimedQty = billCanonical(carried)
	correction.CarriedClaimedAmount = carriedAmt.String()
	if family.dim == "money" {
		if issuedAmt.valid() && issuedAmt.amount.Cmp(amount.amount) > 0 {
			over, err := billSub(issuedAmt.amount, amount.amount)
			if err != nil {
				return BillCorrection{}, err
			}
			overMoney, err := billCents(over, amount.currency)
			if err != nil {
				return BillCorrection{}, err
			}
			correction.AdjustmentRequired = true
			correction.OverIssuedAmount = overMoney.String()
		}
	} else if issued.Cmp(qty) > 0 {
		over, err := billSub(issued, qty)
		if err != nil {
			return BillCorrection{}, err
		}
		correction.AdjustmentRequired = true
		correction.OverIssuedQty = billCanonical(over)
	}
	detail := fmt.Sprintf("rev %d carries %s", input.Revision, correction.CarriedClaimedQty+correction.CarriedClaimedAmount)
	if correction.AdjustmentRequired {
		detail += fmt.Sprintf(", over-issued %s", correction.OverIssuedQty+correction.OverIssuedAmount)
	}
	family.events = append(family.events, BillEvent{Kind: "SOURCE_CORRECTED", Actor: op.ActorID, At: source.RegisteredAt, Detail: detail})
	return correction, nil
}

// activeTotals sums issued claims plus live (unexpired) holds.
func (r *SourceClaimRegistry) activeTotals(family *billFamily) (values.Decimal, billMoneyValue) {
	now := billNow(r)
	zero, _ := values.NewDecimal("0", 0, values.RoundingExactRequired)
	claimed := zero
	var claimedAmt billMoneyValue
	for _, claim := range family.claims {
		if claim.Status == BillStatusIssued {
			claimed, claimedAmt = billAccumulate(claimed, claimedAmt, claim, family)
		} else if claim.Status == BillStatusHeld && !billHoldExpired(claim, now) {
			claimed, claimedAmt = billAccumulate(claimed, claimedAmt, claim, family)
		}
	}
	return claimed, claimedAmt
}

func (r *SourceClaimRegistry) issuedTotals(family *billFamily) (values.Decimal, billMoneyValue) {
	zero, _ := values.NewDecimal("0", 0, values.RoundingExactRequired)
	issued := zero
	var issuedAmt billMoneyValue
	for _, claim := range family.claims {
		if claim.Status == BillStatusIssued {
			issued, issuedAmt = billAccumulate(issued, issuedAmt, claim, family)
		}
	}
	return issued, issuedAmt
}

func billAccumulate(qty values.Decimal, amt billMoneyValue, claim *BillClaim, family *billFamily) (values.Decimal, billMoneyValue) {
	if family.dim == "money" {
		parsed, err := billParseMoney(claim.Amount)
		if err != nil {
			return qty, amt
		}
		if !amt.valid() {
			return qty, parsed
		}
		if parsed.currency != amt.currency {
			return qty, amt
		}
		sum, err := billAdd(amt.amount, parsed.amount)
		if err != nil {
			return qty, amt
		}
		return qty, billMoneyValue{amount: sum, currency: amt.currency}
	}
	parsed, err := billParseDecimal(claim.Qty)
	if err != nil {
		return qty, amt
	}
	sum, err := billAdd(qty, parsed)
	if err != nil {
		return qty, amt
	}
	return sum, amt
}

func billHoldExpired(claim *BillClaim, now time.Time) bool {
	if claim.LeaseExpiry == "" {
		return false
	}
	expiry, err := time.Parse(time.RFC3339, claim.LeaseExpiry)
	if err != nil {
		return true
	}
	return !expiry.After(now)
}

// QuerySources lists the customer-safe family rows for one work order inside
// the inclusive service-date window. Unknown orders read as empty; denied
// roles and foreign scopes share the neutral denied response.
func (r *SourceClaimRegistry) QuerySources(op BillOp, workOrderID string, from, to time.Time) ([]BillSourceView, error) {
	if err := op.validate(); err != nil {
		return nil, err
	}
	if !op.BillingAllowed {
		return nil, ErrBillSourceDenied
	}
	workOrderID = strings.TrimSpace(workOrderID)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, family := range r.families {
		if family.workOrder == workOrderID && (family.tenant != op.TenantID || family.entity != op.LegalEntityID) {
			return nil, ErrBillSourceDenied
		}
	}
	for _, expense := range r.expenses {
		if expense.record.WorkOrderID == workOrderID && (expense.record.TenantID != op.TenantID || expense.record.LegalEntityID != op.LegalEntityID) {
			return nil, ErrBillSourceDenied
		}
	}
	views := []BillSourceView{}
	for _, family := range r.families {
		if family.tenant != op.TenantID || family.entity != op.LegalEntityID || family.workOrder != workOrderID {
			continue
		}
		head, ok := family.revs[family.head]
		if !ok {
			continue
		}
		day, err := billServiceDate(head.ServiceDate)
		if err != nil {
			continue
		}
		if day.Before(billDay(from)) || day.After(billDay(to)) {
			continue
		}
		views = append(views, r.familyView(family, head))
	}
	for _, expense := range r.expenses {
		record := expense.record
		if record.TenantID != op.TenantID || record.LegalEntityID != op.LegalEntityID || record.WorkOrderID != workOrderID {
			continue
		}
		day, err := billServiceDate(record.ServiceDate)
		if err != nil {
			continue
		}
		if day.Before(billDay(from)) || day.After(billDay(to)) {
			continue
		}
		views = append(views, r.expenseView(expense))
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].ServiceDate != views[j].ServiceDate {
			return views[i].ServiceDate < views[j].ServiceDate
		}
		return views[i].FamilyID < views[j].FamilyID
	})
	return views, nil
}

func billDay(t time.Time) time.Time {
	utc := t.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

func (r *SourceClaimRegistry) familyView(family *billFamily, head *BillSource) BillSourceView {
	view := BillSourceView{
		FamilyID: head.FamilyID, Revision: head.Revision, Description: head.Description,
		ServiceDate: head.ServiceDate, Kind: head.Kind, Unit: head.Unit,
		UnitDimension: head.UnitDimension, ApprovedQty: head.ApprovedQty,
		ApprovedAmount: head.ApprovedAmount, BillableUnit: head.BillableUnit,
		RateRef: head.RateRef, PricingMode: head.PricingMode, Category: head.Category,
		MilestoneRef: head.MilestoneRef, Eligible: true,
	}
	claimed, claimedAmt := r.activeTotals(family)
	if family.dim == "money" {
		view.ClaimedAmount = claimedAmt.String()
		if view.ClaimedAmount == "" {
			empty, _ := billCents(mustBillZero(), family.approvedAmt.currency)
			view.ClaimedAmount = empty.String()
		}
		remaining, err := billSub(family.approvedAmt.amount, mustBillAmount(claimedAmt, family.approvedAmt.currency))
		if err != nil {
			view.Eligible = false
			view.Reasons = append(view.Reasons, "remaining amount is not exact")
			return view
		}
		quantized, err := billCents(remaining, family.approvedAmt.currency)
		if err != nil {
			view.Eligible = false
			view.Reasons = append(view.Reasons, "remaining amount is not exact")
			return view
		}
		view.RemainingAmount = quantized.String()
		if remaining.Sign() <= 0 {
			view.Eligible = false
			view.Reasons = append(view.Reasons, "fully billed")
		}
		return view
	}
	view.ClaimedQty = billCanonical(claimed)
	remaining, err := billSub(family.approvedQty, claimed)
	if err != nil {
		view.Eligible = false
		view.Reasons = append(view.Reasons, "remaining quantity is not exact")
		return view
	}
	view.RemainingQty = billCanonical(remaining)
	if remaining.Sign() <= 0 {
		view.Eligible = false
		view.Reasons = append(view.Reasons, "fully billed")
	}
	if head.PricingMode == PricingUnitPrice && remaining.Sign() > 0 {
		billable, err := billConvert(remaining, head.Unit, head.BillableUnit)
		if err != nil {
			view.Eligible = false
			view.Reasons = append(view.Reasons, fmt.Sprintf("no exact billable conversion from %s to %s", head.Unit, head.BillableUnit))
		} else {
			view.BillableRemaining = billCanonical(billable)
		}
	}
	return view
}

func mustBillZero() values.Decimal {
	zero, _ := values.NewDecimal("0", 0, values.RoundingExactRequired)
	return zero
}

func mustBillAmount(m billMoneyValue, currency string) values.Decimal {
	if m.valid() {
		return m.amount
	}
	zero, _ := values.NewDecimal("0", 0, values.RoundingExactRequired)
	_ = currency
	return zero
}

func (r *SourceClaimRegistry) expenseView(expense *billExpense) BillSourceView {
	record := expense.record
	view := BillSourceView{
		FamilyID: record.Owner + ":" + record.RootID, Revision: 1,
		Description: record.Description, ServiceDate: record.ServiceDate,
		Kind: BillKindExpense, UnitDimension: "money",
		ApprovedAmount: record.CustomerAmount, ClaimedAmount: record.ClaimedAmount,
		CostAmount: record.CostAmount,
		RateRef:    "expense:" + record.ExpenseID, PricingMode: PricingTimeMaterial,
		Category: record.Category, Eligible: true,
	}
	if record.Status == "claimed" {
		empty, _ := billCents(mustBillZero(), billCurrencyOf(record.CustomerAmount))
		view.RemainingAmount = empty.String()
		view.Eligible = false
		view.Reasons = append(view.Reasons, "fully billed")
		return view
	}
	view.RemainingAmount = record.CustomerAmount
	if !record.RechargeEligible {
		view.Eligible = false
		view.Reasons = append(view.Reasons, "recharge is not eligible")
	}
	return view
}

func billCurrencyOf(text string) string {
	parts := strings.SplitN(strings.TrimSpace(text), " ", 2)
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}

func billClaimFingerprint(input BillClaimInput) string {
	return strings.Join([]string{
		input.FamilyID, fmt.Sprintf("%d", input.Revision), input.Qty, input.Amount,
		input.PolicyID, input.PolicyVersion, string(input.Kind),
		fmt.Sprintf("%d", input.LeaseMinutes), input.DraftRef, input.InvoiceRef,
	}, "\x00")
}

type billValidatedClaim struct {
	family   *billFamily
	qty      values.Decimal
	amount   billMoneyValue
	billable values.Decimal
	customer billMoneyValue
}

// validateClaim checks scope, revision, payload, conversion and remainder
// without mutating. It runs under the registry lock.
func (r *SourceClaimRegistry) validateClaim(op BillOp, input BillClaimInput) (*billValidatedClaim, error) {
	family, ok := r.families[strings.TrimSpace(input.FamilyID)]
	if !ok || family.tenant != op.TenantID || family.entity != op.LegalEntityID {
		return nil, ErrBillSourceDenied
	}
	if input.Revision != family.head {
		return nil, fmt.Errorf("%w: claim pins revision %d, head is %d", ErrBillSourceStale, input.Revision, family.head)
	}
	if strings.TrimSpace(input.PolicyID) == "" || strings.TrimSpace(input.PolicyVersion) == "" {
		return nil, fmt.Errorf("%w: claim needs its selected policy", ErrBillSourceValidation)
	}
	if input.Kind != BillClaimHold && input.Kind != BillClaimIssue {
		return nil, fmt.Errorf("%w: unknown claim kind %q", ErrBillSourceValidation, input.Kind)
	}
	if input.Kind == BillClaimHold && input.LeaseMinutes <= 0 {
		return nil, fmt.Errorf("%w: draft hold needs an explicit lease", ErrBillSourceValidation)
	}
	if input.Kind == BillClaimIssue && input.LeaseMinutes != 0 {
		return nil, fmt.Errorf("%w: issued allocations never expire", ErrBillSourceValidation)
	}
	checked := &billValidatedClaim{family: family}
	if family.dim == "money" {
		if strings.TrimSpace(input.Qty) != "" || strings.TrimSpace(input.Amount) == "" {
			return nil, fmt.Errorf("%w: money source takes an amount", ErrBillSourceValidation)
		}
		amount, err := billParseMoney(input.Amount)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrBillSourceValidation, err)
		}
		if amount.amount.Sign() <= 0 {
			return nil, fmt.Errorf("%w: claim amount must be positive", ErrBillSourceValidation)
		}
		if amount.currency != family.approvedAmt.currency {
			return nil, fmt.Errorf("%w: claim currency %s does not match %s", ErrBillSourceValidation, amount.currency, family.approvedAmt.currency)
		}
		_, claimedAmt := r.activeTotals(family)
		remaining, err := billSub(family.approvedAmt.amount, mustBillAmount(claimedAmt, family.approvedAmt.currency))
		if err != nil {
			return nil, err
		}
		if amount.amount.Cmp(remaining) > 0 {
			left, _ := billCents(remaining, family.approvedAmt.currency)
			return nil, fmt.Errorf("%w: %s remains", ErrBillSourceShortfall, left.String())
		}
		checked.amount = amount
		checked.customer = amount
		return checked, nil
	}
	if strings.TrimSpace(input.Amount) != "" || strings.TrimSpace(input.Qty) == "" {
		return nil, fmt.Errorf("%w: measured source takes a quantity", ErrBillSourceValidation)
	}
	qty, err := billParseDecimal(input.Qty)
	if err != nil {
		return nil, fmt.Errorf("%w: claim quantity: %v", ErrBillSourceValidation, err)
	}
	if qty.Sign() <= 0 {
		return nil, fmt.Errorf("%w: claim quantity must be positive", ErrBillSourceValidation)
	}
	claimed, _ := r.activeTotals(family)
	remaining, err := billSub(family.approvedQty, claimed)
	if err != nil {
		return nil, err
	}
	if qty.Cmp(remaining) > 0 {
		return nil, fmt.Errorf("%w: %s remains", ErrBillSourceShortfall, billCanonical(remaining))
	}
	head := family.revs[family.head]
	rate, ok := r.rates[family.rateRef]
	if !ok {
		return nil, fmt.Errorf("%w: rate %q is not registered", ErrBillSourceValidation, family.rateRef)
	}
	if family.mode == PricingUnitPrice {
		billable, err := billConvert(qty, family.unit, family.billUnit)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrBillSourceValidation, err)
		}
		product, err := billable.Mul(rate.amount, billable.Scale()+rate.amount.Scale(), values.RoundingExactRequired)
		if err != nil {
			return nil, fmt.Errorf("%w: claim extension: %v", ErrBillSourceValidation, err)
		}
		customer, err := billCents(product, rate.currency)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrBillSourceValidation, err)
		}
		checked.qty = qty
		checked.billable = billable
		checked.customer = customer
		_ = head
		return checked, nil
	}
	checked.qty = qty
	return checked, nil
}

// PreviewClaim prices one hypothetical claim without recording anything.
func (r *SourceClaimRegistry) PreviewClaim(op BillOp, input BillClaimInput) (BillPreview, error) {
	if err := op.validate(); err != nil {
		return BillPreview{}, err
	}
	if !op.BillingAllowed {
		return BillPreview{}, ErrBillSourceDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	checked, err := r.validateClaim(op, input)
	if err != nil {
		return BillPreview{}, err
	}
	family := checked.family
	preview := BillPreview{FamilyID: family.id, Revision: family.head, BillableUnit: family.billUnit}
	if family.dim == "money" {
		_, claimedAmt := r.activeTotals(family)
		after, err := billSub(family.approvedAmt.amount, mustBillAmount(claimedAmt, family.approvedAmt.currency))
		if err != nil {
			return BillPreview{}, err
		}
		after, err = billSub(after, checked.amount.amount)
		if err != nil {
			return BillPreview{}, err
		}
		left, err := billCents(after, family.approvedAmt.currency)
		if err != nil {
			return BillPreview{}, err
		}
		preview.Amount = checked.customer.String()
		preview.Currency = checked.customer.currency
		preview.RemainingAfterAmount = left.String()
		return preview, nil
	}
	claimed, _ := r.activeTotals(family)
	after, err := billSub(family.approvedQty, claimed)
	if err != nil {
		return BillPreview{}, err
	}
	after, err = billSub(after, checked.qty)
	if err != nil {
		return BillPreview{}, err
	}
	preview.RemainingAfterQty = billCanonical(after)
	if family.mode == PricingUnitPrice {
		preview.BillableQty = billCanonical(checked.billable)
		preview.Amount = checked.customer.String()
		preview.Currency = checked.customer.currency
	}
	return preview, nil
}

// Claim records one draft hold or issued allocation with tenant-scoped
// idempotency: the same key and canonical payload replays the original
// receipt verbatim, the same key with a changed payload conflicts.
func (r *SourceClaimRegistry) Claim(op BillOp, input BillClaimInput, key string) (BillReceipt, error) {
	if err := op.validate(); err != nil {
		return BillReceipt{}, err
	}
	if !op.BillingAllowed {
		return BillReceipt{}, ErrBillSourceDenied
	}
	if strings.TrimSpace(key) == "" {
		return BillReceipt{}, fmt.Errorf("%w: claim needs an idempotency key", ErrBillSourceValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	fingerprint := billClaimFingerprint(input)
	idemKey := billIdemKey(op.TenantID, strings.TrimSpace(key))
	if prior, ok := r.idem[idemKey]; ok {
		if prior.fingerprint != fingerprint {
			return BillReceipt{}, fmt.Errorf("%w: key is already used", ErrBillSourceConflict)
		}
		if prior.tenant != op.TenantID || prior.entity != op.LegalEntityID {
			return BillReceipt{}, ErrBillSourceDenied
		}
		return prior.receipt, nil
	}
	checked, err := r.validateClaim(op, input)
	if err != nil {
		return BillReceipt{}, err
	}
	family := checked.family
	r.seq++
	claim := &BillClaim{
		ClaimID:  fmt.Sprintf("claim-%d", r.seq),
		TenantID: op.TenantID, LegalEntityID: op.LegalEntityID,
		FamilyID: family.id, Revision: input.Revision,
		PolicyID: strings.TrimSpace(input.PolicyID), PolicyVersion: strings.TrimSpace(input.PolicyVersion),
		Kind: input.Kind, DraftRef: strings.TrimSpace(input.DraftRef), InvoiceRef: strings.TrimSpace(input.InvoiceRef),
		Key: strings.TrimSpace(key), CreatedAt: billStamp(r),
	}
	if family.dim == "money" {
		claim.Amount = checked.amount.String()
		claim.MoneyAmount = checked.customer.String()
	} else {
		claim.Qty = billCanonical(checked.qty)
		claim.MoneyAmount = checked.customer.String()
	}
	if input.Kind == BillClaimHold {
		claim.Status = BillStatusHeld
		claim.LeaseExpiry = billNow(r).Add(time.Duration(input.LeaseMinutes) * time.Minute).Format(time.RFC3339)
	} else {
		claim.Status = BillStatusIssued
	}
	family.claims = append(family.claims, claim)
	r.claims[claim.ClaimID] = claim
	recorded := r.receipt(claim)
	r.idem[idemKey] = billIdem{fingerprint: fingerprint, claimID: claim.ClaimID, tenant: op.TenantID, entity: op.LegalEntityID, receipt: recorded}
	family.events = append(family.events, BillEvent{Kind: "CLAIM_RECORDED", Actor: op.ActorID, At: claim.CreatedAt,
		Detail: fmt.Sprintf("%s %s by key %s", claim.Status, claim.Qty+claim.Amount, key)})
	return recorded, nil
}

func (r *SourceClaimRegistry) receipt(claim *BillClaim) BillReceipt {
	receipt := BillReceipt{
		ClaimID: claim.ClaimID, FamilyID: claim.FamilyID, Revision: claim.Revision,
		ClaimedQty: claim.Qty, ClaimedAmount: claim.Amount, Amount: claim.MoneyAmount,
		Status: claim.Status, LeaseExpiry: claim.LeaseExpiry, Key: claim.Key,
	}
	family, ok := r.families[claim.FamilyID]
	if !ok {
		return receipt
	}
	claimed, claimedAmt := r.activeTotals(family)
	if family.dim == "money" {
		remaining, err := billSub(family.approvedAmt.amount, mustBillAmount(claimedAmt, family.approvedAmt.currency))
		if err == nil {
			if quantized, err := billCents(remaining, family.approvedAmt.currency); err == nil {
				receipt.RemainingAmount = quantized.String()
			}
		}
		return receipt
	}
	remaining, err := billSub(family.approvedQty, claimed)
	if err == nil {
		receipt.RemainingQty = billCanonical(remaining)
	}
	return receipt
}

// ClaimStatus replays the live receipt for one idempotency key. Unknown keys
// share the neutral denied response: an observed timeout never becomes an
// invented success.
func (r *SourceClaimRegistry) ClaimStatus(op BillOp, key string) (BillReceipt, error) {
	if err := op.validate(); err != nil {
		return BillReceipt{}, err
	}
	if !op.BillingAllowed {
		return BillReceipt{}, ErrBillSourceDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	prior, ok := r.idem[billIdemKey(op.TenantID, strings.TrimSpace(key))]
	if !ok || prior.tenant != op.TenantID || prior.entity != op.LegalEntityID {
		return BillReceipt{}, ErrBillSourceDenied
	}
	if _, ok := r.claims[prior.claimID]; !ok {
		return BillReceipt{}, ErrBillSourceDenied
	}
	return prior.receipt, nil
}

// CommitHold issues one live draft hold. Expired holds cannot issue.
func (r *SourceClaimRegistry) CommitHold(op BillOp, claimID, key string) (BillReceipt, error) {
	if err := op.validate(); err != nil {
		return BillReceipt{}, err
	}
	if !op.BillingAllowed {
		return BillReceipt{}, ErrBillSourceDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	claim, ok := r.claims[strings.TrimSpace(claimID)]
	if !ok || claim.TenantID != op.TenantID || claim.LegalEntityID != op.LegalEntityID {
		return BillReceipt{}, ErrBillSourceDenied
	}
	if claim.Status != BillStatusHeld || billHoldExpired(claim, billNow(r)) {
		return BillReceipt{}, fmt.Errorf("%w: hold %s is not issuable", ErrBillSourceState, claim.ClaimID)
	}
	claim.Status = BillStatusIssued
	now := billStamp(r)
	if family, ok := r.families[claim.FamilyID]; ok {
		family.events = append(family.events, BillEvent{Kind: "HOLD_COMMITTED", Actor: op.ActorID, At: now, Detail: claim.ClaimID})
	}
	_ = key
	return r.receipt(claim), nil
}

// ReleaseClaim cancels one draft hold, freeing its quantity. Issued
// allocations need an authorized reversal instead.
func (r *SourceClaimRegistry) ReleaseClaim(op BillOp, claimID, key string) (BillReceipt, error) {
	if err := op.validate(); err != nil {
		return BillReceipt{}, err
	}
	if !op.BillingAllowed {
		return BillReceipt{}, ErrBillSourceDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	claim, ok := r.claims[strings.TrimSpace(claimID)]
	if !ok || claim.TenantID != op.TenantID || claim.LegalEntityID != op.LegalEntityID {
		return BillReceipt{}, ErrBillSourceDenied
	}
	if claim.Status != BillStatusHeld && claim.Status != BillStatusExpired {
		return BillReceipt{}, fmt.Errorf("%w: %s claims release through reversal", ErrBillSourceState, claim.Status)
	}
	claim.Status = BillStatusReleased
	now := billStamp(r)
	if family, ok := r.families[claim.FamilyID]; ok {
		family.events = append(family.events, BillEvent{Kind: "CLAIM_RELEASED", Actor: op.ActorID, At: now, Detail: claim.ClaimID})
	}
	_ = key
	return r.receipt(claim), nil
}

// ReverseIssued reverses one issued allocation with an authorized adjustment
// reference, freeing its quantity for an explicit rebill.
func (r *SourceClaimRegistry) ReverseIssued(op BillOp, claimID, reason, adjustmentRef, key string) (BillReceipt, error) {
	if err := op.validate(); err != nil {
		return BillReceipt{}, err
	}
	if !op.BillingAllowed {
		return BillReceipt{}, ErrBillSourceDenied
	}
	if strings.TrimSpace(reason) == "" || strings.TrimSpace(adjustmentRef) == "" {
		return BillReceipt{}, fmt.Errorf("%w: reversal needs a reason and an adjustment receipt", ErrBillSourceValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	claim, ok := r.claims[strings.TrimSpace(claimID)]
	if !ok || claim.TenantID != op.TenantID || claim.LegalEntityID != op.LegalEntityID {
		return BillReceipt{}, ErrBillSourceDenied
	}
	if claim.Status != BillStatusIssued {
		return BillReceipt{}, fmt.Errorf("%w: only issued claims reverse", ErrBillSourceState)
	}
	claim.Status = BillStatusReversed
	claim.ReversalReason = strings.TrimSpace(reason)
	claim.ReversalRef = strings.TrimSpace(adjustmentRef)
	now := billStamp(r)
	if family, ok := r.families[claim.FamilyID]; ok {
		family.events = append(family.events, BillEvent{Kind: "CLAIM_REVERSED", Actor: op.ActorID, At: now,
			Detail: fmt.Sprintf("%s via %s", claim.ClaimID, claim.ReversalRef)})
	}
	_ = key
	return r.receipt(claim), nil
}

// SweepExpired marks expired draft holds and reports how many flipped.
func (r *SourceClaimRegistry) SweepExpired(now time.Time) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	swept := 0
	for _, claim := range r.claims {
		if claim.Status == BillStatusHeld && billHoldExpired(claim, now.UTC()) {
			claim.Status = BillStatusExpired
			swept++
		}
	}
	return swept
}

// RegisterExpense stores one producer expense decision: the worker-paid cost,
// the customer recharge rate and the untouched reimbursement eligibility.
// Zero or missing approval timestamps are refused, never substituted.
func (r *SourceClaimRegistry) RegisterExpense(op BillOp, input BillExpenseInput) (BillExpenseEligibility, error) {
	if err := op.validate(); err != nil {
		return BillExpenseEligibility{}, err
	}
	if !op.BillingAllowed {
		return BillExpenseEligibility{}, ErrBillSourceDenied
	}
	if strings.TrimSpace(input.Owner) == "" || strings.TrimSpace(input.RootID) == "" {
		return BillExpenseEligibility{}, fmt.Errorf("%w: expense needs owner and root", ErrBillSourceValidation)
	}
	if strings.TrimSpace(input.WorkOrderID) == "" {
		return BillExpenseEligibility{}, fmt.Errorf("%w: expense needs a work order", ErrBillSourceValidation)
	}
	cost, err := billParseMoney(input.CostAmount)
	if err != nil {
		return BillExpenseEligibility{}, fmt.Errorf("%w: %v", ErrBillSourceValidation, err)
	}
	if cost.amount.Sign() <= 0 {
		return BillExpenseEligibility{}, fmt.Errorf("%w: expense cost must be positive", ErrBillSourceValidation)
	}
	rate, err := billParseDecimal(input.RechargeRate)
	if err != nil {
		return BillExpenseEligibility{}, fmt.Errorf("%w: recharge rate: %v", ErrBillSourceValidation, err)
	}
	if rate.Sign() < 0 {
		return BillExpenseEligibility{}, fmt.Errorf("%w: recharge rate cannot be negative", ErrBillSourceValidation)
	}
	reimbursement, err := billParseMoney(input.ReimbursementAmount)
	if err != nil {
		return BillExpenseEligibility{}, fmt.Errorf("%w: reimbursement: %v", ErrBillSourceValidation, err)
	}
	if reimbursement.currency != cost.currency {
		return BillExpenseEligibility{}, fmt.Errorf("%w: reimbursement currency does not match cost", ErrBillSourceValidation)
	}
	if strings.TrimSpace(input.EvidenceRef) == "" || strings.TrimSpace(input.RevisionBinding) == "" || strings.TrimSpace(input.ApprovedBy) == "" {
		return BillExpenseEligibility{}, fmt.Errorf("%w: expense approval is unknown", ErrBillSourceValidation)
	}
	if err := billApprovedAt(input.ApprovedAt); err != nil {
		return BillExpenseEligibility{}, fmt.Errorf("%w: %v", ErrBillSourceValidation, err)
	}
	if _, err := billServiceDate(input.ServiceDate); err != nil {
		return BillExpenseEligibility{}, fmt.Errorf("%w: %v", ErrBillSourceValidation, err)
	}
	if strings.TrimSpace(input.Description) == "" {
		return BillExpenseEligibility{}, fmt.Errorf("%w: expense needs a customer-safe description", ErrBillSourceValidation)
	}
	category := strings.TrimSpace(input.Category)
	if category == "" {
		category = "recharge"
	}
	one, _ := values.NewDecimal("1", 0, values.RoundingExactRequired)
	factor, err := billAdd(one, rate)
	if err != nil {
		return BillExpenseEligibility{}, err
	}
	customer, err := billMoneyMul(cost, factor)
	if err != nil {
		return BillExpenseEligibility{}, fmt.Errorf("%w: recharge extension: %v", ErrBillSourceValidation, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	id := "exp:" + strings.TrimSpace(input.Owner) + ":" + strings.TrimSpace(input.RootID)
	fingerprint := strings.Join([]string{input.Owner, input.RootID, input.WorkOrderID, input.JobRef, input.CostAmount, input.RechargeRate, fmt.Sprintf("%v", input.RechargeEligible), input.ReimbursementAmount, input.EvidenceRef, input.RevisionBinding, input.ApprovedBy, input.ApprovedAt, input.Description, input.ServiceDate, category}, "\x00")
	if existing, ok := r.expenses[id]; ok {
		if existing.fingerprint != fingerprint || existing.record.TenantID != op.TenantID || existing.record.LegalEntityID != op.LegalEntityID {
			if existing.record.TenantID != op.TenantID || existing.record.LegalEntityID != op.LegalEntityID {
				return BillExpenseEligibility{}, ErrBillSourceDenied
			}
			return BillExpenseEligibility{}, fmt.Errorf("%w: expense %s is already registered", ErrBillSourceConflict, id)
		}
		return existing.record, nil
	}
	zero, _ := billCents(mustBillZero(), cost.currency)
	record := BillExpenseEligibility{
		ExpenseID: id, TenantID: op.TenantID, LegalEntityID: op.LegalEntityID,
		Owner: strings.TrimSpace(input.Owner), RootID: strings.TrimSpace(input.RootID),
		WorkOrderID: strings.TrimSpace(input.WorkOrderID), JobRef: strings.TrimSpace(input.JobRef),
		CostAmount: cost.String(), RechargeRate: billCanonical(rate), RechargeEligible: input.RechargeEligible,
		ReimbursementAmount: reimbursement.String(), CustomerAmount: customer.String(),
		EvidenceRef: strings.TrimSpace(input.EvidenceRef), RevisionBinding: strings.TrimSpace(input.RevisionBinding),
		ApprovedBy: strings.TrimSpace(input.ApprovedBy), ApprovedAt: strings.TrimSpace(input.ApprovedAt),
		Description: strings.TrimSpace(input.Description), ServiceDate: strings.TrimSpace(input.ServiceDate),
		Category: category, Status: "eligible", ClaimedAmount: zero.String(), RegisteredAt: billStamp(r),
	}
	stored := &billExpense{record: record, cost: cost, fingerprint: fingerprint}
	stored.events = append(stored.events, BillEvent{Kind: "EXPENSE_REGISTERED", Actor: op.ActorID, At: record.RegisteredAt, Detail: record.CustomerAmount})
	r.expenses[id] = stored
	return record, nil
}

// ClaimExpense consumes one registered expense for the customer (include) or
// records an explicit exclusion (zero claim). Either path is idempotent by
// key; reimbursement eligibility is never touched.
func (r *SourceClaimRegistry) ClaimExpense(op BillOp, expenseID string, include bool, policyID, policyVersion, key string) (BillReceipt, error) {
	if err := op.validate(); err != nil {
		return BillReceipt{}, err
	}
	if !op.BillingAllowed {
		return BillReceipt{}, ErrBillSourceDenied
	}
	if strings.TrimSpace(policyID) == "" || strings.TrimSpace(policyVersion) == "" {
		return BillReceipt{}, fmt.Errorf("%w: expense claim needs its selected policy", ErrBillSourceValidation)
	}
	if strings.TrimSpace(key) == "" {
		return BillReceipt{}, fmt.Errorf("%w: expense claim needs an idempotency key", ErrBillSourceValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	idemKey := billIdemKey(op.TenantID, "expense\x00"+strings.TrimSpace(key))
	fingerprint := strings.Join([]string{strings.TrimSpace(expenseID), fmt.Sprintf("%v", include), policyID, policyVersion}, "\x00")
	if prior, ok := r.expenseIdem[idemKey]; ok {
		if prior.fingerprint != fingerprint {
			return BillReceipt{}, fmt.Errorf("%w: key is already used", ErrBillSourceConflict)
		}
		return prior.receipt, nil
	}
	expense, ok := r.expenses[strings.TrimSpace(expenseID)]
	if !ok || expense.record.TenantID != op.TenantID || expense.record.LegalEntityID != op.LegalEntityID {
		return BillReceipt{}, ErrBillSourceDenied
	}
	currency := billCurrencyOf(expense.record.CustomerAmount)
	zero, _ := billCents(mustBillZero(), currency)
	if !include {
		receipt := BillReceipt{
			ClaimID: "excluded-" + expense.record.ExpenseID, FamilyID: expense.record.Owner + ":" + expense.record.RootID,
			ClaimedAmount: zero.String(), Amount: zero.String(), RemainingAmount: expense.record.CustomerAmount,
			Status: BillStatusExcluded, Key: strings.TrimSpace(key),
		}
		r.expenseIdem[idemKey] = billExpenseIdem{fingerprint: fingerprint, receipt: receipt}
		expense.events = append(expense.events, BillEvent{Kind: "EXPENSE_EXCLUDED", Actor: op.ActorID, At: billStamp(r), Detail: receipt.ClaimID})
		return receipt, nil
	}
	if !expense.record.RechargeEligible {
		return BillReceipt{}, fmt.Errorf("%w: expense recharge is not eligible", ErrBillSourceValidation)
	}
	if expense.record.Status == "claimed" {
		return BillReceipt{}, fmt.Errorf("%w: %s remains", ErrBillSourceShortfall, zero.String())
	}
	r.seq++
	expense.record.Status = "claimed"
	expense.record.ClaimedAmount = expense.record.CustomerAmount
	receipt := BillReceipt{
		ClaimID: fmt.Sprintf("claim-%d", r.seq), FamilyID: expense.record.Owner + ":" + expense.record.RootID,
		ClaimedAmount: expense.record.CustomerAmount, Amount: expense.record.CustomerAmount,
		RemainingAmount: zero.String(), Status: BillStatusIssued, Key: strings.TrimSpace(key),
	}
	r.expenseIdem[idemKey] = billExpenseIdem{fingerprint: fingerprint, receipt: receipt}
	expense.events = append(expense.events, BillEvent{Kind: "EXPENSE_CLAIMED", Actor: op.ActorID, At: billStamp(r), Detail: receipt.ClaimID})
	return receipt, nil
}

type billExpenseIdem struct {
	fingerprint string
	receipt     BillReceipt
}

// RegisterCost stores one restricted cost observation. Observations are
// never billable and never visible without the cost grant.
func (r *SourceClaimRegistry) RegisterCost(op BillOp, input BillCostInput) (BillCostObservation, error) {
	if err := op.validate(); err != nil {
		return BillCostObservation{}, err
	}
	if !op.BillingAllowed {
		return BillCostObservation{}, ErrBillSourceDenied
	}
	if strings.TrimSpace(input.Owner) == "" || strings.TrimSpace(input.RootID) == "" || strings.TrimSpace(input.WorkOrderID) == "" {
		return BillCostObservation{}, fmt.Errorf("%w: cost needs owner, root and work order", ErrBillSourceValidation)
	}
	amount, err := billParseMoney(input.Amount)
	if err != nil {
		return BillCostObservation{}, fmt.Errorf("%w: %v", ErrBillSourceValidation, err)
	}
	if strings.TrimSpace(input.Detail) == "" || strings.TrimSpace(input.EvidenceRef) == "" {
		return BillCostObservation{}, fmt.Errorf("%w: cost needs detail and evidence", ErrBillSourceValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	id := "cost:" + strings.TrimSpace(input.Owner) + ":" + strings.TrimSpace(input.RootID)
	if existing, ok := r.costs[id]; ok {
		if existing.TenantID != op.TenantID || existing.LegalEntityID != op.LegalEntityID {
			return BillCostObservation{}, ErrBillSourceDenied
		}
		return *existing, nil
	}
	record := &BillCostObservation{
		CostID: id, TenantID: op.TenantID, LegalEntityID: op.LegalEntityID,
		Owner: strings.TrimSpace(input.Owner), RootID: strings.TrimSpace(input.RootID),
		WorkOrderID: strings.TrimSpace(input.WorkOrderID), Detail: strings.TrimSpace(input.Detail),
		Amount: amount.String(), EvidenceRef: strings.TrimSpace(input.EvidenceRef), RegisteredAt: billStamp(r),
	}
	r.costs[id] = record
	return *record, nil
}

// ReadCost returns one cost observation to holders of the cost grant.
func (r *SourceClaimRegistry) ReadCost(op BillOp, costID string) (BillCostObservation, error) {
	if err := op.validate(); err != nil {
		return BillCostObservation{}, err
	}
	if !op.BillingAllowed || !op.CostVisible {
		return BillCostObservation{}, ErrBillSourceDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.costs[strings.TrimSpace(costID)]
	if !ok || record.TenantID != op.TenantID || record.LegalEntityID != op.LegalEntityID {
		return BillCostObservation{}, ErrBillSourceDenied
	}
	return *record, nil
}

// FamilyTrace replays the audit events for one source family or expense.
func (r *SourceClaimRegistry) FamilyTrace(op BillOp, familyID string) ([]BillEvent, error) {
	if err := op.validate(); err != nil {
		return nil, err
	}
	if !op.BillingAllowed {
		return nil, ErrBillSourceDenied
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	trimmed := strings.TrimSpace(familyID)
	if family, ok := r.families[trimmed]; ok {
		if family.tenant != op.TenantID || family.entity != op.LegalEntityID {
			return nil, ErrBillSourceDenied
		}
		return append([]BillEvent(nil), family.events...), nil
	}
	for _, expense := range r.expenses {
		if expense.record.Owner+":"+expense.record.RootID == trimmed {
			if expense.record.TenantID != op.TenantID || expense.record.LegalEntityID != op.LegalEntityID {
				return nil, ErrBillSourceDenied
			}
			return append([]BillEvent(nil), expense.events...), nil
		}
	}
	return nil, ErrBillSourceDenied
}

type billSnapshotRate struct {
	Currency string
	Amount   string
	Unit     string
}

type billSnapshotFamily struct {
	ID          string
	Tenant      string
	Entity      string
	Owner       string
	Root        string
	WorkOrder   string
	JobRef      string
	Kind        BillSourceKind
	Dim         string
	Unit        string
	RateRef     string
	Mode        PricingMode
	BillUnit    string
	Category    string
	Milestone   string
	Head        uint64
	Revs        map[uint64]*BillSource
	ApprovedQty string
	ApprovedAmt string
	Claims      []*BillClaim
	Events      []BillEvent
}

type billSnapshotExpense struct {
	Record      BillExpenseEligibility
	Fingerprint string
	Events      []BillEvent
}

type billSnapshotIdem struct {
	Fingerprint string
	ClaimID     string
	Tenant      string
	Entity      string
	Receipt     BillReceipt
}

type billSnapshot struct {
	Rates       map[string]billSnapshotRate
	Families    map[string]*billSnapshotFamily
	Claims      map[string]*BillClaim
	Idem        map[string]billSnapshotIdem
	Expenses    map[string]*billSnapshotExpense
	ExpenseIdem map[string]billExpenseIdem
	Costs       map[string]*BillCostObservation
	Seq         uint64
}

// Export snapshots the registry for durable persistence and restart replay.
func (r *SourceClaimRegistry) Export() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap := billSnapshot{
		Rates:       map[string]billSnapshotRate{},
		Families:    map[string]*billSnapshotFamily{},
		Claims:      map[string]*BillClaim{},
		Idem:        map[string]billSnapshotIdem{},
		Expenses:    map[string]*billSnapshotExpense{},
		ExpenseIdem: map[string]billExpenseIdem{},
		Costs:       map[string]*BillCostObservation{},
		Seq:         r.seq,
	}
	for ref, rate := range r.rates {
		snap.Rates[ref] = billSnapshotRate{Currency: rate.currency, Amount: billCanonical(rate.amount), Unit: rate.unit}
	}
	for id, family := range r.families {
		snap.Families[id] = &billSnapshotFamily{
			ID: id, Tenant: family.tenant, Entity: family.entity,
			Owner: family.owner, Root: family.root, WorkOrder: family.workOrder, JobRef: family.jobRef,
			Kind: family.kind, Dim: family.dim, Unit: family.unit, RateRef: family.rateRef,
			Mode: family.mode, BillUnit: family.billUnit, Category: family.category,
			Milestone: family.milestone, Head: family.head, Revs: family.revs,
			ApprovedQty: billCanonical(family.approvedQty), ApprovedAmt: family.approvedAmt.String(),
			Claims: family.claims, Events: family.events,
		}
	}
	for id, claim := range r.claims {
		snap.Claims[id] = claim
	}
	for key, idem := range r.idem {
		snap.Idem[key] = billSnapshotIdem{Fingerprint: idem.fingerprint, ClaimID: idem.claimID, Tenant: idem.tenant, Entity: idem.entity, Receipt: idem.receipt}
	}
	for id, expense := range r.expenses {
		snap.Expenses[id] = &billSnapshotExpense{Record: expense.record, Fingerprint: expense.fingerprint, Events: expense.events}
	}
	for key, idem := range r.expenseIdem {
		snap.ExpenseIdem[key] = idem
	}
	for id, cost := range r.costs {
		snap.Costs[id] = cost
	}
	return json.Marshal(snap)
}

// Import restores a registry previously saved with Export.
func (r *SourceClaimRegistry) Import(data []byte) error {
	var snap billSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("%w: snapshot: %v", ErrBillSourceValidation, err)
	}
	rates := map[string]billRate{}
	for ref, saved := range snap.Rates {
		amount, err := billParseDecimal(saved.Amount)
		if err != nil {
			return fmt.Errorf("%w: snapshot rate %s: %v", ErrBillSourceValidation, ref, err)
		}
		rates[ref] = billRate{currency: saved.Currency, amount: amount, unit: saved.Unit}
	}
	families := map[string]*billFamily{}
	for id, saved := range snap.Families {
		approvedQty := mustBillZero()
		if saved.ApprovedQty != "" {
			parsed, err := billParseDecimal(saved.ApprovedQty)
			if err != nil {
				return fmt.Errorf("%w: snapshot family %s: %v", ErrBillSourceValidation, id, err)
			}
			approvedQty = parsed
		}
		var approvedAmt billMoneyValue
		if saved.ApprovedAmt != "" {
			parsed, err := billParseMoney(saved.ApprovedAmt)
			if err != nil {
				return fmt.Errorf("%w: snapshot family %s: %v", ErrBillSourceValidation, id, err)
			}
			approvedAmt = parsed
		}
		if saved.Revs == nil {
			saved.Revs = map[uint64]*BillSource{}
		}
		families[id] = &billFamily{
			id: id, tenant: saved.Tenant, entity: saved.Entity,
			owner: saved.Owner, root: saved.Root, workOrder: saved.WorkOrder, jobRef: saved.JobRef,
			kind: saved.Kind, dim: saved.Dim, unit: saved.Unit, rateRef: saved.RateRef,
			mode: saved.Mode, billUnit: saved.BillUnit, category: saved.Category,
			milestone: saved.Milestone, head: saved.Head, revs: saved.Revs,
			approvedQty: approvedQty, approvedAmt: approvedAmt,
			claims: saved.Claims, events: saved.Events,
		}
	}
	expenses := map[string]*billExpense{}
	for id, saved := range snap.Expenses {
		cost, err := billParseMoney(saved.Record.CostAmount)
		if err != nil {
			return fmt.Errorf("%w: snapshot expense %s: %v", ErrBillSourceValidation, id, err)
		}
		expenses[id] = &billExpense{record: saved.Record, cost: cost, fingerprint: saved.Fingerprint, events: saved.Events}
	}
	idem := map[string]billIdem{}
	for key, saved := range snap.Idem {
		idem[key] = billIdem{fingerprint: saved.Fingerprint, claimID: saved.ClaimID, tenant: saved.Tenant, entity: saved.Entity, receipt: saved.Receipt}
	}
	if snap.Claims == nil {
		snap.Claims = map[string]*BillClaim{}
	}
	if snap.ExpenseIdem == nil {
		snap.ExpenseIdem = map[string]billExpenseIdem{}
	}
	if snap.Costs == nil {
		snap.Costs = map[string]*BillCostObservation{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rates = rates
	r.families = families
	r.claims = snap.Claims
	r.idem = idem
	r.expenses = expenses
	r.expenseIdem = snap.ExpenseIdem
	r.costs = snap.Costs
	r.seq = snap.Seq
	return nil
}

// SMB-BILL-004: versioned tax treatment and invoice rounding through
// existing policy boundaries. This file owns no tax engine: rates,
// modes and rounding orders arrive pinned on a published TaxProfile
// served by a TaxResolver (the rules/legal owners publish; a static
// resolver proves the mechanics in tests and a provider-backed resolver
// stays behind the connectivity operation boundary, unimplemented
// here). Resolve freezes one treatment into an immutable TaxSnapshot
// bound to an invoice revision; new policy revisions never rewrite
// issued snapshots and corrections form separate TaxAdjustment chains.
// Missing treatment resolves to UNRESOLVED and blocks issuance through
// the optional InvoiceStore gate; it never silently becomes zero tax.
package workorderbilling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrTaxValidation refuses malformed profiles, inputs, money and clocks.
	ErrTaxValidation = errors.New("work order billing: tax input is invalid")
	// ErrTaxConflict reports stale revisions and idempotency keys reused
	// with changed payloads.
	ErrTaxConflict = errors.New("work order billing: tax revision or key conflict")
	// ErrTaxDenied is the neutral response for cross-tenant references,
	// guessed IDs and missing capability grants alike.
	ErrTaxDenied = errors.New("work order billing: tax action denied")
	// ErrTaxUnresolved blocks issuance while treatment is unresolved and
	// names the next action; it never presents a payable total.
	ErrTaxUnresolved = errors.New("work order billing: tax treatment unresolved")
	// ErrTaxStale blocks issuance on expired, superseded or tampered
	// treatment until re-resolution.
	ErrTaxStale = errors.New("work order billing: tax treatment stale")
	// ErrTaxProfileNotFound reports a policy reference no publication
	// carries. Resolve maps it to an UNRESOLVED snapshot, not an error.
	ErrTaxProfileNotFound = errors.New("work order billing: tax policy not published")
)

// TaxMode is the pinned tax application mode: tax added on top of the
// billed amounts, or decomposed out of tax-inclusive amounts.
type TaxMode string

const (
	// TaxExclusive adds tax on top of the billed subtotal.
	TaxExclusive TaxMode = "EXCLUSIVE"
	// TaxInclusive decomposes tax out of tax-inclusive billed amounts.
	TaxInclusive TaxMode = "INCLUSIVE"
)

// TaxRoundingOrder pins the granularity tax rounds at: per line with an
// exact invoice roll-up, or once on the invoice base.
type TaxRoundingOrder string

const (
	// TaxRoundLine rounds tax per line.
	TaxRoundLine TaxRoundingOrder = "LINE"
	// TaxRoundInvoice rounds tax once on the invoice base.
	TaxRoundInvoice TaxRoundingOrder = "INVOICE"
)

// TaxStatus is the distinct treatment state of one snapshot.
type TaxStatus string

const (
	// TaxResolved carries computed, reconciled tax.
	TaxResolved TaxStatus = "RESOLVED"
	// TaxExempt carries evidenced zero tax.
	TaxExempt TaxStatus = "EXEMPT"
	// TaxNotApplicable carries zero tax where treatment does not apply.
	TaxNotApplicable TaxStatus = "NOT_APPLICABLE"
	// TaxUnresolved names a next action and blocks issuance.
	TaxUnresolved TaxStatus = "UNRESOLVED"
	// TaxStale names re-resolution and blocks issuance.
	TaxStale TaxStatus = "STALE"
)

// TaxProfile is one published treatment version. Rates arrive as percent
// text ("7.5"); jurisdictions and evidence are opaque references owned
// by the legal/rules publishers, never interpreted here.
type TaxProfile struct {
	ID                string
	Version           string
	Mode              TaxMode
	RatePercent       string
	Rounding          values.RoundingMode
	RoundingOrder     TaxRoundingOrder
	DiscountBeforeTax bool
	Currency          string
	Scale             int32
	Jurisdictions     []string
	EvidenceRef       string
	NotAfter          string
	Exempt            bool
	ExemptReason      string
	Applicable        bool
}

// TaxResolver serves the published treatment for a tenant policy
// reference at an effective time. The static resolver below proves the
// mechanics; production selection stays behind the operation boundary.
type TaxResolver interface {
	TreatmentProfile(ctx context.Context, tenant, policyRef, asOf string) (TaxProfile, error)
}

// StaticTaxResolver is a map-backed TaxResolver for tests and sandbox
// adapters. Publish before sharing; concurrent resolve is read-only.
type StaticTaxResolver struct {
	profiles map[string]TaxProfile
}

// NewStaticTaxResolver returns an empty static resolver.
func NewStaticTaxResolver() *StaticTaxResolver {
	return &StaticTaxResolver{profiles: make(map[string]TaxProfile)}
}

// Publish installs the current publication for one tenant policy
// reference. Later versions overwrite; snapshots pin what they used.
func (r *StaticTaxResolver) Publish(tenant, policyRef string, profile TaxProfile) {
	r.profiles[tenant+"\x00"+policyRef] = profile
}

// TreatmentProfile returns the published profile or
// ErrTaxProfileNotFound. The static resolver ignores asOf; expiry is
// enforced from the profile NotAfter at resolve and issue time.
func (r *StaticTaxResolver) TreatmentProfile(ctx context.Context, tenant, policyRef, asOf string) (TaxProfile, error) {
	if err := ctx.Err(); err != nil {
		return TaxProfile{}, err
	}
	profile, ok := r.profiles[tenant+"\x00"+policyRef]
	if !ok {
		return TaxProfile{}, ErrTaxProfileNotFound
	}
	return profile, nil
}

// TaxCaps are the explicit tax grants a command checks before mutating.
// Role resolution stays with callers.
type TaxCaps struct {
	CanResolve bool
	CanApprove bool
	CanCorrect bool
}

// ResolveTaxInput resolves one invoice against a published policy. The
// discount is a canonical decimal in invoice currency and scale, empty
// for zero.
type ResolveTaxInput struct {
	Key             string
	InvoiceID       string
	InvoiceRevision uint64
	PolicyRef       string
	Discount        string
	At              string
}

// ManualTaxInput asserts one human determination with reason and
// evidence. Unapproved determinations resolve UNRESOLVED and route to
// policy approval; nothing asserts custody of a total before approval.
type ManualTaxInput struct {
	Key             string
	InvoiceID       string
	InvoiceRevision uint64
	RatePercent     string
	Mode            TaxMode
	Rounding        values.RoundingMode
	RoundingOrder   TaxRoundingOrder
	Currency        string
	Scale           int32
	Reason          string
	Evidence        string
	Approved        bool
	At              string
}

// CorrectTaxInput re-resolves one invoice and chains the correction as
// an adjustment. Exactly one of Resolve and Manual carries the
// corrected treatment; ExpectedChainLength is the compare-and-set
// boundary on the adjustment chain.
type CorrectTaxInput struct {
	Key                 string
	InvoiceID           string
	ExpectedChainLength int
	Reason              string
	Actor               string
	At                  string
	Resolve             *ResolveTaxInput
	Manual              *ManualTaxInput
}

// TaxLineComponent is one computed tax row at the profile rounding
// granularity: per line for LINE order, one whole-invoice row with an
// empty line ID for INVOICE order. Amounts are canonical decimals.
type TaxLineComponent struct {
	LineID string `json:"line_id"`
	Basis  string `json:"basis"`
	Tax    string `json:"tax"`
	Total  string `json:"total"`
}

// TaxSnapshot is one frozen treatment bound to an invoice revision and
// its line amounts. Gross is the billed line sum in both modes;
// Subtotal is the net taxable base (gross for exclusive, decomposed
// net for inclusive). Amounts are canonical decimals; Digest freezes
// the content. Snapshots are immutable once stored.
type TaxSnapshot struct {
	ID              string             `json:"id"`
	Sequence        uint64             `json:"sequence"`
	TenantID        string             `json:"tenant_id"`
	InvoiceID       string             `json:"invoice_id"`
	InvoiceRevision uint64             `json:"invoice_revision"`
	LinesDigest     string             `json:"lines_digest"`
	ProfileID       string             `json:"profile_id"`
	ProfileVersion  string             `json:"profile_version"`
	Mode            TaxMode            `json:"mode"`
	RatePercent     string             `json:"rate_percent"`
	RoundingOrder   TaxRoundingOrder   `json:"rounding_order"`
	Status          TaxStatus          `json:"status"`
	Components      []TaxLineComponent `json:"components"`
	Gross           string             `json:"gross"`
	Subtotal        string             `json:"subtotal"`
	Discount        string             `json:"discount"`
	TotalTax        string             `json:"total_tax"`
	Payable         string             `json:"payable"`
	Currency        string             `json:"currency"`
	Scale           int32              `json:"scale"`
	Manual          bool               `json:"manual"`
	ManualBy        string             `json:"manual_by,omitempty"`
	ManualReason    string             `json:"manual_reason,omitempty"`
	ManualEvidence  string             `json:"manual_evidence,omitempty"`
	ExemptReason    string             `json:"exempt_reason,omitempty"`
	EvidenceRef     string             `json:"evidence_ref,omitempty"`
	Jurisdictions   []string           `json:"jurisdictions,omitempty"`
	NotAfter        string             `json:"not_after,omitempty"`
	ResolvedAt      string             `json:"resolved_at"`
	Note            string             `json:"note,omitempty"`
	Digest          string             `json:"digest"`
}

// TaxAdjustment chains one treatment correction: the immutable
// original digest plus the corrected snapshot and signed deltas.
type TaxAdjustment struct {
	ID             string      `json:"id"`
	Sequence       uint64      `json:"sequence"`
	TenantID       string      `json:"tenant_id"`
	InvoiceID      string      `json:"invoice_id"`
	OriginalDigest string      `json:"original_digest"`
	Corrected      TaxSnapshot `json:"corrected"`
	DeltaTax       string      `json:"delta_tax"`
	DeltaPayable   string      `json:"delta_payable"`
	Reason         string      `json:"reason"`
	Actor          string      `json:"actor"`
	At             string      `json:"at"`
}

// TaxReceipt is the idempotent result of one tax command.
type TaxReceipt struct {
	Key          string `json:"key"`
	TenantID     string `json:"tenant_id"`
	InvoiceID    string `json:"invoice_id"`
	SnapshotID   string `json:"snapshot_id"`
	AdjustmentID string `json:"adjustment_id,omitempty"`
	Action       string `json:"action"`
	Payload      string `json:"payload"`
}

// TaxPreview is the uncommitted resolve projection for draft display.
// DisplayTotal reads "Tax pending" while unresolved or stale and never
// a definitive total before treatment exists.
type TaxPreview struct {
	Status       TaxStatus
	Subtotal     string
	TotalTax     string
	Payable      string
	Components   []TaxLineComponent
	Currency     string
	DisplayTotal string
	Note         string
}

// TaxSection is the invoice tax display projection.
type TaxSection struct {
	Status       TaxStatus
	DisplayTotal string
	Pending      bool
	Basis        string
	Mode         TaxMode
	Components   []TaxLineComponent
	SourceDate   string
}

// TaxTreatmentDetail is the finance-authorized treatment drawer: reason,
// evidence and profile without protected provider payload.
type TaxTreatmentDetail struct {
	ProfileID      string
	ProfileVersion string
	Reason         string
	EvidenceRef    string
	Jurisdictions  []string
	Manual         bool
	ResolvedAt     string
}

// TaxRecalculation compares two resolutions and flags reapproval when
// totals move.
type TaxRecalculation struct {
	BeforePayable      string
	AfterPayable       string
	DeltaPayable       string
	BeforeTax          string
	AfterTax           string
	DeltaTax           string
	TotalsChanged      bool
	RequiresReapproval bool
}

// TaxIssueGate validates tax treatment before an invoice issues. The
// InvoiceStore consults it only when set; a nil gate preserves the
// exact BILL-001 behavior.
type TaxIssueGate interface {
	ValidateForIssue(tenant string, inv CustomerInvoice, at string) error
}

func validTaxRounding(mode values.RoundingMode) bool {
	switch mode {
	case values.RoundingHalfEven, values.RoundingHalfUp, values.RoundingHalfAwayFromZero,
		values.RoundingFloor, values.RoundingCeiling, values.RoundingTowardZero,
		values.RoundingAwayFromZero, values.RoundingExactRequired:
		return true
	default:
		return false
	}
}

func parseTaxClock(raw, label string) (time.Time, error) {
	at, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %s time %q is not RFC3339", ErrTaxValidation, label, raw)
	}
	return at, nil
}

func parseTaxDecimal(raw string, scale int32) (values.Decimal, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return values.Decimal{}, fmt.Errorf("%w: decimal is blank", ErrTaxValidation)
	}
	dec, err := values.NewDecimal(trimmed, scale, values.RoundingExactRequired)
	if err != nil {
		return values.Decimal{}, fmt.Errorf("%w: decimal %q: %v", ErrTaxValidation, raw, err)
	}
	return dec, nil
}

func decimalScale(raw string) int32 {
	trimmed := strings.TrimSpace(raw)
	digits := trimmed
	if i := strings.IndexAny(trimmed, "."); i >= 0 {
		digits = trimmed[i+1:]
	} else {
		return 0
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return -1
		}
	}
	return int32(len(digits))
}

// parseRateFraction parses percent text into an exact fraction: the
// fraction scale always fits the text, so ingestion never rounds.
func parseRateFraction(raw string) (values.Decimal, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return values.Decimal{}, fmt.Errorf("%w: rate is blank", ErrTaxValidation)
	}
	scale := decimalScale(trimmed)
	if scale < 0 {
		return values.Decimal{}, fmt.Errorf("%w: rate %q is not a decimal", ErrTaxValidation, raw)
	}
	percent, err := values.NewPercentageFromPercent(trimmed, scale+2, values.RoundingExactRequired)
	if err != nil {
		return values.Decimal{}, fmt.Errorf("%w: rate %q: %v", ErrTaxValidation, raw, err)
	}
	return percent.Fraction(), nil
}

func zeroTaxDecimal(scale int32) values.Decimal {
	dec, err := values.NewDecimal("0", scale, values.RoundingExactRequired)
	if err != nil {
		return mustZeroDecimal(scale)
	}
	return dec
}

func taxLinesDigest(lines []InvoiceLine) string {
	parts := make([]string, 0, len(lines)*2)
	for _, line := range lines {
		parts = append(parts, line.ID, moneyText(line.Amount))
	}
	return hashPayload(parts...)
}

func taxSnapshotDigest(snapshot TaxSnapshot) string {
	parts := []string{
		snapshot.TenantID, snapshot.InvoiceID, fmt.Sprint(snapshot.InvoiceRevision), snapshot.LinesDigest,
		snapshot.ProfileID, snapshot.ProfileVersion, string(snapshot.Mode), snapshot.RatePercent,
		string(snapshot.RoundingOrder), string(snapshot.Status),
		snapshot.Gross, snapshot.Subtotal, snapshot.Discount, snapshot.TotalTax, snapshot.Payable,
		snapshot.Currency, fmt.Sprint(snapshot.Scale),
		fmt.Sprint(snapshot.Manual), snapshot.ManualBy, snapshot.ManualReason, snapshot.ManualEvidence,
		snapshot.ExemptReason, snapshot.EvidenceRef, strings.Join(snapshot.Jurisdictions, ","),
		snapshot.NotAfter, snapshot.ResolvedAt, snapshot.Note,
	}
	for _, component := range snapshot.Components {
		parts = append(parts, component.LineID, component.Basis, component.Tax, component.Total)
	}
	return hashPayload(parts...)
}

type taxComputation struct {
	profile  TaxProfile
	fraction values.Decimal
	subtotal values.Money
	gross    values.Money
	discount values.Money
	scale    int32
}

func prepareTaxComputation(inv CustomerInvoice, profile TaxProfile, discountRaw, at string) (taxComputation, error) {
	if _, err := parseTaxClock(at, "resolve"); err != nil {
		return taxComputation{}, err
	}
	var computed taxComputation
	if len(inv.Lines) == 0 {
		return computed, fmt.Errorf("%w: invoice has no lines", ErrTaxValidation)
	}
	if _, err := values.NewMoney("0", profile.Currency, 2, values.RoundingExactRequired); err != nil {
		return computed, fmt.Errorf("%w: profile currency: %v", ErrTaxValidation, err)
	}
	if profile.Currency != inv.Currency {
		return computed, fmt.Errorf("%w: profile currency %q does not match invoice %q", ErrTaxValidation, profile.Currency, inv.Currency)
	}
	if profile.Scale < 0 {
		return computed, fmt.Errorf("%w: profile scale is negative", ErrTaxValidation)
	}
	lineScale := inv.Lines[0].Amount.Amount().Scale()
	for _, line := range inv.Lines {
		if err := line.Amount.Validate(); err != nil || line.Amount.Currency() != inv.Currency {
			return computed, fmt.Errorf("%w: line %q amount or currency is invalid", ErrTaxValidation, line.ID)
		}
		if line.Amount.Amount().Scale() != lineScale {
			return computed, fmt.Errorf("%w: line scales must match", ErrTaxValidation)
		}
		if line.Amount.Amount().Sign() <= 0 {
			return computed, fmt.Errorf("%w: line amounts must be positive", ErrTaxValidation)
		}
	}
	if profile.Scale != lineScale {
		return computed, fmt.Errorf("%w: profile scale %d does not match invoice scale %d", ErrTaxValidation, profile.Scale, lineScale)
	}
	gross, err := values.NewMoneyFromDecimal(zeroTaxDecimal(lineScale), inv.Currency)
	if err != nil {
		return computed, fmt.Errorf("%w: initialize subtotal: %v", ErrTaxValidation, err)
	}
	for _, line := range inv.Lines {
		gross, err = gross.Add(line.Amount)
		if err != nil {
			return computed, fmt.Errorf("%w: sum invoice lines: %v", ErrTaxValidation, err)
		}
	}
	if err := inv.Total.Validate(); err != nil || inv.Total.Currency() != inv.Currency {
		return computed, fmt.Errorf("%w: invoice total is invalid", ErrTaxValidation)
	}
	if !gross.Amount().Equal(inv.Total.Amount()) {
		return computed, fmt.Errorf("%w: invoice total does not equal its line sum", ErrTaxValidation)
	}
	discount := zeroTaxDecimal(lineScale)
	if strings.TrimSpace(discountRaw) != "" {
		raw, err := parseTaxDecimal(discountRaw, lineScale)
		if err != nil {
			return computed, fmt.Errorf("%w: discount: %v", ErrTaxValidation, err)
		}
		quantized, err := raw.Quantize(lineScale, values.RoundingExactRequired)
		if err != nil {
			return computed, fmt.Errorf("%w: discount precision: %v", ErrTaxValidation, err)
		}
		if quantized.Sign() < 0 {
			return computed, fmt.Errorf("%w: discount is negative", ErrTaxValidation)
		}
		discount = quantized
	}
	if discount.Cmp(gross.Amount()) > 0 {
		return computed, fmt.Errorf("%w: discount exceeds the subtotal", ErrTaxValidation)
	}
	discountMoney, err := values.NewMoneyFromDecimal(discount, inv.Currency)
	if err != nil {
		return computed, fmt.Errorf("%w: discount money: %v", ErrTaxValidation, err)
	}
	fraction, err := parseRateFraction(profile.RatePercent)
	if err != nil {
		return computed, err
	}
	if fraction.Sign() < 0 {
		return computed, fmt.Errorf("%w: rate is negative", ErrTaxValidation)
	}
	hundred, err := values.NewDecimal("1", 0, values.RoundingExactRequired)
	if err != nil {
		return computed, fmt.Errorf("%w: rate bound: %v", ErrTaxValidation, err)
	}
	if fraction.Cmp(hundred) > 0 {
		return computed, fmt.Errorf("%w: rate exceeds 100 percent", ErrTaxValidation)
	}
	computed = taxComputation{profile: profile, fraction: fraction, subtotal: gross, gross: gross, discount: discountMoney, scale: lineScale}
	return computed, nil
}

func decimalFromUnscaled(unscaled *big.Int, scale int32) (values.Decimal, error) {
	digits := new(big.Int).Abs(unscaled).String()
	if scale > 0 {
		for int32(len(digits)) <= scale {
			digits = "0" + digits
		}
		split := len(digits) - int(scale)
		digits = digits[:split] + "." + digits[split:]
	}
	if unscaled.Sign() < 0 {
		digits = "-" + digits
	}
	return values.NewDecimal(digits, scale, values.RoundingExactRequired)
}

// allocateDiscount splits a discount across line bases in exact integer
// units with the remainder on the last line, so the parts always sum
// exactly to the whole. Bases share one scale and are positive.
func allocateDiscount(bases []values.Decimal, discount values.Decimal) ([]values.Decimal, error) {
	allocs := make([]values.Decimal, len(bases))
	scale := discount.Scale()
	zero, err := values.NewDecimal("0", scale, values.RoundingExactRequired)
	if err != nil {
		return nil, err
	}
	if discount.IsZero() {
		for i := range bases {
			allocs[i] = zero
		}
		return allocs, nil
	}
	total := new(big.Int)
	for _, base := range bases {
		total = total.Add(total, base.Unscaled())
	}
	if total.Sign() <= 0 {
		return nil, fmt.Errorf("%w: discount base is empty", ErrTaxValidation)
	}
	assigned := new(big.Int)
	discountUnits := discount.Unscaled()
	for i, base := range bases {
		if i == len(bases)-1 {
			rest := new(big.Int).Sub(discountUnits, assigned)
			alloc, err := decimalFromUnscaled(rest, scale)
			if err != nil {
				return nil, err
			}
			allocs[i] = alloc
			break
		}
		share := new(big.Int).Mul(discountUnits, base.Unscaled())
		share = share.Quo(share, total)
		alloc, err := decimalFromUnscaled(share, scale)
		if err != nil {
			return nil, err
		}
		allocs[i] = alloc
		assigned = assigned.Add(assigned, share)
	}
	for i, alloc := range allocs {
		if alloc.Cmp(bases[i]) > 0 {
			return nil, fmt.Errorf("%w: discount split exceeds a line base", ErrTaxValidation)
		}
	}
	return allocs, nil
}

func sumTaxDecimals(first values.Decimal, rest ...values.Decimal) (values.Decimal, error) {
	acc := first
	var err error
	for _, next := range rest {
		acc, err = acc.Add(next)
		if err != nil {
			return values.Decimal{}, err
		}
	}
	return acc, nil
}

func taxOnBase(base, fraction values.Decimal, currency string, scale int32, mode values.RoundingMode) (values.Money, error) {
	amount, err := base.Mul(fraction, scale, mode)
	if err != nil {
		return values.Money{}, err
	}
	return values.NewMoneyFromDecimal(amount, currency)
}

// computeExclusive resolves exclusive tax: per-line rounded components
// for LINE order, one rolled-up component for INVOICE order. Discounts
// before tax reduce the base; discounts after tax reduce the payable.
func computeExclusive(computed taxComputation, lines []InvoiceLine, discount values.Decimal) ([]TaxLineComponent, values.Money, values.Money, error) {
	profile := computed.profile
	currency := profile.Currency
	scale := computed.scale
	failed := func(err error) ([]TaxLineComponent, values.Money, values.Money, error) {
		return nil, values.Money{}, values.Money{}, err
	}
	bases := make([]values.Decimal, len(lines))
	for i, line := range lines {
		bases[i] = line.Amount.Amount()
	}
	taxBase := bases
	if profile.DiscountBeforeTax && !discount.IsZero() {
		allocs, err := allocateDiscount(bases, discount)
		if err != nil {
			return failed(err)
		}
		taxBase = make([]values.Decimal, len(bases))
		for i, base := range bases {
			net, err := base.Sub(allocs[i])
			if err != nil {
				return failed(err)
			}
			taxBase[i] = net
		}
	}
	afterDiscount := func(total values.Money) (values.Money, error) {
		if profile.DiscountBeforeTax || discount.IsZero() {
			return total, nil
		}
		discountMoney, err := values.NewMoneyFromDecimal(discount, currency)
		if err != nil {
			return values.Money{}, err
		}
		return total.Sub(discountMoney)
	}
	if profile.RoundingOrder == TaxRoundInvoice {
		rollup, err := sumTaxDecimals(taxBase[0], taxBase[1:]...)
		if err != nil {
			return failed(err)
		}
		tax, err := taxOnBase(rollup, computed.fraction, currency, scale, profile.Rounding)
		if err != nil {
			return failed(err)
		}
		rollupMoney, err := values.NewMoneyFromDecimal(rollup, currency)
		if err != nil {
			return failed(err)
		}
		total, err := rollupMoney.Add(tax)
		if err != nil {
			return failed(err)
		}
		payable, err := afterDiscount(total)
		if err != nil {
			return failed(err)
		}
		components := []TaxLineComponent{{Basis: rollup.String(), Tax: tax.Amount().String(), Total: total.Amount().String()}}
		return components, tax, payable, nil
	}
	components := make([]TaxLineComponent, 0, len(lines))
	taxes := make([]values.Decimal, 0, len(lines))
	totals := make([]values.Decimal, 0, len(lines))
	for i, base := range taxBase {
		tax, err := taxOnBase(base, computed.fraction, currency, scale, profile.Rounding)
		if err != nil {
			return failed(err)
		}
		baseMoney, err := values.NewMoneyFromDecimal(base, currency)
		if err != nil {
			return failed(err)
		}
		total, err := baseMoney.Add(tax)
		if err != nil {
			return failed(err)
		}
		components = append(components, TaxLineComponent{LineID: lines[i].ID, Basis: base.String(), Tax: tax.Amount().String(), Total: total.Amount().String()})
		taxes = append(taxes, tax.Amount())
		totals = append(totals, total.Amount())
	}
	rollTax, err := sumTaxDecimals(taxes[0], taxes[1:]...)
	if err != nil {
		return failed(err)
	}
	rollTotal, err := sumTaxDecimals(totals[0], totals[1:]...)
	if err != nil {
		return failed(err)
	}
	taxMoney, err := values.NewMoneyFromDecimal(rollTax, currency)
	if err != nil {
		return failed(err)
	}
	payableMoney, err := values.NewMoneyFromDecimal(rollTotal, currency)
	if err != nil {
		return failed(err)
	}
	payableMoney, err = afterDiscount(payableMoney)
	if err != nil {
		return failed(err)
	}
	return components, taxMoney, payableMoney, nil
}

// computeInclusive decomposes inclusive tax out of gross line amounts:
// net basis first by division at the pinned rounding, tax as the exact
// difference so net plus tax always equals the billed gross.
func computeInclusive(computed taxComputation, lines []InvoiceLine, discount values.Decimal) ([]TaxLineComponent, values.Money, values.Money, error) {
	profile := computed.profile
	currency := profile.Currency
	scale := computed.scale
	failed := func(err error) ([]TaxLineComponent, values.Money, values.Money, error) {
		return nil, values.Money{}, values.Money{}, err
	}
	grosses := make([]values.Decimal, len(lines))
	for i, line := range lines {
		grosses[i] = line.Amount.Amount()
	}
	if profile.DiscountBeforeTax && !discount.IsZero() {
		allocs, err := allocateDiscount(grosses, discount)
		if err != nil {
			return failed(err)
		}
		for i, gross := range grosses {
			reduced, err := gross.Sub(allocs[i])
			if err != nil {
				return failed(err)
			}
			grosses[i] = reduced
		}
	}
	one, err := values.NewDecimal("1", computed.fraction.Scale(), values.RoundingExactRequired)
	if err != nil {
		return failed(err)
	}
	divisor, err := one.Add(computed.fraction)
	if err != nil {
		return failed(err)
	}
	if divisor.IsZero() {
		return failed(fmt.Errorf("%w: inclusive divisor is zero", ErrTaxValidation))
	}
	decompose := func(gross values.Decimal) (net, tax values.Decimal, err error) {
		net, err = gross.Div(divisor, scale, profile.Rounding)
		if err != nil {
			return values.Decimal{}, values.Decimal{}, err
		}
		tax, err = gross.Sub(net)
		if err != nil {
			return values.Decimal{}, values.Decimal{}, err
		}
		return net, tax, nil
	}
	afterDiscount := func(total values.Money) (values.Money, error) {
		if profile.DiscountBeforeTax || discount.IsZero() {
			return total, nil
		}
		discountMoney, err := values.NewMoneyFromDecimal(discount, currency)
		if err != nil {
			return values.Money{}, err
		}
		return total.Sub(discountMoney)
	}
	if profile.RoundingOrder == TaxRoundInvoice {
		rollup, err := sumTaxDecimals(grosses[0], grosses[1:]...)
		if err != nil {
			return failed(err)
		}
		net, tax, err := decompose(rollup)
		if err != nil {
			return failed(err)
		}
		components := []TaxLineComponent{{Basis: net.String(), Tax: tax.String(), Total: rollup.String()}}
		taxMoney, err := values.NewMoneyFromDecimal(tax, currency)
		if err != nil {
			return failed(err)
		}
		payableMoney, err := values.NewMoneyFromDecimal(rollup, currency)
		if err != nil {
			return failed(err)
		}
		payableMoney, err = afterDiscount(payableMoney)
		if err != nil {
			return failed(err)
		}
		return components, taxMoney, payableMoney, nil
	}
	components := make([]TaxLineComponent, 0, len(lines))
	taxes := make([]values.Decimal, 0, len(lines))
	totals := make([]values.Decimal, 0, len(lines))
	for i, gross := range grosses {
		net, tax, err := decompose(gross)
		if err != nil {
			return failed(err)
		}
		components = append(components, TaxLineComponent{LineID: lines[i].ID, Basis: net.String(), Tax: tax.String(), Total: gross.String()})
		taxes = append(taxes, tax)
		totals = append(totals, gross)
	}
	rollTax, err := sumTaxDecimals(taxes[0], taxes[1:]...)
	if err != nil {
		return failed(err)
	}
	rollGross, err := sumTaxDecimals(totals[0], totals[1:]...)
	if err != nil {
		return failed(err)
	}
	taxMoney, err := values.NewMoneyFromDecimal(rollTax, currency)
	if err != nil {
		return failed(err)
	}
	payableMoney, err := values.NewMoneyFromDecimal(rollGross, currency)
	if err != nil {
		return failed(err)
	}
	payableMoney, err = afterDiscount(payableMoney)
	if err != nil {
		return failed(err)
	}
	return components, taxMoney, payableMoney, nil
}

// verifySnapshotArithmetic rechecks every stored amount relationship with
type taxIdemRecord struct {
	Key          string `json:"key"`
	Action       string `json:"action"`
	Payload      string `json:"payload"`
	SnapshotID   string `json:"snapshot_id"`
	AdjustmentID string `json:"adjustment_id,omitempty"`
}

// TaxStore is the SMB-BILL-004 treatment registry: immutable snapshots
// bound to invoice revisions, separate correction chains, and
// tenant-scoped idempotent commands. Snapshots are never mutated once
// stored; re-resolution supersedes by sequence.
type TaxStore struct {
	mu          sync.Mutex
	resolver    TaxResolver
	seq         uint64
	snapshots   map[string]*TaxSnapshot
	byInvoice   map[string][]string
	adjustments map[string]*TaxAdjustment
	adjustChain map[string][]string
	idem        map[string]taxIdemRecord
}

// NewTaxStore returns an empty store fronting the given resolver.
func NewTaxStore(resolver TaxResolver) *TaxStore {
	return &TaxStore{
		resolver:    resolver,
		snapshots:   make(map[string]*TaxSnapshot),
		byInvoice:   make(map[string][]string),
		adjustments: make(map[string]*TaxAdjustment),
		adjustChain: make(map[string][]string),
		idem:        make(map[string]taxIdemRecord),
	}
}

func taxSnapshotKey(tenant, invoiceID string) string { return tenant + "\x00" + invoiceID }

func taxIdemKey(tenant, key string) string { return tenant + "\x00" + key }

func (s *TaxStore) latestLocked(tenant, invoiceID string) (TaxSnapshot, bool) {
	ids := s.byInvoice[taxSnapshotKey(tenant, invoiceID)]
	if len(ids) == 0 {
		return TaxSnapshot{}, false
	}
	stored, ok := s.snapshots[ids[len(ids)-1]]
	if !ok {
		return TaxSnapshot{}, false
	}
	return *stored, true
}

func validateTaxProfile(profile TaxProfile) error {
	if strings.TrimSpace(profile.ID) == "" || strings.TrimSpace(profile.Version) == "" {
		return fmt.Errorf("%w: profile identity and version are required", ErrTaxValidation)
	}
	if profile.Mode != TaxExclusive && profile.Mode != TaxInclusive {
		return fmt.Errorf("%w: mode %q is unknown", ErrTaxValidation, profile.Mode)
	}
	if profile.RoundingOrder != TaxRoundLine && profile.RoundingOrder != TaxRoundInvoice {
		return fmt.Errorf("%w: rounding order %q is unknown", ErrTaxValidation, profile.RoundingOrder)
	}
	if !validTaxRounding(profile.Rounding) {
		return fmt.Errorf("%w: rounding mode is unknown", ErrTaxValidation)
	}
	if _, err := parseRateFraction(profile.RatePercent); err != nil {
		return err
	}
	return nil
}

// assembleSnapshot computes and freezes one resolution. Profiles that
// are structurally malformed refuse with an error and store nothing;
// missing, expired, inapplicable and exempt treatments freeze to their
// honest statuses instead.
func (s *TaxStore) assembleSnapshot(tenant, actor string, inv CustomerInvoice, profile TaxProfile, profileKnown bool, discountRaw, at string, manual *ManualTaxInput) (TaxSnapshot, error) {
	var snapshot TaxSnapshot
	resolvedAt, err := parseTaxClock(at, "resolve")
	if err != nil {
		return snapshot, err
	}
	if inv.TenantID != tenant {
		return snapshot, ErrTaxDenied
	}
	base := TaxSnapshot{
		TenantID: tenant, InvoiceID: inv.ID, InvoiceRevision: inv.Revision,
		LinesDigest: taxLinesDigest(inv.Lines), ResolvedAt: at,
	}
	unresolved := func(note string, scale int32) (TaxSnapshot, error) {
		zero := zeroTaxDecimal(scale)
		out := base
		out.Status = TaxUnresolved
		out.Subtotal = zero.String()
		out.Gross = zero.String()
		out.Discount = zero.String()
		out.TotalTax = zero.String()
		out.Payable = zero.String()
		out.Currency = inv.Currency
		out.Scale = scale
		out.Note = note
		return out, nil
	}
	if manual != nil {
		return s.assembleManual(base, inv, *manual, actor, at)
	}
	if !profileKnown {
		return unresolved(fmt.Sprintf("no tax policy published for %q; select an approved policy and re-resolve", inv.TaxPolicyRef), invoiceTaxScale(inv))
	}
	if err := validateTaxProfile(profile); err != nil {
		return snapshot, err
	}
	base.ProfileID = profile.ID
	base.ProfileVersion = profile.Version
	base.Mode = profile.Mode
	base.RatePercent = profile.RatePercent
	base.RoundingOrder = profile.RoundingOrder
	base.Currency = profile.Currency
	base.Scale = profile.Scale
	base.EvidenceRef = profile.EvidenceRef
	base.Jurisdictions = append([]string(nil), profile.Jurisdictions...)
	base.NotAfter = profile.NotAfter
	if strings.TrimSpace(profile.NotAfter) != "" {
		expiry, err := parseTaxClock(profile.NotAfter, "profile expiry")
		if err != nil {
			return snapshot, err
		}
		now := resolvedAt
		if !now.Before(expiry) && !now.Equal(expiry) {
			out := base
			zero := zeroTaxDecimal(profile.Scale)
			out.Status = TaxStale
			out.Subtotal = zero.String()
			out.Gross = grossTaxString(inv, profile.Scale)
			out.Discount = zero.String()
			out.TotalTax = zero.String()
			out.Payable = zero.String()
			out.Note = fmt.Sprintf("policy %q expired at %s; select a current publication and re-resolve", profile.ID, profile.NotAfter)
			return out, nil
		}
	}
	computed, err := prepareTaxComputation(inv, profile, discountRaw, at)
	if err != nil {
		return snapshot, err
	}
	fraction := computed.fraction
	zeroRate := fraction.IsZero()
	switch {
	case !profile.Applicable:
		if !zeroRate {
			return snapshot, fmt.Errorf("%w: inapplicable profile carries a nonzero rate", ErrTaxValidation)
		}
		return s.zeroSnapshot(base, inv, computed, profile, TaxNotApplicable, "")
	case profile.Exempt:
		if !zeroRate {
			return snapshot, fmt.Errorf("%w: exempt profile carries a nonzero rate", ErrTaxValidation)
		}
		if strings.TrimSpace(profile.ExemptReason) == "" || strings.TrimSpace(profile.EvidenceRef) == "" {
			return snapshot, fmt.Errorf("%w: exemption needs a reason and evidence", ErrTaxValidation)
		}
		return s.zeroSnapshot(base, inv, computed, profile, TaxExempt, profile.ExemptReason)
	case zeroRate:
		out := base
		zero := zeroTaxDecimal(profile.Scale)
		out.Status = TaxUnresolved
		out.Subtotal = computed.subtotal.Amount().String()
		out.Gross = out.Subtotal
		out.Discount = computed.discount.Amount().String()
		out.TotalTax = zero.String()
		out.Payable = zero.String()
		out.Note = "zero rate without exemption evidence; attest an exemption or select a rated policy"
		return out, nil
	}
	discount := computed.discount.Amount()
	var components []TaxLineComponent
	var tax, payable values.Money
	if profile.Mode == TaxInclusive {
		components, tax, payable, err = computeInclusive(computed, inv.Lines, discount)
	} else {
		components, tax, payable, err = computeExclusive(computed, inv.Lines, discount)
	}
	if err != nil {
		return snapshot, fmt.Errorf("%w: compute treatment: %v", ErrTaxValidation, err)
	}
	subtotal := zeroTaxDecimal(profile.Scale)
	for _, component := range components {
		basis, err := parseTaxDecimal(component.Basis, profile.Scale)
		if err != nil {
			return snapshot, err
		}
		subtotal, err = subtotal.Add(basis)
		if err != nil {
			return snapshot, fmt.Errorf("%w: sum bases: %v", ErrTaxValidation, err)
		}
	}
	out := base
	out.Status = TaxResolved
	out.Components = components
	out.Gross = computed.gross.Amount().String()
	out.Subtotal = subtotal.String()
	out.Discount = discount.String()
	out.TotalTax = tax.Amount().String()
	out.Payable = payable.Amount().String()
	if err := verifySnapshotArithmetic(out); err != nil {
		return snapshot, err
	}
	return out, nil
}

func (s *TaxStore) zeroSnapshot(base TaxSnapshot, inv CustomerInvoice, computed taxComputation, profile TaxProfile, status TaxStatus, reason string) (TaxSnapshot, error) {
	var snapshot TaxSnapshot
	zero := zeroTaxDecimal(profile.Scale)
	discount := computed.discount.Amount()
	if profile.RoundingOrder == TaxRoundInvoice {
		baseMoney := computed.gross.Amount()
		if profile.DiscountBeforeTax {
			reduced, err := baseMoney.Sub(discount)
			if err != nil {
				return snapshot, err
			}
			baseMoney = reduced
		}
		base.Components = []TaxLineComponent{{Basis: baseMoney.String(), Tax: zero.String(), Total: baseMoney.String()}}
	} else {
		bases := make([]values.Decimal, len(inv.Lines))
		for i, line := range inv.Lines {
			bases[i] = line.Amount.Amount()
		}
		if profile.DiscountBeforeTax && !discount.IsZero() {
			allocs, err := allocateDiscount(bases, discount)
			if err != nil {
				return snapshot, err
			}
			for i, base := range bases {
				reduced, err := base.Sub(allocs[i])
				if err != nil {
					return snapshot, err
				}
				bases[i] = reduced
			}
		}
		for i, basis := range bases {
			base.Components = append(base.Components, TaxLineComponent{LineID: inv.Lines[i].ID, Basis: basis.String(), Tax: zero.String(), Total: basis.String()})
		}
	}
	subtotal := zero
	for _, component := range base.Components {
		basis, err := parseTaxDecimal(component.Basis, profile.Scale)
		if err != nil {
			return snapshot, err
		}
		subtotal, err = subtotal.Add(basis)
		if err != nil {
			return snapshot, err
		}
	}
	payable := subtotal
	if !profile.DiscountBeforeTax && !discount.IsZero() {
		reduced, err := subtotal.Sub(discount)
		if err != nil {
			return snapshot, err
		}
		payable = reduced
	}
	out := base
	out.Status = status
	out.Gross = computed.gross.Amount().String()
	out.Subtotal = subtotal.String()
	out.Discount = discount.String()
	out.TotalTax = zero.String()
	out.Payable = payable.String()
	out.ExemptReason = reason
	if err := verifySnapshotArithmetic(out); err != nil {
		return snapshot, err
	}
	return out, nil
}

func (s *TaxStore) assembleManual(base TaxSnapshot, inv CustomerInvoice, input ManualTaxInput, actor, at string) (TaxSnapshot, error) {
	var snapshot TaxSnapshot
	profile := TaxProfile{
		ID: "MANUAL", Version: at, Mode: input.Mode, RatePercent: input.RatePercent,
		Rounding: input.Rounding, RoundingOrder: input.RoundingOrder,
		Currency: input.Currency, Scale: input.Scale, DiscountBeforeTax: true,
		Applicable: true,
	}
	if input.Mode != TaxExclusive && input.Mode != TaxInclusive {
		return snapshot, fmt.Errorf("%w: manual mode %q is unknown", ErrTaxValidation, input.Mode)
	}
	if input.RoundingOrder != TaxRoundLine && input.RoundingOrder != TaxRoundInvoice {
		return snapshot, fmt.Errorf("%w: manual rounding order %q is unknown", ErrTaxValidation, input.RoundingOrder)
	}
	if !validTaxRounding(input.Rounding) {
		return snapshot, fmt.Errorf("%w: manual rounding mode is unknown", ErrTaxValidation)
	}
	if strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.Evidence) == "" {
		return snapshot, fmt.Errorf("%w: manual determination needs a reason and evidence", ErrTaxValidation)
	}
	computed, err := prepareTaxComputation(inv, profile, "", at)
	if err != nil {
		return snapshot, err
	}
	if computed.fraction.IsZero() {
		return snapshot, fmt.Errorf("%w: manual rate is zero without exemption", ErrTaxValidation)
	}
	out := base
	out.ProfileID = "MANUAL"
	out.ProfileVersion = at
	out.Mode = input.Mode
	out.RatePercent = input.RatePercent
	out.RoundingOrder = input.RoundingOrder
	out.Currency = input.Currency
	out.Scale = input.Scale
	out.Manual = true
	out.ManualBy = actor
	out.ManualReason = input.Reason
	out.ManualEvidence = input.Evidence
	if !input.Approved {
		zero := zeroTaxDecimal(input.Scale)
		out.Status = TaxUnresolved
		out.Subtotal = zero.String()
		out.Gross = zero.String()
		out.Discount = zero.String()
		out.TotalTax = zero.String()
		out.Payable = zero.String()
		out.Note = "manual determination awaits policy approval"
		return out, nil
	}
	zero := zeroTaxDecimal(input.Scale)
	discount := zero
	var components []TaxLineComponent
	var tax, payable values.Money
	if input.Mode == TaxInclusive {
		components, tax, payable, err = computeInclusive(computed, inv.Lines, discount)
	} else {
		components, tax, payable, err = computeExclusive(computed, inv.Lines, discount)
	}
	if err != nil {
		return snapshot, fmt.Errorf("%w: compute manual treatment: %v", ErrTaxValidation, err)
	}
	subtotal := zero
	for _, component := range components {
		basis, err := parseTaxDecimal(component.Basis, input.Scale)
		if err != nil {
			return snapshot, err
		}
		subtotal, err = subtotal.Add(basis)
		if err != nil {
			return snapshot, err
		}
	}
	out.Status = TaxResolved
	out.Components = components
	out.Gross = computed.gross.Amount().String()
	out.Subtotal = subtotal.String()
	out.Discount = zero.String()
	out.TotalTax = tax.Amount().String()
	out.Payable = payable.String()
	if err := verifySnapshotArithmetic(out); err != nil {
		return snapshot, err
	}
	return out, nil
}

func invoiceTaxScale(inv CustomerInvoice) int32 {
	if len(inv.Lines) == 0 {
		return 2
	}
	return inv.Lines[0].Amount.Amount().Scale()
}

func grossTaxString(inv CustomerInvoice, scale int32) string {
	zero := zeroTaxDecimal(scale)
	acc := zero
	for _, line := range inv.Lines {
		next, err := acc.Add(line.Amount.Amount())
		if err != nil {
			return zero.String()
		}
		acc = next
	}
	return acc.String()
}

// verifySnapshotArithmetic rechecks every stored amount relationship with
// exact decimal arithmetic. Unresolved and stale snapshots carry zeros,
// no components and a next action. Computed snapshots satisfy the
// disjunction of the before-tax and after-tax discount shapes, which
// are mutually exclusive for nonzero discounts, plus the mode equation
// against billed gross. Both the compute path and Import enforce it.
func verifySnapshotArithmetic(snapshot TaxSnapshot) error {
	parse := func(raw, label string) (values.Decimal, error) {
		dec, err := parseTaxDecimal(raw, snapshot.Scale)
		if err != nil {
			return values.Decimal{}, fmt.Errorf("%w: %s: %v", ErrTaxValidation, label, err)
		}
		return dec, nil
	}
	gross, err := parse(snapshot.Gross, "gross")
	if err != nil {
		return err
	}
	subtotal, err := parse(snapshot.Subtotal, "subtotal")
	if err != nil {
		return err
	}
	discount, err := parse(snapshot.Discount, "discount")
	if err != nil {
		return err
	}
	totalTax, err := parse(snapshot.TotalTax, "total tax")
	if err != nil {
		return err
	}
	payable, err := parse(snapshot.Payable, "payable")
	if err != nil {
		return err
	}
	switch snapshot.Status {
	case TaxUnresolved, TaxStale:
		if !totalTax.IsZero() || !payable.IsZero() {
			return fmt.Errorf("%w: %s snapshot carries definitive tax or payable", ErrTaxValidation, snapshot.Status)
		}
		if len(snapshot.Components) != 0 {
			return fmt.Errorf("%w: %s snapshot carries components", ErrTaxValidation, snapshot.Status)
		}
		if strings.TrimSpace(snapshot.Note) == "" {
			return fmt.Errorf("%w: %s snapshot names no next action", ErrTaxValidation, snapshot.Status)
		}
		return nil
	case TaxResolved, TaxExempt, TaxNotApplicable:
	default:
		return fmt.Errorf("%w: status %q is unknown", ErrTaxValidation, snapshot.Status)
	}
	if len(snapshot.Components) == 0 {
		return fmt.Errorf("%w: computed snapshot has no components", ErrTaxValidation)
	}
	basisSum := zeroTaxDecimal(snapshot.Scale)
	taxSum := zeroTaxDecimal(snapshot.Scale)
	totalSum := zeroTaxDecimal(snapshot.Scale)
	for _, component := range snapshot.Components {
		basis, err := parse(component.Basis, "component basis")
		if err != nil {
			return err
		}
		tax, err := parse(component.Tax, "component tax")
		if err != nil {
			return err
		}
		total, err := parse(component.Total, "component total")
		if err != nil {
			return err
		}
		joined, err := basis.Add(tax)
		if err != nil || !joined.Equal(total) {
			return fmt.Errorf("%w: component %q basis plus tax does not equal total", ErrTaxValidation, component.LineID)
		}
		basisSum, err = basisSum.Add(basis)
		if err != nil {
			return fmt.Errorf("%w: sum bases: %v", ErrTaxValidation, err)
		}
		taxSum, err = taxSum.Add(tax)
		if err != nil {
			return fmt.Errorf("%w: sum taxes: %v", ErrTaxValidation, err)
		}
		totalSum, err = totalSum.Add(total)
		if err != nil {
			return fmt.Errorf("%w: sum totals: %v", ErrTaxValidation, err)
		}
	}
	if !basisSum.Equal(subtotal) || !taxSum.Equal(totalTax) {
		return fmt.Errorf("%w: component sums disagree with rolled-up money", ErrTaxValidation)
	}
	expected, err := subtotal.Add(totalTax)
	if err != nil {
		return fmt.Errorf("%w: subtotal plus tax: %v", ErrTaxValidation, err)
	}
	withDiscount, err := payable.Add(discount)
	if err != nil {
		return fmt.Errorf("%w: payable plus discount: %v", ErrTaxValidation, err)
	}
	beforeShape := totalSum.Equal(payable) && payable.Equal(expected)
	afterShape := totalSum.Equal(expected) && withDiscount.Equal(expected)
	if !beforeShape && !afterShape {
		return fmt.Errorf("%w: component totals disagree with payable and discount", ErrTaxValidation)
	}
	switch snapshot.Mode {
	case TaxExclusive:
		beforeGross, err := subtotal.Add(discount)
		if err != nil {
			return fmt.Errorf("%w: exclusive base: %v", ErrTaxValidation, err)
		}
		beforeOK := beforeShape && beforeGross.Equal(gross)
		afterOK := afterShape && subtotal.Equal(gross)
		if !beforeOK && !afterOK {
			return fmt.Errorf("%w: exclusive base disagrees with billed gross", ErrTaxValidation)
		}
	case TaxInclusive:
		beforeGross, err := totalSum.Add(discount)
		if err != nil {
			return fmt.Errorf("%w: inclusive base: %v", ErrTaxValidation, err)
		}
		beforeOK := beforeShape && beforeGross.Equal(gross)
		afterOK := afterShape && totalSum.Equal(gross)
		if !beforeOK && !afterOK {
			return fmt.Errorf("%w: inclusive base disagrees with billed gross", ErrTaxValidation)
		}
	default:
		return fmt.Errorf("%w: mode %q is unknown", ErrTaxValidation, snapshot.Mode)
	}
	return nil
}

func taxPayload(parts ...string) string { return hashPayload(parts...) }

// Resolve freezes one treatment for an invoice revision. The same
// tenant-scoped key with the same payload replays the original receipt;
// a reused key with a changed payload is a conflict. Refusals store
// nothing. Missing or expired treatments freeze to honest UNRESOLVED
// or STALE snapshots that block issuance with a next action.
func (s *TaxStore) Resolve(ctx context.Context, tenant, actor string, caps TaxCaps, inv CustomerInvoice, input ResolveTaxInput) (TaxSnapshot, TaxReceipt, error) {
	var empty TaxSnapshot
	if err := ctx.Err(); err != nil {
		return empty, TaxReceipt{}, err
	}
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(actor) == "" || strings.TrimSpace(input.Key) == "" {
		return empty, TaxReceipt{}, fmt.Errorf("%w: tenant, actor and idempotency key are required", ErrTaxValidation)
	}
	if !caps.CanResolve {
		return empty, TaxReceipt{}, ErrTaxDenied
	}
	if inv.TenantID != tenant || inv.ID != input.InvoiceID {
		return empty, TaxReceipt{}, ErrTaxDenied
	}
	if strings.TrimSpace(input.PolicyRef) == "" {
		return empty, TaxReceipt{}, fmt.Errorf("%w: policy reference is required", ErrTaxValidation)
	}
	profile, err := s.resolver.TreatmentProfile(ctx, tenant, input.PolicyRef, input.At)
	known := err == nil
	if err != nil && !errors.Is(err, ErrTaxProfileNotFound) {
		return empty, TaxReceipt{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	payload := taxPayload("resolve", tenant, inv.ID, fmt.Sprint(input.InvoiceRevision), input.PolicyRef, input.Discount, input.At, taxLinesDigest(inv.Lines))
	key := taxIdemKey(tenant, input.Key)
	if prior, ok := s.idem[key]; ok {
		if prior.Payload != payload {
			return empty, TaxReceipt{}, fmt.Errorf("%w: resolve key %q was used with a different payload", ErrTaxConflict, input.Key)
		}
		stored, ok := s.snapshots[prior.SnapshotID]
		if !ok {
			return empty, TaxReceipt{}, fmt.Errorf("%w: resolve receipt references an unknown snapshot", ErrTaxConflict)
		}
		return *stored, TaxReceipt{Key: input.Key, TenantID: tenant, InvoiceID: inv.ID, SnapshotID: stored.ID, Action: "resolve", Payload: payload}, nil
	}
	if input.InvoiceRevision != inv.Revision {
		return empty, TaxReceipt{}, fmt.Errorf("%w: expected invoice revision %d, current is %d", ErrTaxConflict, input.InvoiceRevision, inv.Revision)
	}
	snapshot, err := s.assembleSnapshot(tenant, actor, inv, profile, known, input.Discount, input.At, nil)
	if err != nil {
		return empty, TaxReceipt{}, err
	}
	s.seq++
	snapshot.Sequence = s.seq
	snapshot.ID = fmt.Sprintf("tax-%06d", s.seq)
	snapshot.Digest = taxSnapshotDigest(snapshot)
	stored := snapshot
	s.snapshots[stored.ID] = &stored
	s.byInvoice[taxSnapshotKey(tenant, inv.ID)] = append(s.byInvoice[taxSnapshotKey(tenant, inv.ID)], stored.ID)
	s.idem[key] = taxIdemRecord{Key: input.Key, Action: "resolve", Payload: payload, SnapshotID: stored.ID}
	return stored, TaxReceipt{Key: input.Key, TenantID: tenant, InvoiceID: inv.ID, SnapshotID: stored.ID, Action: "resolve", Payload: payload}, nil
}

// ResolveManual freezes one human determination. Unapproved
// determinations resolve UNRESOLVED and route to policy approval;
// unattested assertions refuse without storing anything.
func (s *TaxStore) ResolveManual(ctx context.Context, tenant, actor string, caps TaxCaps, inv CustomerInvoice, input ManualTaxInput) (TaxSnapshot, TaxReceipt, error) {
	var empty TaxSnapshot
	if err := ctx.Err(); err != nil {
		return empty, TaxReceipt{}, err
	}
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(actor) == "" || strings.TrimSpace(input.Key) == "" {
		return empty, TaxReceipt{}, fmt.Errorf("%w: tenant, actor and idempotency key are required", ErrTaxValidation)
	}
	if !caps.CanResolve {
		return empty, TaxReceipt{}, ErrTaxDenied
	}
	if inv.TenantID != tenant || inv.ID != input.InvoiceID {
		return empty, TaxReceipt{}, ErrTaxDenied
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	payload := taxPayload("manual", tenant, inv.ID, fmt.Sprint(input.InvoiceRevision), input.RatePercent, string(input.Mode), string(input.RoundingOrder), input.Currency, fmt.Sprint(input.Scale), input.Reason, input.Evidence, fmt.Sprint(input.Approved), input.At, taxLinesDigest(inv.Lines))
	key := taxIdemKey(tenant, input.Key)
	if prior, ok := s.idem[key]; ok {
		if prior.Payload != payload {
			return empty, TaxReceipt{}, fmt.Errorf("%w: manual key %q was used with a different payload", ErrTaxConflict, input.Key)
		}
		stored, ok := s.snapshots[prior.SnapshotID]
		if !ok {
			return empty, TaxReceipt{}, fmt.Errorf("%w: manual receipt references an unknown snapshot", ErrTaxConflict)
		}
		return *stored, TaxReceipt{Key: input.Key, TenantID: tenant, InvoiceID: inv.ID, SnapshotID: stored.ID, Action: "manual", Payload: payload}, nil
	}
	if input.InvoiceRevision != inv.Revision {
		return empty, TaxReceipt{}, fmt.Errorf("%w: expected invoice revision %d, current is %d", ErrTaxConflict, input.InvoiceRevision, inv.Revision)
	}
	snapshot, err := s.assembleSnapshot(tenant, actor, inv, TaxProfile{}, false, "", input.At, &input)
	if err != nil {
		return empty, TaxReceipt{}, err
	}
	s.seq++
	snapshot.Sequence = s.seq
	snapshot.ID = fmt.Sprintf("tax-%06d", s.seq)
	snapshot.Digest = taxSnapshotDigest(snapshot)
	stored := snapshot
	s.snapshots[stored.ID] = &stored
	s.byInvoice[taxSnapshotKey(tenant, inv.ID)] = append(s.byInvoice[taxSnapshotKey(tenant, inv.ID)], stored.ID)
	s.idem[key] = taxIdemRecord{Key: input.Key, Action: "manual", Payload: payload, SnapshotID: stored.ID}
	return stored, TaxReceipt{Key: input.Key, TenantID: tenant, InvoiceID: inv.ID, SnapshotID: stored.ID, Action: "manual", Payload: payload}, nil
}

// Correct re-resolves one invoice and chains the correction as an
// adjustment with signed deltas. The original snapshot is never
// rewritten; the corrected resolution supersedes by sequence. The
// expected chain length is compared under the store lock before any
// write, so exactly one contender wins a chain race.
func (s *TaxStore) Correct(ctx context.Context, tenant, actor string, caps TaxCaps, inv CustomerInvoice, input CorrectTaxInput) (TaxAdjustment, TaxReceipt, error) {
	var empty TaxAdjustment
	if err := ctx.Err(); err != nil {
		return empty, TaxReceipt{}, err
	}
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(actor) == "" || strings.TrimSpace(input.Key) == "" {
		return empty, TaxReceipt{}, fmt.Errorf("%w: tenant, actor and idempotency key are required", ErrTaxValidation)
	}
	if !caps.CanCorrect {
		return empty, TaxReceipt{}, ErrTaxDenied
	}
	if inv.TenantID != tenant || inv.ID != input.InvoiceID {
		return empty, TaxReceipt{}, ErrTaxDenied
	}
	if strings.TrimSpace(input.Reason) == "" {
		return empty, TaxReceipt{}, fmt.Errorf("%w: correction reason is required", ErrTaxValidation)
	}
	if _, err := parseTaxClock(input.At, "correct"); err != nil {
		return empty, TaxReceipt{}, err
	}
	if (input.Resolve == nil) == (input.Manual == nil) {
		return empty, TaxReceipt{}, fmt.Errorf("%w: correction carries exactly one of resolve or manual treatment", ErrTaxValidation)
	}
	var preProfile TaxProfile
	preKnown := false
	if input.Resolve != nil {
		profile, err := s.resolver.TreatmentProfile(ctx, tenant, input.Resolve.PolicyRef, input.Resolve.At)
		if err != nil && !errors.Is(err, ErrTaxProfileNotFound) {
			return empty, TaxReceipt{}, err
		}
		preProfile, preKnown = profile, err == nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	payload := taxPayload("correct", tenant, inv.ID, fmt.Sprint(input.ExpectedChainLength), input.Reason, actor, input.At, taxLinesDigest(inv.Lines))
	key := taxIdemKey(tenant, input.Key)
	if prior, ok := s.idem[key]; ok {
		if prior.Payload != payload {
			return empty, TaxReceipt{}, fmt.Errorf("%w: correct key %q was used with a different payload", ErrTaxConflict, input.Key)
		}
		stored, ok := s.adjustments[prior.AdjustmentID]
		if !ok {
			return empty, TaxReceipt{}, fmt.Errorf("%w: correct receipt references an unknown adjustment", ErrTaxConflict)
		}
		return *stored, TaxReceipt{Key: input.Key, TenantID: tenant, InvoiceID: inv.ID, SnapshotID: stored.Corrected.ID, AdjustmentID: stored.ID, Action: "correct", Payload: payload}, nil
	}
	original, ok := s.latestLocked(tenant, inv.ID)
	if !ok {
		return empty, TaxReceipt{}, fmt.Errorf("%w: invoice has no treatment to correct", ErrTaxValidation)
	}
	chainKey := taxSnapshotKey(tenant, inv.ID)
	if input.ExpectedChainLength != len(s.adjustChain[chainKey]) {
		return empty, TaxReceipt{}, fmt.Errorf("%w: expected chain length %d, current is %d", ErrTaxConflict, input.ExpectedChainLength, len(s.adjustChain[chainKey]))
	}
	var corrected TaxSnapshot
	var innerKey string
	if input.Manual != nil {
		innerKey = input.Manual.Key
		innerPayload := taxPayload("manual", tenant, inv.ID, fmt.Sprint(input.Manual.InvoiceRevision), input.Manual.RatePercent, string(input.Manual.Mode), string(input.Manual.RoundingOrder), input.Manual.Currency, fmt.Sprint(input.Manual.Scale), input.Manual.Reason, input.Manual.Evidence, fmt.Sprint(input.Manual.Approved), input.Manual.At, taxLinesDigest(inv.Lines))
		if prior, seen := s.idem[taxIdemKey(tenant, innerKey)]; seen && prior.Payload != innerPayload {
			return empty, TaxReceipt{}, fmt.Errorf("%w: correction treatment key was used with a different payload", ErrTaxConflict)
		}
		if input.Manual.InvoiceRevision != inv.Revision {
			return empty, TaxReceipt{}, fmt.Errorf("%w: correction treatment revision is stale", ErrTaxConflict)
		}
		var err error
		corrected, err = s.assembleSnapshot(tenant, actor, inv, TaxProfile{}, false, "", input.Manual.At, input.Manual)
		if err != nil {
			return empty, TaxReceipt{}, err
		}
		s.idem[taxIdemKey(tenant, innerKey)] = taxIdemRecord{Key: innerKey, Action: "manual", Payload: innerPayload, SnapshotID: ""}
	} else {
		inner := *input.Resolve
		innerKey = inner.Key
		innerPayload := taxPayload("resolve", tenant, inv.ID, fmt.Sprint(inner.InvoiceRevision), inner.PolicyRef, inner.Discount, inner.At, taxLinesDigest(inv.Lines))
		if prior, seen := s.idem[taxIdemKey(tenant, innerKey)]; seen && prior.Payload != innerPayload {
			return empty, TaxReceipt{}, fmt.Errorf("%w: correction treatment key was used with a different payload", ErrTaxConflict)
		}
		if inner.InvoiceRevision != inv.Revision {
			return empty, TaxReceipt{}, fmt.Errorf("%w: correction treatment revision is stale", ErrTaxConflict)
		}
		profile, known := preProfile, preKnown
		correctedInner, err := s.assembleSnapshot(tenant, actor, inv, profile, known, inner.Discount, inner.At, nil)
		if err != nil {
			return empty, TaxReceipt{}, err
		}
		corrected = correctedInner
		s.idem[taxIdemKey(tenant, innerKey)] = taxIdemRecord{Key: innerKey, Action: "resolve", Payload: innerPayload, SnapshotID: ""}
	}
	switch corrected.Status {
	case TaxResolved, TaxExempt, TaxNotApplicable:
	default:
		delete(s.idem, taxIdemKey(tenant, innerKey))
		return empty, TaxReceipt{}, fmt.Errorf("%w: corrected treatment is %s, want a decisive status", ErrTaxValidation, corrected.Status)
	}
	s.seq++
	corrected.Sequence = s.seq
	corrected.ID = fmt.Sprintf("tax-%06d", s.seq)
	corrected.Digest = taxSnapshotDigest(corrected)
	s.snapshots[corrected.ID] = &corrected
	s.byInvoice[chainKey] = append(s.byInvoice[chainKey], corrected.ID)
	innerRecord := s.idem[taxIdemKey(tenant, innerKey)]
	innerRecord.SnapshotID = corrected.ID
	s.idem[taxIdemKey(tenant, innerKey)] = innerRecord
	deltaTax, err := taxDelta(original.TotalTax, corrected.TotalTax, corrected.Scale)
	if err != nil {
		return empty, TaxReceipt{}, err
	}
	deltaPayable, err := taxDelta(original.Payable, corrected.Payable, corrected.Scale)
	if err != nil {
		return empty, TaxReceipt{}, err
	}
	s.seq++
	adjustment := TaxAdjustment{
		ID: fmt.Sprintf("taxadj-%06d", s.seq), Sequence: s.seq,
		TenantID: tenant, InvoiceID: inv.ID, OriginalDigest: original.Digest, Corrected: corrected,
		DeltaTax: deltaTax, DeltaPayable: deltaPayable, Reason: input.Reason, Actor: actor, At: input.At,
	}
	s.adjustments[adjustment.ID] = &adjustment
	s.adjustChain[chainKey] = append(s.adjustChain[chainKey], adjustment.ID)
	s.idem[key] = taxIdemRecord{Key: input.Key, Action: "correct", Payload: payload, AdjustmentID: adjustment.ID}
	return adjustment, TaxReceipt{Key: input.Key, TenantID: tenant, InvoiceID: inv.ID, SnapshotID: corrected.ID, AdjustmentID: adjustment.ID, Action: "correct", Payload: payload}, nil
}

// SnapshotOf returns the latest snapshot for one invoice.
func (s *TaxStore) SnapshotOf(tenant, invoiceID string) (TaxSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.latestLocked(tenant, invoiceID)
}

// SnapshotsOf returns every snapshot for one invoice in sequence order.
func (s *TaxStore) SnapshotsOf(tenant, invoiceID string) []TaxSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.byInvoice[taxSnapshotKey(tenant, invoiceID)]
	out := make([]TaxSnapshot, 0, len(ids))
	for _, id := range ids {
		if stored, ok := s.snapshots[id]; ok {
			out = append(out, *stored)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sequence < out[j].Sequence })
	return out
}

// AdjustmentsOf returns the correction chain for one invoice in
// sequence order.
func (s *TaxStore) AdjustmentsOf(tenant, invoiceID string) []TaxAdjustment {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.adjustChain[taxSnapshotKey(tenant, invoiceID)]
	out := make([]TaxAdjustment, 0, len(ids))
	for _, id := range ids {
		if stored, ok := s.adjustments[id]; ok {
			out = append(out, *stored)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sequence < out[j].Sequence })
	return out
}

// ValidateForIssue enforces treatment before issuance: the latest
// snapshot for the invoice must exist, match the invoice lines and
// named policy, verify its digest, stay within its
// treatment window, and carry a decisive status. Manual snapshots
// supersede the named policy; they are visibly manual. Anything else
// blocks with the next action; cross-tenant and unknown references
// refuse neutrally.
func (s *TaxStore) ValidateForIssue(tenant string, inv CustomerInvoice, at string) error {
	if strings.TrimSpace(tenant) == "" {
		return ErrTaxDenied
	}
	if inv.TenantID != tenant {
		return ErrTaxDenied
	}
	moment, err := parseTaxClock(at, "issue")
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, ok := s.latestLocked(tenant, inv.ID)
	if !ok {
		return fmt.Errorf("%w: no tax treatment resolved for invoice %q; resolve before issue", ErrTaxUnresolved, inv.ID)
	}
	if snapshot.TenantID != tenant {
		return ErrTaxDenied
	}
	if snapshot.LinesDigest != taxLinesDigest(inv.Lines) {
		return fmt.Errorf("%w: invoice lines changed since resolution; re-resolve", ErrTaxStale)
	}
	decisive := snapshot.Status == TaxResolved || snapshot.Status == TaxExempt || snapshot.Status == TaxNotApplicable
	if decisive && !snapshot.Manual && strings.TrimSpace(inv.TaxPolicyRef) != "" && inv.TaxPolicyRef != snapshot.ProfileID {
		return fmt.Errorf("%w: invoice names policy %q, treatment resolved under %q; re-resolve", ErrTaxStale, inv.TaxPolicyRef, snapshot.ProfileID)
	}
	if snapshot.Digest != taxSnapshotDigest(snapshot) {
		return fmt.Errorf("%w: treatment snapshot integrity failed; re-resolve", ErrTaxStale)
	}
	if strings.TrimSpace(snapshot.NotAfter) != "" {
		expiry, err := parseTaxClock(snapshot.NotAfter, "treatment expiry")
		if err != nil {
			return fmt.Errorf("%w: treatment expiry is malformed; re-resolve", ErrTaxStale)
		}
		if moment.After(expiry) {
			return fmt.Errorf("%w: treatment expired at %s; re-resolve under a current publication", ErrTaxStale, snapshot.NotAfter)
		}
	}
	switch snapshot.Status {
	case TaxResolved, TaxExempt, TaxNotApplicable:
		return nil
	case TaxUnresolved:
		return fmt.Errorf("%w: %s", ErrTaxUnresolved, snapshot.Note)
	case TaxStale:
		return fmt.Errorf("%w: %s", ErrTaxStale, snapshot.Note)
	default:
		return fmt.Errorf("%w: treatment status %q is unknown; re-resolve", ErrTaxStale, snapshot.Status)
	}
}

// Preview computes the resolve projection without storing anything: the
// draft display path. Malformed inputs refuse; missing treatments
// preview UNRESOLVED with a pending display total.
func (s *TaxStore) Preview(ctx context.Context, tenant string, inv CustomerInvoice, policyRef, discount, at string) (TaxPreview, error) {
	var preview TaxPreview
	if err := ctx.Err(); err != nil {
		return preview, err
	}
	if strings.TrimSpace(tenant) == "" {
		return preview, ErrTaxDenied
	}
	if inv.TenantID != tenant {
		return preview, ErrTaxDenied
	}
	if strings.TrimSpace(policyRef) == "" {
		return preview, fmt.Errorf("%w: policy reference is required", ErrTaxValidation)
	}
	profile, err := s.resolver.TreatmentProfile(ctx, tenant, policyRef, at)
	known := err == nil
	if err != nil && !errors.Is(err, ErrTaxProfileNotFound) {
		return preview, err
	}
	snapshot, err := s.assembleSnapshot(tenant, "", inv, profile, known, discount, at, nil)
	if err != nil {
		return preview, err
	}
	preview = TaxPreview{
		Status: snapshot.Status, Subtotal: snapshot.Subtotal, TotalTax: snapshot.TotalTax,
		Payable: snapshot.Payable, Components: snapshot.Components, Currency: snapshot.Currency,
		Note: snapshot.Note,
	}
	switch snapshot.Status {
	case TaxUnresolved, TaxStale:
		preview.DisplayTotal = "Tax pending"
	default:
		preview.DisplayTotal = snapshot.Currency + " " + snapshot.Payable
	}
	return preview, nil
}

// ProjectTaxSection builds the invoice tax display: status, basis and
// components with a pending display while treatment is missing or
// undecided. A nil snapshot displays pending, never zero.
func ProjectTaxSection(inv CustomerInvoice, snapshot *TaxSnapshot) TaxSection {
	if snapshot == nil {
		return TaxSection{Status: TaxUnresolved, DisplayTotal: "Tax pending", Pending: true}
	}
	section := TaxSection{
		Status: snapshot.Status, Basis: snapshot.Subtotal, Mode: snapshot.Mode,
		Components: append([]TaxLineComponent(nil), snapshot.Components...),
		SourceDate: snapshot.ResolvedAt,
	}
	switch snapshot.Status {
	case TaxUnresolved, TaxStale:
		section.DisplayTotal = "Tax pending"
		section.Pending = true
	default:
		section.DisplayTotal = snapshot.Currency + " " + snapshot.Payable
	}
	return section
}

// ProjectTreatmentDetail builds the finance-authorized treatment view:
// profile, reason and evidence without protected provider payload.
func ProjectTreatmentDetail(snapshot TaxSnapshot) TaxTreatmentDetail {
	detail := TaxTreatmentDetail{
		ProfileID: snapshot.ProfileID, ProfileVersion: snapshot.ProfileVersion,
		EvidenceRef: snapshot.EvidenceRef, Jurisdictions: append([]string(nil), snapshot.Jurisdictions...),
		Manual: snapshot.Manual, ResolvedAt: snapshot.ResolvedAt,
	}
	switch {
	case snapshot.Manual:
		detail.Reason = snapshot.ManualReason
		detail.EvidenceRef = snapshot.ManualEvidence
	case snapshot.Status == TaxExempt:
		detail.Reason = snapshot.ExemptReason
	}
	return detail
}

// ProjectRecalculation compares two resolutions and flags renewed issue
// approval when totals move.
func ProjectRecalculation(before, after TaxSnapshot) TaxRecalculation {
	recalc := TaxRecalculation{
		BeforePayable: before.Payable, AfterPayable: after.Payable,
		BeforeTax: before.TotalTax, AfterTax: after.TotalTax,
	}
	deltaPayable, err := taxDelta(before.Payable, after.Payable, after.Scale)
	if err == nil {
		recalc.DeltaPayable = deltaPayable
	}
	deltaTax, err := taxDelta(before.TotalTax, after.TotalTax, after.Scale)
	if err == nil {
		recalc.DeltaTax = deltaTax
	}
	recalc.TotalsChanged = before.Payable != after.Payable || before.TotalTax != after.TotalTax
	recalc.RequiresReapproval = recalc.TotalsChanged
	return recalc
}

type taxSnapshotExport struct {
	Seq         uint64                   `json:"seq"`
	Snapshots   []TaxSnapshot            `json:"snapshots"`
	Adjustments []TaxAdjustment          `json:"adjustments"`
	Idem        map[string]taxIdemRecord `json:"idem"`
}

// Export captures the registry for restart recovery with deterministic
// order.
func (s *TaxStore) Export() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	exported := taxSnapshotExport{
		Seq:         s.seq,
		Snapshots:   make([]TaxSnapshot, 0, len(s.snapshots)),
		Adjustments: make([]TaxAdjustment, 0, len(s.adjustments)),
		Idem:        make(map[string]taxIdemRecord, len(s.idem)),
	}
	for _, snapshot := range s.snapshots {
		stored := *snapshot
		stored.Components = append([]TaxLineComponent(nil), snapshot.Components...)
		stored.Jurisdictions = append([]string(nil), snapshot.Jurisdictions...)
		exported.Snapshots = append(exported.Snapshots, stored)
	}
	sort.Slice(exported.Snapshots, func(i, j int) bool { return exported.Snapshots[i].Sequence < exported.Snapshots[j].Sequence })
	for _, adjustment := range s.adjustments {
		stored := *adjustment
		stored.Corrected.Components = append([]TaxLineComponent(nil), adjustment.Corrected.Components...)
		stored.Corrected.Jurisdictions = append([]string(nil), adjustment.Corrected.Jurisdictions...)
		exported.Adjustments = append(exported.Adjustments, stored)
	}
	sort.Slice(exported.Adjustments, func(i, j int) bool { return exported.Adjustments[i].Sequence < exported.Adjustments[j].Sequence })
	for key, record := range s.idem {
		exported.Idem[key] = record
	}
	return json.Marshal(exported)
}

// Import restores a snapshot, revalidating every field fail-closed:
// sequence floors, snapshot digests and arithmetic, adjustment chains
// with recomputed deltas, and idempotency key bindings.
func (s *TaxStore) Import(data []byte) error {
	var exported taxSnapshotExport
	if err := json.Unmarshal(data, &exported); err != nil {
		return fmt.Errorf("%w: tax snapshot is not valid", ErrTaxValidation)
	}
	snapshots := make(map[string]*TaxSnapshot, len(exported.Snapshots))
	byDigest := make(map[string]bool, len(exported.Snapshots))
	var maxSeq uint64
	for i := range exported.Snapshots {
		snapshot := &exported.Snapshots[i]
		if strings.TrimSpace(snapshot.ID) == "" || strings.TrimSpace(snapshot.TenantID) == "" || strings.TrimSpace(snapshot.InvoiceID) == "" {
			return fmt.Errorf("%w: snapshot identity is incomplete", ErrTaxValidation)
		}
		if snapshot.Sequence == 0 {
			return fmt.Errorf("%w: snapshot sequence is malformed", ErrTaxValidation)
		}
		if snapshot.Sequence > maxSeq {
			maxSeq = snapshot.Sequence
		}
		if _, dup := snapshots[snapshot.ID]; dup {
			return fmt.Errorf("%w: snapshot %q repeats", ErrTaxValidation, snapshot.ID)
		}
		if err := validateTaxSnapshotFields(*snapshot); err != nil {
			return err
		}
		if err := verifySnapshotArithmetic(*snapshot); err != nil {
			return err
		}
		if snapshot.Digest != taxSnapshotDigest(*snapshot) {
			return fmt.Errorf("%w: snapshot %q digest disagrees", ErrTaxValidation, snapshot.ID)
		}
		snapshots[snapshot.ID] = snapshot
		byDigest[snapshot.Digest] = true
	}
	adjustments := make(map[string]*TaxAdjustment, len(exported.Adjustments))
	for i := range exported.Adjustments {
		adjustment := &exported.Adjustments[i]
		if strings.TrimSpace(adjustment.ID) == "" || strings.TrimSpace(adjustment.TenantID) == "" || strings.TrimSpace(adjustment.InvoiceID) == "" {
			return fmt.Errorf("%w: adjustment identity is incomplete", ErrTaxValidation)
		}
		if adjustment.Sequence == 0 {
			return fmt.Errorf("%w: adjustment sequence is malformed", ErrTaxValidation)
		}
		if adjustment.Sequence > maxSeq {
			maxSeq = adjustment.Sequence
		}
		if _, dup := adjustments[adjustment.ID]; dup {
			return fmt.Errorf("%w: adjustment %q repeats", ErrTaxValidation, adjustment.ID)
		}
		if !byDigest[adjustment.OriginalDigest] {
			return fmt.Errorf("%w: adjustment references an unknown original", ErrTaxValidation)
		}
		if err := validateTaxSnapshotFields(adjustment.Corrected); err != nil {
			return fmt.Errorf("%w: corrected snapshot: %v", ErrTaxValidation, err)
		}
		if err := verifySnapshotArithmetic(adjustment.Corrected); err != nil {
			return fmt.Errorf("%w: corrected snapshot: %v", ErrTaxValidation, err)
		}
		if adjustment.Corrected.Digest != taxSnapshotDigest(adjustment.Corrected) {
			return fmt.Errorf("%w: corrected snapshot digest disagrees", ErrTaxValidation)
		}
		if adjustment.Corrected.TenantID != adjustment.TenantID || adjustment.Corrected.InvoiceID != adjustment.InvoiceID {
			return fmt.Errorf("%w: corrected snapshot scope disagrees", ErrTaxValidation)
		}
		switch adjustment.Corrected.Status {
		case TaxResolved, TaxExempt, TaxNotApplicable:
		default:
			return fmt.Errorf("%w: corrected treatment is not decisive", ErrTaxValidation)
		}
		original := snapshotByDigest(exported.Snapshots, adjustment.OriginalDigest)
		deltaTax, err := taxDelta(original.TotalTax, adjustment.Corrected.TotalTax, adjustment.Corrected.Scale)
		if err != nil || deltaTax != adjustment.DeltaTax {
			return fmt.Errorf("%w: adjustment tax delta disagrees", ErrTaxValidation)
		}
		deltaPayable, err := taxDelta(original.Payable, adjustment.Corrected.Payable, adjustment.Corrected.Scale)
		if err != nil || deltaPayable != adjustment.DeltaPayable {
			return fmt.Errorf("%w: adjustment payable delta disagrees", ErrTaxValidation)
		}
		adjustments[adjustment.ID] = adjustment
	}
	if exported.Seq < maxSeq {
		return fmt.Errorf("%w: snapshot sequence regressed below issued records", ErrTaxValidation)
	}
	for key, record := range exported.Idem {
		parts := strings.SplitN(key, "\x00", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("%w: idempotency key is malformed", ErrTaxValidation)
		}
		if record.Key == "" || record.Key != parts[1] || record.Payload == "" {
			return fmt.Errorf("%w: idempotency record disagrees with its key", ErrTaxValidation)
		}
		switch record.Action {
		case "resolve", "manual":
			stored, ok := snapshots[record.SnapshotID]
			if !ok || stored.TenantID != parts[0] {
				return fmt.Errorf("%w: idempotency record references an unknown snapshot", ErrTaxValidation)
			}
		case "correct":
			stored, ok := adjustments[record.AdjustmentID]
			if !ok || stored.TenantID != parts[0] {
				return fmt.Errorf("%w: idempotency record references an unknown adjustment", ErrTaxValidation)
			}
		default:
			return fmt.Errorf("%w: idempotency action %q is unknown", ErrTaxValidation, record.Action)
		}
	}
	byInvoice := make(map[string][]string)
	for _, snapshot := range exported.Snapshots {
		key := taxSnapshotKey(snapshot.TenantID, snapshot.InvoiceID)
		byInvoice[key] = append(byInvoice[key], snapshot.ID)
	}
	for key := range byInvoice {
		ids := byInvoice[key]
		sort.Slice(ids, func(i, j int) bool { return snapshots[ids[i]].Sequence < snapshots[ids[j]].Sequence })
		byInvoice[key] = ids
	}
	adjustChain := make(map[string][]string)
	for _, adjustment := range exported.Adjustments {
		key := taxSnapshotKey(adjustment.TenantID, adjustment.InvoiceID)
		adjustChain[key] = append(adjustChain[key], adjustment.ID)
	}
	for key := range adjustChain {
		ids := adjustChain[key]
		sort.Slice(ids, func(i, j int) bool { return adjustments[ids[i]].Sequence < adjustments[ids[j]].Sequence })
		adjustChain[key] = ids
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq = exported.Seq
	s.snapshots = make(map[string]*TaxSnapshot, len(snapshots))
	for id, snapshot := range snapshots {
		stored := *snapshot
		stored.Components = append([]TaxLineComponent(nil), snapshot.Components...)
		stored.Jurisdictions = append([]string(nil), snapshot.Jurisdictions...)
		s.snapshots[id] = &stored
	}
	s.byInvoice = byInvoice
	s.adjustments = make(map[string]*TaxAdjustment, len(adjustments))
	for id, adjustment := range adjustments {
		stored := *adjustment
		stored.Corrected.Components = append([]TaxLineComponent(nil), adjustment.Corrected.Components...)
		stored.Corrected.Jurisdictions = append([]string(nil), adjustment.Corrected.Jurisdictions...)
		s.adjustments[id] = &stored
	}
	s.adjustChain = adjustChain
	s.idem = make(map[string]taxIdemRecord, len(exported.Idem))
	for key, record := range exported.Idem {
		s.idem[key] = record
	}
	return nil
}

func snapshotByDigest(snapshots []TaxSnapshot, digest string) TaxSnapshot {
	for _, snapshot := range snapshots {
		if snapshot.Digest == digest {
			return snapshot
		}
	}
	return TaxSnapshot{}
}

func validateTaxSnapshotFields(snapshot TaxSnapshot) error {
	if snapshot.TenantID == "" || snapshot.InvoiceID == "" {
		return fmt.Errorf("%w: snapshot scope is incomplete", ErrTaxValidation)
	}
	if _, err := values.NewMoney("0", snapshot.Currency, 2, values.RoundingExactRequired); err != nil {
		return fmt.Errorf("%w: snapshot currency: %v", ErrTaxValidation, err)
	}
	if snapshot.Scale < 0 {
		return fmt.Errorf("%w: snapshot scale is negative", ErrTaxValidation)
	}
	if _, err := parseTaxClock(snapshot.ResolvedAt, "resolved"); err != nil {
		return err
	}
	if snapshot.Manual && (strings.TrimSpace(snapshot.ManualBy) == "" || strings.TrimSpace(snapshot.ManualReason) == "" || strings.TrimSpace(snapshot.ManualEvidence) == "") {
		return fmt.Errorf("%w: manual snapshot lacks attribution", ErrTaxValidation)
	}
	if snapshot.Status == TaxExempt && strings.TrimSpace(snapshot.ExemptReason) == "" {
		return fmt.Errorf("%w: exempt snapshot lacks its reason", ErrTaxValidation)
	}
	computedStatus := snapshot.Status == TaxResolved || snapshot.Status == TaxExempt || snapshot.Status == TaxNotApplicable
	if computedStatus && snapshot.Mode != TaxExclusive && snapshot.Mode != TaxInclusive {
		return fmt.Errorf("%w: snapshot mode %q is unknown", ErrTaxValidation, snapshot.Mode)
	}
	return nil
}
func taxDelta(before, after string, scale int32) (string, error) {
	width := scale
	if w := decimalScale(strings.TrimSpace(before)); w > width {
		width = w
	}
	if w := decimalScale(strings.TrimSpace(after)); w > width {
		width = w
	}
	from, err := parseTaxDecimal(before, width)
	if err != nil {
		return "", err
	}
	to, err := parseTaxDecimal(after, width)
	if err != nil {
		return "", err
	}
	delta, err := to.Sub(from)
	if err != nil {
		return "", err
	}
	return delta.String(), nil
}

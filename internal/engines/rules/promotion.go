package rules

import (
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// This file compiles the Promotion approval-threshold rule described by
// planning/reference-workflows/promote-into-management.md's representative
// approval graph and planning/specs/human-work-forms-and-rules.md's
// RaiseApprovalTable example, using the decision-table engine above it in
// this package. It is a decision table like any other this package can
// evaluate; nothing here is a special case in the engine.
//
// The reference workflow's approval graph always routes a management
// promotion through the current manager, the HRBP and the compensation
// partner. What varies by customer configuration is whether a fourth,
// heavier reviewer joins that baseline: a finance partner for the affected
// cost center, or - for the most material changes - an executive/committee
// review. This table answers exactly that question, from the four factors
// the reference workflow and the budget-authority contract actually carry:
// how large the raise is, where the resulting pay lands in the job's band,
// whether the compensation-pool budget authority backing the raise is
// sufficient, and whether the promotion also changes the worker's grade.
//
// The numeric thresholds below (10% finance, 20% executive) are this
// package's reference/example configuration - the human-work-and-rules spec
// is explicit that "customer thresholds ... are compiled rule/workflow
// configuration," not a constant this engine hardcodes for every tenant. A
// customer that needs a different threshold publishes a new table version
// with PromotionApprovalTableID and an incremented version, never edits this
// one's rows in place: RULE-003's REFACTOR contract is that a parameter
// change is a new version with its own digest, so a historical approval keeps
// citing the exact table it was decided against.

// PromotionApprovalTableID identifies the compiled Promotion approval
// threshold rule across every version.
const PromotionApprovalTableID = "hcmnext.rules.promotion_approval_threshold"

// PromotionApprovalTableVersion is the version of the reference threshold
// configuration this file compiles. A different set of thresholds is a
// different version string, never an edit to this one.
const PromotionApprovalTableVersion = "2026.1"

// PromotionWorkflowThresholdTableVersion is the first published threshold
// body that exposes its routing result alongside the approval tier.
const PromotionWorkflowThresholdTableVersion = "2026.2"

// Promotion approval input column names, exported so a caller can build a
// rules.Evaluate input map directly if it needs the untyped engine, or read
// PromotionApprovalDecision.Trace's ColumnTrace.Column entries against them.
const (
	ColumnIncreasePercent = "increase_percent"
	ColumnBandPosition    = "band_position"
	ColumnBudgetAuthority = "budget_authority"
	ColumnGradeChange     = "grade_change"
)

// IncreasePercentScale is the declared number of fractional digits an
// increase percentage carries into this table. A caller's decimal may be
// declared at any scale; values.Decimal.Cmp compares numerically regardless,
// so a percentage measured to whole points still compares exactly against a
// four-digit threshold.
const IncreasePercentScale int32 = 4

// BandPosition is where the promotion's resulting pay lands relative to the
// target job's pay band, as the caller's own band-position determination
// (e.g. internal/engines/payband) reports it. BandPositionUnknown is a
// distinct, legal value from the zero value: a caller that genuinely could
// not resolve the band position must say so explicitly, because the zero
// value staying unset is a caller defect, not a business fact.
type BandPosition string

// Band positions.
const (
	// BandPositionUnspecified is the zero value and is never legal input.
	BandPositionUnspecified BandPosition = ""
	// BandPositionBelowMinimum means the resulting pay sits under the band minimum.
	BandPositionBelowMinimum BandPosition = "BELOW_MINIMUM"
	// BandPositionInBand means the resulting pay sits within the band.
	BandPositionInBand BandPosition = "IN_BAND"
	// BandPositionAboveMaximum means the resulting pay sits over the band maximum.
	BandPositionAboveMaximum BandPosition = "ABOVE_MAXIMUM"
	// BandPositionUnknown means the band position could not be resolved. It is
	// a legal, explicit value, never inferred from an absent one.
	BandPositionUnknown BandPosition = "UNKNOWN"
)

var bandPositionWire = map[BandPosition]bool{
	BandPositionBelowMinimum: true,
	BandPositionInBand:       true,
	BandPositionAboveMaximum: true,
	BandPositionUnknown:      true,
}

// Valid reports whether p is a declared band position.
func (p BandPosition) Valid() bool { return bandPositionWire[p] }

// BudgetAuthority is whether the compensation-pool budget authority backing
// the raise, per planning/specs/workforce-budget-authority.md, is sufficient.
// BudgetAuthorityUnknown is the legal explicit value for "not yet resolved";
// it is never the same thing as the field being left unset.
type BudgetAuthority string

// Budget authority states.
const (
	// BudgetAuthorityUnspecified is the zero value and is never legal input.
	BudgetAuthorityUnspecified BudgetAuthority = ""
	// BudgetAuthoritySufficient means the reserved or available budget covers the raise.
	BudgetAuthoritySufficient BudgetAuthority = "SUFFICIENT"
	// BudgetAuthorityInsufficient means the available budget does not cover the raise.
	BudgetAuthorityInsufficient BudgetAuthority = "INSUFFICIENT"
	// BudgetAuthorityUnknown means the budget authority has not been resolved.
	BudgetAuthorityUnknown BudgetAuthority = "UNKNOWN"
)

var budgetAuthorityWire = map[BudgetAuthority]bool{
	BudgetAuthoritySufficient:   true,
	BudgetAuthorityInsufficient: true,
	BudgetAuthorityUnknown:      true,
}

// Valid reports whether b is a declared budget authority state.
func (b BudgetAuthority) Valid() bool { return budgetAuthorityWire[b] }

// ApprovalTier is the required approval tier the table produces, on top of
// the reference workflow's constant baseline of current manager, HRBP and
// compensation partner.
type ApprovalTier string

// Approval tiers.
const (
	// ApprovalTierUnspecified is the zero value and is never a legal result.
	ApprovalTierUnspecified ApprovalTier = ""
	// ApprovalTierStandard means the baseline reviewers are sufficient; no
	// finance or executive review is required.
	ApprovalTierStandard ApprovalTier = "STANDARD"
	// ApprovalTierFinanceRequired adds the finance partner for the affected
	// cost center to the baseline.
	ApprovalTierFinanceRequired ApprovalTier = "FINANCE_REQUIRED"
	// ApprovalTierExecutiveRequired adds executive/compensation-committee
	// review for the most material changes.
	ApprovalTierExecutiveRequired ApprovalTier = "EXECUTIVE_REQUIRED"
	// ApprovalTierUnknownBlocked means a required input could not be resolved
	// (an explicit UNKNOWN band position or budget authority) or the table
	// found no matching row. Execution is blocked pending resolution; it is
	// never treated as "no finance approval required."
	ApprovalTierUnknownBlocked ApprovalTier = "UNKNOWN_BLOCKED"
)

// promotion rule row ids, cited in PromotionApprovalDecision.MatchedRowID.
const (
	rowUnknownBudget       = "unknown-budget-authority"
	rowUnknownBandPosition = "unknown-band-position"
	rowInsufficientBudget  = "insufficient-budget-authority"
	rowGradeChangeAboveMax = "grade-change-above-band-maximum"
	rowLargeRaise          = "increase-exceeds-executive-threshold"
	rowRaiseOverThreshold  = "increase-exceeds-finance-threshold"
	rowAboveMaximum        = "resulting-pay-above-band-maximum"
	rowGradeChange         = "grade-change-standard"
	rowOtherwise           = "otherwise-standard"
)

// financeThreshold and executiveThreshold are this reference configuration's
// increase-percent cut points, both exclusive: a raise of exactly the
// threshold does not itself escalate.
var (
	financeThreshold   = values.MustDecimal("10.0000", IncreasePercentScale, values.RoundingHalfEven)
	executiveThreshold = values.MustDecimal("20.0000", IncreasePercentScale, values.RoundingHalfEven)
)

// PromotionApprovalThresholdTable builds the reference Promotion
// approval-threshold decision table at PromotionApprovalTableVersion.
//
// It returns a fresh value on every call - every column and row slice is
// newly allocated - so concurrent callers never share, and cannot corrupt
// each other's, backing storage. The table is otherwise a constant: the same
// version always builds byte-identical rows, which Table.Digest proves.
//
// Row order is the table's priority under HitPolicyFirst. An explicitly
// unresolved band position or budget authority is checked first, so a caller
// that has not yet finished resolving those facts is blocked rather than
// falling through to whatever the remaining columns happen to say. Budget
// insufficiency and the largest raises and grade changes escalate next; the
// final row is the explicit "otherwise" catch-all that keeps the table total
// rather than leaving small, in-band, sufficiently-funded raises with no
// matching row.
func PromotionApprovalThresholdTable() Table {
	return Table{
		ID:      PromotionApprovalTableID,
		Version: PromotionApprovalTableVersion,
		Inputs: []Column{
			{Name: ColumnIncreasePercent, Kind: KindDecimal},
			{Name: ColumnBandPosition, Kind: KindString},
			{Name: ColumnBudgetAuthority, Kind: KindString},
			{Name: ColumnGradeChange, Kind: KindBool},
		},
		Outputs: []Column{
			{Name: "approval_tier", Kind: KindString},
		},
		HitPolicy: HitPolicyFirst,
		Rows: []Row{
			{
				ID:         rowUnknownBudget,
				Conditions: []Condition{Any(), Any(), Equal(StringValue(string(BudgetAuthorityUnknown))), Any()},
				Outputs:    []Value{StringValue(string(ApprovalTierUnknownBlocked))},
			},
			{
				ID:         rowUnknownBandPosition,
				Conditions: []Condition{Any(), Equal(StringValue(string(BandPositionUnknown))), Any(), Any()},
				Outputs:    []Value{StringValue(string(ApprovalTierUnknownBlocked))},
			},
			{
				ID:         rowInsufficientBudget,
				Conditions: []Condition{Any(), Any(), Equal(StringValue(string(BudgetAuthorityInsufficient))), Any()},
				Outputs:    []Value{StringValue(string(ApprovalTierFinanceRequired))},
			},
			{
				ID: rowGradeChangeAboveMax,
				Conditions: []Condition{
					Any(),
					Equal(StringValue(string(BandPositionAboveMaximum))),
					Any(),
					Equal(BoolValue(true)),
				},
				Outputs: []Value{StringValue(string(ApprovalTierExecutiveRequired))},
			},
			{
				ID:         rowLargeRaise,
				Conditions: []Condition{GreaterThan(DecimalValue(executiveThreshold)), Any(), Any(), Any()},
				Outputs:    []Value{StringValue(string(ApprovalTierExecutiveRequired))},
			},
			{
				ID:         rowRaiseOverThreshold,
				Conditions: []Condition{GreaterThan(DecimalValue(financeThreshold)), Any(), Any(), Any()},
				Outputs:    []Value{StringValue(string(ApprovalTierFinanceRequired))},
			},
			{
				ID:         rowAboveMaximum,
				Conditions: []Condition{Any(), Equal(StringValue(string(BandPositionAboveMaximum))), Any(), Any()},
				Outputs:    []Value{StringValue(string(ApprovalTierFinanceRequired))},
			},
			{
				ID:         rowGradeChange,
				Conditions: []Condition{Any(), Any(), Any(), Equal(BoolValue(true))},
				Outputs:    []Value{StringValue(string(ApprovalTierFinanceRequired))},
			},
			{
				ID:         rowOtherwise,
				Conditions: []Condition{Any(), Any(), Any(), Any()},
				Outputs:    []Value{StringValue(string(ApprovalTierStandard))},
			},
		},
	}
}

// PromotionWorkflowThresholdTable preserves the 2026.1 approval-tier table
// and publishes an additional explicit workflow route result. The original
// version remains available for historical decisions and v1 workflow plans.
func PromotionWorkflowThresholdTable() Table {
	table := PromotionApprovalThresholdTable()
	table.Version = PromotionWorkflowThresholdTableVersion
	table.Outputs = append(table.Outputs, Column{Name: "route_key", Kind: KindString})
	for i := range table.Rows {
		tier := ApprovalTier(table.Rows[i].Outputs[0].String())
		route := "EXCEEDS_THRESHOLD"
		switch tier {
		case ApprovalTierStandard:
			route = "WITHIN_THRESHOLD"
		case ApprovalTierUnknownBlocked:
			route = "UNKNOWN"
		}
		table.Rows[i].Outputs = append(table.Rows[i].Outputs, StringValue(route))
	}
	return table
}

// PromotionApprovalInput is the typed question this table answers for one
// promotion proposal.
type PromotionApprovalInput struct {
	// IncreasePercent is the proposed base-pay increase, as a percentage (10
	// meaning 10%), at any declared decimal scale.
	IncreasePercent values.Decimal
	// BandPosition is where the resulting pay lands in the target job's band.
	BandPosition BandPosition
	// BudgetAuthority is whether the compensation-pool budget authority
	// backing the raise is sufficient.
	BudgetAuthority BudgetAuthority
	// GradeChange reports whether the promotion also changes the worker's
	// grade, not merely their job or title.
	GradeChange bool
}

// ErrPromotionInputInvalid is returned by PromotionApprovalInput.Validate and
// by EvaluatePromotionApproval for an incomplete or malformed input. It is
// matchable with errors.Is.
var ErrPromotionInputInvalid = errors.New("rules: promotion approval input is incomplete or malformed")

// Validate reports whether in is complete enough to evaluate. A zero-value
// BandPosition or BudgetAuthority fails here rather than reaching the table,
// because "not yet decided whether this is known" is a caller defect, while
// the explicit *Unknown value is a legal business fact the table itself
// routes to ApprovalTierUnknownBlocked.
func (in PromotionApprovalInput) Validate() error {
	if err := in.IncreasePercent.Validate(); err != nil {
		return fmt.Errorf("%w: increase percent: %w", ErrPromotionInputInvalid, err)
	}
	if !in.BandPosition.Valid() {
		return fmt.Errorf("%w: band position %q", ErrPromotionInputInvalid, string(in.BandPosition))
	}
	if !in.BudgetAuthority.Valid() {
		return fmt.Errorf("%w: budget authority %q", ErrPromotionInputInvalid, string(in.BudgetAuthority))
	}
	return nil
}

// values converts the input into the untyped engine's input map.
func (in PromotionApprovalInput) values() map[string]Value {
	return map[string]Value{
		ColumnIncreasePercent: DecimalValue(in.IncreasePercent),
		ColumnBandPosition:    StringValue(string(in.BandPosition)),
		ColumnBudgetAuthority: StringValue(string(in.BudgetAuthority)),
		ColumnGradeChange:     BoolValue(in.GradeChange),
	}
}

// PromotionApprovalDecision is the whole answer: the required approval tier
// or the honest UNKNOWN_BLOCKED finding, the exact table version it was
// decided against, and the row-by-row trace that produced it.
type PromotionApprovalDecision struct {
	// Tier is the required approval tier. It is ApprovalTierUnknownBlocked,
	// never ApprovalTierStandard, when a required input was unresolved or the
	// table found no matching row.
	Tier ApprovalTier
	// MatchedRowID is the row that produced Tier, or "" when the result is
	// UNKNOWN_BLOCKED because no row matched at all.
	MatchedRowID string
	// TableID, TableVersion and TableDigest cite the exact table version this
	// decision was computed against, so a stored decision remains auditable
	// after the table is later republished under a new version.
	TableID      string
	TableVersion string
	TableDigest  string
	// Trace is the full per-row explanation from the underlying evaluation.
	Trace []RowTrace
}

// ErrPromotionRuleConflict is returned when the table's own hit policy
// reports a conflict. HitPolicyFirst never produces this; it is retained as a
// defensive check against a future table edit that changes the hit policy
// without updating this contract.
var ErrPromotionRuleConflict = errors.New("rules: promotion approval table reported a hit-policy conflict")

// EvaluatePromotionApproval evaluates the Promotion approval-threshold table
// for one proposal.
//
// A missing or malformed input (an unset decimal, an unset band position or
// budget authority) is a caller defect and is returned as an error. An
// explicit BandPositionUnknown or BudgetAuthorityUnknown is a legal business
// fact and never an error: it is routed by the table itself to
// ApprovalTierUnknownBlocked, which is the RULE-003 contract that a missing
// value can never resolve to "no finance approval required."
func EvaluatePromotionApproval(table Table, in PromotionApprovalInput) (PromotionApprovalDecision, error) {
	if err := in.Validate(); err != nil {
		return PromotionApprovalDecision{}, err
	}
	result, err := Evaluate(table, in.values())
	if err != nil {
		return PromotionApprovalDecision{}, fmt.Errorf("rules: promotion approval: %w", err)
	}

	decision := PromotionApprovalDecision{
		TableID:      result.TableID,
		TableVersion: result.TableVersion,
		TableDigest:  result.TableDigest,
		Trace:        result.Trace,
	}

	switch result.Status {
	case StatusMatched:
		decision.MatchedRowID = result.Matches[0].RowID
		decision.Tier = ApprovalTier(result.Matches[0].Outputs[0].String())
	case StatusUnknown:
		decision.Tier = ApprovalTierUnknownBlocked
	case StatusConflict:
		return PromotionApprovalDecision{}, fmt.Errorf("%w: rows %v", ErrPromotionRuleConflict, result.MatchedRowIDs)
	default:
		return PromotionApprovalDecision{}, fmt.Errorf("rules: promotion approval: unrecognized status %s", result.Status)
	}
	return decision, nil
}

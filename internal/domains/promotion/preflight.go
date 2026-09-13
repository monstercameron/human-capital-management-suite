// Package promotion is the composite promote_worker slice of P1A: the domain
// preflight and the deterministic, zero-effect simulation behind
// hcmnext.people.promote_worker/v1 in DRAFT, PREFLIGHT and SIMULATE modes.
//
// Semantic owner: People domain (composite ChangeRequest). Phase: P1A.
//
// A promotion is one parent ChangeRequest composed of a People placement
// change and a Rewards compensation change. This package owns the business
// rules that decide whether that composite is coherent and what it would do;
// it does not own the intent lifecycle, the proposal revision, the approval
// graph or any write. The kernel drives DRAFT -> PREFLIGHT -> SIMULATE and
// calls in here for the domain answers.
//
// Two properties are deliberate. First, worker facts arrive only as a
// people.Explanation - the governed read - so a caller cannot assert the
// baseline it wants to be promoted from. Second, every result carries counted
// effects and a receipt: P1A's entire claim is that running a promotion
// preflight and simulation changes nothing.
package promotion

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Intent identity for the promotion slice.
const (
	// IntentType is the catalog identifier.
	IntentType = "hcmnext.people.promote_worker"
	// IntentVersion is the contract version.
	IntentVersion = "v1"
	// RulePackVersion versions the preflight rules in this file. It changes
	// whenever a code is added, removed or re-classified, because a stored
	// finding set must remain interpretable by the rules that produced it.
	RulePackVersion = "people.promotion.preflight.rules/1.0.0"
)

const promotionSchemaVer = 1

// Promotion errors. All are matchable with errors.Is.
var (
	// ErrRequestInvalid is returned for a malformed preflight request. It is
	// reserved for contract failures the caller must fix; a business problem
	// with the promotion is a Finding, not an error.
	ErrRequestInvalid = errors.New("promotion: preflight request is invalid")
	// ErrPreflightDenied is returned when simulation is attempted on a subject
	// the caller is not authorized to see.
	ErrPreflightDenied = errors.New("promotion: preflight was denied; simulation is not available")
	// ErrCatalogFailed wraps a non-miss failure from the pay band catalog.
	ErrCatalogFailed = errors.New("promotion: pay band catalog failed")
)

// Finding codes. Codes carried over from the legacy TypeScript/Go block
// implementation keep their original strings so that legacy regression
// fixtures still assert on the same identifiers.
const (
	// Ported from the legacy compensation preflight block.
	CodeCurrentAmountInvalid   = "compensation.current_amount_invalid"
	CodeProposedAmountInvalid  = "compensation.proposed_amount_invalid"
	CodeCurrencyRequired       = "compensation.currency_required"
	CodePayBasisRequired       = "compensation.pay_basis_required"
	CodeCurrencyChangeNotV0    = "compensation.currency_change_not_supported_v0"
	CodeNotARaise              = "compensation.not_a_raise"
	CodeBusinessReasonRequired = "compensation.business_reason_required"
	CodeEffectiveAtRequired    = "compensation.effective_at_required"
	CodeEffectiveAtTooFarPast  = "compensation.effective_at_too_far_in_past"
	CodeIncreaseOverThreshold  = "compensation.increase_over_ten_percent"

	// Promotion-specific rules.
	CodeSubjectNotDisclosable   = "promotion.subject_not_disclosable"
	CodeRequiredFieldDenied     = "promotion.required_field_denied"
	CodeRequiredFieldUnavailabl = "promotion.required_field_unavailable"
	CodeWorkerNotActive         = "promotion.worker_not_active"
	CodeTargetJobRequired       = "promotion.target_job_required"
	CodeTargetGradeRequired     = "promotion.target_grade_required"
	CodeSameGrade               = "promotion.same_grade"
	CodeEffectiveBeforeHire     = "promotion.effective_date_before_hire_date"
	CodePayBandNotFound         = "promotion.pay_band_not_found"
	CodeBelowBandMinimum        = "promotion.pay_below_band_minimum"
	CodeAboveBandMaximum        = "promotion.pay_above_band_maximum"
	CodeBudgetAuthorityMissing  = "promotion.budget_authority_missing"
	CodeBudgetObservationOnly   = "promotion.budget_authority_observation_only"
	CodeBudgetObservedShort     = "promotion.budget_observed_insufficient"
)

// Severity is how a finding affects the preflight verdict.
type Severity uint8

// Severities.
const (
	// SeverityUnspecified is the zero value and is never legal.
	SeverityUnspecified Severity = iota
	// SeverityAdvisory records something a reviewer should weigh. It never
	// changes the status on its own.
	SeverityAdvisory
	// SeverityBlocking prevents the promotion from being proposed as valid.
	SeverityBlocking
	// SeverityNeedsData means a required input could not be resolved. It is
	// distinct from blocking because the fix is to supply data, not to change
	// the proposal.
	SeverityNeedsData
	// SeverityDenied means authorization refused. It is distinct from blocking
	// because the caller is not entitled to know whether the promotion is
	// otherwise valid.
	SeverityDenied
)

var severityWire = map[Severity]string{
	SeverityAdvisory:  "ADVISORY",
	SeverityBlocking:  "BLOCKING",
	SeverityNeedsData: "NEEDS_DATA",
	SeverityDenied:    "DENIED",
}

// String returns the stable wire token, or "SEVERITY_UNSPECIFIED".
func (s Severity) String() string {
	if v, ok := severityWire[s]; ok {
		return v
	}
	return "SEVERITY_UNSPECIFIED"
}

// Finding is one typed preflight result. Message is an operator-facing
// sentence; Code is the stable identity that rules, tests and UIs key on.
//
// Owner and CorroboratedBy exist for PROMOUX-009's canonicalization: Owner
// names the subsystem that sourced this finding (never caller-forged; a rule
// engine sets it once, at construction), and CorroboratedBy carries every
// other distinct owner that independently reported the same observation once
// [DeduplicateFindings] has folded a group of findings that share a
// [FindingIdentity] into one. A Finding built without going through dedup
// (every finding this package's own rules produce today) simply carries an
// empty Owner and a nil CorroboratedBy, which is the correct answer for "was
// this ever corroborated": no, and it does not need to be for the finding to
// be usable -- Owner is metadata about provenance, never part of what makes a
// finding valid. See [Finding.Identity].
type Finding struct {
	Code     string
	Severity Severity
	Field    string
	Message  string
	Owner    string
	// CorroboratedBy lists every distinct owner, other than Owner, that
	// independently reported this same observation. It is sorted and nil
	// unless dedup found more than one distinct owner for the identity.
	CorroboratedBy []string
}

// Status is the typed preflight verdict required by INTENT-004.
type Status uint8

// Statuses.
const (
	// StatusUnspecified is the zero value and is never legal.
	StatusUnspecified Status = iota
	// StatusReady means the promotion may enter simulation as valid.
	StatusReady
	// StatusNeedsData means a required input is missing or unresolved.
	StatusNeedsData
	// StatusBlocked means a business rule refuses the promotion as proposed.
	StatusBlocked
	// StatusDenied means authorization refused the subject or a required field.
	StatusDenied
)

var statusWire = map[Status]string{
	StatusReady:     "READY",
	StatusNeedsData: "NEEDS_DATA",
	StatusBlocked:   "BLOCKED",
	StatusDenied:    "DENIED",
}

// String returns the stable wire token, or "STATUS_UNSPECIFIED".
func (s Status) String() string {
	if v, ok := statusWire[s]; ok {
		return v
	}
	return "STATUS_UNSPECIFIED"
}

// statusFor collapses a finding set into the single verdict. The precedence is
// DENIED > NEEDS_DATA > BLOCKED > READY: a caller who may not see the subject
// must not be told the promotion would otherwise be blocked, and missing data
// outranks a rule violation because the violation may be an artifact of the
// missing data.
func statusFor(findings []Finding) Status {
	status := StatusReady
	rank := map[Status]int{StatusReady: 0, StatusBlocked: 1, StatusNeedsData: 2, StatusDenied: 3}
	for _, f := range findings {
		var candidate Status
		switch f.Severity {
		case SeverityDenied:
			candidate = StatusDenied
		case SeverityNeedsData:
			candidate = StatusNeedsData
		case SeverityBlocking:
			candidate = StatusBlocked
		default:
			continue
		}
		if rank[candidate] > rank[status] {
			status = candidate
		}
	}
	return status
}

// sortFindings orders findings deterministically so that two runs over the
// same inputs digest identically.
func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Code != findings[j].Code {
			return findings[i].Code < findings[j].Code
		}
		if findings[i].Field != findings[j].Field {
			return findings[i].Field < findings[j].Field
		}
		return findings[i].Message < findings[j].Message
	})
}

// TargetPlacement is the proposed job, grade and organizational placement.
type TargetPlacement struct {
	JobCode    string
	Grade      string
	OrgUnit    string
	PositionID string
	PayZone    string
}

// Canonical returns the canonical byte encoding.
func (t TargetPlacement) Canonical() []byte {
	raw, err := canonicalbytes.New("hcmnext.domains.promotion.TargetPlacement", promotionSchemaVer).
		String("job_code", t.JobCode).
		String("grade", t.Grade).
		String("org_unit", t.OrgUnit).
		String("position_id", t.PositionID).
		String("pay_zone", t.PayZone).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// BudgetAuthorityRef is the workforce-budget observation a promotion cites.
//
// In P1A this is an observation, never a reservation: Human Capital Management Suite reads the
// incumbent finance authority and reports what it saw. AvailableAmount is
// Presence-wrapped so that "the pool has 40,000 left" and "we could not read
// the pool" stay different answers, and neither can be presented as a
// guarantee that the spend is authorized.
type BudgetAuthorityRef struct {
	BudgetType      string
	OwnerSystem     string
	PolicyRef       string
	Scope           string
	Period          string
	Currency        string
	Unit            string
	BaselineVersion string
	AvailableAmount values.Presence[values.Money]
	ObservationID   string
}

// BudgetTypeCompensationPool is the only workforce budget type a promotion
// binds in Phase 1.
const BudgetTypeCompensationPool = "COMPENSATION_POOL"

// Validate reports whether the reference is complete enough to cite.
func (b BudgetAuthorityRef) Validate() error {
	switch {
	case b.BudgetType == "":
		return fmt.Errorf("%w: budget authority needs a budget type", ErrRequestInvalid)
	case b.OwnerSystem == "" || b.PolicyRef == "":
		return fmt.Errorf("%w: budget authority needs an owner system and policy fingerprint", ErrRequestInvalid)
	case b.Scope == "" || b.Period == "":
		return fmt.Errorf("%w: budget authority needs a scope and a period", ErrRequestInvalid)
	case b.BaselineVersion == "" || b.ObservationID == "":
		return fmt.Errorf("%w: budget authority needs a baseline version and an observation id", ErrRequestInvalid)
	}
	return b.AvailableAmount.Validate()
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (b BudgetAuthorityRef) Canonical() []byte {
	if b.Validate() != nil {
		return nil
	}
	available, err := values.MarshalPresence(b.AvailableAmount, moneyCodec{})
	if err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.promotion.BudgetAuthorityRef", promotionSchemaVer).
		String("budget_type", b.BudgetType).
		String("owner_system", b.OwnerSystem).
		String("policy_ref", b.PolicyRef).
		String("scope", b.Scope).
		String("period", b.Period).
		String("currency", b.Currency).
		String("unit", b.Unit).
		String("baseline_version", b.BaselineVersion).
		Field("available_amount", available).
		String("observation_id", b.ObservationID).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// moneyCodec encodes a money presence payload through the kernel encoding.
type moneyCodec struct{}

// EncodeValue implements values.ValueCodec.
func (moneyCodec) EncodeValue(m values.Money) ([]byte, error) {
	raw := m.Canonical()
	if raw == nil {
		return nil, fmt.Errorf("%w: money has no canonical encoding", ErrRequestInvalid)
	}
	return raw, nil
}

// DecodeValue implements values.ValueCodec and is deliberately unsupported.
func (moneyCodec) DecodeValue([]byte) (values.Money, error) {
	return values.Money{}, errors.New("promotion: money presence decoding is not supported")
}

// Policy is the tenant-configured threshold set the preflight evaluates
// against. It is an input rather than a constant because every one of these
// numbers is a customer decision, and a rule engine that hardcodes them
// becomes a rule engine that cannot be configured.
type Policy struct {
	Version string
	// MaxRetroactiveDays bounds how far into the past an effective date may
	// reach. The legacy compensation block used 180.
	MaxRetroactiveDays int
	// LargeIncreasePercent is the advisory review threshold, in percent. The
	// legacy compensation block used 10.
	LargeIncreasePercent values.Decimal
	// RequireBusinessReason mirrors the legacy mandatory business reason.
	RequireBusinessReason bool
	// RequireIncrease mirrors the legacy "must be a raise" rule.
	RequireIncrease bool
	// RequireBudgetAuthority makes a missing budget observation blocking.
	RequireBudgetAuthority bool
	// AllowSameGrade permits a lateral move to be called a promotion.
	AllowSameGrade bool
}

// DefaultPolicy returns the P1A policy, carrying the legacy thresholds.
func DefaultPolicy() Policy {
	return Policy{
		Version:                "people.promotion.policy/1.0.0",
		MaxRetroactiveDays:     180,
		LargeIncreasePercent:   values.MustDecimal("10.0000", 4, values.RoundingHalfEven),
		RequireBusinessReason:  true,
		RequireIncrease:        true,
		RequireBudgetAuthority: true,
		AllowSameGrade:         false,
	}
}

// Validate reports whether the policy is usable.
func (p Policy) Validate() error {
	if p.Version == "" {
		return fmt.Errorf("%w: policy version is required", ErrRequestInvalid)
	}
	if p.MaxRetroactiveDays < 0 {
		return fmt.Errorf("%w: max retroactive days is negative", ErrRequestInvalid)
	}
	if err := p.LargeIncreasePercent.Validate(); err != nil {
		return fmt.Errorf("%w: large increase threshold: %w", ErrRequestInvalid, err)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (p Policy) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.promotion.Policy", promotionSchemaVer).
		String("version", p.Version).
		Int("max_retroactive_days", int64(p.MaxRetroactiveDays)).
		Value("large_increase_percent", p.LargeIncreasePercent).
		Bool("require_business_reason", p.RequireBusinessReason).
		Bool("require_increase", p.RequireIncrease).
		Bool("require_budget_authority", p.RequireBudgetAuthority).
		Bool("allow_same_grade", p.AllowSameGrade).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// requiredFields are the worker-state fields a promotion cannot be evaluated
// without. Anything not on this list is not read.
var requiredFields = []people.FieldID{
	people.FieldLifecycleStatus,
	people.FieldEmploymentStatus,
	people.FieldHireDate,
	people.FieldJobCode,
	people.FieldGrade,
	people.FieldOrgUnit,
	people.FieldPayZone,
}

// RequiredWorkerFields returns the projection a promotion preflight needs, so
// the kernel can ask people.ExplainWorkerState for exactly that and no more.
func RequiredWorkerFields() []people.FieldID {
	return append([]people.FieldID(nil), requiredFields...)
}

// WorkerBaseline is the promotion-relevant slice of a governed worker read.
//
// The only constructor takes a people.Explanation. There is no literal
// constructor and no exported setters, which is what makes "preflight does not
// trust caller-supplied current facts" a property of the type rather than a
// rule someone has to remember.
type WorkerBaseline struct {
	Worker           values.EntityRef
	LifecycleStatus  string
	EmploymentStatus string
	HireDate         values.LocalDate
	JobCode          string
	Grade            string
	OrgUnit          string
	PayZone          string
	Watermark        values.RevisionToken
	AsOf             people.AsOf
}

// Canonical returns the canonical byte encoding.
//
// A withheld read produces an empty baseline, and that empty baseline still
// has to encode: the preflight result for a denied subject is digested and
// receipted like any other. Each optional coordinate therefore carries a
// present/absent marker rather than being silently skipped.
func (b WorkerBaseline) Canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.promotion.WorkerBaseline", promotionSchemaVer).
		Bool("worker?", b.Worker.Validate() == nil)
	if b.Worker.Validate() == nil {
		w.Value("worker", b.Worker)
	}
	w.String("lifecycle_status", b.LifecycleStatus).
		String("employment_status", b.EmploymentStatus).
		Bool("hire_date?", b.HireDate.IsSet())
	if b.HireDate.IsSet() {
		w.Value("hire_date", b.HireDate)
	}
	w.String("job_code", b.JobCode).
		String("grade", b.Grade).
		String("org_unit", b.OrgUnit).
		String("pay_zone", b.PayZone).
		Bool("as_of?", b.AsOf.Validate() == nil)
	if b.AsOf.Validate() == nil {
		w.Value("as_of", b.AsOf)
	}
	w.Bool("watermark?", b.Watermark.IsSpecified())
	if b.Watermark.IsSpecified() {
		w.Value("watermark", b.Watermark)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// baselineFrom extracts the promotion baseline from a governed explanation,
// returning the findings for anything it could not obtain.
//
// A denied field becomes a DENIED finding and an unresolved field becomes a
// NEEDS_DATA finding. Neither is silently defaulted: the whole point of
// reading through ExplainWorkerState is that the gaps stay visible.
func baselineFrom(e people.Explanation) (WorkerBaseline, []Finding) {
	if e.Disclosure == people.DisclosureWithheld {
		return WorkerBaseline{}, []Finding{{
			Code:     CodeSubjectNotDisclosable,
			Severity: SeverityDenied,
			Field:    "subject",
			Message:  "the caller is not authorized to learn anything about this subject",
		}}
	}

	baseline := WorkerBaseline{
		Worker:    e.Worker,
		Watermark: e.Watermark,
		AsOf:      e.AsOf,
	}
	var findings []Finding
	got := make(map[people.FieldID]string, len(requiredFields))
	for _, field := range requiredFields {
		var fact people.ExplainedFact
		found := false
		for _, f := range e.Fields {
			if f.Field == field {
				fact, found = f, true
				break
			}
		}
		switch {
		case !found:
			findings = append(findings, Finding{
				Code:     CodeRequiredFieldUnavailabl,
				Severity: SeverityNeedsData,
				Field:    field.String(),
				Message:  "the governed read did not cover this field",
			})
		case fact.Access == people.AccessDenied:
			findings = append(findings, Finding{
				Code:     CodeRequiredFieldDenied,
				Severity: SeverityDenied,
				Field:    field.String(),
				Message:  "authorization denied this field: " + fact.DenialReason,
			})
		default:
			v, ok := fact.Value.Get()
			if !ok {
				findings = append(findings, Finding{
					Code:     CodeRequiredFieldUnavailabl,
					Severity: SeverityNeedsData,
					Field:    field.String(),
					Message:  "field is " + fact.Value.State().String() + " at the requested coordinate",
				})
				continue
			}
			got[field] = v
		}
	}

	baseline.LifecycleStatus = got[people.FieldLifecycleStatus]
	baseline.EmploymentStatus = got[people.FieldEmploymentStatus]
	baseline.JobCode = got[people.FieldJobCode]
	baseline.Grade = got[people.FieldGrade]
	baseline.OrgUnit = got[people.FieldOrgUnit]
	baseline.PayZone = got[people.FieldPayZone]
	if raw, ok := got[people.FieldHireDate]; ok {
		hire, err := values.ParseLocalDate(raw)
		if err != nil {
			findings = append(findings, Finding{
				Code:     CodeRequiredFieldUnavailabl,
				Severity: SeverityNeedsData,
				Field:    people.FieldHireDate.String(),
				Message:  "hire date is not a canonical business date: " + err.Error(),
			})
		} else {
			baseline.HireDate = hire
		}
	}
	return baseline, findings
}

// lifecycleActive is the single spelling of an active worker. The legacy
// implementation compared against the literal "active"; keeping one constant
// here stops that comparison from being re-spelled per call site.
const lifecycleActive = "active"

// InputSnapshot is the exact input a preflight ran on, echoed back so the
// verdict can be re-derived without re-resolving anything. INTENT-004 requires
// preflight to return this alongside its findings.
type InputSnapshot struct {
	Tenant         values.TenantId
	Subject        values.EntityRef
	Baseline       WorkerBaseline
	Target         TargetPlacement
	Current        rewards.CompensationSnapshot
	Proposed       rewards.CompensationSnapshot
	EffectiveDate  values.LocalDate
	EvaluationDate values.LocalDate
	BusinessReason string
	Budget         *BudgetAuthorityRef
	Policy         Policy
	Annualization  rewards.AnnualizationRule
	BandQuery      *rewards.BandQuery
	// ManagementImpact is PROMOUX-005's typed review requirement: what a
	// management promotion changes in the reporting graph. Its zero value
	// means no target-manager selection was evaluated.
	ManagementImpact ManagementImpact
}

// Canonical returns the canonical byte encoding, or nil when incoherent.
func (s InputSnapshot) Canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.promotion.InputSnapshot", promotionSchemaVer).
		String("tenant", string(s.Tenant)).
		Value("subject", s.Subject).
		Value("baseline", s.Baseline).
		Value("target", s.Target).
		Value("current", s.Current).
		Value("proposed", s.Proposed).
		Bool("effective_date?", s.EffectiveDate.IsSet())
	if s.EffectiveDate.IsSet() {
		w.Value("effective_date", s.EffectiveDate)
	}
	w.Value("evaluation_date", s.EvaluationDate).
		String("business_reason", s.BusinessReason).
		Value("policy", s.Policy).
		Value("annualization", s.Annualization).
		Bool("budget?", s.Budget != nil)
	if s.Budget != nil {
		w.Value("budget", *s.Budget)
	}
	w.Bool("band_query?", s.BandQuery != nil)
	if s.BandQuery != nil {
		w.Value("band_query", *s.BandQuery)
	}
	w.Field("management_impact", s.ManagementImpact.Canonical())
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// PreflightRequest is the whole preflight input.
type PreflightRequest struct {
	Tenant  values.TenantId
	Subject values.EntityRef

	// WorkerState is the governed read of the current worker facts. It is the
	// only source of the baseline; there is no field for caller-asserted facts.
	WorkerState people.Explanation

	Target TargetPlacement

	Current  rewards.CompensationSnapshot
	Proposed rewards.CompensationSnapshot

	EffectiveDate values.LocalDate
	// EvaluationDate is "today" as the caller declares it. The domain never
	// reads a clock: a preflight that consulted the wall clock would produce a
	// different verdict on a replay.
	EvaluationDate values.LocalDate

	BusinessReason string
	Budget         *BudgetAuthorityRef
	Policy         Policy
	Annualization  rewards.AnnualizationRule

	// PositionReader answers the Position domain's own ports
	// (CheckCompatibility, CalculateCapacity) fresh for a selected target
	// position (PROMOUX-004). It is required whenever TargetPositionSelection
	// is set; a nil reader with no selection is fine, because the
	// legacy job/grade-only path never consults it.
	PositionReader position.PositionFacts
	// Reservations is PROMOUX-004's ground-five boundary: reservation
	// ownership. Required whenever TargetPositionSelection is set. See
	// [PositionReservationAdmitter].
	Reservations PositionReservationAdmitter
	// TargetPositionSelection is the picker-disclosed position reference a
	// promotion binds its target position to (PROMOUX-004). nil means no
	// position was selected through the picker; in that case a non-empty
	// legacy Target.PositionID is refused rather than trusted as-is (see
	// [evaluateTargetPositionSelection]).
	TargetPositionSelection *PositionSelection

	// ManagerFacts answers the Organization capability's own
	// org.WorkerFacts port (PROMOUX-005): existence, disclosure and
	// reporting-chain reachability for a selected target manager and every
	// affected direct report. Required whenever TargetManagerSelection is
	// set; a nil reader with no selection is fine, because a promotion that
	// names no target manager never needs one.
	ManagerFacts org.WorkerFacts
	// TargetManagerSelection is the caller-declared management-promotion
	// intention (PROMOUX-005): the candidate manager and the affected
	// direct-report scope. nil means this is not a management promotion in
	// that sense -- the job/grade/org-only preflight this package always
	// ran is unaffected. See [evaluateTargetManagerSelection].
	TargetManagerSelection *TargetManagerSelection
}

// Validate reports whether the request is well formed. Business problems are
// findings; only contract failures are errors here.
func (r PreflightRequest) Validate() error {
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %w", ErrRequestInvalid, err)
	}
	if err := r.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: subject: %w", ErrRequestInvalid, err)
	}
	if r.Subject.Tenant != r.Tenant {
		return fmt.Errorf("%w: subject %s is outside tenant %s", ErrRequestInvalid, r.Subject, r.Tenant)
	}
	if r.WorkerState.Disclosure != people.DisclosureWithheld && r.WorkerState.Worker != r.Subject {
		return fmt.Errorf("%w: worker state describes %s, request subject is %s",
			ErrRequestInvalid, r.WorkerState.Worker, r.Subject)
	}
	if err := r.EvaluationDate.Validate(); err != nil {
		return fmt.Errorf("%w: evaluation date: %w", ErrRequestInvalid, err)
	}
	if err := r.Policy.Validate(); err != nil {
		return err
	}
	return r.Annualization.Validate()
}

// bandQuery derives the pay-band question from the target placement, falling
// back to the worker's current pay zone when the target does not move it. It
// returns false when the target is not specified enough to ask.
func (r PreflightRequest) bandQuery(baseline WorkerBaseline) (rewards.BandQuery, bool) {
	currency := ""
	if m, ok := r.Proposed.Base.Get(); ok {
		currency = m.Currency()
	}
	payZone := r.Target.PayZone
	if payZone == "" {
		payZone = baseline.PayZone
	}
	if r.Target.JobCode == "" || r.Target.Grade == "" || payZone == "" || currency == "" {
		return rewards.BandQuery{}, false
	}
	if r.EffectiveDate.Validate() != nil {
		return rewards.BandQuery{}, false
	}
	return rewards.BandQuery{
		Tenant:   r.Tenant,
		JobCode:  r.Target.JobCode,
		Grade:    r.Target.Grade,
		PayZone:  payZone,
		Currency: currency,
		AsOf:     r.EffectiveDate,
	}, true
}

// PreflightResult is the typed verdict INTENT-004 requires: a status, the
// findings that produced it, and the exact input snapshot it ran on.
type PreflightResult struct {
	IntentType    string
	IntentVersion string

	Status   Status
	Findings []Finding
	Input    InputSnapshot
	Band     rewards.BandResult

	PolicyVersion   string
	RulePackVersion string
	InputsDigest    string
	ResultDigest    string
	Effects         evidence.EffectCounters
	Receipt         evidence.ZeroEffectReceipt
}

// Blocking returns the findings that prevent the promotion as proposed.
func (r PreflightResult) Blocking() []Finding {
	out := make([]Finding, 0, len(r.Findings))
	for _, f := range r.Findings {
		if f.Severity == SeverityBlocking || f.Severity == SeverityDenied || f.Severity == SeverityNeedsData {
			out = append(out, f)
		}
	}
	return out
}

// Advisory returns the findings a reviewer should weigh.
func (r PreflightResult) Advisory() []Finding {
	out := make([]Finding, 0, len(r.Findings))
	for _, f := range r.Findings {
		if f.Severity == SeverityAdvisory {
			out = append(out, f)
		}
	}
	return out
}

// HasCode reports whether the result carries a finding with the given code.
func (r PreflightResult) HasCode(code string) bool {
	for _, f := range r.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

// canonicalBody encodes everything the result digest covers.
func (r PreflightResult) canonicalBody() ([]byte, error) {
	w := canonicalbytes.New("hcmnext.domains.promotion.PreflightResult", promotionSchemaVer).
		String("intent_type", r.IntentType).
		String("intent_version", r.IntentVersion).
		String("status", r.Status.String()).
		Count("findings", len(r.Findings))
	for _, f := range r.Findings {
		w.String("finding.code", f.Code).
			String("finding.severity", f.Severity.String()).
			String("finding.field", f.Field).
			String("finding.message", f.Message)
	}
	return w.
		Value("input", r.Input).
		Value("band", r.Band).
		String("policy_version", r.PolicyVersion).
		String("rule_pack_version", r.RulePackVersion).
		Value("effects", r.Effects).
		Bytes()
}

// Canonical returns the canonical byte encoding of the whole result.
func (r PreflightResult) Canonical() []byte {
	body, err := r.canonicalBody()
	if err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.promotion.PreflightEnvelope", promotionSchemaVer).
		Field("body", body).
		String("inputs_digest", r.InputsDigest).
		Value("receipt", r.Receipt).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

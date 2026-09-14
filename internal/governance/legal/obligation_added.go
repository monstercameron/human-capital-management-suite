package legal

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// This file carries the twelve obligation kinds LEGAL-011 adds, from the
// contract's section 4.2. Three rules govern every type here.
//
// Field order is the digest's field order. The contract's section 3.2 digests
// "every typed body field, in the field order declared in section 4", so the
// struct fields below appear in exactly the order section 4.2's table lists
// them, and canonicalBody walks them in that same order. Reordering a field
// is a release-digest change and therefore a major version bump.
//
// Optional numeric fields use zero as "absent". None of them has a meaningful
// zero: a zero-day deadline, a zero-employee size floor or a zero-month audit
// period would all be nonsense, so the encoding does not need a separate
// presence bit and a definition file may simply omit the key.
//
// Standard is not in section 4.2's field list. It comes from section 7.1: an
// extractor may narrow a research claim or drop it, never broaden it, so a
// rule the research states as a recommendation is carried as
// [RuleStandardRecommended] with [ConfidenceMarkerVerify] rather than
// promoted to a statutory requirement. It is a typed body field and is
// therefore inside the digest.

// RuleStandard separates a statutory requirement from a research
// recommendation the extractor refused to promote.
type RuleStandard uint8

// Rule standards.
const (
	// RuleStandardUnspecified is the zero value and reads as REQUIRED, so a
	// hand-built pack that predates the qualifier keeps its meaning.
	RuleStandardUnspecified RuleStandard = iota
	// RuleStandardRequired means the source states the rule as a legal
	// requirement.
	RuleStandardRequired
	// RuleStandardRecommended means the source states the rule as a best
	// practice or a recommendation. It never becomes a mandatory prohibition.
	RuleStandardRecommended
)

var ruleStandardWire = map[RuleStandard]string{
	RuleStandardRequired:    "REQUIRED",
	RuleStandardRecommended: "RECOMMENDED",
}

// String returns the stable wire token. The zero value reads as REQUIRED.
func (s RuleStandard) String() string {
	if s == RuleStandardUnspecified {
		return "REQUIRED"
	}
	if w, ok := ruleStandardWire[s]; ok {
		return w
	}
	return "REQUIRED"
}

// ParseRuleStandard maps a wire token back to a standard.
func ParseRuleStandard(token string) (RuleStandard, error) {
	switch token {
	case "", "REQUIRED":
		return RuleStandardRequired, nil
	case "RECOMMENDED":
		return RuleStandardRecommended, nil
	default:
		return RuleStandardUnspecified, fmt.Errorf("legal: unknown rule standard %q", token)
	}
}

// requireEnum reports an error when got is outside the allowed set. The
// empty string is always allowed and means "the source does not state it": an
// extractor that invented an enum value to satisfy a validator would be
// broadening a research claim, which the contract's section 7.1 forbids. A
// release above UNREVIEWED closes the gap through validateComplete.
func requireEnum(kind, id, field, got string, allowed ...string) error {
	if got == "" {
		return nil
	}
	for _, a := range allowed {
		if got == a {
			return nil
		}
	}
	return fmt.Errorf("legal: %s %q field %s = %q, want one of %v", kind, id, field, got, allowed)
}

// WageFloorRule is a statutory minimum rate of pay for a class of worker, and
// how that rate moves over time. Thirty research files assert a state-level
// minimum above or apart from the federal floor.
//
// Trigger: unconditional. Lifecycle: PREFLIGHT, EXECUTE. Binding: GUARD.
type WageFloorRule struct {
	ID          string
	FloorAmount values.Money
	WorkerClass string
	// Basis is HOURLY, WEEKLY or ANNUAL.
	Basis string
	// Indexation is NONE, CPI or SCHEDULE.
	Indexation string
	// NextAdjustmentDate is optional; an unset date means the source states
	// no next adjustment.
	NextAdjustmentDate values.LocalDate
	Standard           RuleStandard
	Citation           Citation
}

func (o WageFloorRule) validate() error {
	if o.ID == "" {
		return ErrObligationID
	}
	if err := requireEnum("wage floor", o.ID, "basis", o.Basis, "HOURLY", "WEEKLY", "ANNUAL"); err != nil {
		return err
	}
	if err := requireEnum("wage floor", o.ID, "indexation", o.Indexation, "NONE", "CPI", "SCHEDULE"); err != nil {
		return err
	}
	return o.Citation.Validate()
}

func (o WageFloorRule) canonicalBody(dst []byte) []byte {
	dst = appendField(dst, "id", o.ID)
	dst = appendField(dst, "floor_amount", o.FloorAmount.String())
	dst = appendField(dst, "worker_class", o.WorkerClass)
	dst = appendField(dst, "basis", o.Basis)
	dst = appendField(dst, "indexation", o.Indexation)
	dst = appendField(dst, "next_adjustment_date", o.NextAdjustmentDate.String())
	return appendField(dst, "standard", o.Standard.String())
}

// PayEquityReviewRule is a duty to justify a pay decision against a
// comparator standard before it commits. Thirty-six research files assert one.
//
// Trigger: base pay rate changed. Lifecycle: SIMULATE, APPROVAL. Binding:
// HUMAN_TASK.
type PayEquityReviewRule struct {
	ID                 string
	ProtectedBases     []string
	ComparatorStandard string
	// EmployerSizeFloor is optional; zero means the rule applies at any size.
	EmployerSizeFloor      int
	PermittedDifferentials []string
	DocumentationRequired  bool
	Standard               RuleStandard
	Citation               Citation
}

func (o PayEquityReviewRule) validate() error {
	if o.ID == "" {
		return ErrObligationID
	}
	if o.EmployerSizeFloor < 0 {
		return fmt.Errorf("legal: pay equity review %q has a negative employer size floor", o.ID)
	}
	return o.Citation.Validate()
}

func (o PayEquityReviewRule) canonicalBody(dst []byte) []byte {
	dst = appendField(dst, "id", o.ID)
	dst = appendStringSlice(dst, "protected_bases", o.ProtectedBases)
	dst = appendField(dst, "comparator_standard", o.ComparatorStandard)
	dst = appendUint32Field(dst, "employer_size_floor", uint32(o.EmployerSizeFloor))
	dst = appendStringSlice(dst, "permitted_differentials", o.PermittedDifferentials)
	dst = appendFieldBool(dst, "documentation_required", o.DocumentationRequired)
	return appendField(dst, "standard", o.Standard.String())
}

// PayStatementRule is what a wage statement must contain and how it may be
// delivered after a pay change. Eighteen research files assert one.
//
// Trigger: base pay rate changed. Lifecycle: POST-COMMIT. Binding: NODE.
type PayStatementRule struct {
	ID             string
	RequiredFields []string
	// Delivery is PAPER, ELECTRONIC or EITHER.
	Delivery        string
	ConsentRequired bool
	Standard        RuleStandard
	Citation        Citation
}

func (o PayStatementRule) validate() error {
	if o.ID == "" {
		return ErrObligationID
	}
	if err := requireEnum("pay statement", o.ID, "delivery", o.Delivery, "PAPER", "ELECTRONIC", "EITHER"); err != nil {
		return err
	}
	return o.Citation.Validate()
}

func (o PayStatementRule) canonicalBody(dst []byte) []byte {
	dst = appendField(dst, "id", o.ID)
	dst = appendStringSlice(dst, "required_fields", o.RequiredFields)
	dst = appendField(dst, "delivery", o.Delivery)
	dst = appendFieldBool(dst, "consent_required", o.ConsentRequired)
	return appendField(dst, "standard", o.Standard.String())
}

// ClassificationRule is a typed exemption, overtime-threshold or contractor
// test the platform must surface — never decide. Ten research files assert a
// state test distinct from the federal one.
//
// Trigger: role, hours, pay basis or pay rate changed. Lifecycle: PREFLIGHT,
// APPROVAL. Binding: GUARD, HUMAN_TASK.
type ClassificationRule struct {
	ID string
	// Dimension is EXEMPTION, OVERTIME_THRESHOLD or CONTRACTOR.
	Dimension       string
	TestDescription string
	// SalaryThreshold is optional; an invalid/unset Money means the source
	// states no salary threshold.
	SalaryThreshold values.Money
	// OvertimeTrigger is optional; empty means the source states none.
	OvertimeTrigger string
	Standard        RuleStandard
	Citation        Citation
}

func (o ClassificationRule) validate() error {
	if o.ID == "" {
		return ErrObligationID
	}
	if err := requireEnum("classification", o.ID, "dimension", o.Dimension,
		"EXEMPTION", "OVERTIME_THRESHOLD", "CONTRACTOR"); err != nil {
		return err
	}
	return o.Citation.Validate()
}

func (o ClassificationRule) canonicalBody(dst []byte) []byte {
	dst = appendField(dst, "id", o.ID)
	dst = appendField(dst, "dimension", o.Dimension)
	dst = appendField(dst, "test_description", o.TestDescription)
	dst = appendField(dst, "salary_threshold", o.SalaryThreshold.String())
	dst = appendField(dst, "overtime_trigger", o.OvertimeTrigger)
	return appendField(dst, "standard", o.Standard.String())
}

// PersonnelFileRule is the worker's right to inspect their own file and the
// deadline the employer answers on. Eighteen research files assert one.
//
// Trigger: unconditional. Lifecycle: POST-COMMIT. Binding: NODE.
type PersonnelFileRule struct {
	ID           string
	ResponseDays int
	// DayBasis is CALENDAR or BUSINESS.
	DayBasis string
	// FrequencyCapPerYear is optional; zero means the source states no cap.
	FrequencyCapPerYear int
	CopyFeePermitted    bool
	Standard            RuleStandard
	Citation            Citation
}

func (o PersonnelFileRule) validate() error {
	if o.ID == "" {
		return ErrObligationID
	}
	if o.ResponseDays < 0 {
		return fmt.Errorf("legal: personnel file %q has a negative response deadline", o.ID)
	}
	if err := requireEnum("personnel file", o.ID, "day_basis", o.DayBasis, "CALENDAR", "BUSINESS"); err != nil {
		return err
	}
	return o.Citation.Validate()
}

func (o PersonnelFileRule) canonicalBody(dst []byte) []byte {
	dst = appendField(dst, "id", o.ID)
	dst = appendUint32Field(dst, "response_days", uint32(o.ResponseDays))
	dst = appendField(dst, "day_basis", o.DayBasis)
	dst = appendUint32Field(dst, "frequency_cap_per_year", uint32(o.FrequencyCapPerYear))
	dst = appendFieldBool(dst, "copy_fee_permitted", o.CopyFeePermitted)
	return appendField(dst, "standard", o.Standard.String())
}

// AntiRetaliationRule is the protected-activity check a pay or role change
// runs before it commits. Every one of the fifty-one research files records a
// protected-activity exception, which is precisely why the lookback windows
// and dispositions are typed and cited rather than assumed.
//
// Trigger: a protected activity is recorded inside the lookback. Lifecycle:
// PREFLIGHT, APPROVAL. Binding: GUARD, HUMAN_TASK.
type AntiRetaliationRule struct {
	ID                  string
	ProtectedActivities []string
	LookbackDays        int
	// Disposition is FLAG or BLOCK.
	Disposition string
	Standard    RuleStandard
	Citation    Citation
}

func (o AntiRetaliationRule) validate() error {
	if o.ID == "" {
		return ErrObligationID
	}
	if o.LookbackDays < 0 {
		return fmt.Errorf("legal: anti-retaliation %q has a negative lookback window", o.ID)
	}
	if err := requireEnum("anti-retaliation", o.ID, "disposition", o.Disposition, "FLAG", "BLOCK"); err != nil {
		return err
	}
	return o.Citation.Validate()
}

func (o AntiRetaliationRule) canonicalBody(dst []byte) []byte {
	dst = appendField(dst, "id", o.ID)
	dst = appendStringSlice(dst, "protected_activities", o.ProtectedActivities)
	dst = appendUint32Field(dst, "lookback_days", uint32(o.LookbackDays))
	dst = appendField(dst, "disposition", o.Disposition)
	return appendField(dst, "standard", o.Standard.String())
}

// JobSecurityRule is the standard an adverse change is judged against. Four
// states impose a statutory standard that changes the shape of the flow; the
// rest route a handbook promise to review. Standard distinguishes them.
//
// Trigger: adverse change (pay decrease, demotion, separation). Lifecycle:
// APPROVAL. Binding: HUMAN_TASK.
type JobSecurityRule struct {
	ID string
	// StandardKind is AT_WILL, GOOD_CAUSE_AFTER_PROBATION,
	// HANDBOOK_DISCLAIMER or IMPLIED_CONTRACT_REVIEW. It is section 4.2's
	// `standard` field; it is named StandardKind here so it does not collide
	// with the section 7.1 requirement/recommendation qualifier.
	StandardKind string
	// ProbationDays is optional; zero means the source states no probation
	// period.
	ProbationDays         int
	JustificationRequired bool
	Standard              RuleStandard
	Citation              Citation
}

func (o JobSecurityRule) validate() error {
	if o.ID == "" {
		return ErrObligationID
	}
	if err := requireEnum("job security", o.ID, "standard", o.StandardKind,
		"AT_WILL", "GOOD_CAUSE_AFTER_PROBATION", "HANDBOOK_DISCLAIMER", "IMPLIED_CONTRACT_REVIEW"); err != nil {
		return err
	}
	return o.Citation.Validate()
}

func (o JobSecurityRule) canonicalBody(dst []byte) []byte {
	dst = appendField(dst, "id", o.ID)
	dst = appendField(dst, "standard_kind", o.StandardKind)
	dst = appendUint32Field(dst, "probation_days", uint32(o.ProbationDays))
	dst = appendFieldBool(dst, "justification_required", o.JustificationRequired)
	return appendField(dst, "standard", o.Standard.String())
}

// SeparationFilingRule is a form a separation owes a state authority. Nine
// research files name a form and a deadline.
//
// The obligation produces a deadline, an owner and content — never a
// transmission. Non-goal 2 in the contract's section 10 forbids Human Capital Management Suite from
// filing with any state agency, and no rule pack may declare a transmitting
// effect.
//
// Trigger: concurrent separation. Lifecycle: POST-COMMIT. Binding: NODE.
type SeparationFilingRule struct {
	ID                 string
	FormName           string
	RecipientAuthority string
	DeadlineDays       int
	// DayBasis is CALENDAR or BUSINESS.
	DayBasis      string
	ContentFields []string
	Standard      RuleStandard
	Citation      Citation
}

func (o SeparationFilingRule) validate() error {
	if o.ID == "" {
		return ErrObligationID
	}
	if o.DeadlineDays < 0 {
		return fmt.Errorf("legal: separation filing %q has a negative deadline", o.ID)
	}
	if err := requireEnum("separation filing", o.ID, "day_basis", o.DayBasis, "CALENDAR", "BUSINESS"); err != nil {
		return err
	}
	return o.Citation.Validate()
}

func (o SeparationFilingRule) canonicalBody(dst []byte) []byte {
	dst = appendField(dst, "id", o.ID)
	dst = appendField(dst, "form_name", o.FormName)
	dst = appendField(dst, "recipient_authority", o.RecipientAuthority)
	dst = appendUint32Field(dst, "deadline_days", uint32(o.DeadlineDays))
	dst = appendField(dst, "day_basis", o.DayBasis)
	dst = appendStringSlice(dst, "content_fields", o.ContentFields)
	return appendField(dst, "standard", o.Standard.String())
}

// DrugTestingRule is when a test may lawfully be ordered and who is protected
// from one. Thirteen research files assert a state rule.
//
// Trigger: the role becomes safety-sensitive, or a test is ordered.
// Lifecycle: PREFLIGHT. Binding: GUARD.
type DrugTestingRule struct {
	ID                    string
	PermittedBases        []string
	WrittenPolicyRequired bool
	// AdvanceNoticeDays is optional; zero means the source states none.
	AdvanceNoticeDays int
	ProtectedStatus   []string
	Standard          RuleStandard
	Citation          Citation
}

func (o DrugTestingRule) validate() error {
	if o.ID == "" {
		return ErrObligationID
	}
	return o.Citation.Validate()
}

func (o DrugTestingRule) canonicalBody(dst []byte) []byte {
	dst = appendField(dst, "id", o.ID)
	dst = appendStringSlice(dst, "permitted_bases", o.PermittedBases)
	dst = appendFieldBool(dst, "written_policy_required", o.WrittenPolicyRequired)
	dst = appendUint32Field(dst, "advance_notice_days", uint32(o.AdvanceNoticeDays))
	dst = appendStringSlice(dst, "protected_status", o.ProtectedStatus)
	return appendField(dst, "standard", o.Standard.String())
}

// BreachNotificationRule is the notification clock a personal-data breach
// starts. Forty-nine research files assert one; only Massachusetts is
// unresolved. The day counts and authority thresholds differ per state, which
// is why this is a typed, cited kind and not a platform constant.
//
// Trigger: a personal-data breach incident is opened. Lifecycle:
// POST-COMMIT. Binding: TIMER.
type BreachNotificationRule struct {
	ID                  string
	SubjectDeadlineDays int
	// DayBasis is CALENDAR or BUSINESS.
	DayBasis string
	// AuthorityThresholdCount and AuthorityDeadlineDays are optional; zero
	// means the source states none.
	AuthorityThresholdCount  int
	AuthorityDeadlineDays    int
	CreditMonitoringRequired bool
	Standard                 RuleStandard
	Citation                 Citation
}

func (o BreachNotificationRule) validate() error {
	if o.ID == "" {
		return ErrObligationID
	}
	if o.SubjectDeadlineDays < 0 {
		return fmt.Errorf("legal: breach notification %q has a negative subject deadline", o.ID)
	}
	if err := requireEnum("breach notification", o.ID, "day_basis", o.DayBasis, "CALENDAR", "BUSINESS"); err != nil {
		return err
	}
	return o.Citation.Validate()
}

func (o BreachNotificationRule) canonicalBody(dst []byte) []byte {
	dst = appendField(dst, "id", o.ID)
	dst = appendUint32Field(dst, "subject_deadline_days", uint32(o.SubjectDeadlineDays))
	dst = appendField(dst, "day_basis", o.DayBasis)
	dst = appendUint32Field(dst, "authority_threshold_count", uint32(o.AuthorityThresholdCount))
	dst = appendUint32Field(dst, "authority_deadline_days", uint32(o.AuthorityDeadlineDays))
	dst = appendFieldBool(dst, "credit_monitoring_required", o.CreditMonitoringRequired)
	return appendField(dst, "standard", o.Standard.String())
}

// AutomatedDecisionRule governs a model that scored, ranked or recommended
// the subject of the transaction. Three research files assert a rule
// (Colorado, Illinois, and New York City as a locality).
//
// Trigger: a model scored, ranked or recommended the subject. Lifecycle:
// DRAFT, APPROVAL. Binding: GUARD, HUMAN_TASK.
type AutomatedDecisionRule struct {
	ID                string
	CoveredUses       []string
	BiasAuditRequired bool
	// AuditPeriodMonths and CandidateNoticeDays are optional; zero means the
	// source states none.
	AuditPeriodMonths   int
	CandidateNoticeDays int
	DisclosureRequired  bool
	Standard            RuleStandard
	Citation            Citation
}

func (o AutomatedDecisionRule) validate() error {
	if o.ID == "" {
		return ErrObligationID
	}
	return o.Citation.Validate()
}

func (o AutomatedDecisionRule) canonicalBody(dst []byte) []byte {
	dst = appendField(dst, "id", o.ID)
	dst = appendStringSlice(dst, "covered_uses", o.CoveredUses)
	dst = appendFieldBool(dst, "bias_audit_required", o.BiasAuditRequired)
	dst = appendUint32Field(dst, "audit_period_months", uint32(o.AuditPeriodMonths))
	dst = appendUint32Field(dst, "candidate_notice_days", uint32(o.CandidateNoticeDays))
	dst = appendFieldBool(dst, "disclosure_required", o.DisclosureRequired)
	return appendField(dst, "standard", o.Standard.String())
}

// MonitoringConsentRule governs reading or writing a covered worker-data
// category. Five research files assert a state rule and none of the five
// binds a promotion; the kind is declared so a later timekeeping or
// monitoring capability does not invent it.
//
// Trigger: the transaction reads or writes a covered data category.
// Lifecycle: DRAFT. Binding: FIELD_MASK.
type MonitoringConsentRule struct {
	ID             string
	DataCategories []string
	// ConsentForm is WRITTEN or NOTICE_ONLY.
	ConsentForm string
	// RetentionLimitMonths and DeletionDeadlineDays are optional; zero means
	// the source states none.
	RetentionLimitMonths int
	DeletionDeadlineDays int
	Standard             RuleStandard
	Citation             Citation
}

func (o MonitoringConsentRule) validate() error {
	if o.ID == "" {
		return ErrObligationID
	}
	if err := requireEnum("monitoring consent", o.ID, "consent_form", o.ConsentForm, "WRITTEN", "NOTICE_ONLY"); err != nil {
		return err
	}
	return o.Citation.Validate()
}

func (o MonitoringConsentRule) canonicalBody(dst []byte) []byte {
	dst = appendField(dst, "id", o.ID)
	dst = appendStringSlice(dst, "data_categories", o.DataCategories)
	dst = appendField(dst, "consent_form", o.ConsentForm)
	dst = appendUint32Field(dst, "retention_limit_months", uint32(o.RetentionLimitMonths))
	dst = appendUint32Field(dst, "deletion_deadline_days", uint32(o.DeletionDeadlineDays))
	return appendField(dst, "standard", o.Standard.String())
}

// PreemptionAssertion is a subdivision-level release's claim that it preempts
// locality-level rules of a named kind. It is a first-class assertion rather
// than a composition heuristic precisely so a naive "most protective wins"
// rule can never attach a preempted local obligation.
//
// The composition step that consumes it is LEGAL-013; this package carries,
// digests and validates the assertion.
type PreemptionAssertion struct {
	Kind ObligationType
	// Scope is LOCALITY_ONLY. An assertion never removes a subdivision or
	// country obligation, and never applies across kinds.
	Scope    string
	Citation Citation
}

// Validate reports whether the assertion is well formed.
func (a PreemptionAssertion) Validate() error {
	if a.Kind == ObligationTypeUnspecified {
		return fmt.Errorf("legal: preemption assertion names no obligation kind")
	}
	if a.Scope != "LOCALITY_ONLY" {
		return fmt.Errorf("legal: preemption assertion scope = %q, want LOCALITY_ONLY", a.Scope)
	}
	return a.Citation.Validate()
}

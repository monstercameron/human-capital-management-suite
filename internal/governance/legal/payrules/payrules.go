// Package payrules contains kernel-pure, versioned state employment-law
// parameters used by pay-change governance. The package deliberately owns no
// persistence or transmission behavior; callers provide YAML bytes or a file
// path and receive a validated, digested value.
package payrules

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const SchemaVersion uint32 = 1

var (
	ErrValidation       = errors.New("payrules: PACK_VALIDATION_FAILED")
	ErrDigestMismatch   = errors.New("payrules: digest mismatch")
	ErrUnknownState     = errors.New("payrules: unknown state")
	ErrUnknownEnum      = errors.New("payrules: unknown enum value")
	ErrIncompleteStates = errors.New("payrules: registry must contain one row per state")
)

// ValidationRefusal is a typed, field-addressed refusal. Field always names
// the failing schema path, including the row index when applicable.
type ValidationRefusal struct {
	Field  string
	Reason string
}

func (e *ValidationRefusal) Error() string {
	return fmt.Sprintf("%v: field %q: %s", ErrValidation, e.Field, e.Reason)
}

func (e *ValidationRefusal) Unwrap() error { return ErrValidation }

func refuse(field, reason string) error { return &ValidationRefusal{Field: field, Reason: reason} }

// ReviewStatus is the review state of one registry row.
type ReviewStatus string

const (
	Reviewed   ReviewStatus = "REVIEWED"
	Unreviewed ReviewStatus = "UNREVIEWED"
	// ReviewStatusReviewed and ReviewStatusUnreviewed are descriptive aliases
	// for callers that prefer the type-qualified naming convention.
	ReviewStatusReviewed   = Reviewed
	ReviewStatusUnreviewed = Unreviewed
)

// Citation is the statute or regulation reference supporting one parameter.
// SourceFile points back to the research corpus; Section preserves the
// citation spelling used by that corpus.
type Citation struct {
	SourceFile string `yaml:"source_file" json:"source_file"`
	Section    string `yaml:"section" json:"section"`
	Note       string `yaml:"note,omitempty" json:"note,omitempty"`
}

func (c Citation) validate(field string) error {
	if strings.TrimSpace(c.SourceFile) == "" {
		return refuse(field+".source_file", "source file is required")
	}
	if strings.TrimSpace(c.Section) == "" {
		return refuse(field+".section", "statute citation is required")
	}
	return nil
}

// TemporalSchema is shared by every parameter set. EffectiveFrom and
// EffectiveUntil are business-law dates; KnownAt is the knowledge timestamp
// at which the source interpretation was available.
type TemporalSchema struct {
	SchemaVersion  uint32
	Version        uint32
	EffectiveFrom  values.LocalDate
	EffectiveUntil *values.LocalDate
	KnownAt        values.KnownAt
	Digest         string
}

func (m TemporalSchema) validate(field string) error {
	if m.SchemaVersion != SchemaVersion {
		return refuse(field+".schema_version", fmt.Sprintf("want %d, got %d", SchemaVersion, m.SchemaVersion))
	}
	if m.Version == 0 {
		return refuse(field+".version", "must be positive")
	}
	if err := m.EffectiveFrom.Validate(); err != nil {
		return refuse(field+".effective_from", "must be YYYY-MM-DD")
	}
	if m.EffectiveUntil != nil {
		if err := m.EffectiveUntil.Validate(); err != nil {
			return refuse(field+".effective_until", "must be YYYY-MM-DD")
		}
		if m.EffectiveUntil.Compare(m.EffectiveFrom) <= 0 {
			return refuse(field+".effective_until", "must be after effective_from")
		}
	}
	if err := m.KnownAt.Instant().Validate(); err != nil {
		return refuse(field+".known_at", "must be a valid UTC instant")
	}
	if len(m.Digest) != sha256.Size*2 {
		return refuse(field+".digest", "must be a lowercase SHA-256 digest")
	}
	if m.Digest != strings.ToLower(m.Digest) {
		return refuse(field+".digest", "must be lowercase")
	}
	return nil
}

func parseDate(s, field string) (values.LocalDate, error) {
	d, err := values.ParseLocalDate(strings.TrimSpace(s))
	if err != nil {
		return values.LocalDate{}, refuse(field, "must be YYYY-MM-DD")
	}
	return d, nil
}

func parseInstant(s, field string) (values.KnownAt, error) {
	var instant values.Instant
	if err := instant.UnmarshalText([]byte(strings.TrimSpace(s))); err != nil {
		return values.KnownAt{}, refuse(field, "must be a valid UTC instant")
	}
	known, err := values.NewKnownAt(instant)
	if err != nil {
		return values.KnownAt{}, refuse(field, "must be a valid UTC instant")
	}
	return known, nil
}

func parseTemporal(schemaVersion, version uint32, from, until, known, digest string) (TemporalSchema, error) {
	start, err := parseDate(from, "effective_from")
	if err != nil {
		return TemporalSchema{}, err
	}
	var end *values.LocalDate
	if strings.TrimSpace(until) != "" {
		d, err := parseDate(until, "effective_until")
		if err != nil {
			return TemporalSchema{}, err
		}
		end = &d
	}
	k, err := parseInstant(known, "known_at")
	if err != nil {
		return TemporalSchema{}, err
	}
	return TemporalSchema{
		SchemaVersion:  schemaVersion,
		Version:        version,
		EffectiveFrom:  start,
		EffectiveUntil: end,
		KnownAt:        k,
		Digest:         digest,
	}, nil
}

func (m TemporalSchema) appliesOn(date values.LocalDate) bool {
	if date.Validate() != nil || m.EffectiveFrom.Validate() != nil || date.Compare(m.EffectiveFrom) < 0 {
		return false
	}
	return m.EffectiveUntil == nil || date.Compare(*m.EffectiveUntil) < 0
}

// Frequency is the minimum permitted pay cadence.
type Frequency string

const (
	Weekly      Frequency = "WEEKLY"
	Biweekly    Frequency = "BIWEEKLY"
	Semimonthly Frequency = "SEMIMONTHLY"
	Monthly     Frequency = "MONTHLY"
)

// NoticeLeadUnit describes the clock used for advance notice.
type NoticeLeadUnit string

const (
	CalendarDays NoticeLeadUnit = "CALENDAR_DAYS"
	BusinessDays NoticeLeadUnit = "BUSINESS_DAYS"
	PayPeriods   NoticeLeadUnit = "PAY_PERIODS"
	PriorPayday  NoticeLeadUnit = "PRIOR_PAYDAY"
)

// ChangeDirection scopes a lead time to pay increases, decreases, or both.
type ChangeDirection string

const (
	Increase ChangeDirection = "INCREASE"
	Decrease ChangeDirection = "DECREASE"
	Both     ChangeDirection = "BOTH"
)

// NoticeLeadTime is the typed advance-notice requirement for a pay change.
type NoticeLeadTime struct {
	LeadUnit  NoticeLeadUnit  `yaml:"lead_unit" json:"lead_unit"`
	LeadCount int             `yaml:"lead_count" json:"lead_count"`
	AppliesTo ChangeDirection `yaml:"applies_to" json:"applies_to"`
}

func (n NoticeLeadTime) validate(field string) error {
	switch n.LeadUnit {
	case CalendarDays, BusinessDays, PayPeriods, PriorPayday:
	default:
		return refuse(field+".lead_unit", fmt.Sprintf("%v: %q", ErrUnknownEnum, n.LeadUnit))
	}
	if n.LeadCount < 0 {
		return refuse(field+".lead_count", "must not be negative")
	}
	switch n.AppliesTo {
	case Increase, Decrease, Both:
	default:
		return refuse(field+".applies_to", fmt.Sprintf("%v: %q", ErrUnknownEnum, n.AppliesTo))
	}
	if n.LeadUnit == PriorPayday && n.LeadCount != 0 {
		return refuse(field+".lead_count", "PRIOR_PAYDAY uses lead_count 0")
	}
	return nil
}

// PayFrequencyConstraint is one state or worker-class pay-frequency rule.
type PayFrequencyConstraint struct {
	ID                   string         `yaml:"id" json:"id"`
	StateCode            string         `yaml:"state_code" json:"state_code"`
	MinFrequency         Frequency      `yaml:"min_frequency" json:"min_frequency"`
	NoticeLeadTime       NoticeLeadTime `yaml:"notice_lead_time" json:"notice_lead_time"`
	Citation             Citation       `yaml:"citation" json:"citation"`
	Status               ReviewStatus   `yaml:"status" json:"status"`
	AppliesToWorkerClass string         `yaml:"applies_to_worker_class,omitempty" json:"applies_to_worker_class,omitempty"`
}

func (c PayFrequencyConstraint) validate(field string) error {
	if strings.TrimSpace(c.ID) == "" {
		return refuse(field+".id", "is required")
	}
	if err := validateState(c.StateCode, field+".state_code"); err != nil {
		return err
	}
	switch c.MinFrequency {
	case Weekly, Biweekly, Semimonthly, Monthly:
	default:
		return refuse(field+".min_frequency", fmt.Sprintf("%v: %q", ErrUnknownEnum, c.MinFrequency))
	}
	if err := c.NoticeLeadTime.validate(field + ".notice_lead_time"); err != nil {
		return err
	}
	if c.Status != Reviewed && c.Status != Unreviewed {
		return refuse(field+".status", "must be REVIEWED or UNREVIEWED")
	}
	return c.Citation.validate(field + ".citation")
}

// PayFrequencyParameterSet is a versioned, digested collection of typed
// pay-frequency and change-notice constraints.
type PayFrequencyParameterSet struct {
	TemporalSchema
	Constraints []PayFrequencyConstraint `yaml:"constraints" json:"constraints"`
}

func (s PayFrequencyParameterSet) Validate() error {
	if err := s.TemporalSchema.validate("schema"); err != nil {
		return err
	}
	if len(s.Constraints) == 0 {
		return refuse("constraints", "must contain at least one rule")
	}
	seen := map[string]bool{}
	for i, c := range s.Constraints {
		if err := c.validate(fmt.Sprintf("constraints[%d]", i)); err != nil {
			return err
		}
		if seen[c.ID] {
			return refuse(fmt.Sprintf("constraints[%d].id", i), "duplicate id")
		}
		seen[c.ID] = true
	}
	if s.ComputeDigest() != s.Digest {
		return refuse("schema.digest", ErrDigestMismatch.Error())
	}
	return nil
}

// AppliesOn reports whether business date is inside this set's effective
// window.
func (s PayFrequencyParameterSet) AppliesOn(date values.LocalDate) bool { return s.appliesOn(date) }

// Explain returns a deterministic audit-oriented summary.
func (s PayFrequencyParameterSet) Explain() string {
	return fmt.Sprintf("pay-frequency schema=%d version=%d effective=%s rules=%d digest=%s", s.SchemaVersion, s.Version, windowString(s.EffectiveFrom, s.EffectiveUntil), len(s.Constraints), s.Digest)
}

// PayChange is the fact set needed to evaluate a notice lead time.
type PayChange struct {
	EffectiveDate    values.LocalDate
	NoticeDate       values.LocalDate
	Direction        ChangeDirection
	PriorPayday      values.LocalDate
	PayPeriodsBefore int
}

// NoticeDecision is a pure result; it never sends or records a notice.
type NoticeDecision struct {
	Allowed    bool
	RequiredBy values.LocalDate
	Reason     string
}

// CheckNotice evaluates the existing NOTICE timing concept against a dated
// pay change. A zero-day calendar requirement is strict: same-day notice is
// insufficient when the source says “prior to the effective date”.
func (c PayFrequencyConstraint) CheckNotice(change PayChange) NoticeDecision {
	if change.EffectiveDate.Validate() != nil || change.NoticeDate.Validate() != nil {
		return NoticeDecision{Reason: "effective_date and notice_date are required"}
	}
	if c.NoticeLeadTime.AppliesTo != Both && c.NoticeLeadTime.AppliesTo != change.Direction {
		return NoticeDecision{Allowed: true, Reason: "change direction is outside the lead-time rule"}
	}
	n := c.NoticeLeadTime
	switch n.LeadUnit {
	case CalendarDays:
		required := change.EffectiveDate.AddDays(-n.LeadCount)
		if n.LeadCount == 0 {
			return NoticeDecision{Allowed: change.NoticeDate.Compare(change.EffectiveDate) < 0, RequiredBy: required, Reason: "notice must precede the effective date"}
		}
		return NoticeDecision{Allowed: change.NoticeDate.Compare(required) <= 0, RequiredBy: required, Reason: "notice is due by the lead-time date"}
	case BusinessDays:
		required := addBusinessDays(change.EffectiveDate, -n.LeadCount)
		return NoticeDecision{Allowed: change.NoticeDate.Compare(required) <= 0, RequiredBy: required, Reason: "notice is due by the business-day lead-time date"}
	case PayPeriods:
		return NoticeDecision{Allowed: change.PayPeriodsBefore >= n.LeadCount, Reason: "notice must precede the required number of pay periods"}
	case PriorPayday:
		if change.PriorPayday.Validate() != nil {
			return NoticeDecision{Reason: "prior_payday is required for PRIOR_PAYDAY"}
		}
		return NoticeDecision{Allowed: change.NoticeDate.Compare(change.PriorPayday) <= 0, RequiredBy: change.PriorPayday, Reason: "notice is due by the prior payday"}
	default:
		return NoticeDecision{Reason: "unknown lead unit"}
	}
}

func addBusinessDays(date values.LocalDate, n int) values.LocalDate {
	step := 1
	if n < 0 {
		step = -1
		n = -n
	}
	for n > 0 {
		date = date.AddDays(step)
		weekday := time.Date(int(date.Year()), date.Month(), int(date.Day()), 0, 0, 0, 0, time.UTC).Weekday()
		if weekday != time.Saturday && weekday != time.Sunday {
			n--
		}
	}
	return date
}

// PayStatementMandate resolves the Table A PAY_STMT cell.
type PayStatementMandate string

const (
	Mandatory   PayStatementMandate = "MANDATORY"
	NotMandated PayStatementMandate = "NOT_MANDATED"
)

type DeliveryMedium string

const (
	Paper      DeliveryMedium = "PAPER"
	Electronic DeliveryMedium = "ELECTRONIC"
	Either     DeliveryMedium = "EITHER"
)

// PayStatementRule is one state row in the pay-statement content registry.
type PayStatementRule struct {
	StateCode       string              `yaml:"state_code" json:"state_code"`
	Mandate         PayStatementMandate `yaml:"mandate" json:"mandate"`
	RequiredFields  []string            `yaml:"required_fields" json:"required_fields"`
	Delivery        DeliveryMedium      `yaml:"delivery" json:"delivery"`
	ConsentRequired bool                `yaml:"consent_required" json:"consent_required"`
	Citation        Citation            `yaml:"citation" json:"citation"`
	Status          ReviewStatus        `yaml:"status" json:"status"`
}

func (r PayStatementRule) validate(field string) error {
	if err := validateState(r.StateCode, field+".state_code"); err != nil {
		return err
	}
	if r.Mandate != Mandatory && r.Mandate != NotMandated {
		return refuse(field+".mandate", "must be MANDATORY or NOT_MANDATED")
	}
	switch r.Delivery {
	case Paper, Electronic, Either:
	default:
		return refuse(field+".delivery", "must be PAPER, ELECTRONIC, or EITHER")
	}
	if r.Mandate == NotMandated && len(r.RequiredFields) != 0 {
		return refuse(field+".required_fields", "must be empty when mandate is NOT_MANDATED")
	}
	seen := map[string]bool{}
	for i, v := range r.RequiredFields {
		if strings.TrimSpace(v) == "" {
			return refuse(fmt.Sprintf("%s.required_fields[%d]", field, i), "must not be empty")
		}
		if seen[v] {
			return refuse(fmt.Sprintf("%s.required_fields[%d]", field, i), "duplicate field")
		}
		seen[v] = true
	}
	if r.Status != Reviewed && r.Status != Unreviewed {
		return refuse(field+".status", "must be REVIEWED or UNREVIEWED")
	}
	return r.Citation.validate(field + ".citation")
}

// PayStatementRegistry is the complete 50-state content-mandate registry.
type PayStatementRegistry struct {
	TemporalSchema
	States []PayStatementRule `yaml:"states" json:"states"`
}

func (r PayStatementRegistry) Validate() error {
	if err := r.TemporalSchema.validate("schema"); err != nil {
		return err
	}
	if len(r.States) != len(USStateCodes) {
		return fmt.Errorf("%w: got %d rows, want %d", ErrIncompleteStates, len(r.States), len(USStateCodes))
	}
	seen := map[string]bool{}
	for i, row := range r.States {
		if err := row.validate(fmt.Sprintf("states[%d]", i)); err != nil {
			return err
		}
		if seen[row.StateCode] {
			return refuse(fmt.Sprintf("states[%d].state_code", i), "duplicate state row")
		}
		seen[row.StateCode] = true
	}
	for _, state := range USStateCodes {
		if !seen[state] {
			return refuse("states", "missing state "+state)
		}
	}
	if r.ComputeDigest() != r.Digest {
		return refuse("schema.digest", ErrDigestMismatch.Error())
	}
	return nil
}

func (r PayStatementRegistry) AppliesOn(date values.LocalDate) bool { return r.appliesOn(date) }

func (r PayStatementRegistry) ForState(state string) (PayStatementRule, bool) {
	for _, row := range r.States {
		if row.StateCode == state {
			return row, true
		}
	}
	return PayStatementRule{}, false
}

func (r PayStatementRegistry) Explain() string {
	return fmt.Sprintf("pay-statement-registry schema=%d version=%d effective=%s states=%d digest=%s", r.SchemaVersion, r.Version, windowString(r.EffectiveFrom, r.EffectiveUntil), len(r.States), r.Digest)
}

// LeaveProgramParameters is one paid sick/family leave program.
type LeaveProgramParameters struct {
	ID                         string             `yaml:"id" json:"id"`
	StateCode                  string             `yaml:"state_code" json:"state_code"`
	Program                    string             `yaml:"program" json:"program"`
	AccrualHoursPerHoursWorked Ratio              `yaml:"accrual_hours_per_hours_worked" json:"accrual_hours_per_hours_worked"`
	AnnualCapHours             int                `yaml:"annual_cap_hours" json:"annual_cap_hours"`
	EmployerSizeTiers          []EmployerSizeTier `yaml:"employer_size_tiers" json:"employer_size_tiers"`
	CarryoverCapHours          *int               `yaml:"carryover_cap_hours,omitempty" json:"carryover_cap_hours,omitempty"`
	FrontLoadPermitted         bool               `yaml:"front_load_permitted" json:"front_load_permitted"`
	WaitingPeriodDays          *int               `yaml:"waiting_period_days,omitempty" json:"waiting_period_days,omitempty"`
	NoForfeitureOnRoleChange   bool               `yaml:"no_forfeiture_on_role_change" json:"no_forfeiture_on_role_change"`
	Citation                   Citation           `yaml:"citation" json:"citation"`
	Status                     ReviewStatus       `yaml:"status" json:"status"`
}

type Ratio struct {
	Numerator   int `yaml:"numerator" json:"numerator"`
	Denominator int `yaml:"denominator" json:"denominator"`
}

// EmployerSizeTier captures a cap pair without forcing callers to infer the
// Michigan small-employer paid-plus-unpaid correction.
type EmployerSizeTier struct {
	MinimumEmployees int `yaml:"minimum_employees" json:"minimum_employees"`
	PaidCapHours     int `yaml:"paid_cap_hours" json:"paid_cap_hours"`
	UnpaidCapHours   int `yaml:"unpaid_cap_hours,omitempty" json:"unpaid_cap_hours,omitempty"`
}

func (p LeaveProgramParameters) validate(field string) error {
	if strings.TrimSpace(p.ID) == "" {
		return refuse(field+".id", "is required")
	}
	if err := validateState(p.StateCode, field+".state_code"); err != nil {
		return err
	}
	if strings.TrimSpace(p.Program) == "" {
		return refuse(field+".program", "is required")
	}
	if p.AccrualHoursPerHoursWorked.Numerator <= 0 {
		return refuse(field+".accrual_hours_per_hours_worked.numerator", "must be positive")
	}
	if p.AccrualHoursPerHoursWorked.Denominator <= 0 {
		return refuse(field+".accrual_hours_per_hours_worked.denominator", "must be positive")
	}
	if p.AnnualCapHours <= 0 {
		return refuse(field+".annual_cap_hours", "must be positive")
	}
	if len(p.EmployerSizeTiers) == 0 {
		return refuse(field+".employer_size_tiers", "must contain at least one tier")
	}
	for i, tier := range p.EmployerSizeTiers {
		if tier.MinimumEmployees < 0 {
			return refuse(fmt.Sprintf("%s.employer_size_tiers[%d].minimum_employees", field, i), "must not be negative")
		}
		if tier.PaidCapHours <= 0 {
			return refuse(fmt.Sprintf("%s.employer_size_tiers[%d].paid_cap_hours", field, i), "must be positive")
		}
		if tier.UnpaidCapHours < 0 {
			return refuse(fmt.Sprintf("%s.employer_size_tiers[%d].unpaid_cap_hours", field, i), "must not be negative")
		}
	}
	if p.CarryoverCapHours != nil && *p.CarryoverCapHours < 0 {
		return refuse(field+".carryover_cap_hours", "must not be negative")
	}
	if p.WaitingPeriodDays != nil && *p.WaitingPeriodDays < 0 {
		return refuse(field+".waiting_period_days", "must not be negative")
	}
	if !p.NoForfeitureOnRoleChange {
		return refuse(field+".no_forfeiture_on_role_change", "must be true")
	}
	if p.Status != Reviewed && p.Status != Unreviewed {
		return refuse(field+".status", "must be REVIEWED or UNREVIEWED")
	}
	return p.Citation.validate(field + ".citation")
}

// LeaveParameterSet is a versioned, digested collection of accrual and
// carryover rules. It carries parameters only; arithmetic belongs elsewhere.
type LeaveParameterSet struct {
	TemporalSchema
	Programs []LeaveProgramParameters `yaml:"programs" json:"programs"`
}

func (s LeaveParameterSet) Validate() error {
	if err := s.TemporalSchema.validate("schema"); err != nil {
		return err
	}
	if len(s.Programs) == 0 {
		return refuse("programs", "must contain at least one program")
	}
	seen := map[string]bool{}
	for i, p := range s.Programs {
		if err := p.validate(fmt.Sprintf("programs[%d]", i)); err != nil {
			return err
		}
		if seen[p.ID] {
			return refuse(fmt.Sprintf("programs[%d].id", i), "duplicate id")
		}
		seen[p.ID] = true
	}
	if s.ComputeDigest() != s.Digest {
		return refuse("schema.digest", ErrDigestMismatch.Error())
	}
	return nil
}

func (s LeaveParameterSet) AppliesOn(date values.LocalDate) bool { return s.appliesOn(date) }

func (s LeaveParameterSet) Explain() string {
	return fmt.Sprintf("leave schema=%d version=%d effective=%s programs=%d digest=%s", s.SchemaVersion, s.Version, windowString(s.EffectiveFrom, s.EffectiveUntil), len(s.Programs), s.Digest)
}

var USStateCodes = []string{"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA", "HI", "ID", "IL", "IN", "IA", "KS", "KY", "LA", "ME", "MD", "MA", "MI", "MN", "MS", "MO", "MT", "NE", "NV", "NH", "NJ", "NM", "NY", "NC", "ND", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VT", "VA", "WA", "WV", "WI", "WY"}

func validateState(state, field string) error {
	for _, code := range USStateCodes {
		if state == code {
			return nil
		}
	}
	return fmt.Errorf("%w: %s: %q", ErrUnknownState, field, state)
}

func windowString(start values.LocalDate, end *values.LocalDate) string {
	if end == nil {
		return "[" + start.String() + ",open)"
	}
	return "[" + start.String() + "," + end.String() + ")"
}

type frequencyWire struct {
	Kind           string                   `json:"kind"`
	SchemaVersion  uint32                   `json:"schema_version"`
	Version        uint32                   `json:"version"`
	EffectiveFrom  string                   `json:"effective_from"`
	EffectiveUntil string                   `json:"effective_until,omitempty"`
	KnownAt        string                   `json:"known_at"`
	Constraints    []PayFrequencyConstraint `json:"constraints"`
}
type statementWire struct {
	Kind           string             `json:"kind"`
	SchemaVersion  uint32             `json:"schema_version"`
	Version        uint32             `json:"version"`
	EffectiveFrom  string             `json:"effective_from"`
	EffectiveUntil string             `json:"effective_until,omitempty"`
	KnownAt        string             `json:"known_at"`
	States         []PayStatementRule `json:"states"`
}
type leaveWire struct {
	Kind           string                   `json:"kind"`
	SchemaVersion  uint32                   `json:"schema_version"`
	Version        uint32                   `json:"version"`
	EffectiveFrom  string                   `json:"effective_from"`
	EffectiveUntil string                   `json:"effective_until,omitempty"`
	KnownAt        string                   `json:"known_at"`
	Programs       []LeaveProgramParameters `json:"programs"`
}

func (s PayFrequencyParameterSet) canonical() []byte {
	rows := append([]PayFrequencyConstraint(nil), s.Constraints...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	b, _ := json.Marshal(frequencyWire{"PAY_FREQUENCY", s.SchemaVersion, s.Version, s.EffectiveFrom.String(), dateString(s.EffectiveUntil), s.KnownAt.String(), rows})
	return b
}
func (r PayStatementRegistry) canonical() []byte {
	rows := append([]PayStatementRule(nil), r.States...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].StateCode < rows[j].StateCode })
	b, _ := json.Marshal(statementWire{"PAY_STATEMENT", r.SchemaVersion, r.Version, r.EffectiveFrom.String(), dateString(r.EffectiveUntil), r.KnownAt.String(), rows})
	return b
}
func (s LeaveParameterSet) canonical() []byte {
	rows := append([]LeaveProgramParameters(nil), s.Programs...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	b, _ := json.Marshal(leaveWire{"LEAVE", s.SchemaVersion, s.Version, s.EffectiveFrom.String(), dateString(s.EffectiveUntil), s.KnownAt.String(), rows})
	return b
}
func dateString(d *values.LocalDate) string {
	if d == nil {
		return ""
	}
	return d.String()
}
func digest(b []byte) string                             { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func (s PayFrequencyParameterSet) ComputeDigest() string { return digest(s.canonical()) }
func (r PayStatementRegistry) ComputeDigest() string     { return digest(r.canonical()) }
func (s LeaveParameterSet) ComputeDigest() string        { return digest(s.canonical()) }

type yamlHeader struct {
	SchemaVersion  uint32 `yaml:"schema_version" json:"schema_version"`
	Version        uint32 `yaml:"version" json:"version"`
	EffectiveFrom  string `yaml:"effective_from" json:"effective_from"`
	EffectiveUntil string `yaml:"effective_until" json:"effective_until"`
	KnownAt        string `yaml:"known_at" json:"known_at"`
	Digest         string `yaml:"digest" json:"digest"`
}
type yamlFrequency struct {
	yamlHeader  `yaml:",inline" json:",inline"`
	Constraints []PayFrequencyConstraint `yaml:"constraints" json:"constraints"`
}
type yamlStatements struct {
	yamlHeader `yaml:",inline" json:",inline"`
	States     []PayStatementRule `yaml:"states" json:"states"`
}
type yamlLeave struct {
	yamlHeader `yaml:",inline" json:",inline"`
	Programs   []LeaveProgramParameters `yaml:"programs" json:"programs"`
}

func decode(data []byte, out any) error {
	node, err := parseYAMLSubset(string(data))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	raw, err := json.Marshal(node)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return nil
}

// parseYAMLSubset accepts the data-only YAML vocabulary used by the fixtures:
// indentation-based maps and lists, quoted or plain scalars, and flow
// maps/lists. It intentionally has no anchors, tags, aliases, or executable
// YAML features, keeping the legal plane on standard-library parsing only.
func parseYAMLSubset(input string) (any, error) {
	var lines []yamlLine
	for number, raw := range strings.Split(strings.ReplaceAll(input, "\r\n", "\n"), "\n") {
		line, err := yamlContent(raw)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", number+1, err)
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if strings.Contains(line[:indent], "\t") {
			return nil, fmt.Errorf("line %d: tabs are not allowed", number+1)
		}
		lines = append(lines, yamlLine{indent: indent, text: strings.TrimSpace(line)})
	}
	if len(lines) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	node, next, err := parseYAMLBlock(lines, 0, lines[0].indent)
	if err != nil {
		return nil, err
	}
	if next != len(lines) {
		return nil, fmt.Errorf("unexpected content at line %d", next+1)
	}
	return node, nil
}

type yamlLine struct {
	indent int
	text   string
}

func yamlContent(line string) (string, error) {
	var quote rune
	for i, r := range line {
		if quote != 0 {
			if r == quote && (quote != '"' || i == 0 || line[i-1] != '\\') {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if r == '#' && (i == 0 || line[i-1] == ' ') {
			line = line[:i]
			break
		}
	}
	if quote != 0 {
		return "", errors.New("unterminated quoted scalar")
	}
	return line, nil
}

func parseYAMLBlock(lines []yamlLine, index, indent int) (any, int, error) {
	if index >= len(lines) || lines[index].indent != indent {
		return nil, index, fmt.Errorf("expected indentation %d", indent)
	}
	if strings.HasPrefix(lines[index].text, "-") {
		return parseYAMLList(lines, index, indent)
	}
	return parseYAMLMap(lines, index, indent)
}

func parseYAMLMap(lines []yamlLine, index, indent int) (map[string]any, int, error) {
	out := map[string]any{}
	for index < len(lines) && lines[index].indent == indent && !strings.HasPrefix(lines[index].text, "-") {
		key, value, ok := splitYAMLColon(lines[index].text)
		if !ok || key == "" {
			return nil, index, fmt.Errorf("line %d is not a map entry", index+1)
		}
		index++
		if value == "" {
			if index >= len(lines) || lines[index].indent <= indent {
				out[key] = nil
				continue
			}
			nested, next, err := parseYAMLBlock(lines, index, lines[index].indent)
			if err != nil {
				return nil, index, err
			}
			out[key], index = nested, next
			continue
		}
		parsed, err := parseYAMLValue(value)
		if err != nil {
			return nil, index, err
		}
		out[key] = parsed
	}
	return out, index, nil
}

func parseYAMLList(lines []yamlLine, index, indent int) ([]any, int, error) {
	var out []any
	for index < len(lines) && lines[index].indent == indent && strings.HasPrefix(lines[index].text, "-") {
		text := strings.TrimSpace(strings.TrimPrefix(lines[index].text, "-"))
		index++
		if text == "" {
			if index >= len(lines) || lines[index].indent <= indent {
				return nil, index, errors.New("empty list item")
			}
			nested, next, err := parseYAMLBlock(lines, index, lines[index].indent)
			if err != nil {
				return nil, index, err
			}
			out, index = append(out, nested), next
			continue
		}
		if key, value, ok := splitYAMLColon(text); ok && !strings.HasPrefix(text, "{") {
			item := map[string]any{}
			if value == "" {
				if index >= len(lines) || lines[index].indent <= indent {
					return nil, index, fmt.Errorf("list map item %q has no value", key)
				}
				nested, next, err := parseYAMLBlock(lines, index, lines[index].indent)
				if err != nil {
					return nil, index, err
				}
				item[key], index = nested, next
			} else {
				parsed, err := parseYAMLValue(value)
				if err != nil {
					return nil, index, err
				}
				item[key] = parsed
			}
			if index < len(lines) && lines[index].indent > indent {
				extra, next, err := parseYAMLMap(lines, index, lines[index].indent)
				if err != nil {
					return nil, index, err
				}
				for k, v := range extra {
					item[k] = v
				}
				index = next
			}
			out = append(out, item)
			continue
		}
		parsed, err := parseYAMLValue(text)
		if err != nil {
			return nil, index, err
		}
		out = append(out, parsed)
	}
	return out, index, nil
}

func splitYAMLColon(text string) (string, string, bool) {
	depth := 0
	var quote rune
	for i, r := range text {
		if quote != 0 {
			if r == quote && (quote != '"' || i == 0 || text[i-1] != '\\') {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		switch r {
		case '[', '{':
			depth++
		case ']', '}':
			depth--
		}
		if r == ':' && depth == 0 && (i+1 == len(text) || text[i+1] == ' ' || text[i+1] == '\t') {
			return strings.TrimSpace(text[:i]), strings.TrimSpace(text[i+1:]), true
		}
	}
	return "", "", false
}

func parseYAMLValue(text string) (any, error) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") {
		return parseYAMLFlow(text[1:len(text)-1], false)
	}
	if strings.HasPrefix(text, "{") && strings.HasSuffix(text, "}") {
		return parseYAMLFlow(text[1:len(text)-1], true)
	}
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		return strconv.Unquote(text)
	}
	if len(text) >= 2 && text[0] == '\'' && text[len(text)-1] == '\'' {
		return strings.ReplaceAll(text[1:len(text)-1], "''", "'"), nil
	}
	switch text {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null", "~":
		return nil, nil
	}
	if integer, err := strconv.ParseInt(text, 10, 64); err == nil {
		return integer, nil
	}
	return text, nil
}

func parseYAMLFlow(text string, mapMode bool) (any, error) {
	parts := splitYAMLComma(text)
	if mapMode {
		out := map[string]any{}
		for _, part := range parts {
			key, value, ok := splitYAMLColon(strings.TrimSpace(part))
			if !ok {
				return nil, fmt.Errorf("invalid flow map entry %q", part)
			}
			parsed, err := parseYAMLValue(value)
			if err != nil {
				return nil, err
			}
			out[strings.Trim(key, "\"'")] = parsed
		}
		return out, nil
	}
	var out []any
	for _, part := range parts {
		parsed, err := parseYAMLValue(part)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	return out, nil
}

func splitYAMLComma(text string) []string {
	var out []string
	start, depth := 0, 0
	var quote rune
	for i, r := range text {
		if quote != 0 {
			if r == quote && (quote != '"' || i == 0 || text[i-1] != '\\') {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		switch r {
		case '[', '{':
			depth++
		case ']', '}':
			depth--
		}
		if r == ',' && depth == 0 {
			out = append(out, text[start:i])
			start = i + 1
		}
	}
	if strings.TrimSpace(text[start:]) != "" {
		out = append(out, text[start:])
	}
	return out
}

func LoadPayFrequencyParameters(data []byte) (PayFrequencyParameterSet, error) {
	var raw yamlFrequency
	if err := decode(data, &raw); err != nil {
		return PayFrequencyParameterSet{}, err
	}
	meta, err := parseTemporal(raw.SchemaVersion, raw.Version, raw.EffectiveFrom, raw.EffectiveUntil, raw.KnownAt, raw.Digest)
	if err != nil {
		return PayFrequencyParameterSet{}, err
	}
	out := PayFrequencyParameterSet{TemporalSchema: meta, Constraints: raw.Constraints}
	computed := out.ComputeDigest()
	if raw.Digest != "" && raw.Digest != computed {
		return PayFrequencyParameterSet{}, refuse("digest", fmt.Sprintf("recorded %s, computed %s", raw.Digest, computed))
	}
	out.Digest = computed
	if err := out.Validate(); err != nil {
		return PayFrequencyParameterSet{}, err
	}
	return out, nil
}
func LoadPayFrequencyParametersFile(path string) (PayFrequencyParameterSet, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return PayFrequencyParameterSet{}, err
	}
	return LoadPayFrequencyParameters(b)
}

//go:embed testdata/pay-statements.yaml
var defaultPayStatementRegistry []byte

// DefaultPayStatementRegistry loads the repository's resolved pay-statement
// field and delivery rules for release-time validation.
func DefaultPayStatementRegistry() (PayStatementRegistry, error) {
	return LoadPayStatementRegistry(defaultPayStatementRegistry)
}

func LoadPayStatementRegistry(data []byte) (PayStatementRegistry, error) {
	var raw yamlStatements
	if err := decode(data, &raw); err != nil {
		return PayStatementRegistry{}, err
	}
	meta, err := parseTemporal(raw.SchemaVersion, raw.Version, raw.EffectiveFrom, raw.EffectiveUntil, raw.KnownAt, raw.Digest)
	if err != nil {
		return PayStatementRegistry{}, err
	}
	out := PayStatementRegistry{TemporalSchema: meta, States: raw.States}
	computed := out.ComputeDigest()
	if raw.Digest != "" && raw.Digest != computed {
		return PayStatementRegistry{}, refuse("digest", fmt.Sprintf("recorded %s, computed %s", raw.Digest, computed))
	}
	out.Digest = computed
	if err := out.Validate(); err != nil {
		return PayStatementRegistry{}, err
	}
	return out, nil
}
func LoadPayStatementRegistryFile(path string) (PayStatementRegistry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return PayStatementRegistry{}, err
	}
	return LoadPayStatementRegistry(b)
}

func LoadLeaveParameterSet(data []byte) (LeaveParameterSet, error) {
	var raw yamlLeave
	if err := decode(data, &raw); err != nil {
		return LeaveParameterSet{}, err
	}
	meta, err := parseTemporal(raw.SchemaVersion, raw.Version, raw.EffectiveFrom, raw.EffectiveUntil, raw.KnownAt, raw.Digest)
	if err != nil {
		return LeaveParameterSet{}, err
	}
	out := LeaveParameterSet{TemporalSchema: meta, Programs: raw.Programs}
	computed := out.ComputeDigest()
	if raw.Digest != "" && raw.Digest != computed {
		return LeaveParameterSet{}, refuse("digest", fmt.Sprintf("recorded %s, computed %s", raw.Digest, computed))
	}
	out.Digest = computed
	if err := out.Validate(); err != nil {
		return LeaveParameterSet{}, err
	}
	return out, nil
}
func LoadLeaveParameterSetFile(path string) (LeaveParameterSet, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return LeaveParameterSet{}, err
	}
	return LoadLeaveParameterSet(b)
}

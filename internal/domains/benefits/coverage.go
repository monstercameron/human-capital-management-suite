// Coverage tiers and dependent qualification: BEN-004 resolves which
// dependents a coverage tier admits under exact, effective-dated rules.
//
// Tier, dependent relation, evidence, age, student and disability rules
// are evaluated as a pure function over pinned facts and versioned
// rules: no election, enrollment or coverage state is created. An
// unsupported or ambiguous dependent — unknown relation, unadmitted
// relation, unverified age or missing evidence — is resolved UNCOVERED
// with named reasons, never silently covered. A missing covering rule is
// UNKNOWN, never eligible by default.
//
// The clock is injected by the caller (TierFacts.AsOf), so resolution is
// pure: no database, no wall clock, no network.
package benefits

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrInvalidTierInput rejects a tier evaluation that lacks worker,
	// plan, date, tier or well-formed dependent input.
	ErrInvalidTierInput = errors.New("benefits: invalid tier coverage input")
	// ErrTierTenant confines every evaluation to a single tenant.
	ErrTierTenant = errors.New("benefits: tier coverage tenant mismatch")
)

// TierStatus is the closed BEN-004 outcome vocabulary.
type TierStatus string

const (
	TierCovered   TierStatus = "COVERED"
	TierPartial   TierStatus = "PARTIAL"
	TierUncovered TierStatus = "UNCOVERED"
	TierUnknown   TierStatus = "UNKNOWN"
)

// Valid reports whether the status is in the closed vocabulary.
func (s TierStatus) Valid() bool {
	switch s {
	case TierCovered, TierPartial, TierUncovered, TierUnknown:
		return true
	default:
		return false
	}
}

// TierCode is the closed coverage-tier vocabulary.
type TierCode string

const (
	TierEmployeeOnly     TierCode = "EMPLOYEE_ONLY"
	TierEmployeeSpouse   TierCode = "EMPLOYEE_SPOUSE"
	TierEmployeeChildren TierCode = "EMPLOYEE_CHILDREN"
	TierFamily           TierCode = "FAMILY"
)

// Valid reports whether the tier is in the closed vocabulary.
func (t TierCode) Valid() bool {
	switch t {
	case TierEmployeeOnly, TierEmployeeSpouse, TierEmployeeChildren, TierFamily:
		return true
	default:
		return false
	}
}

// DependentRelation is the closed dependent-relationship vocabulary.
type DependentRelation string

const (
	RelationSpouse          DependentRelation = "SPOUSE"
	RelationChild           DependentRelation = "CHILD"
	RelationStepchild       DependentRelation = "STEPCHILD"
	RelationDomesticPartner DependentRelation = "DOMESTIC_PARTNER"
)

// Valid reports whether the relation is in the closed vocabulary.
func (r DependentRelation) Valid() bool {
	switch r {
	case RelationSpouse, RelationChild, RelationStepchild, RelationDomesticPartner:
		return true
	default:
		return false
	}
}

// Evidence kinds consumed by tier qualification.
const (
	EvidenceMarriageCertificate     = "marriage-certificate"
	EvidenceBirthCertificate        = "birth-certificate"
	EvidencePartnershipAffidavit    = "partnership-affidavit"
	EvidenceStudentVerification     = "student-verification"
	EvidenceDisabilityCertification = "disability-certification"
)

// TierRule is one versioned, effective-dated coverage-tier rule owned by
// the benefits catalogue. AllowedRelations names the admitted relations;
// an empty list admits none (employee-only tiers). RequiredEvidence
// names, per relation, the evidence every dependent of that relation
// must present. MaxDependentAge is the base age cutoff; Student,
// extended to StudentExtensionAge with student evidence, and Disabled,
// with disability evidence, are the only exemptions.
type TierRule struct {
	ID                  string
	Version             string
	Tenant              string
	Tier                TierCode
	AllowedRelations    []DependentRelation
	MaxDependentAge     int32
	StudentExtensionAge int32
	RequiredEvidence    map[DependentRelation][]string
	EffectiveFrom       time.Time
	EffectiveTo         time.Time
}

// Validate applies the tier-rule contract.
func (r TierRule) Validate() error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Version) == "" || strings.TrimSpace(r.Tenant) == "" {
		return fmt.Errorf("%w: rule identity is required", ErrInvalidTierInput)
	}
	if !r.Tier.Valid() {
		return fmt.Errorf("%w: tier %q is not in the closed vocabulary", ErrInvalidTierInput, r.Tier)
	}
	for _, relation := range r.AllowedRelations {
		if !relation.Valid() {
			return fmt.Errorf("%w: relation %q is not in the closed vocabulary", ErrInvalidTierInput, relation)
		}
	}
	if r.MaxDependentAge <= 0 {
		return fmt.Errorf("%w: rule %q needs a positive maximum dependent age", ErrInvalidTierInput, r.ID)
	}
	if r.StudentExtensionAge < r.MaxDependentAge {
		return fmt.Errorf("%w: rule %q needs a student extension at or above the maximum age", ErrInvalidTierInput, r.ID)
	}
	for relation, kinds := range r.RequiredEvidence {
		if !relation.Valid() {
			return fmt.Errorf("%w: relation %q is not in the closed vocabulary", ErrInvalidTierInput, relation)
		}
		if len(kinds) == 0 {
			return fmt.Errorf("%w: rule %q names no evidence for relation %q", ErrInvalidTierInput, r.ID, relation)
		}
	}
	if r.EffectiveFrom.IsZero() || r.EffectiveTo.IsZero() || !r.EffectiveFrom.Before(r.EffectiveTo) {
		return fmt.Errorf("%w: rule %q needs a valid effective window", ErrInvalidTierInput, r.ID)
	}
	return nil
}

func (r TierRule) covers(asOf time.Time) bool {
	return !asOf.Before(r.EffectiveFrom) && asOf.Before(r.EffectiveTo)
}

func (r TierRule) admits(relation DependentRelation) bool {
	for _, allowed := range r.AllowedRelations {
		if allowed == relation {
			return true
		}
	}
	return false
}

// Dependent is one dependent presented for tier qualification. AgeSet
// distinguishes unknown age (unqualifiable) from a stated age: missing
// data never becomes zero.
type Dependent struct {
	DependentRef string
	Relation     DependentRelation
	AgeYears     int32
	AgeSet       bool
	Student      bool
	Disabled     bool
	Evidence     []string
}

func (d Dependent) hasEvidence(kind string) bool {
	for _, held := range d.Evidence {
		if held == kind {
			return true
		}
	}
	return false
}

// TierFacts is the pinned qualification snapshot: one worker, one plan,
// one tier and its presented dependents at an as-of date.
type TierFacts struct {
	Tenant     string
	WorkerRef  string
	PlanRef    string
	AsOf       time.Time
	Tier       TierCode
	Dependents []Dependent
}

// Validate applies the tier-facts contract.
func (f TierFacts) Validate() error {
	if strings.TrimSpace(f.Tenant) == "" || strings.TrimSpace(f.WorkerRef) == "" || strings.TrimSpace(f.PlanRef) == "" {
		return fmt.Errorf("%w: tenant, worker and plan are required", ErrInvalidTierInput)
	}
	if f.AsOf.IsZero() {
		return fmt.Errorf("%w: evaluation date is required", ErrInvalidTierInput)
	}
	if !f.Tier.Valid() {
		return fmt.Errorf("%w: tier %q is not in the closed vocabulary", ErrInvalidTierInput, f.Tier)
	}
	for i := range f.Dependents {
		if strings.TrimSpace(f.Dependents[i].DependentRef) == "" {
			return fmt.Errorf("%w: dependent %d needs a reference", ErrInvalidTierInput, i)
		}
		if f.Dependents[i].AgeYears < 0 {
			return fmt.Errorf("%w: dependent %q has a negative age", ErrInvalidTierInput, f.Dependents[i].DependentRef)
		}
	}
	return nil
}

// DependentDecision is the per-dependent qualification outcome. An
// uncovered dependent always names its reasons; ambiguity is named, not
// defaulted.
type DependentDecision struct {
	DependentRef string
	Covered      bool
	Reasons      []string
}

// TierResolution is the typed BEN-004 result with rule-trace evidence.
type TierResolution struct {
	Status      TierStatus
	RuleID      string
	RuleVersion string
	Decisions   []DependentDecision
	Reasons     []string
	Missing     []string
	Evidence    map[string]string
	Digest      string
}

func tierDigest(facts TierFacts, rule TierRule, status TierStatus, decisions []DependentDecision) string {
	parts := []string{
		facts.Tenant, facts.WorkerRef, facts.PlanRef,
		facts.AsOf.UTC().Format(time.RFC3339Nano), string(facts.Tier),
		rule.ID, rule.Version, string(status),
	}
	for _, d := range decisions {
		covered := "0"
		if d.Covered {
			covered = "1"
		}
		parts = append(parts, d.DependentRef+"\x01"+covered)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ResolveTierCoverage qualifies every presented dependent against the
// effective-dated rule for the requested tier. It is pure: no election,
// enrollment or coverage state is created, and unsupported or ambiguous
// dependents resolve UNCOVERED with reasons rather than coverage.
func ResolveTierCoverage(facts TierFacts, rules []TierRule) (TierResolution, error) {
	if err := facts.Validate(); err != nil {
		return TierResolution{}, err
	}
	if len(rules) == 0 {
		return TierResolution{}, fmt.Errorf("%w: at least one rule is required", ErrInvalidTierInput)
	}
	for _, rule := range rules {
		if err := rule.Validate(); err != nil {
			return TierResolution{}, err
		}
		if rule.Tenant != facts.Tenant {
			return TierResolution{}, fmt.Errorf("%w: rule %q belongs to another tenant", ErrTierTenant, rule.ID)
		}
	}
	var rule *TierRule
	for i := range rules {
		if rules[i].Tier == facts.Tier && rules[i].covers(facts.AsOf) {
			rule = &rules[i]
			break
		}
	}
	evidence := map[string]string{
		"tenant": facts.Tenant, "worker": facts.WorkerRef, "plan": facts.PlanRef,
		"tier": string(facts.Tier), "as_of": facts.AsOf.UTC().Format(time.RFC3339Nano),
	}
	if rule == nil {
		return TierResolution{
			Status:   TierUnknown,
			Missing:  []string{"applicable_rule"},
			Evidence: evidence,
			Digest:   tierDigest(facts, TierRule{}, TierUnknown, nil),
		}, nil
	}
	evidence["rule"] = rule.ID
	evidence["rule_version"] = rule.Version
	if len(facts.Dependents) == 0 {
		return TierResolution{
			Status:      TierCovered,
			RuleID:      rule.ID,
			RuleVersion: rule.Version,
			Reasons:     []string{"no dependents require qualification"},
			Evidence:    evidence,
			Digest:      tierDigest(facts, *rule, TierCovered, nil),
		}, nil
	}
	decisions := make([]DependentDecision, 0, len(facts.Dependents))
	for _, dep := range facts.Dependents {
		decisions = append(decisions, qualifyDependent(dep, *rule))
	}
	covered := 0
	for _, d := range decisions {
		if d.Covered {
			covered++
		}
	}
	var status TierStatus
	switch {
	case covered == len(decisions):
		status = TierCovered
	case covered == 0:
		status = TierUncovered
	default:
		status = TierPartial
	}
	return TierResolution{
		Status:      status,
		RuleID:      rule.ID,
		RuleVersion: rule.Version,
		Decisions:   decisions,
		Reasons: []string{fmt.Sprintf("%d of %d dependents qualify under rule %q version %q",
			covered, len(decisions), rule.ID, rule.Version)},
		Evidence: evidence,
		Digest:   tierDigest(facts, *rule, status, decisions),
	}, nil
}

// qualifyDependent applies the exact relation, age, student, disability
// and evidence rules to one dependent. Any failure resolves UNCOVERED
// with its reason; nothing defaults to covered.
func qualifyDependent(dep Dependent, rule TierRule) DependentDecision {
	out := DependentDecision{DependentRef: dep.DependentRef}
	uncovered := func(reason string) DependentDecision {
		out.Reasons = []string{reason}
		return out
	}
	if !dep.Relation.Valid() {
		return uncovered(fmt.Sprintf("relation %q is ambiguous or outside the closed vocabulary", dep.Relation))
	}
	if !rule.admits(dep.Relation) {
		return uncovered(fmt.Sprintf("relation %q is not admitted by rule %q for tier %q", dep.Relation, rule.ID, rule.Tier))
	}
	if !dep.AgeSet {
		return uncovered(fmt.Sprintf("dependent %q has unverified age and cannot qualify", dep.DependentRef))
	}
	// The age cap binds child relations only: spouses and domestic
	// partners qualify at any verified age, exactly like production
	// plan rules.
	ageCapped := dep.Relation == RelationChild || dep.Relation == RelationStepchild
	if ageCapped && dep.AgeYears > rule.MaxDependentAge {
		switch {
		case dep.Disabled && dep.hasEvidence(EvidenceDisabilityCertification):
			// Certified disability exempts the age cap.
		case dep.Student && dep.AgeYears <= rule.StudentExtensionAge && dep.hasEvidence(EvidenceStudentVerification):
			// Verified students qualify to the extension age.
		default:
			return uncovered(fmt.Sprintf("age %d exceeds maximum %d without a qualifying student or disability exemption", dep.AgeYears, rule.MaxDependentAge))
		}
	}
	var missing []string
	for _, kind := range rule.RequiredEvidence[dep.Relation] {
		if !dep.hasEvidence(kind) {
			missing = append(missing, kind)
		}
	}
	if ageCapped && dep.AgeYears > rule.MaxDependentAge && dep.Student && !dep.Disabled && !dep.hasEvidence(EvidenceStudentVerification) {
		missing = append(missing, EvidenceStudentVerification)
	}
	if ageCapped && dep.AgeYears > rule.MaxDependentAge && dep.Disabled && !dep.hasEvidence(EvidenceDisabilityCertification) {
		missing = append(missing, EvidenceDisabilityCertification)
	}
	if len(missing) > 0 {
		return uncovered(fmt.Sprintf("dependent %q is missing evidence: %s", dep.DependentRef, strings.Join(missing, ", ")))
	}
	out.Covered = true
	out.Reasons = []string{fmt.Sprintf("dependent %q satisfies rule %q version %q", dep.DependentRef, rule.ID, rule.Version)}
	return out
}

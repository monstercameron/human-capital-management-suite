package benefits

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrInvalidEligibilityInput rejects an evaluation that lacks worker,
	// plan, date, population or rule input.
	ErrInvalidEligibilityInput = errors.New("benefits: invalid eligibility input")
	// ErrEligibilityTenant confines every evaluation to a single tenant.
	ErrEligibilityTenant = errors.New("benefits: eligibility tenant mismatch")
)

// EligibilityStatus is the closed BEN-003 outcome vocabulary. Missing
// hours or classification is CONDITIONAL; missing jurisdiction or an
// inapplicable rule pack is UNKNOWN. Neither ever defaults to eligible.
type EligibilityStatus string

const (
	StatusEligible    EligibilityStatus = "ELIGIBLE"
	StatusConditional EligibilityStatus = "CONDITIONAL"
	StatusIneligible  EligibilityStatus = "INELIGIBLE"
	StatusUnknown     EligibilityStatus = "UNKNOWN"
)

// Valid reports whether the status is in the closed vocabulary.
func (s EligibilityStatus) Valid() bool {
	switch s {
	case StatusEligible, StatusConditional, StatusIneligible, StatusUnknown:
		return true
	default:
		return false
	}
}

// EligibilityRule is one versioned, effective-dated eligibility rule owned by
// the benefits catalogue. It names minimum hours, admitted worker
// classifications and covered jurisdictions; anything unlisted is not covered.
type EligibilityRule struct {
	ID                     string
	Version                string
	Tenant                 string
	MinHoursPerWeek        values.Decimal
	AllowedClassifications []string
	Jurisdictions          []string
	EffectiveFrom          time.Time
	EffectiveTo            time.Time
}

func (r EligibilityRule) Validate() error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Version) == "" || strings.TrimSpace(r.Tenant) == "" {
		return fmt.Errorf("%w: rule identity is required", ErrInvalidEligibilityInput)
	}
	if err := r.MinHoursPerWeek.Validate(); err != nil {
		return fmt.Errorf("%w: minimum hours: %v", ErrInvalidEligibilityInput, err)
	}
	if len(r.AllowedClassifications) == 0 || len(r.Jurisdictions) == 0 {
		return fmt.Errorf("%w: rule %q needs classifications and jurisdictions", ErrInvalidEligibilityInput, r.ID)
	}
	if r.EffectiveFrom.IsZero() || r.EffectiveTo.IsZero() || !r.EffectiveFrom.Before(r.EffectiveTo) {
		return fmt.Errorf("%w: rule %q needs a valid effective window", ErrInvalidEligibilityInput, r.ID)
	}
	return nil
}

func (r EligibilityRule) covers(asOf time.Time) bool {
	return !asOf.Before(r.EffectiveFrom) && asOf.Before(r.EffectiveTo)
}

// WorkerFacts is the pinned worker snapshot evaluated against the rules.
// HoursSet distinguishes unknown hours (conditional) from zero hours
// (ineligible): missing data never becomes zero.
type WorkerFacts struct {
	Tenant         string
	WorkerRef      string
	PlanRef        string
	PopulationRef  string
	AsOf           time.Time
	HoursPerWeek   values.Decimal
	HoursSet       bool
	Classification string
	Jurisdiction   string
}

func (f WorkerFacts) Validate() error {
	if strings.TrimSpace(f.Tenant) == "" || strings.TrimSpace(f.WorkerRef) == "" ||
		strings.TrimSpace(f.PlanRef) == "" || strings.TrimSpace(f.PopulationRef) == "" {
		return fmt.Errorf("%w: tenant, worker, plan and population are required", ErrInvalidEligibilityInput)
	}
	if f.AsOf.IsZero() {
		return fmt.Errorf("%w: evaluation date is required", ErrInvalidEligibilityInput)
	}
	if f.HoursSet {
		if err := f.HoursPerWeek.Validate(); err != nil {
			return fmt.Errorf("%w: hours: %v", ErrInvalidEligibilityInput, err)
		}
	}
	return nil
}

// EligibilityEvaluation is the typed BEN-003 result with rule-trace evidence.
type EligibilityEvaluation struct {
	Status      EligibilityStatus
	RuleID      string
	RuleVersion string
	Reasons     []string
	Missing     []string
	Evidence    map[string]string
	Digest      string
}

func eligibilityDigest(facts WorkerFacts, rule EligibilityRule, status EligibilityStatus) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		facts.Tenant, facts.WorkerRef, facts.PlanRef, facts.PopulationRef,
		facts.AsOf.UTC().Format(time.RFC3339Nano),
		facts.HoursPerWeek.String(), facts.Classification, facts.Jurisdiction,
		rule.ID, rule.Version, string(status),
	}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// EvaluateEligibility evaluates one worker snapshot against versioned rules.
// It is pure: no election, enrollment or coverage state is created.
func EvaluateEligibility(facts WorkerFacts, rules []EligibilityRule) (EligibilityEvaluation, error) {
	if err := facts.Validate(); err != nil {
		return EligibilityEvaluation{}, err
	}
	if len(rules) == 0 {
		return EligibilityEvaluation{}, fmt.Errorf("%w: at least one rule is required", ErrInvalidEligibilityInput)
	}
	for _, rule := range rules {
		if err := rule.Validate(); err != nil {
			return EligibilityEvaluation{}, err
		}
		if rule.Tenant != facts.Tenant {
			return EligibilityEvaluation{}, fmt.Errorf("%w: rule %q belongs to another tenant", ErrEligibilityTenant, rule.ID)
		}
	}
	var rule *EligibilityRule
	for i := range rules {
		if rules[i].covers(facts.AsOf) {
			rule = &rules[i]
			break
		}
	}
	evidence := map[string]string{
		"tenant": facts.Tenant, "worker": facts.WorkerRef, "plan": facts.PlanRef,
		"population": facts.PopulationRef, "as_of": facts.AsOf.UTC().Format(time.RFC3339Nano),
	}
	if rule == nil {
		return EligibilityEvaluation{
			Status:   StatusUnknown,
			Missing:  []string{"applicable_rule"},
			Evidence: evidence,
			Digest:   eligibilityDigest(facts, EligibilityRule{}, StatusUnknown),
		}, nil
	}
	evidence["rule"] = rule.ID
	evidence["rule_version"] = rule.Version
	mk := func(status EligibilityStatus, reasons, missing []string) (EligibilityEvaluation, error) {
		return EligibilityEvaluation{
			Status: status, RuleID: rule.ID, RuleVersion: rule.Version,
			Reasons: reasons, Missing: missing, Evidence: evidence,
			Digest: eligibilityDigest(facts, *rule, status),
		}, nil
	}
	if facts.Jurisdiction == "" {
		return mk(StatusUnknown, []string{"jurisdiction is unknown; coverage cannot be determined"}, []string{"jurisdiction"})
	}
	if !facts.HoursSet {
		return mk(StatusConditional, []string{"weekly hours are unverified; eligibility is conditional on hours evidence"}, []string{"hours_per_week"})
	}
	if facts.Classification == "" {
		return mk(StatusConditional, []string{"worker classification is unverified; eligibility is conditional on classification evidence"}, []string{"classification"})
	}
	var reasons []string
	if facts.HoursPerWeek.Cmp(rule.MinHoursPerWeek) < 0 {
		reasons = append(reasons, fmt.Sprintf("weekly hours %s below minimum %s", facts.HoursPerWeek.String(), rule.MinHoursPerWeek.String()))
	}
	allowed := false
	for _, c := range rule.AllowedClassifications {
		if c == facts.Classification {
			allowed = true
			break
		}
	}
	if !allowed {
		reasons = append(reasons, fmt.Sprintf("classification %q is not admitted by rule %q", facts.Classification, rule.ID))
	}
	covered := false
	for _, j := range rule.Jurisdictions {
		if j == facts.Jurisdiction {
			covered = true
			break
		}
	}
	if !covered {
		reasons = append(reasons, fmt.Sprintf("jurisdiction %q is not covered by rule %q", facts.Jurisdiction, rule.ID))
	}
	if len(reasons) > 0 {
		return mk(StatusIneligible, reasons, nil)
	}
	return mk(StatusEligible, []string{fmt.Sprintf("all conditions of rule %q version %q are satisfied", rule.ID, rule.Version)}, nil)
}

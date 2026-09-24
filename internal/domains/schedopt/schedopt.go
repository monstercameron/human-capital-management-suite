// Package schedopt defines a bounded, descriptive workforce optimization
// problem. It validates inputs and performs a deterministic feasibility
// pre-check; it deliberately contains no solver or assignment authority.
package schedopt

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/demand"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/matching"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/qualification"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const schemaVersion = 1

// Version reports the schedopt contract version.
func Version() int { return schemaVersion }

var (
	ErrInvalidProblem     = errors.New("schedopt: invalid optimization problem")
	ErrUnboundedProblem   = errors.New("schedopt: optimization problem must declare finite bounds")
	ErrUnknownConstraint  = errors.New("schedopt: unknown constraint kind")
	ErrInvalidFeasibility = errors.New("schedopt: invalid feasibility report")
)

// ObjectiveKind is a closed set. An objective is never inferred from the
// order in which constraints happen to be supplied.
type ObjectiveKind string

const (
	ObjectiveMaximizeCoverage             ObjectiveKind = "MAXIMIZE_COVERAGE"
	ObjectiveMinimizeCost                 ObjectiveKind = "MINIMIZE_COST"
	ObjectiveMinimizeSoftConstraintWeight ObjectiveKind = "MINIMIZE_SOFT_CONSTRAINT_WEIGHT"
)

func (k ObjectiveKind) Valid() bool {
	return k == ObjectiveMaximizeCoverage || k == ObjectiveMinimizeCost || k == ObjectiveMinimizeSoftConstraintWeight
}

type Objective struct {
	Kind   ObjectiveKind
	Weight int64
}

func (o Objective) Validate() error {
	if !o.Kind.Valid() {
		return fmt.Errorf("%w: objective %q", ErrInvalidProblem, o.Kind)
	}
	if o.Weight <= 0 {
		return fmt.Errorf("%w: objective weight must be positive", ErrInvalidProblem)
	}
	return nil
}

// ConstraintKind is the closed vocabulary understood by the pre-check. A
// future kind must be added here with explicit semantics before acceptance.
type ConstraintKind string

const (
	ConstraintAvailability        ConstraintKind = "AVAILABILITY"
	ConstraintLocation            ConstraintKind = "LOCATION"
	ConstraintQualification       ConstraintKind = "QUALIFICATION"
	ConstraintCostCeiling         ConstraintKind = "COST_CEILING"
	ConstraintCandidateUniqueness ConstraintKind = "CANDIDATE_UNIQUENESS"
	ConstraintCoverage            ConstraintKind = "COVERAGE"
	// ConstraintLegalAuthorization (SCHED-OPT-003) requires the worker to
	// hold every listed legal authorization for the window.
	ConstraintLegalAuthorization ConstraintKind = "LEGAL_AUTHORIZATION"
	// ConstraintFatigueLimit (SCHED-OPT-003) caps accrued fatigue minutes
	// plus the window's own minutes: a worker that would exceed the cap
	// is blocked no matter how it scores.
	ConstraintFatigueLimit ConstraintKind = "FATIGUE_LIMIT"
)

func (k ConstraintKind) Valid() bool {
	switch k {
	case ConstraintAvailability, ConstraintLocation, ConstraintQualification, ConstraintCostCeiling, ConstraintCandidateUniqueness, ConstraintCoverage, ConstraintLegalAuthorization, ConstraintFatigueLimit:
		return true
	default:
		return false
	}
}

// Constraint is used in either HardConstraints or SoftConstraints. Weight is
// meaningful for soft constraints and must be zero for hard constraints.
type Constraint struct {
	Kind              ConstraintKind
	Weight            int64
	Location          string
	Cost              values.Money
	QualificationRefs []values.EntityRef
	// AuthorizationRefs carries LEGAL_AUTHORIZATION evidence: the legal
	// authorizations a worker must hold for the window.
	AuthorizationRefs []values.EntityRef
	// MaxFatigueMinutes carries the FATIGUE_LIMIT cap: accrued worker
	// fatigue plus the window's own minutes must not exceed it.
	MaxFatigueMinutes int64
}

// WeightedConstraint is a descriptive alias for callers building soft rules.
type WeightedConstraint = Constraint

func (c Constraint) validate(hard bool, tenant values.TenantId) error {
	if !c.Kind.Valid() {
		return fmt.Errorf("%w: %q", ErrUnknownConstraint, c.Kind)
	}
	if hard {
		if c.Weight != 0 {
			return fmt.Errorf("%w: hard constraint %q must not carry a weight", ErrInvalidProblem, c.Kind)
		}
	} else if c.Weight <= 0 {
		return fmt.Errorf("%w: soft constraint %q requires a positive weight", ErrInvalidProblem, c.Kind)
	}
	switch c.Kind {
	case ConstraintLocation:
		if strings.TrimSpace(c.Location) == "" {
			return fmt.Errorf("%w: location constraint requires a location", ErrInvalidProblem)
		}
	case ConstraintCostCeiling:
		if err := c.Cost.Validate(); err != nil {
			return fmt.Errorf("%w: cost ceiling: %v", ErrInvalidProblem, err)
		}
	case ConstraintQualification:
		if len(c.QualificationRefs) == 0 {
			return fmt.Errorf("%w: qualification constraint requires references", ErrInvalidProblem)
		}
		seen := make(map[string]struct{}, len(c.QualificationRefs))
		for _, ref := range c.QualificationRefs {
			if err := ref.Validate(); err != nil || ref.Tenant != tenant {
				return fmt.Errorf("%w: qualification reference is invalid or crosses tenant", ErrInvalidProblem)
			}
			if _, ok := seen[ref.String()]; ok {
				return fmt.Errorf("%w: duplicate qualification reference", ErrInvalidProblem)
			}
			seen[ref.String()] = struct{}{}
		}
	case ConstraintLegalAuthorization:
		if len(c.AuthorizationRefs) == 0 {
			return fmt.Errorf("%w: legal authorization constraint requires references", ErrInvalidProblem)
		}
		seen := make(map[string]struct{}, len(c.AuthorizationRefs))
		for _, ref := range c.AuthorizationRefs {
			if err := ref.Validate(); err != nil || ref.Tenant != tenant {
				return fmt.Errorf("%w: legal authorization reference is invalid or crosses tenant", ErrInvalidProblem)
			}
			if _, ok := seen[ref.String()]; ok {
				return fmt.Errorf("%w: duplicate legal authorization reference", ErrInvalidProblem)
			}
			seen[ref.String()] = struct{}{}
		}
	case ConstraintFatigueLimit:
		if c.MaxFatigueMinutes <= 0 {
			return fmt.Errorf("%w: fatigue limit requires a positive minute cap", ErrInvalidProblem)
		}
	default:
		if c.Location != "" || c.Weight < 0 || len(c.QualificationRefs) != 0 || c.Cost != (values.Money{}) || len(c.AuthorizationRefs) != 0 || c.MaxFatigueMinutes != 0 {
			return fmt.Errorf("%w: constraint %q has fields not defined for its kind", ErrInvalidProblem, c.Kind)
		}
	}
	return nil
}

// DemandWindow is the versioned demand signal used as one schedulable window.
// The alias keeps demand's canonical identity and validation rules intact.
type DemandWindow = demand.DemandSignal

// Bounds makes the problem finite. No default bound is supplied implicitly.
type Bounds struct {
	MaxCandidates        int
	MaxDemandWindows     int
	MaxDecisionVariables int
	MaxHardConstraints   int
	MaxSoftConstraints   int
}

func (b Bounds) validate() error {
	if b.MaxCandidates <= 0 || b.MaxDemandWindows <= 0 || b.MaxDecisionVariables <= 0 || b.MaxHardConstraints <= 0 || b.MaxSoftConstraints <= 0 {
		return ErrUnboundedProblem
	}
	return nil
}

// SolverSpec records the intended external solver contract without shipping
// or invoking a solver in this package.
type SolverSpec struct {
	Name    string
	Version string
}

func (s SolverSpec) validate() error {
	if strings.TrimSpace(s.Name) == "" || strings.TrimSpace(s.Version) == "" {
		return fmt.Errorf("%w: solver name and version are required", ErrInvalidProblem)
	}
	return nil
}

// DecisionVariable is one candidate/window pair. It is generated solely from
// the frozen population and the declared demand windows.
type DecisionVariable struct {
	CandidateRef   values.EntityRef
	DemandWindowID string
}

func (v DecisionVariable) validate(tenant values.TenantId) error {
	if err := v.CandidateRef.Validate(); err != nil || v.CandidateRef.Tenant != tenant || strings.TrimSpace(v.DemandWindowID) == "" {
		return fmt.Errorf("%w: invalid decision variable", ErrInvalidProblem)
	}
	return nil
}

// WorkforceOptimizationProblem is a complete, bounded problem definition.
type WorkforceOptimizationProblem struct {
	ProblemID         string
	Version           string
	Population        matching.CandidatePopulation
	DemandWindows     []DemandWindow
	Horizon           values.EffectiveInterval
	Objective         Objective
	HardConstraints   []Constraint
	SoftConstraints   []Constraint
	Bounds            Bounds
	Solver            SolverSpec
	DecisionVariables []DecisionVariable
	CanonicalDigest   string
}

// Problem is the concise name used by callers.
type Problem = WorkforceOptimizationProblem

func validateWindows(windows []DemandWindow, horizon values.EffectiveInterval) error {
	if len(windows) == 0 {
		return fmt.Errorf("%w: at least one demand window is required", ErrInvalidProblem)
	}
	if err := horizon.Validate(); err != nil || horizon.Kind() != values.IntervalKindInstant {
		return fmt.Errorf("%w: horizon must use INSTANT boundaries", ErrInvalidProblem)
	}
	seen := make(map[string]struct{}, len(windows))
	for i, window := range windows {
		if err := window.Validate(); err != nil {
			return fmt.Errorf("%w: demand window %d: %v", ErrInvalidProblem, i, err)
		}
		if window.Work.Kind() != values.IntervalKindInstant {
			return fmt.Errorf("%w: demand window %d must use INSTANT boundaries", ErrInvalidProblem, i)
		}
		if window.Quantity.Value().Sign() <= 0 {
			return fmt.Errorf("%w: demand window %d quantity must be positive", ErrInvalidProblem, i)
		}
		if _, ok := seen[window.SignalID]; ok {
			return fmt.Errorf("%w: duplicate demand window %q", ErrInvalidProblem, window.SignalID)
		}
		seen[window.SignalID] = struct{}{}
		covered, err := horizonContains(horizon, window.Work)
		if err != nil || !covered {
			return fmt.Errorf("%w: demand window %q is outside the horizon", ErrInvalidProblem, window.SignalID)
		}
	}
	return nil
}

func horizonContains(container, target values.EffectiveInterval) (bool, error) {
	start, _ := container.StartInstant()
	targetStart, _ := target.StartInstant()
	if start.After(targetStart) {
		return false, nil
	}
	end, hasEnd := container.EndInstant()
	targetEnd, targetHasEnd := target.EndInstant()
	return !targetHasEnd || hasEnd && !end.Before(targetEnd), nil
}

func validateConstraints(constraints []Constraint, hard bool, tenant values.TenantId, bound int) error {
	if len(constraints) > bound {
		return fmt.Errorf("%w: constraint count exceeds declared bound", ErrInvalidProblem)
	}
	seen := make(map[ConstraintKind]struct{}, len(constraints))
	for i, constraint := range constraints {
		if err := constraint.validate(hard, tenant); err != nil {
			return fmt.Errorf("%w at index %d: %w", ErrInvalidProblem, i, err)
		}
		if _, ok := seen[constraint.Kind]; ok {
			return fmt.Errorf("%w: duplicate %q constraint", ErrInvalidProblem, constraint.Kind)
		}
		seen[constraint.Kind] = struct{}{}
	}
	return nil
}

func expectedVariables(population matching.CandidatePopulation, windows []DemandWindow) []DecisionVariable {
	candidates := population.CandidateFactsList()
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].CandidateRef.String() < candidates[j].CandidateRef.String() })
	orderedWindows := append([]DemandWindow(nil), windows...)
	sort.Slice(orderedWindows, func(i, j int) bool { return orderedWindows[i].SignalID < orderedWindows[j].SignalID })
	variables := make([]DecisionVariable, 0, len(candidates)*len(orderedWindows))
	for _, candidate := range candidates {
		for _, window := range orderedWindows {
			variables = append(variables, DecisionVariable{CandidateRef: candidate.CandidateRef, DemandWindowID: window.SignalID})
		}
	}
	return variables
}

func (p WorkforceOptimizationProblem) validateVariables(expected []DecisionVariable) error {
	if len(p.DecisionVariables) != len(expected) {
		return fmt.Errorf("%w: decision variables do not bind the frozen population and demand windows", ErrInvalidProblem)
	}
	for i, variable := range p.DecisionVariables {
		if err := variable.validate(p.Population.RequesterScope.Tenant); err != nil {
			return err
		}
		if variable != expected[i] {
			return fmt.Errorf("%w: decision variable %d is not canonical", ErrInvalidProblem, i)
		}
	}
	return nil
}

func (p WorkforceOptimizationProblem) Validate() error {
	if strings.TrimSpace(p.ProblemID) == "" || strings.TrimSpace(p.Version) == "" {
		return fmt.Errorf("%w: problem id and version are required", ErrInvalidProblem)
	}
	if err := p.Bounds.validate(); err != nil {
		return err
	}
	if err := p.Population.Validate(); err != nil {
		return fmt.Errorf("%w: population: %v", ErrInvalidProblem, err)
	}
	if err := p.Objective.Validate(); err != nil {
		return err
	}
	if err := p.Solver.validate(); err != nil {
		return err
	}
	if len(p.Population.Candidates) > p.Bounds.MaxCandidates {
		return fmt.Errorf("%w: candidate count exceeds declared bound", ErrInvalidProblem)
	}
	if len(p.DemandWindows) > p.Bounds.MaxDemandWindows {
		return fmt.Errorf("%w: demand window count exceeds declared bound", ErrInvalidProblem)
	}
	if err := validateWindows(p.DemandWindows, p.Horizon); err != nil {
		return err
	}
	if err := validateConstraints(p.HardConstraints, true, p.Population.RequesterScope.Tenant, p.Bounds.MaxHardConstraints); err != nil {
		return err
	}
	if err := validateConstraints(p.SoftConstraints, false, p.Population.RequesterScope.Tenant, p.Bounds.MaxSoftConstraints); err != nil {
		return err
	}
	expected := expectedVariables(p.Population, p.DemandWindows)
	if len(expected) > p.Bounds.MaxDecisionVariables {
		return fmt.Errorf("%w: decision variable count exceeds declared bound", ErrInvalidProblem)
	}
	if err := p.validateVariables(expected); err != nil {
		return err
	}
	if p.CanonicalDigest != "" && p.CanonicalDigest != p.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidProblem)
	}
	return nil
}

// NewProblem builds the canonical decision-variable cross product and freezes
// the problem definition. It does not invoke a solver.
func NewProblem(p WorkforceOptimizationProblem) (WorkforceOptimizationProblem, error) {
	p.DemandWindows = append([]DemandWindow(nil), p.DemandWindows...)
	p.HardConstraints = append([]Constraint(nil), p.HardConstraints...)
	p.SoftConstraints = append([]Constraint(nil), p.SoftConstraints...)
	p.DecisionVariables = expectedVariables(p.Population, p.DemandWindows)
	p.CanonicalDigest = ""
	if err := p.Validate(); err != nil {
		return WorkforceOptimizationProblem{}, err
	}
	p.CanonicalDigest = p.computedDigest()
	return p, nil
}

// Canonical returns the deterministic problem definition encoding.
func (p WorkforceOptimizationProblem) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	return p.body()
}

func constraintCanonical(c Constraint, mode string) []byte {
	w := canonicalbytes.New("hcmnext.domains.schedopt.Constraint", schemaVersion).
		String("kind", string(c.Kind)).String("mode", mode).Int("weight", c.Weight)
	if c.Location != "" {
		w.String("location", c.Location)
	}
	if c.Cost != (values.Money{}) {
		w.Value("cost", c.Cost)
	}
	refs := make([]string, 0, len(c.QualificationRefs))
	for _, ref := range c.QualificationRefs {
		refs = append(refs, ref.String())
	}
	w.SortedStrings("qualification_ref", refs)
	authorizations := make([]string, 0, len(c.AuthorizationRefs))
	for _, ref := range c.AuthorizationRefs {
		authorizations = append(authorizations, ref.String())
	}
	w.SortedStrings("authorization_ref", authorizations).Int("max_fatigue_minutes", c.MaxFatigueMinutes)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (p WorkforceOptimizationProblem) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.schedopt.WorkforceOptimizationProblem", schemaVersion).
		String("problem_id", p.ProblemID).String("version", p.Version).
		Field("population", p.Population.Canonical()).Count("demand_windows", len(p.DemandWindows))
	windows := append([]DemandWindow(nil), p.DemandWindows...)
	sort.Slice(windows, func(i, j int) bool { return windows[i].SignalID < windows[j].SignalID })
	for _, window := range windows {
		w.Value("demand_window", window)
	}
	w.Value("horizon", p.Horizon).String("objective", string(p.Objective.Kind)).Int("objective_weight", p.Objective.Weight).
		String("solver", p.Solver.Name).String("solver_version", p.Solver.Version).
		Int("max_candidates", int64(p.Bounds.MaxCandidates)).Int("max_demand_windows", int64(p.Bounds.MaxDemandWindows)).
		Int("max_decision_variables", int64(p.Bounds.MaxDecisionVariables)).Int("max_hard_constraints", int64(p.Bounds.MaxHardConstraints)).Int("max_soft_constraints", int64(p.Bounds.MaxSoftConstraints))
	w.Count("hard_constraints", len(p.HardConstraints))
	for _, constraint := range p.HardConstraints {
		w.Field("hard_constraint", constraintCanonical(constraint, "HARD"))
	}
	w.Count("soft_constraints", len(p.SoftConstraints))
	for _, constraint := range p.SoftConstraints {
		w.Field("soft_constraint", constraintCanonical(constraint, "SOFT"))
	}
	w.Count("decision_variables", len(p.DecisionVariables))
	for _, variable := range p.DecisionVariables {
		w.Value("decision_candidate", variable.CandidateRef).String("decision_window", variable.DemandWindowID)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (p WorkforceOptimizationProblem) computedDigest() string {
	raw := p.body()
	if raw == nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

func (p WorkforceOptimizationProblem) Digest() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return p.CanonicalDigest, nil
}

// HardConstraintFailure identifies a hard rule for which no candidate can
// satisfy the declared demand window. Candidate references are not included,
// so the report cannot become an enumeration channel.
type HardConstraintFailure struct {
	DemandWindowID string
	ConstraintKind ConstraintKind
	Reason         string
}

// FeasibilityReport is produced before any solver could run.
type FeasibilityReport struct {
	ProblemID       string
	ProblemDigest   string
	Feasible        bool
	Unmet           []HardConstraintFailure
	CanonicalDigest string
}

func candidateSatisfies(candidate matching.CandidateFacts, window DemandWindow, constraint Constraint) bool {
	switch constraint.Kind {
	case ConstraintAvailability:
		return intervalContains(candidate.Availability, window.Work)
	case ConstraintLocation:
		return candidate.Location == constraint.Location
	case ConstraintQualification:
		available := make(map[string]struct{}, len(candidate.QualificationRefs))
		for _, ref := range candidate.QualificationRefs {
			available[ref.String()] = struct{}{}
		}
		for _, required := range constraint.QualificationRefs {
			if _, ok := available[required.String()]; !ok {
				return false
			}
		}
		return true
	case ConstraintCostCeiling:
		comparison, err := candidate.Cost.Cmp(constraint.Cost)
		return err == nil && comparison <= 0
	case ConstraintCandidateUniqueness, ConstraintCoverage:
		return true
	default:
		return false
	}
}

func intervalContains(candidates []values.EffectiveInterval, target values.EffectiveInterval) bool {
	for _, candidate := range candidates {
		if candidate.Validate() != nil || target.Validate() != nil || candidate.Kind() != values.IntervalKindInstant || target.Kind() != values.IntervalKindInstant {
			continue
		}
		candidateStart, _ := candidate.StartInstant()
		targetStart, _ := target.StartInstant()
		if candidateStart.After(targetStart) {
			continue
		}
		candidateEnd, hasCandidateEnd := candidate.EndInstant()
		targetEnd, hasTargetEnd := target.EndInstant()
		if !hasTargetEnd || hasCandidateEnd && !candidateEnd.Before(targetEnd) {
			return true
		}
	}
	return false
}

func reportDigest(report FeasibilityReport) string {
	w := canonicalbytes.New("hcmnext.domains.schedopt.FeasibilityReport", schemaVersion).
		String("problem_id", report.ProblemID).String("problem_digest", report.ProblemDigest).Bool("feasible", report.Feasible).
		Count("unmet", len(report.Unmet))
	for _, failure := range report.Unmet {
		w.String("window", failure.DemandWindowID).String("constraint", string(failure.ConstraintKind)).String("reason", failure.Reason)
	}
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// FeasibilityPrecheck reports hard constraints that cannot be met for a
// demand window. It is a pure structural check and does not select or assign
// a candidate.
func (p WorkforceOptimizationProblem) FeasibilityPrecheck() (FeasibilityReport, error) {
	if err := p.Validate(); err != nil {
		return FeasibilityReport{}, err
	}
	windows := append([]DemandWindow(nil), p.DemandWindows...)
	sort.Slice(windows, func(i, j int) bool { return windows[i].SignalID < windows[j].SignalID })
	candidates := p.Population.CandidateFactsList()
	constraints := append([]Constraint(nil), p.HardConstraints...)
	var unmet []HardConstraintFailure
	for _, window := range windows {
		viable := false
		for _, candidate := range candidates {
			matchesAll := true
			for _, constraint := range constraints {
				if !candidateSatisfies(candidate, window, constraint) {
					matchesAll = false
					break
				}
			}
			if matchesAll {
				viable = true
				break
			}
		}
		if viable {
			continue
		}
		if len(candidates) == 0 {
			unmet = append(unmet, HardConstraintFailure{DemandWindowID: window.SignalID, ConstraintKind: ConstraintCoverage, Reason: "no authorized candidate is available"})
			continue
		}
		for _, constraint := range constraints {
			matched := false
			for _, candidate := range candidates {
				if candidateSatisfies(candidate, window, constraint) {
					matched = true
					break
				}
			}
			if !matched {
				unmet = append(unmet, HardConstraintFailure{DemandWindowID: window.SignalID, ConstraintKind: constraint.Kind, Reason: "no authorized candidate satisfies the hard constraint"})
			}
		}
		if len(unmet) == 0 || unmet[len(unmet)-1].DemandWindowID != window.SignalID {
			unmet = append(unmet, HardConstraintFailure{DemandWindowID: window.SignalID, ConstraintKind: ConstraintCoverage, Reason: "hard constraints have no common candidate"})
		}
	}
	report := FeasibilityReport{ProblemID: p.ProblemID, ProblemDigest: p.CanonicalDigest, Feasible: len(unmet) == 0, Unmet: unmet}
	report.CanonicalDigest = reportDigest(report)
	return report, nil
}

// Precheck is the concise function form of FeasibilityPrecheck.
func Precheck(p WorkforceOptimizationProblem) (FeasibilityReport, error) {
	return p.FeasibilityPrecheck()
}

// Explanation is bounded and records the declared objective and constraints,
// not a solver result or assignment authority.
type Explanation struct {
	ProblemID             string
	ProblemDigest         string
	Objective             ObjectiveKind
	CandidateCount        int
	DemandWindowCount     int
	DecisionVariableCount int
	HardConstraintKinds   []ConstraintKind
	SoftConstraintKinds   []ConstraintKind
	Feasible              bool
	Authority             string
}

func (p WorkforceOptimizationProblem) Explain() (Explanation, error) {
	if err := p.Validate(); err != nil {
		return Explanation{}, err
	}
	report, err := p.FeasibilityPrecheck()
	if err != nil {
		return Explanation{}, err
	}
	hard := make([]ConstraintKind, 0, len(p.HardConstraints))
	for _, constraint := range p.HardConstraints {
		hard = append(hard, constraint.Kind)
	}
	soft := make([]ConstraintKind, 0, len(p.SoftConstraints))
	for _, constraint := range p.SoftConstraints {
		soft = append(soft, constraint.Kind)
	}
	return Explanation{ProblemID: p.ProblemID, ProblemDigest: p.CanonicalDigest, Objective: p.Objective.Kind, CandidateCount: len(p.Population.Candidates), DemandWindowCount: len(p.DemandWindows), DecisionVariableCount: len(p.DecisionVariables), HardConstraintKinds: hard, SoftConstraintKinds: soft, Feasible: report.Feasible, Authority: "bounded descriptive problem only; no solver or assignment authority"}, nil
}

// Explain returns the package-level explanation required by the domain
// contract.
func Explain(p WorkforceOptimizationProblem) (Explanation, error) { return p.Explain() }

// ScheduleQualificationProof binds QUAL-006 to one worker and demand window
// from the frozen scheduling problem.
type ScheduleQualificationProof struct {
	CandidateRef  values.EntityRef
	WindowID      string
	Feasibility   FeasibilityReport
	Qualification qualification.CrossQualResult
}

// EvaluateQualificationForWindow exercises the qualification contract from
// the scheduling consumer with the selected worker and actual demand interval.
func EvaluateQualificationForWindow(p WorkforceOptimizationProblem, candidateRef values.EntityRef, windowID string, input qualification.CrossQualInput) (ScheduleQualificationProof, error) {
	if err := p.Validate(); err != nil {
		return ScheduleQualificationProof{}, err
	}
	if err := candidateRef.Validate(); err != nil || candidateRef.Tenant != p.Population.RequesterScope.Tenant {
		return ScheduleQualificationProof{}, fmt.Errorf("%w: invalid qualification candidate", ErrInvalidProblem)
	}
	if input.Tenant != string(candidateRef.Tenant) {
		return ScheduleQualificationProof{}, fmt.Errorf("%w: qualification tenant does not match scheduling population", ErrInvalidProblem)
	}
	var candidate *matching.CandidateFacts
	for _, fact := range p.Population.CandidateFactsList() {
		if fact.CandidateRef == candidateRef {
			copy := fact
			candidate = &copy
			break
		}
	}
	if candidate == nil {
		return ScheduleQualificationProof{}, fmt.Errorf("%w: qualification candidate is outside the frozen population", ErrInvalidProblem)
	}
	var window *DemandWindow
	for i := range p.DemandWindows {
		if p.DemandWindows[i].SignalID == windowID {
			copy := p.DemandWindows[i]
			window = &copy
			break
		}
	}
	if window == nil {
		return ScheduleQualificationProof{}, fmt.Errorf("%w: qualification demand window is unknown", ErrInvalidProblem)
	}
	start, _ := window.Work.StartInstant()
	end, hasEnd := window.Work.EndInstant()
	if !hasEnd {
		return ScheduleQualificationProof{}, fmt.Errorf("%w: qualification demand window must be bounded", ErrInvalidProblem)
	}
	availabilityDeclared := false
	for _, constraint := range p.HardConstraints {
		if constraint.Kind == ConstraintAvailability {
			availabilityDeclared = true
		}
		if !candidateSatisfies(*candidate, *window, constraint) {
			return ScheduleQualificationProof{}, fmt.Errorf("%w: candidate does not satisfy hard constraint %q for the demand window", ErrInvalidProblem, constraint.Kind)
		}
	}
	if !availabilityDeclared || !intervalContains(candidate.Availability, window.Work) {
		return ScheduleQualificationProof{}, fmt.Errorf("%w: candidate is not available for the demand window", ErrInvalidProblem)
	}
	feasibility, err := p.FeasibilityPrecheck()
	if err != nil {
		return ScheduleQualificationProof{}, err
	}
	qualified, err := qualification.EvaluateCrossQualificationForSchedule(input, qualification.SchedulingBinding{
		WorkerRef: candidateRef.String(), WorkStart: start.Time(), WorkEnd: end.Time(),
	})
	if err != nil {
		return ScheduleQualificationProof{}, err
	}
	return ScheduleQualificationProof{CandidateRef: candidateRef, WindowID: window.SignalID, Feasibility: feasibility, Qualification: qualified}, nil
}

// ProveMatchingForSchedule binds MATCH-007's Scheduling run to the frozen
// candidate population and a real demand window before proving the four-domain
// ranking contract.
func ProveMatchingForSchedule(p WorkforceOptimizationProblem, runs []matching.DomainRun) (matching.FourDomainConformance, error) {
	if err := p.Validate(); err != nil {
		return matching.FourDomainConformance{}, err
	}
	var schedule *matching.DomainRun
	for i := range runs {
		if runs[i].Domain == matching.DomainScheduling {
			schedule = &runs[i]
			break
		}
	}
	if schedule == nil {
		return matching.FourDomainConformance{}, fmt.Errorf("%w: matching proof lacks scheduling run", ErrInvalidProblem)
	}
	if err := matching.ValidateDomainRun(*schedule); err != nil {
		return matching.FourDomainConformance{}, err
	}
	if schedule.Request.RequestID != p.Population.RequestID || schedule.Request.CanonicalDigest != p.Population.RequestDigest ||
		schedule.Request.RequesterScope != p.Population.RequesterScope || schedule.Request.CandidateSourceRef != p.Population.CandidateSourceRef {
		return matching.FourDomainConformance{}, fmt.Errorf("%w: scheduling match request is not bound to the frozen population", ErrInvalidProblem)
	}
	windowBound := false
	for _, constraint := range schedule.Request.Constraints {
		if constraint.Kind != matching.ConstraintAvailabilityWindow || constraint.Mode != matching.ConstraintHard {
			continue
		}
		for _, window := range p.DemandWindows {
			if constraint.Window == window.Work {
				windowBound = true
				break
			}
		}
	}
	if !windowBound {
		return matching.FourDomainConformance{}, fmt.Errorf("%w: scheduling match run has no hard constraint for a demand window", ErrInvalidProblem)
	}
	population := make(map[string]struct{}, len(p.Population.Candidates))
	for _, fact := range p.Population.CandidateFactsList() {
		population[fact.CandidateRef.String()] = struct{}{}
	}
	if len(schedule.Result.Matches) == 0 {
		return matching.FourDomainConformance{}, fmt.Errorf("%w: scheduling ranking contains no candidate data", ErrInvalidProblem)
	}
	for _, result := range schedule.Result.Matches {
		if _, ok := population[result.CandidateRef.String()]; !ok {
			return matching.FourDomainConformance{}, fmt.Errorf("%w: scheduling ranking includes a candidate outside the frozen population", ErrInvalidProblem)
		}
	}
	return matching.ProveFourDomainConformance(runs)
}

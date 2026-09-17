package schedopt

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/matching"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrInvalidWorkerStanding reports standing evidence that cannot be
	// trusted: an unknown or cross-tenant candidate, negative fatigue,
	// or a duplicate record.
	ErrInvalidWorkerStanding = errors.New("schedopt: invalid worker standing")
	// ErrInvalidHardConstraintEvaluation reports a sealed evaluation
	// whose verdicts, conflicts or digest do not reconcile.
	ErrInvalidHardConstraintEvaluation = errors.New("schedopt: invalid hard constraint evaluation")
)

// WorkerStanding is the schedopt-owned eligibility evidence for one
// candidate: the legal authorizations it holds and its accrued fatigue
// minutes. It is supplied alongside the frozen problem, never read from
// a live system, so the evaluation stays pure and deterministic.
type WorkerStanding struct {
	CandidateRef          values.EntityRef
	LegalAuthorizations   []values.EntityRef
	AccruedFatigueMinutes int64
}

// VariableVerdict is ASSIGNABLE only when every hard constraint holds.
// A verdict never weighs the objective: a cheaper or higher-coverage
// variable that violates one hard rule is BLOCKED.
type VariableVerdict string

const (
	VerdictAssignable VariableVerdict = "ASSIGNABLE"
	VerdictBlocked    VariableVerdict = "BLOCKED"
)

// VariableEvaluation is one decision variable's hard-constraint verdict.
// BlockingKinds names every violated hard kind in declared order.
type VariableEvaluation struct {
	CandidateRef   values.EntityRef
	DemandWindowID string
	Verdict        VariableVerdict
	BlockingKinds  []ConstraintKind
}

// KindDefeat counts the candidates one hard kind eliminated on a window.
type KindDefeat struct {
	ConstraintKind       ConstraintKind
	EliminatedCandidates int
}

// WindowConflict is the exact conflict set for one uncovered window:
// every hard kind that eliminated candidates, with counts, and no
// candidate enumeration.
type WindowConflict struct {
	DemandWindowID string
	DefeatedBy     []KindDefeat
}

// HardConstraintEvaluation applies every hard constraint to every
// decision variable. Feasible is true only when each window keeps at
// least one assignable variable; otherwise Conflicts names each
// uncovered window and its coverage gap.
type HardConstraintEvaluation struct {
	ProblemID       string
	ProblemDigest   string
	Evaluations     []VariableEvaluation
	Conflicts       []WindowConflict
	Feasible        bool
	CanonicalDigest string
}

func (s WorkerStanding) validate(tenant values.TenantId, known map[string]struct{}) error {
	if err := s.CandidateRef.Validate(); err != nil || s.CandidateRef.Tenant != tenant {
		return fmt.Errorf("%w: candidate reference is invalid or crosses tenant", ErrInvalidWorkerStanding)
	}
	if _, ok := known[s.CandidateRef.String()]; !ok {
		return fmt.Errorf("%w: candidate %s is not in the frozen population", ErrInvalidWorkerStanding, s.CandidateRef.String())
	}
	if s.AccruedFatigueMinutes < 0 {
		return fmt.Errorf("%w: accrued fatigue minutes must not be negative", ErrInvalidWorkerStanding)
	}
	seen := make(map[string]struct{}, len(s.LegalAuthorizations))
	for _, ref := range s.LegalAuthorizations {
		if err := ref.Validate(); err != nil || ref.Tenant != tenant {
			return fmt.Errorf("%w: legal authorization is invalid or crosses tenant", ErrInvalidWorkerStanding)
		}
		if _, ok := seen[ref.String()]; ok {
			return fmt.Errorf("%w: duplicate legal authorization", ErrInvalidWorkerStanding)
		}
		seen[ref.String()] = struct{}{}
	}
	return nil
}

// windowMinutes returns the finite duration of one demand window. An
// open-ended window has no provable duration, so no fatigue cap can
// admit it.
func windowMinutes(window DemandWindow) (int64, bool) {
	start, _ := window.Work.StartInstant()
	end, hasEnd := window.Work.EndInstant()
	if !hasEnd {
		return 0, false
	}
	minutes := int64(end.Time().Sub(start.Time()).Minutes())
	if minutes < 0 {
		return 0, false
	}
	return minutes, true
}

func standingSatisfies(standing *WorkerStanding, window DemandWindow, windowMins int64, windowBounded bool, constraint Constraint) bool {
	switch constraint.Kind {
	case ConstraintLegalAuthorization:
		if standing == nil {
			return false
		}
		held := make(map[string]struct{}, len(standing.LegalAuthorizations))
		for _, ref := range standing.LegalAuthorizations {
			held[ref.String()] = struct{}{}
		}
		for _, required := range constraint.AuthorizationRefs {
			if _, ok := held[required.String()]; !ok {
				return false
			}
		}
		return true
	case ConstraintFatigueLimit:
		if standing == nil || !windowBounded {
			return false
		}
		return standing.AccruedFatigueMinutes+windowMins <= constraint.MaxFatigueMinutes
	default:
		return false
	}
}

// EvaluateHardConstraints applies every hard constraint of the problem to
// every decision variable against the supplied standing evidence. A
// variable is ASSIGNABLE only when no hard kind blocks it; a window with
// no assignable variable yields an exact conflict entry. The objective
// never overrides a block: hard means hard.
func (p WorkforceOptimizationProblem) EvaluateHardConstraints(standings []WorkerStanding) (HardConstraintEvaluation, error) {
	if err := p.Validate(); err != nil {
		return HardConstraintEvaluation{}, err
	}
	tenant := p.Population.RequesterScope.Tenant
	candidates := p.Population.CandidateFactsList()
	known := make(map[string]struct{}, len(candidates))
	facts := make(map[string]matching.CandidateFacts, len(candidates))
	for _, candidate := range candidates {
		known[candidate.CandidateRef.String()] = struct{}{}
		facts[candidate.CandidateRef.String()] = candidate
	}
	byCandidate := make(map[string]*WorkerStanding, len(standings))
	for i := range standings {
		standing := standings[i]
		if err := standing.validate(tenant, known); err != nil {
			return HardConstraintEvaluation{}, err
		}
		key := standing.CandidateRef.String()
		if _, ok := byCandidate[key]; ok {
			return HardConstraintEvaluation{}, fmt.Errorf("%w: duplicate standing for %s", ErrInvalidWorkerStanding, key)
		}
		copied := standing
		copied.LegalAuthorizations = append([]values.EntityRef(nil), standing.LegalAuthorizations...)
		byCandidate[key] = &copied
	}
	windows := make(map[string]DemandWindow, len(p.DemandWindows))
	for _, window := range p.DemandWindows {
		windows[window.SignalID] = window
	}
	evaluation := HardConstraintEvaluation{ProblemID: p.ProblemID, ProblemDigest: p.CanonicalDigest}
	assignable := make(map[string]bool, len(p.DemandWindows))
	for _, variable := range p.DecisionVariables {
		window := windows[variable.DemandWindowID]
		windowMins, bounded := windowMinutes(window)
		candidate := facts[variable.CandidateRef.String()]
		var blocking []ConstraintKind
		for _, constraint := range p.HardConstraints {
			satisfied := candidateSatisfies(candidate, window, constraint)
			if !satisfied && (constraint.Kind == ConstraintLegalAuthorization || constraint.Kind == ConstraintFatigueLimit) {
				satisfied = standingSatisfies(byCandidate[variable.CandidateRef.String()], window, windowMins, bounded, constraint)
			}
			if !satisfied {
				blocking = append(blocking, constraint.Kind)
			}
		}
		verdict := VariableEvaluation{CandidateRef: variable.CandidateRef, DemandWindowID: variable.DemandWindowID, Verdict: VerdictAssignable, BlockingKinds: blocking}
		if len(blocking) != 0 {
			verdict.Verdict = VerdictBlocked
		} else {
			assignable[variable.DemandWindowID] = true
		}
		evaluation.Evaluations = append(evaluation.Evaluations, verdict)
	}
	ordered := append([]DemandWindow(nil), p.DemandWindows...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].SignalID < ordered[j].SignalID })
	for _, window := range ordered {
		if assignable[window.SignalID] {
			continue
		}
		defeated := make(map[ConstraintKind]int)
		order := make(map[ConstraintKind]int)
		for _, verdict := range evaluation.Evaluations {
			if verdict.DemandWindowID != window.SignalID {
				continue
			}
			for _, kind := range verdict.BlockingKinds {
				if _, ok := defeated[kind]; !ok {
					order[kind] = len(order)
				}
				defeated[kind]++
			}
		}
		conflict := WindowConflict{DemandWindowID: window.SignalID}
		for kind, count := range defeated {
			if count > 0 {
				conflict.DefeatedBy = append(conflict.DefeatedBy, KindDefeat{ConstraintKind: kind, EliminatedCandidates: count})
			}
		}
		sort.Slice(conflict.DefeatedBy, func(i, j int) bool {
			return order[conflict.DefeatedBy[i].ConstraintKind] < order[conflict.DefeatedBy[j].ConstraintKind]
		})
		evaluation.Conflicts = append(evaluation.Conflicts, conflict)
	}
	evaluation.Feasible = len(evaluation.Conflicts) == 0
	evaluation.CanonicalDigest = evaluationDigest(evaluation)
	return evaluation, nil
}

// ApplyHardConstraints is the function form of EvaluateHardConstraints.
func ApplyHardConstraints(p WorkforceOptimizationProblem, standings []WorkerStanding) (HardConstraintEvaluation, error) {
	return p.EvaluateHardConstraints(standings)
}

func evaluationDigest(evaluation HardConstraintEvaluation) string {
	w := canonicalbytes.New("hcmnext.domains.schedopt.HardConstraintEvaluation", schemaVersion).
		String("problem_id", evaluation.ProblemID).String("problem_digest", evaluation.ProblemDigest).
		Bool("feasible", evaluation.Feasible).
		Count("evaluations", len(evaluation.Evaluations))
	for _, verdict := range evaluation.Evaluations {
		w.Value("candidate", verdict.CandidateRef).String("window", verdict.DemandWindowID).String("verdict", string(verdict.Verdict))
		kinds := make([]string, 0, len(verdict.BlockingKinds))
		for _, kind := range verdict.BlockingKinds {
			kinds = append(kinds, string(kind))
		}
		w.SortedStrings("blocking", kinds)
	}
	w.Count("conflicts", len(evaluation.Conflicts))
	for _, conflict := range evaluation.Conflicts {
		w.String("conflict_window", conflict.DemandWindowID)
		for _, defeat := range conflict.DefeatedBy {
			w.String("defeated_by", string(defeat.ConstraintKind)).Int("eliminated", int64(defeat.EliminatedCandidates))
		}
	}
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// Verify checks the seal and the internal consistency of a previously
// constructed evaluation: verdicts must match their blocking sets and
// conflicts must exactly cover the windows with no assignable variable.
// Callers loading durable records must verify them before trusting the
// feasibility claim.
func (e HardConstraintEvaluation) Verify() error {
	if e.ProblemID == "" || e.ProblemDigest == "" || e.CanonicalDigest == "" {
		return ErrInvalidHardConstraintEvaluation
	}
	assignable := make(map[string]bool)
	verdicts := 0
	for _, verdict := range e.Evaluations {
		if err := verdict.CandidateRef.Validate(); err != nil || verdict.DemandWindowID == "" {
			return ErrInvalidHardConstraintEvaluation
		}
		switch verdict.Verdict {
		case VerdictAssignable:
			if len(verdict.BlockingKinds) != 0 {
				return ErrInvalidHardConstraintEvaluation
			}
			assignable[verdict.DemandWindowID] = true
		case VerdictBlocked:
			if len(verdict.BlockingKinds) == 0 {
				return ErrInvalidHardConstraintEvaluation
			}
		default:
			return ErrInvalidHardConstraintEvaluation
		}
		verdicts++
	}
	if verdicts == 0 {
		return ErrInvalidHardConstraintEvaluation
	}
	covered := make(map[string]bool)
	for _, conflict := range e.Conflicts {
		if conflict.DemandWindowID == "" || len(conflict.DefeatedBy) == 0 || covered[conflict.DemandWindowID] {
			return ErrInvalidHardConstraintEvaluation
		}
		covered[conflict.DemandWindowID] = true
		for _, defeat := range conflict.DefeatedBy {
			if !defeat.ConstraintKind.Valid() || defeat.EliminatedCandidates <= 0 {
				return ErrInvalidHardConstraintEvaluation
			}
		}
	}
	for window := range assignable {
		if covered[window] {
			return ErrInvalidHardConstraintEvaluation
		}
	}
	windows := make(map[string]bool)
	for _, verdict := range e.Evaluations {
		windows[verdict.DemandWindowID] = true
	}
	for window := range windows {
		if !assignable[window] && !covered[window] {
			return ErrInvalidHardConstraintEvaluation
		}
	}
	if (len(e.Conflicts) == 0) != e.Feasible {
		return ErrInvalidHardConstraintEvaluation
	}
	if evaluationDigest(e) != e.CanonicalDigest {
		return ErrInvalidHardConstraintEvaluation
	}
	return nil
}

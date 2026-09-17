// SCHED-OPT-004: optimize preferences and cost deterministically.
//
// Optimize scores every hard-assignable decision variable with the declared
// preference weights, breaks ties by lower cost then lower candidate
// reference, and assigns each demand window greedily in canonical window
// order. A CANDIDATE_UNIQUENESS hard constraint limits each candidate to one
// window; otherwise the best candidate may repeat. The result reports the
// weights, the tie-break chain, the approximation rule and the optimality
// gap against the per-window upper bound, sealed under a digest that binds
// the problem, the seed and the solver. The seed binds provenance only: it
// never changes the assignment rule. The package is kernel-pure.
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
	// ErrInvalidOptimization reports an optimization request that cannot be
	// trusted: an empty seed, a negative/duplicate/unknown preference, or
	// an unsealed problem.
	ErrInvalidOptimization = errors.New("schedopt: invalid optimization request")
)

// TieBreakOrder is the declared, total tie-break chain applied after the
// preference score.
const TieBreakOrder = "PREFERENCE_DESC/COST_ASC/CANDIDATE_REF_ASC"

// ApproximationRule names the honest heuristic: greedy per-window maximum,
// exact only when no uniqueness constraint couples the windows.
const ApproximationRule = "GREEDY_PER_WINDOW_MAX_SCORE"

// Preference pins one candidate's preference weight in preference points.
// Candidates without an entry score zero.
type Preference struct {
	CandidateRef values.EntityRef
	Weight       int64
}

// OptimizeRequest is the complete deterministic input: a sealed problem,
// standing evidence, preference weights and a provenance seed.
type OptimizeRequest struct {
	Problem     WorkforceOptimizationProblem
	Standings   []WorkerStanding
	Preferences []Preference
	Seed        string
}

// Assignment is one window's deterministic winner with its preference score.
type Assignment struct {
	CandidateRef   values.EntityRef
	DemandWindowID string
	Score          int64
}

// OptimizationResult is the sealed deterministic schedule.
type OptimizationResult struct {
	ProblemID         string
	ProblemDigest     string
	Seed              string
	Solver            string
	Assignments       []Assignment
	UnassignedWindows []string
	Weights           map[string]int64
	TieBreak          string
	Approximation     string
	TotalScore        int64
	UpperBound        int64
	OptimalityGap     int64
	CanonicalDigest   string
}

// Optimize assigns every demand window with at least one hard-assignable
// candidate. Windows with no assignable candidate are reported, never
// guessed.
func Optimize(req OptimizeRequest) (OptimizationResult, error) {
	if req.Seed == "" {
		return OptimizationResult{}, fmt.Errorf("%w: seed is required", ErrInvalidOptimization)
	}
	if err := req.Problem.Validate(); err != nil {
		return OptimizationResult{}, err
	}
	if req.Problem.CanonicalDigest == "" {
		return OptimizationResult{}, fmt.Errorf("%w: problem is not sealed", ErrInvalidOptimization)
	}
	weights, err := preferenceWeights(req.Problem, req.Preferences)
	if err != nil {
		return OptimizationResult{}, err
	}
	evaluation, err := req.Problem.EvaluateHardConstraints(req.Standings)
	if err != nil {
		return OptimizationResult{}, err
	}
	facts := make(map[string]matching.CandidateFacts, len(req.Problem.Population.Candidates))
	for _, candidate := range req.Problem.Population.CandidateFactsList() {
		facts[candidate.CandidateRef.String()] = candidate
	}
	assignable := make(map[string][]values.EntityRef)
	for _, verdict := range evaluation.Evaluations {
		if verdict.Verdict == VerdictAssignable {
			assignable[verdict.DemandWindowID] = append(assignable[verdict.DemandWindowID], verdict.CandidateRef)
		}
	}
	unique := false
	for _, constraint := range req.Problem.HardConstraints {
		if constraint.Kind == ConstraintCandidateUniqueness {
			unique = true
		}
	}
	windows := append([]DemandWindow(nil), req.Problem.DemandWindows...)
	sort.Slice(windows, func(i, j int) bool { return windows[i].SignalID < windows[j].SignalID })
	result := OptimizationResult{
		ProblemID:     req.Problem.ProblemID,
		ProblemDigest: req.Problem.CanonicalDigest,
		Seed:          req.Seed,
		Solver:        req.Problem.Solver.Name + "/" + req.Problem.Solver.Version,
		Weights:       weights,
		TieBreak:      TieBreakOrder,
		Approximation: ApproximationRule,
	}
	used := make(map[string]bool)
	for _, window := range windows {
		candidates := assignable[window.SignalID]
		ordered := append([]values.EntityRef(nil), candidates...)
		sort.Slice(ordered, func(i, j int) bool {
			return lessPreferred(weights, facts, ordered[i], ordered[j])
		})
		var upper int64
		var picked *values.EntityRef
		for i := range ordered {
			score := weights[ordered[i].String()]
			if i == 0 {
				upper = score
			}
			if picked == nil && (!unique || !used[ordered[i].String()]) {
				picked = &ordered[i]
			}
		}
		if len(ordered) > 0 {
			result.UpperBound += upper
		}
		if picked == nil {
			result.UnassignedWindows = append(result.UnassignedWindows, window.SignalID)
			continue
		}
		score := weights[picked.String()]
		result.Assignments = append(result.Assignments, Assignment{CandidateRef: *picked, DemandWindowID: window.SignalID, Score: score})
		result.TotalScore += score
		used[picked.String()] = true
	}
	result.OptimalityGap = result.UpperBound - result.TotalScore
	result.CanonicalDigest = optimizationDigest(result)
	return result, nil
}

func preferenceWeights(problem WorkforceOptimizationProblem, preferences []Preference) (map[string]int64, error) {
	tenant := problem.Population.RequesterScope.Tenant
	known := make(map[string]struct{})
	for _, candidate := range problem.Population.CandidateFactsList() {
		known[candidate.CandidateRef.String()] = struct{}{}
	}
	weights := make(map[string]int64)
	seen := make(map[string]struct{}, len(preferences))
	for i, preference := range preferences {
		if err := preference.CandidateRef.Validate(); err != nil || preference.CandidateRef.Tenant != tenant {
			return nil, fmt.Errorf("%w: preference %d is invalid or crosses tenant", ErrInvalidOptimization, i)
		}
		if _, ok := known[preference.CandidateRef.String()]; !ok {
			return nil, fmt.Errorf("%w: preference %d names a candidate outside the frozen population", ErrInvalidOptimization, i)
		}
		if preference.Weight < 0 {
			return nil, fmt.Errorf("%w: preference %d weight must not be negative", ErrInvalidOptimization, i)
		}
		key := preference.CandidateRef.String()
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("%w: duplicate preference for %s", ErrInvalidOptimization, key)
		}
		seen[key] = struct{}{}
		// Zero weights are no-ops: dropping them keeps the sealed digest
		// a function of semantic inputs, not of input spelling.
		if preference.Weight != 0 {
			weights[key] = preference.Weight
		}
	}
	return weights, nil
}

// lessPreferred orders candidates by descending preference score, ascending
// cost, then ascending candidate reference: a total deterministic order.
func lessPreferred(weights map[string]int64, facts map[string]matching.CandidateFacts, a, b values.EntityRef) bool {
	if weights[a.String()] != weights[b.String()] {
		return weights[a.String()] > weights[b.String()]
	}
	comparison, err := facts[a.String()].Cost.Cmp(facts[b.String()].Cost)
	if err == nil && comparison != 0 {
		return comparison < 0
	}
	return a.String() < b.String()
}

func optimizationDigest(result OptimizationResult) string {
	keys := make([]string, 0, len(result.Weights))
	for key := range result.Weights {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	w := canonicalbytes.New("hcmnext.domains.schedopt.OptimizationResult", schemaVersion).
		String("problem_id", result.ProblemID).String("problem_digest", result.ProblemDigest).
		String("seed", result.Seed).String("solver", result.Solver).
		String("tie_break", result.TieBreak).String("approximation", result.Approximation).
		Int("total_score", result.TotalScore).Int("upper_bound", result.UpperBound).Int("optimality_gap", result.OptimalityGap).
		Count("assignments", len(result.Assignments))
	for _, assignment := range result.Assignments {
		w.Value("candidate", assignment.CandidateRef).String("window", assignment.DemandWindowID).Int("score", assignment.Score)
	}
	w.Count("unassigned", len(result.UnassignedWindows))
	for _, window := range result.UnassignedWindows {
		w.String("unassigned_window", window)
	}
	w.Count("weights", len(keys))
	for _, key := range keys {
		w.String("weight_candidate", key).Int("weight", result.Weights[key])
	}
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// Verify checks the seal and internal consistency of a previously computed
// result: totals must reconcile, assignments must be canonical, and the
// digest must match.
func (r OptimizationResult) Verify() error {
	if r.ProblemID == "" || r.ProblemDigest == "" || r.Seed == "" || r.Solver == "" || r.CanonicalDigest == "" {
		return ErrInvalidOptimization
	}
	if r.TieBreak != TieBreakOrder || r.Approximation != ApproximationRule {
		return ErrInvalidOptimization
	}
	var total int64
	seen := make(map[string]struct{}, len(r.Assignments))
	for i, assignment := range r.Assignments {
		if err := assignment.CandidateRef.Validate(); err != nil || assignment.DemandWindowID == "" {
			return ErrInvalidOptimization
		}
		if _, dup := seen[assignment.DemandWindowID]; dup {
			return ErrInvalidOptimization
		}
		seen[assignment.DemandWindowID] = struct{}{}
		if i > 0 && r.Assignments[i-1].DemandWindowID >= assignment.DemandWindowID {
			return ErrInvalidOptimization
		}
		total += assignment.Score
	}
	if total != r.TotalScore || r.OptimalityGap != r.UpperBound-r.TotalScore || r.OptimalityGap < 0 {
		return ErrInvalidOptimization
	}
	for _, window := range r.UnassignedWindows {
		if window == "" {
			return ErrInvalidOptimization
		}
		if _, dup := seen[window]; dup {
			return ErrInvalidOptimization
		}
	}
	if optimizationDigest(r) != r.CanonicalDigest {
		return ErrInvalidOptimization
	}
	return nil
}

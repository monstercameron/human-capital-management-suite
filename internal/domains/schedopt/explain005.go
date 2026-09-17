// SCHED-OPT-005: explain candidate schedule assignments.
//
// ExplainAssignments traces every demand window of a sealed optimization
// result: the assigned worker with its preference weight and cost, or the
// exact hard-constraint conflict set when unassigned. Eliminated
// alternatives are reported as counts per hard kind, never as worker
// identities, so one worker's explanation discloses no other worker's
// protected facts. A stale or unsealed problem or result is rejected with
// SCHED_OPT_005_REJECTED. The package is kernel-pure.
package schedopt

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/matching"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ExplainVersion is the rejection version carried by ExplanationRejection.
const ExplainVersion = "schedopt-explain/v1"

var (
	// ErrExplanationRejected is the SCHED-OPT-005 seeded-defect sentinel. A
	// stale frozen input or any unsealed state must fail with this error
	// carrying the offending field, state and version.
	ErrExplanationRejected = errors.New("SCHED_OPT_005_REJECTED")
)

// Explanation reasons are closed: a window is either the deterministic
// greedy winner or it has no assignable candidate.
const (
	ReasonAssigned   = "ASSIGNED_GREEDY_MAX_SCORE"
	ReasonUnassigned = "UNASSIGNED_NO_ASSIGNABLE_CANDIDATE"
)

// ExplanationRejection is the stable SCHED-OPT-005 failure shape.
type ExplanationRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *ExplanationRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrExplanationRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the SCHED_OPT_005_REJECTED sentinel to errors.Is.
func (r *ExplanationRejection) Unwrap() error { return ErrExplanationRejected }

func explainReject(field, state, reason string) error {
	return &ExplanationRejection{Field: field, State: state, Version: ExplainVersion, Reason: reason}
}

// ExplainRequest is the complete explanation input: a sealed problem, the
// sealed result computed from it, the standing evidence the hard
// constraints were evaluated against, and the preference weights.
type ExplainRequest struct {
	Problem     WorkforceOptimizationProblem
	Result      OptimizationResult
	Standings   []WorkerStanding
	Preferences []Preference
}

// WindowExplanation traces one demand window. AssignedCandidate is empty
// when unassigned; eliminated alternatives appear only as counts, with
// per-kind blocking details for hard-constraint losses, never as worker
// identities.
type WindowExplanation struct {
	DemandWindowID         string
	AssignedCandidate      string
	AssignmentScore        int64
	PreferenceWeight       int64
	Cost                   values.Money
	AlternativesEliminated int
	// HardEliminated lost on a hard constraint (traced in BlockingKinds);
	// OutscoredAlternatives lost on preference/cost to the winner.
	HardEliminated        int
	OutscoredAlternatives int
	BlockingKinds         []KindDefeat
	Reason                string
}

// ScheduleExplanation is the sealed per-window trace of one result.
type ScheduleExplanation struct {
	ProblemID       string
	ProblemDigest   string
	ResultDigest    string
	TotalScore      int64
	Windows         []WindowExplanation
	CanonicalDigest string
}

// ExplainAssignments traces every demand window of the sealed result
// against the sealed problem. It writes nothing and preserves caller-owned
// inputs.
func ExplainAssignments(req ExplainRequest) (ScheduleExplanation, error) {
	if err := req.Problem.Validate(); err != nil {
		return ScheduleExplanation{}, explainReject("problem.definition", "UNSEALED", err.Error())
	}
	if err := req.Result.Verify(); err != nil {
		return ScheduleExplanation{}, explainReject("result.seal", "UNSEALED", err.Error())
	}
	if req.Result.ProblemID != req.Problem.ProblemID || req.Result.ProblemDigest != req.Problem.CanonicalDigest {
		return ScheduleExplanation{}, explainReject("result.problem_digest", "STALE", "result was not computed from the supplied frozen problem")
	}
	weights, err := preferenceWeights(req.Problem, req.Preferences)
	if err != nil {
		return ScheduleExplanation{}, explainReject("explanation.preferences", "INVALID", err.Error())
	}
	evaluation, err := req.Problem.EvaluateHardConstraints(req.Standings)
	if err != nil {
		return ScheduleExplanation{}, explainReject("explanation.standing", "INVALID", err.Error())
	}
	facts := make(map[string]matching.CandidateFacts, len(req.Problem.Population.Candidates))
	for _, candidate := range req.Problem.Population.CandidateFactsList() {
		facts[candidate.CandidateRef.String()] = candidate
	}
	assigned := make(map[string]Assignment, len(req.Result.Assignments))
	for _, assignment := range req.Result.Assignments {
		if _, dup := assigned[assignment.DemandWindowID]; dup {
			return ScheduleExplanation{}, explainReject("result.assignments", "DUPLICATE", "two assignments claim window "+assignment.DemandWindowID)
		}
		assigned[assignment.DemandWindowID] = assignment
	}
	unassigned := make(map[string]bool, len(req.Result.UnassignedWindows))
	for _, window := range req.Result.UnassignedWindows {
		if unassigned[window] {
			return ScheduleExplanation{}, explainReject("result.unassigned", "DUPLICATE", "window "+window+" is listed twice")
		}
		unassigned[window] = true
	}
	blocked := make(map[string][]KindDefeat)
	counts := make(map[string]int)
	hard := make(map[string]int)
	for _, verdict := range evaluation.Evaluations {
		counts[verdict.DemandWindowID]++
		if verdict.Verdict != VerdictBlocked {
			continue
		}
		hard[verdict.DemandWindowID]++
		seen := make(map[ConstraintKind]bool, len(verdict.BlockingKinds))
		for _, kind := range verdict.BlockingKinds {
			seen[kind] = true
		}
		// Keep declared hard-constraint order so the trace is canonical.
		for _, constraint := range req.Problem.HardConstraints {
			if !seen[constraint.Kind] {
				continue
			}
			defeats := blocked[verdict.DemandWindowID]
			found := false
			for i := range defeats {
				if defeats[i].ConstraintKind == constraint.Kind {
					defeats[i].EliminatedCandidates++
					found = true
					break
				}
			}
			if !found {
				defeats = append(defeats, KindDefeat{ConstraintKind: constraint.Kind, EliminatedCandidates: 1})
			}
			blocked[verdict.DemandWindowID] = defeats
		}
	}
	windows := append([]DemandWindow(nil), req.Problem.DemandWindows...)
	sort.Slice(windows, func(i, j int) bool { return windows[i].SignalID < windows[j].SignalID })
	explained := ScheduleExplanation{
		ProblemID:     req.Problem.ProblemID,
		ProblemDigest: req.Problem.CanonicalDigest,
		ResultDigest:  req.Result.CanonicalDigest,
		TotalScore:    req.Result.TotalScore,
	}
	for _, window := range windows {
		assignment, isAssigned := assigned[window.SignalID]
		_, isUnassigned := unassigned[window.SignalID]
		if isAssigned == isUnassigned {
			return ScheduleExplanation{}, explainReject("result.coverage", "INCOMPLETE", "window "+window.SignalID+" is neither assigned nor unassigned")
		}
		entry := WindowExplanation{
			DemandWindowID:         window.SignalID,
			BlockingKinds:          append([]KindDefeat(nil), blocked[window.SignalID]...),
			AlternativesEliminated: counts[window.SignalID],
			HardEliminated:         hard[window.SignalID],
		}
		if isAssigned {
			entry.AssignedCandidate = assignment.CandidateRef.String()
			entry.AssignmentScore = assignment.Score
			entry.PreferenceWeight = weights[entry.AssignedCandidate]
			entry.Cost = facts[entry.AssignedCandidate].Cost
			entry.AlternativesEliminated--
			entry.OutscoredAlternatives = entry.AlternativesEliminated - entry.HardEliminated
			entry.Reason = ReasonAssigned
		} else {
			entry.Reason = ReasonUnassigned
		}
		explained.Windows = append(explained.Windows, entry)
	}
	// The result must not name windows outside the sealed problem.
	for id := range assigned {
		found := false
		for _, entry := range explained.Windows {
			if entry.DemandWindowID == id {
				found = true
				break
			}
		}
		if !found {
			return ScheduleExplanation{}, explainReject("result.coverage", "MISMATCH", "assignment names unknown window "+id)
		}
	}
	explained.CanonicalDigest = explanationDigest(explained)
	return explained, nil
}

func explanationDigest(explained ScheduleExplanation) string {
	w := canonicalbytes.New("hcmnext.domains.schedopt.ScheduleExplanation", schemaVersion).
		String("problem_id", explained.ProblemID).String("problem_digest", explained.ProblemDigest).
		String("result_digest", explained.ResultDigest).Int("total_score", explained.TotalScore).
		Count("windows", len(explained.Windows))
	for _, window := range explained.Windows {
		w.String("window", window.DemandWindowID).String("assigned_candidate", window.AssignedCandidate).
			Int("score", window.AssignmentScore).Int("preference_weight", window.PreferenceWeight).
			Bool("has_cost", window.AssignedCandidate != "")
		if window.AssignedCandidate != "" {
			w.Value("cost", window.Cost)
		}
		w.Int("alternatives_eliminated", int64(window.AlternativesEliminated)).
			Int("hard_eliminated", int64(window.HardEliminated)).
			Int("outscored", int64(window.OutscoredAlternatives)).
			String("reason", window.Reason).Count("blocking", len(window.BlockingKinds))
		for _, defeat := range window.BlockingKinds {
			w.String("blocked_by", string(defeat.ConstraintKind)).Int("eliminated", int64(defeat.EliminatedCandidates))
		}
	}
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// Verify checks the seal and internal consistency of a previously computed
// explanation: every window traces its assignment or its conflict set, and
// the digest matches.
func (e ScheduleExplanation) Verify() error {
	if e.ProblemID == "" || e.ProblemDigest == "" || e.ResultDigest == "" || e.CanonicalDigest == "" {
		return explainReject("explanation.seal", "UNSEALED", "identity and digest are required")
	}
	if len(e.Windows) == 0 {
		return explainReject("explanation.windows", "EMPTY", "every demand window must be traced")
	}
	var total int64
	for i, window := range e.Windows {
		if window.DemandWindowID == "" || window.Reason == "" {
			return explainReject("explanation.windows", "INCOMPLETE", "window trace is missing its identity or reason")
		}
		if i > 0 && e.Windows[i-1].DemandWindowID >= window.DemandWindowID {
			return explainReject("explanation.windows", "UNORDERED", "windows are not canonical")
		}
		if window.AlternativesEliminated < 0 || window.HardEliminated < 0 || window.OutscoredAlternatives < 0 {
			return explainReject("explanation.windows", "INVALID", "eliminated counts must not be negative")
		}
		if window.AlternativesEliminated != window.HardEliminated+window.OutscoredAlternatives {
			return explainReject("explanation.windows", "MISMATCH", "eliminated counts do not reconcile")
		}
		if window.AssignedCandidate == "" {
			if window.Reason != ReasonUnassigned || window.AssignmentScore != 0 || window.PreferenceWeight != 0 {
				return explainReject("explanation.windows", "INVALID", "unassigned window carries assignment facts")
			}
			if window.OutscoredAlternatives != 0 {
				return explainReject("explanation.windows", "INVALID", "unassigned window has no winner to be outscored by")
			}
			if len(window.BlockingKinds) == 0 {
				return explainReject("explanation.windows", "INCOMPLETE", "unassigned window must carry its conflict set")
			}
		} else {
			if window.Reason != ReasonAssigned {
				return explainReject("explanation.windows", "INVALID", "assigned window carries the wrong reason")
			}
			total += window.AssignmentScore
		}
		if window.HardEliminated > 0 && len(window.BlockingKinds) == 0 {
			return explainReject("explanation.windows", "INCOMPLETE", "hard eliminations must name blocking kinds")
		}
		for _, defeat := range window.BlockingKinds {
			if !defeat.ConstraintKind.Valid() || defeat.EliminatedCandidates <= 0 {
				return explainReject("explanation.windows", "INVALID", "blocking trace is malformed")
			}
		}
	}
	if total != e.TotalScore {
		return explainReject("explanation.totals", "MISMATCH", "window scores do not reconcile")
	}
	if explanationDigest(e) != e.CanonicalDigest {
		return explainReject("explanation.seal", "MISMATCH", "canonical digest mismatch")
	}
	return nil
}

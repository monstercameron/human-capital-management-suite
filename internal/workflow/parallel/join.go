package parallel

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Join strategies: the closed vocabulary.
const (
	JoinAll         = "ALL"
	JoinAny         = "ANY"
	JoinQuorum      = "QUORUM"
	JoinRequiredSet = "REQUIRED_SET"
	JoinBestEffort  = "BEST_EFFORT"
)

// Join verdicts: the typed aggregate vocabulary.
const (
	JoinSucceeded = "SUCCEEDED"
	JoinPartial   = "PARTIAL"
	JoinFailed    = "FAILED"
	JoinUnknown   = "UNKNOWN"
)

var (
	// ErrJoinPlan reports an uncompilable join plan.
	ErrJoinPlan = errors.New("parallel: join plan is invalid")
	// ErrJoinMissing reports a missing mandatory branch.
	ErrJoinMissing = errors.New("parallel: mandatory branch is missing")
)

// JoinPlan pins the strategy and its version: strategy/version is part of
// the compiled plan, never a call-time surprise.
type JoinPlan struct {
	Strategy   string
	Version    string
	Quorum     int
	RequiredID []string
}

// JoinOutcome is the typed aggregate with degraded/unknown dimensions.
// Unknown branches are named, never coerced false.
type JoinOutcome struct {
	Strategy string
	Version  string
	Verdict  string
	Degraded bool
	Unknown  []string
	Counted  int
}

func (p JoinPlan) validate(total int) error {
	if strings.TrimSpace(p.Version) == "" {
		return fmt.Errorf("%w: strategy version is required", ErrJoinPlan)
	}
	switch p.Strategy {
	case JoinAll, JoinAny, JoinBestEffort:
	case JoinQuorum:
		if p.Quorum <= 0 || p.Quorum > total {
			return fmt.Errorf("%w: quorum %d is impossible over %d branches", ErrJoinPlan, p.Quorum, total)
		}
	case JoinRequiredSet:
		if len(p.RequiredID) == 0 {
			return fmt.Errorf("%w: required set is empty", ErrJoinPlan)
		}
		seen := make(map[string]bool, len(p.RequiredID))
		for _, id := range p.RequiredID {
			if strings.TrimSpace(id) == "" || seen[id] {
				return fmt.Errorf("%w: required branch identities must be unique and non-empty", ErrJoinPlan)
			}
			seen[id] = true
		}
	default:
		return fmt.Errorf("%w: unknown strategy %q", ErrJoinPlan, p.Strategy)
	}
	return nil
}

// Join aggregates branch results under the pinned plan. Missing mandatory
// branches and impossible quorums never report success; unknown outcomes
// surface as unknown dimensions.
func Join(plan JoinPlan, results []BranchResult) (JoinOutcome, error) {
	if len(results) == 0 {
		return JoinOutcome{}, fmt.Errorf("%w: no branch results to join", ErrJoinMissing)
	}
	if err := plan.validate(len(results)); err != nil {
		return JoinOutcome{}, err
	}
	byID := make(map[string]BranchResult, len(results))
	for _, result := range results {
		if _, exists := byID[result.BranchID]; strings.TrimSpace(result.BranchID) == "" || exists {
			return JoinOutcome{}, fmt.Errorf("%w: branch identities must be unique and non-empty", ErrJoinPlan)
		}
		switch result.Outcome {
		case OutcomeSucceeded, OutcomeFailed, OutcomeCancelled, OutcomeUnknown:
		default:
			return JoinOutcome{}, fmt.Errorf("%w: branch %s outcome %q is not joinable", ErrJoinPlan, result.BranchID, result.Outcome)
		}
		byID[result.BranchID] = result
	}
	outcome := JoinOutcome{Strategy: plan.Strategy, Version: plan.Version, Counted: len(results)}
	var succeeded, failed, cancelled, unknown int
	for _, result := range results {
		switch result.Outcome {
		case OutcomeSucceeded:
			succeeded++
		case OutcomeFailed:
			failed++
		case OutcomeCancelled:
			cancelled++
			outcome.Degraded = true
		case OutcomeUnknown:
			unknown++
			outcome.Unknown = append(outcome.Unknown, result.BranchID)
		}
	}
	if plan.Strategy == JoinRequiredSet {
		requiredFailed, requiredUnknown := false, false
		for _, id := range plan.RequiredID {
			result, ok := byID[id]
			if !ok {
				return JoinOutcome{}, fmt.Errorf("%w: required branch %s", ErrJoinMissing, id)
			}
			switch result.Outcome {
			case OutcomeUnknown:
				requiredUnknown = true
			case OutcomeFailed, OutcomeCancelled:
				requiredFailed = true
			}
		}
		// Check every required member before reducing. A known mandatory
		// failure dominates uncertainty, as in ALL, regardless of input order.
		if requiredFailed || requiredUnknown {
			outcome.Verdict = JoinUnknown
			if requiredFailed {
				outcome.Verdict = JoinFailed
			}
			sort.Strings(outcome.Unknown)
			return outcome, nil
		}
		outcome.Verdict = JoinSucceeded
		if failed+cancelled+unknown > 0 {
			outcome.Verdict = JoinPartial
			outcome.Degraded = outcome.Degraded || failed+cancelled > 0
		}
		sort.Strings(outcome.Unknown)
		return outcome, nil
	}
	clean := failed+cancelled+unknown == 0
	switch plan.Strategy {
	case JoinAll:
		switch {
		case failed > 0:
			outcome.Verdict = JoinFailed
		case unknown > 0:
			outcome.Verdict = JoinUnknown
		case cancelled > 0:
			outcome.Verdict = JoinPartial
		default:
			outcome.Verdict = JoinSucceeded
		}
	case JoinAny:
		switch {
		case clean && succeeded > 0:
			outcome.Verdict = JoinSucceeded
		case succeeded > 0:
			outcome.Verdict = JoinPartial
			outcome.Degraded = outcome.Degraded || failed > 0
		case failed > 0:
			outcome.Verdict = JoinFailed
		default:
			outcome.Verdict = JoinUnknown
		}
	case JoinQuorum:
		switch {
		case succeeded >= plan.Quorum && clean:
			outcome.Verdict = JoinSucceeded
		case succeeded >= plan.Quorum:
			outcome.Verdict = JoinPartial
			outcome.Degraded = outcome.Degraded || failed > 0
		case failed > 0:
			outcome.Verdict = JoinFailed
		default:
			outcome.Verdict = JoinUnknown
		}
	case JoinBestEffort:
		switch {
		case clean:
			outcome.Verdict = JoinSucceeded
		case succeeded > 0:
			outcome.Verdict = JoinPartial
			outcome.Degraded = outcome.Degraded || failed > 0
		case failed > 0:
			outcome.Verdict = JoinFailed
		default:
			outcome.Verdict = JoinUnknown
		}
	}
	sort.Strings(outcome.Unknown)
	return outcome, nil
}

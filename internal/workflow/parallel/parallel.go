package parallel

import (
	"context"
	"errors"
	"fmt"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"sort"
	"strings"
	"sync"
)

// Branch outcomes: the exact terminal vocabulary. UNKNOWN arrives only
// from branches the executor never observed: joins name it, never coerce it.
const (
	OutcomeSucceeded = "SUCCEEDED"
	OutcomeFailed    = "FAILED"
	OutcomeCancelled = "CANCELLED"
	OutcomeUnknown   = "UNKNOWN"
)

// Failure policies: the only defined propagation semantics.
const (
	FailFast   = "FAIL_FAST"
	CollectAll = "COLLECT_ALL"
)

var (
	// ErrUnbounded reports a branch count outside the declared bound.
	ErrUnbounded = errors.New("parallel: branch count is unbounded")
	// ErrWriteConflict reports overlapping branch write keys.
	ErrWriteConflict = errors.New("parallel: branches declare conflicting writes")
	// ErrBudgetExceeded reports a batch cost over budget.
	ErrBudgetExceeded = errors.New("parallel: branch cost exceeds budget")
	// ErrUndefinedPropagation reports a missing failure policy.
	ErrUndefinedPropagation = errors.New("parallel: failure propagation is undefined")
	// ErrUnadmitted reports a branch without admission.
	ErrUnadmitted = errors.New("parallel: branch is not admitted")
)

// Branch is one admitted concurrent unit with independent identity,
// idempotency key, declared writes and cost. Work observes ctx: under
// FAIL_FAST a sibling failure cancels it and it must report CANCELLED
// rather than manufacture an outcome.
type Branch struct {
	ID             string
	IdempotencyKey string
	Writes         []string
	Cost           int
	Admitted       bool
	Work           func(ctx context.Context) (string, error)
}

// BranchResult is the exact terminal result of one branch.
type BranchResult struct {
	BranchID       string
	IdempotencyKey string
	Outcome        string
	Detail         string
}

// Spec is one bounded parallel execution.
type Spec struct {
	Branches      []Branch
	MaxBranches   int
	Budget        int
	FailurePolicy string
}

// Report carries every branch result in branch order.
type Report struct {
	Results []BranchResult
	Policy  string
}

func validate(spec Spec) error {
	if spec.MaxBranches <= 0 {
		return fmt.Errorf("%w: positive MaxBranches is required", ErrUnbounded)
	}
	if len(spec.Branches) == 0 || len(spec.Branches) > spec.MaxBranches {
		return fmt.Errorf("%w: %d branches against a bound of %d", ErrUnbounded, len(spec.Branches), spec.MaxBranches)
	}
	if spec.FailurePolicy != FailFast && spec.FailurePolicy != CollectAll {
		return fmt.Errorf("%w: %q", ErrUndefinedPropagation, spec.FailurePolicy)
	}
	if spec.Budget < 0 {
		return fmt.Errorf("%w: negative budget", ErrBudgetExceeded)
	}
	seenID := make(map[string]bool, len(spec.Branches))
	seenKey := make(map[string]bool, len(spec.Branches))
	claimed := make(map[string]string, len(spec.Branches))
	remaining := spec.Budget
	for _, branch := range spec.Branches {
		if strings.TrimSpace(branch.ID) == "" || seenID[branch.ID] {
			return fmt.Errorf("%w: branch identities must be unique and non-empty", ErrUnbounded)
		}
		seenID[branch.ID] = true
		if strings.TrimSpace(branch.IdempotencyKey) == "" || seenKey[branch.IdempotencyKey] {
			return fmt.Errorf("%w: idempotency keys must be unique and non-empty", ErrUnbounded)
		}
		seenKey[branch.IdempotencyKey] = true
		if !branch.Admitted {
			return fmt.Errorf("%w: %s", ErrUnadmitted, branch.ID)
		}
		if branch.Work == nil {
			return fmt.Errorf("%w: %s carries no work", ErrUndefinedPropagation, branch.ID)
		}
		if branch.Cost < 0 {
			return fmt.Errorf("%w: negative branch cost", ErrBudgetExceeded)
		}
		if branch.Cost > remaining {
			return fmt.Errorf("%w: branch cost %d over remaining budget %d", ErrBudgetExceeded, branch.Cost, remaining)
		}
		remaining -= branch.Cost
		for _, key := range branch.Writes {
			if owner, dup := claimed[key]; dup {
				return fmt.Errorf("%w: key %q claimed by %s and %s", ErrWriteConflict, key, owner, branch.ID)
			}
			claimed[key] = branch.ID
		}
	}
	return nil
}

// Execute validates the bound and runs every admitted branch. Under
// FAIL_FAST the first failure cancels its siblings; under COLLECT_ALL
// every branch runs to its own terminal outcome.
func Execute(ctx context.Context, spec Spec) (ret0 Report, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.parallel.execute", spec)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := validate(spec); err != nil {
		return Report{}, err
	}
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([]BranchResult, len(spec.Branches))
	var wg sync.WaitGroup
	for i, branch := range spec.Branches {
		wg.Add(1)
		go func(i int, branch Branch) {
			defer wg.Done()
			branchCtx, branchOp := observe.Begin(run, "workflow.parallel.branch")
			detail, err := branch.Work(branchCtx)
			_ = observe.Done(branchOp, err)
			outcome := OutcomeSucceeded
			switch {
			case err != nil && spec.FailurePolicy == FailFast && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)):
				outcome = OutcomeCancelled
				detail = "cancelled by sibling failure"
			case err != nil:
				outcome = OutcomeFailed
				detail = err.Error()
				if spec.FailurePolicy == FailFast {
					cancel()
				}
			}
			results[i] = BranchResult{BranchID: branch.ID, IdempotencyKey: branch.IdempotencyKey, Outcome: outcome, Detail: detail}
		}(i, branch)
	}
	wg.Wait()
	ordered := append([]BranchResult(nil), results...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].BranchID < ordered[j].BranchID })
	return Report{Results: ordered, Policy: spec.FailurePolicy}, nil
}

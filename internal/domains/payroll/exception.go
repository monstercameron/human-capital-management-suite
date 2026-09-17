package payroll

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	// ErrInvalidResolution identifies a malformed exception resolution.
	ErrInvalidResolution = errors.New("payroll: invalid exception resolution")
	// ErrResolutionUnknown identifies a resolution aimed at no open exception.
	ErrResolutionUnknown = errors.New("payroll: exception resolution is unknown")
	// ErrExceptionsBlocking refuses a recalculation while blocking
	// exceptions remain open. Partial repairs never yield partial totals.
	ErrExceptionsBlocking = errors.New("payroll: payroll exceptions are blocking")
	// ErrResolveRejected is the typed PAYRUN-005 refusal boundary.
	ErrResolveRejected = errors.New("PAYRUN_005_REJECTED")
)

// ResolveError reports the offending field and reason without creating an
// authoritative side effect.
type ResolveError struct {
	Code   string
	Field  string
	Reason string
	Cause  error
}

func (e *ResolveError) Error() string {
	return fmt.Sprintf("%s: field=%s: %s", e.Code, e.Field, e.Reason)
}

// Is reports the typed PAYRUN-005 boundary and any wrapped cause.
func (e *ResolveError) Is(target error) bool {
	return target == ErrResolveRejected || target == e.Cause
}

// Unwrap returns the wrapped cause, if any.
func (e *ResolveError) Unwrap() error { return e.Cause }

func resolveRefusal(field, reason string, cause error) error {
	return &ResolveError{Code: ErrResolveRejected.Error(), Field: field, Reason: reason, Cause: cause}
}

// ResolutionAction is the closed repair vocabulary for one exception.
type ResolutionAction string

const (
	// ResolutionRemapCode retries a worker's unknown earning codes under a
	// ruled replacement code.
	ResolutionRemapCode ResolutionAction = "REMAP_CODE"
	// ResolutionBindBalance binds the missing balance snapshot and prices the
	// worker's retained lines.
	ResolutionBindBalance ResolutionAction = "BIND_BALANCE"
)

// Valid reports whether a is a declared resolution action.
func (a ResolutionAction) Valid() bool {
	return a == ResolutionRemapCode || a == ResolutionBindBalance
}

// ExceptionResolution repairs exactly one open blocking exception.
type ExceptionResolution struct {
	ExceptionID   string
	Action        ResolutionAction
	FromCode      string
	ToCode        string
	BalanceDigest string
}

func (r ExceptionResolution) validate() error {
	if strings.TrimSpace(r.ExceptionID) == "" {
		return resolveRefusal("exception_id", "exception id is required", ErrInvalidResolution)
	}
	if !r.Action.Valid() {
		return resolveRefusal("action", "resolution action is unknown", ErrInvalidResolution)
	}
	switch r.Action {
	case ResolutionRemapCode:
		if strings.TrimSpace(r.FromCode) == "" || strings.TrimSpace(r.ToCode) == "" {
			return resolveRefusal("codes", "remap requires both the offending and the replacement code", ErrInvalidResolution)
		}
		if r.FromCode == r.ToCode {
			return resolveRefusal("codes", "remap replacement must differ from the offending code", ErrInvalidResolution)
		}
		if strings.TrimSpace(r.BalanceDigest) != "" {
			return resolveRefusal("balance_digest", "remap carries no balance", ErrInvalidResolution)
		}
	case ResolutionBindBalance:
		if strings.TrimSpace(r.BalanceDigest) == "" {
			return resolveRefusal("balance_digest", "balance binding requires a balance digest", ErrInvalidResolution)
		}
		if strings.TrimSpace(r.FromCode) != "" || strings.TrimSpace(r.ToCode) != "" {
			return resolveRefusal("codes", "balance binding carries no codes", ErrInvalidResolution)
		}
	}
	return nil
}

// ResolveTrialExceptions repairs the named open exceptions and recalculates
// only the affected workers. Every prior attempt is preserved in history and
// linked by digest; unaffected workers are byte-identical to the prior
// revision. Any remaining blocking exception refuses the whole repair with a
// typed error and yields no recalculation. Nothing is mutated.
func ResolveTrialExceptions(calc TrialCalculation, resolutions []ExceptionResolution) (TrialCalculation, error) {
	if err := calc.Validate(); err != nil {
		return TrialCalculation{}, resolveRefusal("calculation", "prior calculation is invalid", err)
	}
	if len(resolutions) == 0 {
		return TrialCalculation{}, resolveRefusal("resolutions", "at least one resolution is required", ErrInvalidResolution)
	}
	open := map[string]TrialException{}
	for _, e := range calc.Exceptions {
		if e.Status == TrialExceptionOpen && e.Severity == TrialExceptionBlocking {
			open[e.ExceptionID] = e
		}
	}
	seen := map[string]bool{}
	for _, r := range resolutions {
		if err := r.validate(); err != nil {
			return TrialCalculation{}, err
		}
		if seen[r.ExceptionID] {
			return TrialCalculation{}, resolveRefusal("exception_id", "exception "+r.ExceptionID+" is resolved twice", ErrInvalidResolution)
		}
		seen[r.ExceptionID] = true
		exception, ok := open[r.ExceptionID]
		if !ok {
			return TrialCalculation{}, resolveRefusal("exception_id", "exception "+r.ExceptionID+" is not open", ErrResolutionUnknown)
		}
		switch r.Action {
		case ResolutionRemapCode:
			if exception.Code != TrialExceptionUnknownCode {
				return TrialCalculation{}, resolveRefusal("action", "exception "+r.ExceptionID+" carries no unknown code", ErrInvalidResolution)
			}
			known := trialKnownSet(calc.KnownCodes)
			if !known[r.ToCode] {
				return TrialCalculation{}, resolveRefusal("codes", "replacement code "+r.ToCode+" is not ruled", ErrInvalidResolution)
			}
			found := false
			for _, code := range strings.Split(exception.Detail, ",") {
				if code == r.FromCode {
					found = true
				}
			}
			if !found {
				return TrialCalculation{}, resolveRefusal("codes", "code "+r.FromCode+" is not unknown for this worker", ErrInvalidResolution)
			}
		case ResolutionBindBalance:
			if exception.Code != TrialExceptionUnknownBalance {
				return TrialCalculation{}, resolveRefusal("action", "exception "+r.ExceptionID+" carries no unknown balance", ErrInvalidResolution)
			}
		}
	}
	inputs := make([]TrialWorkerInput, len(calc.Inputs))
	for i, in := range calc.Inputs {
		inputs[i] = TrialWorkerInput{
			WorkerRef:     in.WorkerRef,
			BalanceDigest: in.BalanceDigest,
			Lines:         append([]TrialEarningLine(nil), in.Lines...),
		}
	}
	byRef := map[string]int{}
	for i := range inputs {
		byRef[inputs[i].WorkerRef] = i
	}
	resolvedIDs := make([]string, 0, len(resolutions))
	for _, r := range resolutions {
		exception := open[r.ExceptionID]
		i := byRef[exception.WorkerRef]
		switch r.Action {
		case ResolutionRemapCode:
			for j := range inputs[i].Lines {
				if inputs[i].Lines[j].Code == r.FromCode {
					inputs[i].Lines[j].Code = r.ToCode
				}
			}
		case ResolutionBindBalance:
			inputs[i].BalanceDigest = r.BalanceDigest
		}
		resolvedIDs = append(resolvedIDs, r.ExceptionID)
	}
	sort.Strings(resolvedIDs)
	exceptions := append([]TrialException(nil), calc.Exceptions...)
	for i := range exceptions {
		if seen[exceptions[i].ExceptionID] {
			exceptions[i].Status = TrialExceptionResolved
		}
	}
	known := trialKnownSet(calc.KnownCodes)
	workers, fresh, runGross, runDeduction, runTax, runNet, err := trialCore(calc.RunID, calc.Rules, known, inputs)
	if err != nil {
		return TrialCalculation{}, resolveRefusal("calculation", "recalculation failed", err)
	}
	if len(fresh) > 0 {
		return TrialCalculation{}, resolveRefusal("exceptions", "blocking exceptions remain open; repair is incomplete", ErrExceptionsBlocking)
	}
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].WorkerRef < inputs[j].WorkerRef })
	next := TrialCalculation{
		CalculationID: calc.CalculationID, CalculationKey: calc.CalculationKey,
		Tenant: calc.Tenant, RunID: calc.RunID, PeriodDigest: calc.PeriodDigest,
		Rules:            calc.Rules,
		Revision:         calc.Revision + 1,
		SupersedesDigest: calc.CalculationDigest,
		Workers:          workers,
		Inputs:           inputs,
		KnownCodes:       append([]string(nil), calc.KnownCodes...),
		RunGross:         runGross, RunDeduction: runDeduction, RunTax: runTax, RunNet: runNet,
		Exceptions: exceptions,
		Attempts: append(append([]RecalculationAttempt(nil), calc.Attempts...), RecalculationAttempt{
			Attempt:            calc.Revision + 1,
			SupersedesDigest:   calc.CalculationDigest,
			ResolvedExceptions: resolvedIDs,
		}),
	}
	next.CalculationDigest = next.computedDigest()
	// The attempt digest is metadata bound by validation, not by the digest
	// body, so sealing it cannot invalidate the digest it seals.
	next.Attempts[len(next.Attempts)-1].CalculationDigest = next.CalculationDigest
	if err := next.Validate(); err != nil {
		return TrialCalculation{}, err
	}
	return next, nil
}

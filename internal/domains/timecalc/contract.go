// Package timecalc classifies worked time into regular, overtime and
// double-time buckets, computes the FLSA regular rate by the configured
// method and prices the schedule-dependent premiums (WTIME-013, WTIME-014).
//
// Every threshold, factor, floor and premium rule is data carried by a
// RuleSet supplied by a rule pack; nothing in this package is specific to a
// state. Durations are integer seconds between instants, so 23- and 25-hour
// DST days need no special case. Money is integer ten-thousandths of the
// currency unit and every rounding is explicit and declared by the rule set.
//
// The package is pure: no clock, storage, network or package-level mutable
// state. Its output feeds labor.CalculateWages (LABOR-003), which prices
// pre-classified hours; WageHours converts a result into that shape.
package timecalc

import (
	"errors"
	"fmt"
)

const contractVersion = 1

// Version is the engine contract version (ARCH-GO-009). It is part of every
// result digest, so a behaviour change must bump it.
func Version() int { return contractVersion }

// Explain is the package-level audit explanation entry point.
func Explain(r Result) string { return r.Explain() }

// Sentinel errors. Callers match with errors.Is; Rejection carries the field.
var (
	// ErrInvalidRuleSet reports a rule set that cannot be evaluated.
	ErrInvalidRuleSet = errors.New("TIMECALC_RULESET_INVALID")
	// ErrInvalidRequest reports a request that cannot be evaluated.
	ErrInvalidRequest = errors.New("TIMECALC_REQUEST_INVALID")
	// ErrMethodNotAllowed reports an overtime method the rule set does not permit.
	ErrMethodNotAllowed = errors.New("TIMECALC_METHOD_NOT_ALLOWED")
	// ErrOverlap reports two worked intervals covering the same instant; paying
	// both would double count hours.
	ErrOverlap = errors.New("TIMECALC_INTERVAL_OVERLAP")
	// ErrOutsidePeriod reports an interval outside the calculation period.
	ErrOutsidePeriod = errors.New("TIMECALC_OUTSIDE_PERIOD")
	// ErrUnknownDifferential reports an interval tag the rule set does not define.
	ErrUnknownDifferential = errors.New("TIMECALC_UNKNOWN_DIFFERENTIAL")
	// ErrTreatmentUndeclared reports an earning with no regular-rate inclusion
	// decision or no cited basis. It is never defaulted.
	ErrTreatmentUndeclared = errors.New("TIMECALC_TREATMENT_UNDECLARED")
	// ErrRuleDisabled reports premium facts supplied for a rule the rule set
	// does not enable; unknown is never priced as zero.
	ErrRuleDisabled = errors.New("TIMECALC_RULE_DISABLED")
	// ErrBelowMinimumWage reports a fluctuating-workweek regular rate below the
	// minimum wage, which voids the method (29 CFR 778.114(a)).
	ErrBelowMinimumWage = errors.New("TIMECALC_BELOW_MINIMUM_WAGE")
	// ErrLessFavorable reports a CBA override that would reduce a statutory
	// entitlement.
	ErrLessFavorable = errors.New("TIMECALC_OVERRIDE_LESS_FAVORABLE")
	// ErrArithmetic reports overflow or an inexact result under EXACT_REQUIRED.
	ErrArithmetic = errors.New("TIMECALC_ARITHMETIC")
	// ErrResultTampered reports a result whose sums or digest do not verify.
	ErrResultTampered = errors.New("TIMECALC_RESULT_TAMPERED")
)

// Rejection names the field and reason behind a refusal. Unwrap yields the
// sentinel.
type Rejection struct {
	Field  string
	Reason string
	Err    error
}

func (r *Rejection) Error() string {
	return fmt.Sprintf("%v: %s: %s", r.Err, r.Field, r.Reason)
}

func (r *Rejection) Unwrap() error { return r.Err }

func reject(err error, field, format string, args ...any) error {
	return &Rejection{Field: field, Reason: fmt.Sprintf(format, args...), Err: err}
}

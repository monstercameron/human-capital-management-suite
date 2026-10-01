package punchpolicy

import (
	"fmt"
	"time"
)

// Interval is a half-open [Start, End) span of clock time.
type Interval struct {
	Start, End time.Time
}

func (i Interval) validate() error {
	if i.Start.IsZero() || i.End.IsZero() {
		return fmt.Errorf("%w: interval requires a start and an end", ErrInvalidInterval)
	}
	if !i.End.After(i.Start) {
		return fmt.Errorf("%w: interval end must be after its start", ErrInvalidInterval)
	}
	return nil
}

// Duration is End minus Start.
func (i Interval) Duration() time.Duration { return i.End.Sub(i.Start) }

// Shift is the published (or not) schedule a raw interval is evaluated
// against. Published false is itself meaningful: it is what makes
// CheckClockIn route to the override path under Lockout.RequiresPublishedShift.
type Shift struct {
	ID        string
	Published bool
	Interval  Interval
}

// AttestationKind is the closed vocabulary of attestation signals Evaluate
// reads. It is deliberately narrow: Evaluate never reads the full TCLOCK-010
// answer set, only the one signal AutoDeduct.WaivedByAttestation names.
type AttestationKind string

// MealTakenAttestation is the worker's own statement about whether a
// deductible meal break was actually taken.
const MealTakenAttestation AttestationKind = "MEAL_TAKEN"

// Attestation is one signed worker statement bearing on evaluation.
type Attestation struct {
	Kind  AttestationKind
	Taken bool
}

// GraceStatus describes how an evaluated raw interval relates to its shift
// boundaries. Grace is an evaluation result, not a rewrite of the raw
// observation: callers can distinguish a compliant interval from one that is
// outside the configured early or late window while retaining both punch
// timestamps unchanged.
type GraceStatus string

const (
	GraceWithinWindow GraceStatus = "WITHIN_WINDOW"
	GraceEarly        GraceStatus = "EARLY_OUTSIDE_WINDOW"
	GraceLate         GraceStatus = "LATE_OUTSIDE_WINDOW"
	GraceEarlyAndLate GraceStatus = "EARLY_AND_LATE_OUTSIDE_WINDOW"
)

// GraceEvaluation is the policy-pinned grace result for one raw interval.
// EarlyBy and LateBy are zero when the corresponding boundary is not crossed.
type GraceEvaluation struct {
	PolicyID      string
	PolicyVersion int
	Status        GraceStatus
	EarlyBy       time.Duration
	LateBy        time.Duration
}

// EvaluatedInterval is the policy-pinned result of evaluating one raw punch
// interval. RawStart and RawEnd are copied through unchanged: nothing in
// this package, or its caller, may rewrite the stored punch. RoundedStart
// and RoundedEnd are the interval rounding produces; AutoDeductMinutes and
// PaidMinutes fold in the auto-deduct rule. Trace records each step in
// order for ARCH-GO-009 explainability.
type EvaluatedInterval struct {
	PolicyID          string
	PolicyVersion     int
	RawStart, RawEnd  time.Time
	RoundedStart      time.Time
	RoundedEnd        time.Time
	Grace             GraceEvaluation
	AutoDeductMinutes int
	PaidMinutes       int
	Trace             []string
}

// EvaluateGrace applies the policy's early and late grace windows to the raw
// interval. It reports an exception-shaped status rather than rejecting the
// punch: the raw observation remains authoritative and downstream attendance
// evaluation decides how an outside-window result is routed.
func EvaluateGrace(policy Policy, raw Interval, shift Shift) (GraceEvaluation, error) {
	if err := policy.Validate(); err != nil {
		return GraceEvaluation{}, err
	}
	if err := raw.validate(); err != nil {
		return GraceEvaluation{}, err
	}
	if err := shift.Interval.validate(); err != nil {
		return GraceEvaluation{}, err
	}

	out := GraceEvaluation{PolicyID: policy.ID, PolicyVersion: policy.Version, Status: GraceWithinWindow}
	if raw.Start.Before(shift.Interval.Start) {
		out.EarlyBy = shift.Interval.Start.Sub(raw.Start)
		if out.EarlyBy > time.Duration(policy.Grace.EarlyMinutes)*time.Minute {
			out.Status = GraceEarly
		}
	}
	if raw.End.After(shift.Interval.End) {
		out.LateBy = raw.End.Sub(shift.Interval.End)
		if out.LateBy > time.Duration(policy.Grace.LateMinutes)*time.Minute {
			if out.Status == GraceEarly {
				out.Status = GraceEarlyAndLate
			} else {
				out.Status = GraceLate
			}
		}
	}
	return out, nil
}

// Evaluate applies policy to one raw punch interval against its shift and
// the worker's attestations. Raw punches are never rewritten: rounding and
// auto-deduction only ever appear in the returned EvaluatedInterval, which
// pins the exact policy identity that produced it.
func Evaluate(policy Policy, raw Interval, shift Shift, attestations []Attestation) (EvaluatedInterval, error) {
	if err := policy.Validate(); err != nil {
		return EvaluatedInterval{}, err
	}
	if err := raw.validate(); err != nil {
		return EvaluatedInterval{}, err
	}
	if err := shift.Interval.validate(); err != nil {
		return EvaluatedInterval{}, err
	}
	out := EvaluatedInterval{
		PolicyID:      policy.ID,
		PolicyVersion: policy.Version,
		RawStart:      raw.Start,
		RawEnd:        raw.End,
	}
	grace, err := EvaluateGrace(policy, raw, shift)
	if err != nil {
		return EvaluatedInterval{}, err
	}
	out.Grace = grace

	increment := time.Duration(policy.Rounding.IncrementMinutes) * time.Minute
	out.RoundedStart = roundTime(raw.Start, increment, policy.Rounding.Mode)
	out.RoundedEnd = roundTime(raw.End, increment, policy.Rounding.Mode)
	if increment > 0 {
		out.Trace = append(out.Trace, fmt.Sprintf("rounded start %s -> %s and end %s -> %s at %d minute %s increments",
			raw.Start.Format(time.RFC3339), out.RoundedStart.Format(time.RFC3339),
			raw.End.Format(time.RFC3339), out.RoundedEnd.Format(time.RFC3339),
			policy.Rounding.IncrementMinutes, policy.Rounding.Mode))
	} else {
		out.Trace = append(out.Trace, "no rounding configured: evaluated interval equals the raw interval")
	}
	if grace.Status == GraceWithinWindow {
		out.Trace = append(out.Trace, "raw interval is within the configured early and late grace windows")
	} else {
		out.Trace = append(out.Trace, fmt.Sprintf("raw interval is outside the configured grace window: %s", grace.Status))
	}

	if !out.RoundedEnd.After(out.RoundedStart) {
		return EvaluatedInterval{}, fmt.Errorf("%w: rounding collapsed the interval to zero or negative duration", ErrInvalidInterval)
	}
	workedMinutes := int(out.RoundedEnd.Sub(out.RoundedStart) / time.Minute)

	deductMinutes := 0
	if policy.AutoDeduct.DeductMinutes > 0 && workedMinutes >= policy.AutoDeduct.AfterMinutes {
		waived := false
		if policy.AutoDeduct.WaivedByAttestation {
			for _, a := range attestations {
				if a.Kind == MealTakenAttestation && !a.Taken {
					waived = true
					break
				}
			}
		}
		if waived {
			out.Trace = append(out.Trace, fmt.Sprintf("auto-deduct of %d minutes waived: worker attested the meal break was not taken", policy.AutoDeduct.DeductMinutes))
		} else {
			deductMinutes = policy.AutoDeduct.DeductMinutes
			out.Trace = append(out.Trace, fmt.Sprintf("auto-deducted %d minutes after %d worked minutes exceeded the %d minute threshold", deductMinutes, workedMinutes, policy.AutoDeduct.AfterMinutes))
		}
	}
	out.AutoDeductMinutes = deductMinutes
	out.PaidMinutes = workedMinutes - deductMinutes
	if out.PaidMinutes < 0 {
		out.PaidMinutes = 0
	}
	return out, nil
}

// roundTime rounds t to the nearest, next, or previous multiple of increment
// measured from the fixed absolute zero time, exactly as time.Time.Round and
// time.Time.Truncate define it. Using the same fixed origin for every punch,
// regardless of which edge of the interval it is, is what makes the
// direction neutral in expectation: the fractional offset within an
// increment window is uniform for a uniformly distributed punch minute, so
// the expected adjustment at the start of an interval equals the expected
// adjustment at its end and the two cancel in the evaluated duration.
func roundTime(t time.Time, increment time.Duration, mode RoundingDirection) time.Time {
	if increment <= 0 {
		return t
	}
	switch mode {
	case RoundDown:
		return t.Truncate(increment)
	case RoundUp:
		floor := t.Truncate(increment)
		if floor.Equal(t) {
			return t
		}
		return floor.Add(increment)
	default: // RoundNearest
		return t.Round(increment)
	}
}

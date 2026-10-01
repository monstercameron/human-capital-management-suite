package contractortime

import (
	"strconv"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// SignalKind names a classification signal relevant to worker-classification
// review (the ABC test, IR35, the EU Platform Work Directive). These signals
// are reported only; nothing in this package, and nothing this package calls,
// enforces on them. Enforcement, if any, is LEGAL-005's decision.
type SignalKind string

const (
	SignalHoursPattern SignalKind = "HOURS_PATTERN" // regular, schedule-like weekly hours
	SignalExclusivity  SignalKind = "EXCLUSIVITY"   // fraction of the worker's total time on one engagement
	SignalDuration     SignalKind = "DURATION"      // engagement running long past a fixed-term expectation
)

// ClassificationThresholds are rule-pack data: the boundaries at which a
// measured signal is worth reporting. There is no hard-coded threshold in
// this package; a caller with no thresholds gets no signals.
type ClassificationThresholds struct {
	// WeeklyHoursFullTime is the weekly-hours boundary above which a pattern
	// looks like full-time employment rather than project work.
	WeeklyHoursFullTime values.Decimal
	// ExclusivityHigh is the fraction-of-total-time boundary (1.0 = 100%)
	// above which the worker looks economically dependent on one engagement.
	ExclusivityHigh values.Percentage
	// DurationLongDays is the engagement-length boundary, in days, above which
	// an ostensibly project-based engagement looks like an ongoing role.
	DurationLongDays int
}

// ClassificationInputs are the measured facts a caller supplies about one
// engagement's observed pattern. This package computes no averages or
// fractions itself; it only compares supplied measurements to supplied
// thresholds.
type ClassificationInputs struct {
	WeeklyHoursAverage     values.Decimal
	ExclusivityFraction    values.Percentage
	EngagementDurationDays int
	Thresholds             ClassificationThresholds
}

// ClassificationSignal is one reported observation. Value carries the
// measured figure that triggered the report, for audit.
type ClassificationSignal struct {
	Kind   SignalKind
	Detail string
	Value  string
}

// ReportSignals compares the supplied measurements to the supplied
// thresholds and reports which classification signals are present. It never
// rejects a build and never blocks an invoice; a caller wires the result to
// LEGAL-005's review, never to BuildInvoice.
func ReportSignals(in ClassificationInputs) []ClassificationSignal {
	var out []ClassificationSignal

	if in.WeeklyHoursAverage.Validate() == nil && in.Thresholds.WeeklyHoursFullTime.Validate() == nil {
		if in.WeeklyHoursAverage.Cmp(in.Thresholds.WeeklyHoursFullTime) >= 0 {
			out = append(out, ClassificationSignal{
				Kind:   SignalHoursPattern,
				Detail: "weekly hours average meets or exceeds the full-time threshold",
				Value:  in.WeeklyHoursAverage.String(),
			})
		}
	}

	if in.ExclusivityFraction.Validate() == nil && in.Thresholds.ExclusivityHigh.Validate() == nil {
		if in.ExclusivityFraction.Fraction().Cmp(in.Thresholds.ExclusivityHigh.Fraction()) >= 0 {
			out = append(out, ClassificationSignal{
				Kind:   SignalExclusivity,
				Detail: "worker's time on this engagement meets or exceeds the exclusivity threshold",
				Value:  in.ExclusivityFraction.String(),
			})
		}
	}

	if in.Thresholds.DurationLongDays > 0 && in.EngagementDurationDays >= in.Thresholds.DurationLongDays {
		out = append(out, ClassificationSignal{
			Kind:   SignalDuration,
			Detail: "engagement duration meets or exceeds the long-running threshold",
			Value:  strconv.Itoa(in.EngagementDurationDays),
		})
	}

	return out
}

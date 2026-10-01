// Package rules is the time-profile rules façade for WTIME-013 and WTIME-014.
//
// Rule-pack data and the pure calculation engine live in the sibling
// timecalc domain package. This package gives callers that resolve a
// timeprofile a stable, profile-oriented import path while preserving the
// engine's exact types, validation, traces and result digest.
package rules

import (
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timecalc"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Calculation units and input shapes.
type (
	Seconds             = timecalc.Seconds
	Money               = timecalc.Money
	Factor              = timecalc.Factor
	WorkKind            = timecalc.WorkKind
	Interval            = timecalc.Interval
	Workweek            = timecalc.Workweek
	Date                = timecalc.Date
	EarningItem         = timecalc.EarningItem
	ScheduledDay        = timecalc.ScheduledDay
	AlternativeSchedule = timecalc.AlternativeSchedule
	Restrictions        = timecalc.Restrictions
	OnCallPeriod        = timecalc.OnCallPeriod
	ScheduledShift      = timecalc.ScheduledShift
	Request             = timecalc.Request
)

const (
	Hour = timecalc.Hour
	One  = timecalc.One

	KindProductive    = timecalc.KindProductive
	KindNonproductive = timecalc.KindNonproductive
	KindRest          = timecalc.KindRest
)

// Classification categories, reasons, rule-pack parameters and overrides.
type (
	Category         = timecalc.Category
	Reason           = timecalc.Reason
	DailyRule        = timecalc.DailyRule
	PeriodRule       = timecalc.PeriodRule
	SeventhDayRule   = timecalc.SeventhDayRule
	AlternativeRule  = timecalc.AlternativeRule
	PieceRestRule    = timecalc.PieceRestRule
	CompTimeRule     = timecalc.CompTimeRule
	Inclusion        = timecalc.Inclusion
	Treatment        = timecalc.Treatment
	Window           = timecalc.Window
	DifferentialRule = timecalc.DifferentialRule
	OnCallRule       = timecalc.OnCallRule
	RateBasis        = timecalc.RateBasis
	ReportingRule    = timecalc.ReportingRule
	CallBackRule     = timecalc.CallBackRule
	SplitShiftRule   = timecalc.SplitShiftRule
	RuleSet          = timecalc.RuleSet
	Override         = timecalc.Override
)

const (
	Regular    = timecalc.Regular
	Overtime   = timecalc.Overtime
	DoubleTime = timecalc.DoubleTime

	ReasonNone        = timecalc.ReasonNone
	ReasonDaily       = timecalc.ReasonDaily
	ReasonPeriod      = timecalc.ReasonPeriod
	ReasonSeventhDay  = timecalc.ReasonSeventhDay
	ReasonAlternative = timecalc.ReasonAlternative

	Include = timecalc.Include
	Exclude = timecalc.Exclude

	BasisRegularRate = timecalc.BasisRegularRate
	BasisMinimumWage = timecalc.BasisMinimumWage
	BasisShiftRate   = timecalc.BasisShiftRate
)

// Pay result and audit shapes.
type (
	LineKind       = timecalc.LineKind
	PayLine        = timecalc.PayLine
	Buckets        = timecalc.Buckets
	DayResult      = timecalc.DayResult
	PeriodResult   = timecalc.PeriodResult
	CompTimeResult = timecalc.CompTimeResult
	TraceEntry     = timecalc.TraceEntry
	Result         = timecalc.Result
	WageHours      = timecalc.WageHours
	Rejection      = timecalc.Rejection
)

const (
	LineStraightTime  = timecalc.LineStraightTime
	LineSalary        = timecalc.LineSalary
	LinePiece         = timecalc.LinePiece
	LineNonproductive = timecalc.LineNonproductive
	LineRest          = timecalc.LineRest
	LineDifferential  = timecalc.LineDifferential
	LineEarning       = timecalc.LineEarning
	LineStandby       = timecalc.LineStandby
	LineSplitShift    = timecalc.LineSplitShift
	LineOvertime      = timecalc.LineOvertime
	LineDoubleTime    = timecalc.LineDoubleTime
	LineReporting     = timecalc.LineReporting
	LineCallBack      = timecalc.LineCallBack
)

var (
	ErrInvalidRuleSet      = timecalc.ErrInvalidRuleSet
	ErrInvalidRequest      = timecalc.ErrInvalidRequest
	ErrMethodNotAllowed    = timecalc.ErrMethodNotAllowed
	ErrOverlap             = timecalc.ErrOverlap
	ErrOutsidePeriod       = timecalc.ErrOutsidePeriod
	ErrUnknownDifferential = timecalc.ErrUnknownDifferential
	ErrTreatmentUndeclared = timecalc.ErrTreatmentUndeclared
	ErrRuleDisabled        = timecalc.ErrRuleDisabled
	ErrBelowMinimumWage    = timecalc.ErrBelowMinimumWage
	ErrLessFavorable       = timecalc.ErrLessFavorable
	ErrArithmetic          = timecalc.ErrArithmetic
	ErrResultTampered      = timecalc.ErrResultTampered
)

// Calculate classifies intervals, computes the regular rate and applies the
// configured overtime and schedule-dependent premium rules.
func Calculate(req Request, ruleSet RuleSet) (Result, error) {
	return timecalc.Calculate(req, ruleSet)
}

// Explain renders the audit summary for a calculation result.
func Explain(result Result) string { return timecalc.Explain(result) }

// Version returns the calculation contract version.
func Version() int { return timecalc.Version() }

// ParseMoney parses a non-negative fixed-point money amount.
func ParseMoney(s string) (Money, error) { return timecalc.ParseMoney(s) }

// ToDecimal converts a money amount to a kernel decimal.
func ToDecimal(m Money, scale int32, mode values.RoundingMode) (values.Decimal, error) {
	return timecalc.ToDecimal(m, scale, mode)
}

// HoursDecimal converts seconds to a kernel decimal hour value.
func HoursDecimal(s Seconds, scale int32, mode values.RoundingMode) (values.Decimal, error) {
	return timecalc.HoursDecimal(s, scale, mode)
}

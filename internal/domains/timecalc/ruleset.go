package timecalc

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DailyRule is a daily overtime band. A zero threshold disables that band.
type DailyRule struct {
	OTAfter Seconds // hours past this in a workday are overtime
	DTAfter Seconds // hours past this in a workday are double time
}

// PeriodRule is the overtime period: 7 days for the FLSA workweek, 14 for
// 8/80 (29 CFR 778.601), 7 to 28 for a 7(k) work period. The regular rate
// is computed per period. A zero OTAfter disables period overtime.
type PeriodRule struct {
	Days    int
	OTAfter Seconds
}

// SeventhDayRule prices the seventh consecutive workday of a workweek: the
// first OTUpTo at the overtime factor, the rest at the double-time factor.
type SeventhDayRule struct {
	Enabled bool
	OTUpTo  Seconds
}

// AlternativeRule governs an alternative workweek schedule: on a scheduled
// day overtime starts after the scheduled hours and double time after
// DTAfter; an unscheduled day uses UnscheduledDay.
type AlternativeRule struct {
	Allowed        bool
	DTAfter        Seconds
	UnscheduledDay DailyRule
}

// PieceRestRule is separate pay for piece-rate workers' rest and recovery
// and other nonproductive time (California Labor Code 226.2).
type PieceRestRule struct {
	Enabled            bool
	RestFloor          Money // rest paid at max(average hourly rate, RestFloor)
	NonproductiveFloor Money // nonproductive paid at max(interval rate, floor)
}

// CompTimeRule is FLSA 7(o) compensatory time in lieu of cash overtime.
type CompTimeRule struct {
	Enabled       bool
	AccrualFactor Factor  // comp hours earned per overtime hour
	Cap           Seconds // maximum balance (240 or 480 hours)
}

// Inclusion is a declared regular-rate decision for a payment.
type Inclusion string

// Inclusion decisions.
const (
	Include Inclusion = "INCLUDE"
	Exclude Inclusion = "EXCLUDE"
)

// Treatment is a regular-rate decision with its cited basis (29 CFR
// 778.200-224 or the state equivalent). Both are required.
type Treatment struct {
	Inclusion Inclusion
	Basis     string
}

func (t Treatment) validate(field string) error {
	if t.Inclusion != Include && t.Inclusion != Exclude {
		return reject(ErrTreatmentUndeclared, field, "inclusion %q is not INCLUDE or EXCLUDE", t.Inclusion)
	}
	if strings.TrimSpace(t.Basis) == "" {
		return reject(ErrTreatmentUndeclared, field, "a cited basis is required")
	}
	return nil
}

// Window is a local time-of-day range in minutes after midnight. An end at
// or before the start wraps past midnight (22:00-06:00).
type Window struct {
	StartMinute, EndMinute int
}

// DifferentialRule is a shift or location differential selected by an
// interval tag: a flat PerHour amount or a Percent of the interval rate,
// optionally limited to a local-time Window.
type DifferentialRule struct {
	Tag       string
	PerHour   Money
	Percent   Factor
	Window    *Window
	Treatment Treatment
}

// OnCallRule classifies on-call time under 29 CFR 785.15-17. Any enabled
// criterion that matches makes the period engaged to wait (hours worked);
// otherwise it is waiting to be engaged and earns only StandbyPerHour.
type OnCallRule struct {
	Enabled                bool
	OnPremisesEngaged      bool
	EngagedResponseMinutes int // response required within this many minutes
	EngagedExpectedCalls   int // this many or more expected calls
	StandbyPerHour         Money
	Standby                Treatment
}

// RateBasis names the rate a minimum-pay guarantee is priced at.
type RateBasis string

// Rate bases.
const (
	BasisRegularRate RateBasis = "REGULAR_RATE"
	BasisMinimumWage RateBasis = "MINIMUM_WAGE"
	BasisShiftRate   RateBasis = "SHIFT_RATE"
)

func (b RateBasis) valid() bool {
	return b == BasisRegularRate || b == BasisMinimumWage || b == BasisShiftRate
}

// ReportingRule is reporting-time pay. The guarantee is Guarantee x the
// scheduled shift, clamped to [Min, Max] and optionally capped at the
// scheduled length. It applies when worked time is below Trigger x the
// scheduled shift, or below the guarantee when TriggerDen is zero.
type ReportingRule struct {
	Enabled                    bool
	TriggerNum, TriggerDen     int64
	GuaranteeNum, GuaranteeDen int64
	Min, Max                   Seconds
	CapAtScheduled             bool
	Basis                      RateBasis
}

// CallBackRule is a minimum-pay guarantee for a call back to work.
type CallBackRule struct {
	Enabled bool
	Minimum Seconds
	Basis   RateBasis
}

// SplitShiftRule pays PremiumSeconds at the minimum wage for a workday
// interrupted by an unpaid gap longer than GapMoreThan, reduced by the
// day's earnings above the minimum wage when OffsetByExcess is set.
type SplitShiftRule struct {
	Enabled        bool
	GapMoreThan    Seconds
	PremiumSeconds Seconds
	OffsetByExcess bool
	Treatment      Treatment
}

// RuleSet is one jurisdiction's overtime and premium parameters, as
// supplied by a rule pack.
type RuleSet struct {
	ID, Version, Jurisdiction, Citation string

	Methods          []timeprofile.OvertimeMethod
	Daily            DailyRule
	Period           PeriodRule
	SeventhDay       SeventhDayRule
	Alternative      AlternativeRule
	OvertimeFactor   Factor
	DoubleTimeFactor Factor
	RateDecimals     int
	PayDecimals      int
	Rounding         values.RoundingMode
	MinimumWage      Money

	PieceRest     PieceRestRule
	CompTime      CompTimeRule
	Differentials []DifferentialRule
	OnCall        OnCallRule
	ReportingTime ReportingRule
	CallBack      CallBackRule
	SplitShift    SplitShiftRule
}

func (r RuleSet) allows(m timeprofile.OvertimeMethod) bool {
	for _, have := range r.Methods {
		if have == m {
			return true
		}
	}
	return false
}

func (r RuleSet) differential(tag string) (DifferentialRule, bool) {
	for _, d := range r.Differentials {
		if d.Tag == tag {
			return d, true
		}
	}
	return DifferentialRule{}, false
}

// Validate checks the rule set is complete and internally consistent.
func (r RuleSet) Validate() error {
	bad := func(field, format string, args ...any) error {
		return reject(ErrInvalidRuleSet, field, format, args...)
	}
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Version) == "" || strings.TrimSpace(r.Jurisdiction) == "" {
		return bad("id", "id, version and jurisdiction are required")
	}
	if !r.Rounding.Valid() {
		return bad("rounding", "rounding mode must be declared")
	}
	if r.RateDecimals < 0 || r.RateDecimals > moneyScale || r.PayDecimals < 0 || r.PayDecimals > moneyScale {
		return bad("decimals", "rate and pay decimals must be 0..4")
	}
	if r.Period.Days < 1 || r.Period.Days > 28 || r.Period.OTAfter < 0 {
		return bad("period", "period must be 1..28 days with a non-negative threshold")
	}
	if err := r.Daily.validate("daily"); err != nil {
		return err
	}
	if r.OvertimeFactor < One || r.DoubleTimeFactor < r.OvertimeFactor {
		return bad("factors", "overtime factor must be >= 1 and double time >= overtime")
	}
	if r.SeventhDay.Enabled && (r.Period.Days%7 != 0 || r.SeventhDay.OTUpTo <= 0) {
		return bad("seventh_day", "needs whole workweeks and a positive overtime band")
	}
	if r.Alternative.Allowed {
		if r.Alternative.DTAfter <= 0 {
			return bad("alternative", "double-time threshold is required")
		}
		if err := r.Alternative.UnscheduledDay.validate("alternative.unscheduled_day"); err != nil {
			return err
		}
	}
	if r.MinimumWage < 0 {
		return bad("minimum_wage", "cannot be negative")
	}
	if r.CompTime.Enabled && (r.CompTime.AccrualFactor <= 0 || r.CompTime.Cap <= 0) {
		return bad("comp_time", "accrual factor and cap are required")
	}
	if err := r.validateMethods(); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, d := range r.Differentials {
		if strings.TrimSpace(d.Tag) == "" || seen[d.Tag] {
			return bad("differentials", "tag %q is empty or repeated", d.Tag)
		}
		seen[d.Tag] = true
		if (d.PerHour > 0) == (d.Percent > 0) || d.PerHour < 0 || d.Percent < 0 {
			return bad("differentials."+d.Tag, "exactly one of per-hour or percent is required")
		}
		if w := d.Window; w != nil && (w.StartMinute < 0 || w.StartMinute >= 1440 || w.EndMinute < 0 || w.EndMinute >= 1440 || w.StartMinute == w.EndMinute) {
			return bad("differentials."+d.Tag, "window minutes must be distinct and within the day")
		}
		if err := d.Treatment.validate("differentials." + d.Tag); err != nil {
			return err
		}
	}
	return r.validatePremiums()
}

func (d DailyRule) validate(field string) error {
	if d.OTAfter < 0 || d.DTAfter < 0 || (d.OTAfter > 0 && d.DTAfter > 0 && d.DTAfter <= d.OTAfter) {
		return reject(ErrInvalidRuleSet, field, "thresholds must be non-negative and double time after overtime")
	}
	return nil
}

func (r RuleSet) validateMethods() error {
	if len(r.Methods) == 0 {
		return reject(ErrInvalidRuleSet, "methods", "at least one overtime method is required")
	}
	for _, m := range r.Methods {
		var ok bool
		switch m {
		case timeprofile.OvertimeNone, timeprofile.OvertimeSingleRate, timeprofile.OvertimeWeightedAverage, timeprofile.OvertimePieceRateAverage:
			ok = true
		case timeprofile.OvertimeFluctuatingWeek:
			ok = r.Period.Days == 7
		case timeprofile.OvertimeHealthcare880:
			ok = r.Period.Days == 14 && r.Daily.OTAfter > 0
		case timeprofile.OvertimePublicSafety7k:
			ok = r.Period.Days >= 7
		case timeprofile.OvertimePublicCompTime:
			ok = r.CompTime.Enabled
		}
		if !ok {
			return reject(ErrInvalidRuleSet, "methods", "method %q is unknown or inconsistent with the period", m)
		}
	}
	return nil
}

func (r RuleSet) validatePremiums() error {
	bad := func(field, format string, args ...any) error {
		return reject(ErrInvalidRuleSet, field, format, args...)
	}
	needsMin := false
	if o := r.OnCall; o.Enabled {
		if o.EngagedResponseMinutes < 0 || o.EngagedExpectedCalls < 0 || o.StandbyPerHour < 0 {
			return bad("on_call", "criteria and standby pay cannot be negative")
		}
		if o.StandbyPerHour > 0 {
			if err := o.Standby.validate("on_call.standby"); err != nil {
				return err
			}
		}
	}
	if p := r.ReportingTime; p.Enabled {
		if p.GuaranteeNum <= 0 || p.GuaranteeDen <= 0 || p.TriggerNum < 0 || p.TriggerDen < 0 || (p.TriggerDen > 0) != (p.TriggerNum > 0) {
			return bad("reporting_time", "guarantee fraction is required and trigger fraction must be complete")
		}
		if p.Min < 0 || (p.Max > 0 && p.Max < p.Min) || !p.Basis.valid() {
			return bad("reporting_time", "bounds or rate basis are invalid")
		}
		needsMin = needsMin || p.Basis == BasisMinimumWage
	}
	if c := r.CallBack; c.Enabled {
		if c.Minimum <= 0 || !c.Basis.valid() {
			return bad("call_back", "minimum and rate basis are required")
		}
		needsMin = needsMin || c.Basis == BasisMinimumWage
	}
	if s := r.SplitShift; s.Enabled {
		if s.GapMoreThan <= 0 || s.PremiumSeconds <= 0 {
			return bad("split_shift", "gap and premium duration are required")
		}
		if err := s.Treatment.validate("split_shift"); err != nil {
			return err
		}
		needsMin = true
	}
	if needsMin && r.MinimumWage <= 0 {
		return bad("minimum_wage", "a minimum-wage basis needs the minimum wage")
	}
	return nil
}

// Override is a collective-agreement override. Zero fields keep the base
// value. Overrides may only be more favorable to the worker: lower or newly
// added thresholds and higher factors.
type Override struct {
	Source           string
	DailyOTAfter     Seconds
	DailyDTAfter     Seconds
	PeriodOTAfter    Seconds
	OvertimeFactor   Factor
	DoubleTimeFactor Factor
}

// WithOverride applies a CBA override to a validated rule set.
func (r RuleSet) WithOverride(o Override) (RuleSet, error) {
	if err := r.Validate(); err != nil {
		return RuleSet{}, err
	}
	if strings.TrimSpace(o.Source) == "" {
		return RuleSet{}, reject(ErrInvalidRuleSet, "override.source", "the agreement clause is required")
	}
	lower := func(field string, base *Seconds, v Seconds) error {
		if v == 0 {
			return nil
		}
		if v < 0 || (*base > 0 && v > *base) {
			return reject(ErrLessFavorable, field, "%s would raise the threshold above %s", v, *base)
		}
		*base = v
		return nil
	}
	higher := func(field string, base *Factor, v Factor) error {
		if v == 0 {
			return nil
		}
		if v < *base {
			return reject(ErrLessFavorable, field, "%s is below the statutory %s", v, *base)
		}
		*base = v
		return nil
	}
	out := r
	out.Methods = append([]timeprofile.OvertimeMethod(nil), r.Methods...)
	out.Differentials = append([]DifferentialRule(nil), r.Differentials...)
	for _, err := range []error{
		lower("override.daily_ot_after", &out.Daily.OTAfter, o.DailyOTAfter),
		lower("override.daily_dt_after", &out.Daily.DTAfter, o.DailyDTAfter),
		lower("override.period_ot_after", &out.Period.OTAfter, o.PeriodOTAfter),
		higher("override.overtime_factor", &out.OvertimeFactor, o.OvertimeFactor),
		higher("override.double_time_factor", &out.DoubleTimeFactor, o.DoubleTimeFactor),
	} {
		if err != nil {
			return RuleSet{}, err
		}
	}
	out.Version = r.Version + "+cba:" + o.Source
	return out, out.Validate()
}

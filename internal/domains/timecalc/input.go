package timecalc

import (
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
)

// WorkKind separates productive work from time that California Labor Code
// 226.2 requires to be paid separately for piece-rate workers.
type WorkKind string

// Work kinds.
const (
	KindProductive    WorkKind = "PRODUCTIVE"
	KindNonproductive WorkKind = "NONPRODUCTIVE"
	KindRest          WorkKind = "REST"
)

// Interval is one span of worked time between two instants.
type Interval struct {
	ID             string
	AggregationKey string // must equal the request's key: one calculation per key
	Assignment     string // concurrent assignments under one key aggregate
	Start, End     time.Time
	Zone           string // IANA zone where the work happened (differential windows)
	RateCode       string
	Rate           Money // hourly rate for this work; zero for salary or pure piece work
	Job            string
	Kind           WorkKind
	Differentials  []string // tags defined by the rule set
	PieceUnits     int64
	PieceEarnings  Money
	CallBackID     string // intervals sharing an ID form one call back
}

// Workweek is the employer's fixed, recurring workweek. Every workday
// starts at StartHour:StartMinute local time in Zone.
type Workweek struct {
	Zone                   string
	StartWeekday           time.Weekday
	StartHour, StartMinute int
}

// Date is a civil date.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// EarningItem is a non-hourly payment allocated to one overtime period,
// such as a nondiscretionary bonus allocation.
type EarningItem struct {
	Code      string
	Period    int // overtime period index within the request
	Amount    Money
	Treatment Treatment
}

// ScheduledDay is one scheduled day of an alternative workweek schedule.
type ScheduledDay struct {
	Day       int // workday index within the request
	Scheduled Seconds
}

// AlternativeSchedule is an adopted alternative workweek schedule.
type AlternativeSchedule struct {
	ID   string
	Days []ScheduledDay
}

// Restrictions describe how far an on-call worker's time is their own.
type Restrictions struct {
	OnPremises      bool
	ResponseMinutes int // 0 means no response deadline
	ExpectedCalls   int
}

// OnCallPeriod is an on-call schedule entry.
type OnCallPeriod struct {
	ID           string
	Start, End   time.Time
	RateCode     string
	Rate         Money // hourly rate if the period is engaged to wait
	Restrictions Restrictions
}

// ScheduledShift is a scheduled shift the worker reported for.
type ScheduledShift struct {
	ID         string
	Start, End time.Time
	Reported   bool
	Rate       Money // the shift's base rate, for the SHIFT_RATE basis
}

// Request is one calculation for one aggregation key over whole overtime
// periods.
type Request struct {
	CalculationID  string
	AggregationKey string
	Method         timeprofile.OvertimeMethod
	Workweek       Workweek
	PeriodStart    Date
	Days           int
	Intervals      []Interval
	Earnings       []EarningItem
	WeeklySalary   Money // fluctuating workweek only
	Alternative    *AlternativeSchedule
	CompTimeBefore Seconds
	OnCall         []OnCallPeriod
	Shifts         []ScheduledShift
}

func (req Request) validate(rules RuleSet) error {
	bad := func(field, format string, args ...any) error {
		return reject(ErrInvalidRequest, field, format, args...)
	}
	if strings.TrimSpace(req.CalculationID) == "" || strings.TrimSpace(req.AggregationKey) == "" {
		return bad("calculation_id", "calculation id and aggregation key are required")
	}
	if !rules.allows(req.Method) {
		return reject(ErrMethodNotAllowed, "method", "%q is not permitted by %s/%s", req.Method, rules.ID, rules.Version)
	}
	if req.Days <= 0 || req.Days%rules.Period.Days != 0 {
		return bad("days", "%d is not a whole number of %d-day periods", req.Days, rules.Period.Days)
	}
	if req.Workweek.StartHour < 0 || req.Workweek.StartHour > 23 || req.Workweek.StartMinute < 0 || req.Workweek.StartMinute > 59 {
		return bad("workweek", "start time is out of range")
	}
	periods := req.Days / rules.Period.Days
	if req.Method == timeprofile.OvertimeFluctuatingWeek && req.WeeklySalary <= 0 {
		return bad("weekly_salary", "the fluctuating workweek needs a fixed weekly salary")
	}
	if req.CompTimeBefore < 0 {
		return bad("comp_time_before", "cannot be negative")
	}
	for i, iv := range req.Intervals {
		if err := iv.validate(req, rules); err != nil {
			return err
		}
		for _, other := range req.Intervals[:i] {
			if other.ID == iv.ID {
				return bad("intervals", "id %q is repeated", iv.ID)
			}
		}
	}
	if req.Method == timeprofile.OvertimeSingleRate {
		var rate Money
		for _, iv := range req.Intervals {
			if rate != 0 && iv.Rate != 0 && iv.Rate != rate {
				return bad("intervals."+iv.ID, "the single-rate method cannot carry a second rate %s beside %s; use the weighted average", iv.Rate, rate)
			}
			rate = max(rate, iv.Rate)
		}
	}
	for _, e := range req.Earnings {
		if strings.TrimSpace(e.Code) == "" || e.Amount <= 0 || e.Period < 0 || e.Period >= periods {
			return bad("earnings", "item %q needs a code, a positive amount and a period in 0..%d", e.Code, periods-1)
		}
		if err := e.Treatment.validate("earnings." + e.Code); err != nil {
			return err
		}
	}
	if a := req.Alternative; a != nil {
		if !rules.Alternative.Allowed {
			return reject(ErrRuleDisabled, "alternative", "%s does not permit alternative workweek schedules", rules.ID)
		}
		for _, d := range a.Days {
			if d.Day < 0 || d.Day >= req.Days || d.Scheduled <= 0 || d.Scheduled >= rules.Alternative.DTAfter {
				return bad("alternative", "scheduled day %d is out of range", d.Day)
			}
		}
	}
	if len(req.OnCall) > 0 && !rules.OnCall.Enabled {
		return reject(ErrRuleDisabled, "on_call", "%s has no on-call rule", rules.ID)
	}
	for _, o := range req.OnCall {
		if strings.TrimSpace(o.ID) == "" || !o.End.After(o.Start) || o.Rate < 0 || o.Restrictions.ResponseMinutes < 0 || o.Restrictions.ExpectedCalls < 0 {
			return bad("on_call", "period %q is malformed", o.ID)
		}
	}
	if len(req.Shifts) > 0 && !rules.ReportingTime.Enabled {
		return reject(ErrRuleDisabled, "shifts", "%s has no reporting-time rule", rules.ID)
	}
	for _, s := range req.Shifts {
		if strings.TrimSpace(s.ID) == "" || !s.End.After(s.Start) || s.Rate < 0 {
			return bad("shifts", "shift %q is malformed", s.ID)
		}
	}
	return nil
}

func (iv Interval) validate(req Request, rules RuleSet) error {
	bad := func(format string, args ...any) error {
		return reject(ErrInvalidRequest, "intervals."+iv.ID, format, args...)
	}
	if strings.TrimSpace(iv.ID) == "" {
		return reject(ErrInvalidRequest, "intervals", "interval id is required")
	}
	if iv.AggregationKey != req.AggregationKey {
		return bad("aggregation key %q differs from %q", iv.AggregationKey, req.AggregationKey)
	}
	if !iv.End.After(iv.Start) || iv.Start.Nanosecond() != 0 || iv.End.Nanosecond() != 0 {
		return bad("needs whole-second instants with end after start")
	}
	if iv.Zone == "" {
		return bad("zone is required")
	}
	if iv.Kind != KindProductive && iv.Kind != KindNonproductive && iv.Kind != KindRest {
		return bad("kind %q is not declared", iv.Kind)
	}
	if iv.Rate < 0 || iv.PieceEarnings < 0 || iv.PieceUnits < 0 {
		return bad("rates and piece amounts cannot be negative")
	}
	if iv.PieceEarnings > 0 && req.Method != timeprofile.OvertimePieceRateAverage {
		return bad("piece earnings need the piece-rate method")
	}
	if req.Method == timeprofile.OvertimeFluctuatingWeek && iv.Rate != 0 {
		return bad("the fluctuating workweek salary is the straight time; hourly rate must be zero")
	}
	if iv.CallBackID != "" && !rules.CallBack.Enabled {
		return reject(ErrRuleDisabled, "intervals."+iv.ID, "%s has no call-back rule", rules.ID)
	}
	for _, tag := range iv.Differentials {
		if _, ok := rules.differential(tag); !ok {
			return reject(ErrUnknownDifferential, "intervals."+iv.ID, "tag %q is not defined by %s", tag, rules.ID)
		}
	}
	return nil
}

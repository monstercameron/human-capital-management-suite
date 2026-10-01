package timecalc

import (
	"strconv"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
)

// engine carries one calculation's working state. It lives for one call.
type engine struct {
	req     Request
	rules   RuleSet
	z       zones
	cal     calendar
	ivs     []Interval
	segs    []segment
	pieces  []piece
	plans   []dayPlan
	periods []*periodState
	days    []DayResult
	dayEarn []int64 // straight-time money-seconds per workday
	standby []Seconds
	premium Seconds
	comp    CompTimeResult
	trace   []TraceEntry
}

// Calculate classifies the request's worked time under rules, computes the
// regular rate per overtime period and prices every premium. The result is
// deterministic for a given request and rule set, whatever the input order.
func Calculate(req Request, rules RuleSet) (Result, error) {
	if err := rules.Validate(); err != nil {
		return Result{}, err
	}
	if err := req.validate(rules); err != nil {
		return Result{}, err
	}
	e := &engine{req: req, rules: rules, z: zones{}}
	steps := []func() error{e.prepare, e.onCall, e.classify, e.accumulate, e.fixedAmounts, e.splitShift, e.regularRate, e.reportingTime, e.callBack}
	for _, step := range steps {
		if err := step(); err != nil {
			return Result{}, err
		}
	}
	return e.result(), nil
}

func (e *engine) ruleID(name string) string { return e.rules.ID + ":" + name }

func (e *engine) note(rule, params string) {
	e.trace = append(e.trace, TraceEntry{Rule: e.ruleID(rule), Params: params})
}

func (e *engine) prepare() error {
	r := e.rules
	cal, err := newCalendar(e.z, e.req, r.Period.Days)
	if err != nil {
		return err
	}
	e.cal = cal
	for _, iv := range e.req.Intervals {
		if _, err := e.z.load(iv.Zone); err != nil {
			return err
		}
	}
	n := cal.days() / r.Period.Days
	e.periods = make([]*periodState, n)
	for i := range e.periods {
		e.periods[i] = &periodState{res: PeriodResult{Index: i, StartDate: cal.dateOf(i * r.Period.Days)}}
	}
	e.standby = make([]Seconds, n)
	e.dayEarn = make([]int64, cal.days())
	e.note("RULESET", "version="+r.Version+" jurisdiction="+r.Jurisdiction+" citation="+r.Citation)
	e.note("METHOD", "method="+string(e.req.Method)+" aggregation_key="+e.req.AggregationKey)
	e.note("CALENDAR", "zone="+e.req.Workweek.Zone+" start="+cal.dateOf(0)+" workweek_start="+e.req.Workweek.StartWeekday.String()+
		" at="+two(int64(e.req.Workweek.StartHour))+":"+two(int64(e.req.Workweek.StartMinute))+" days="+strconv.Itoa(cal.days()))
	e.note("THRESHOLDS", "daily_ot_after="+r.Daily.OTAfter.String()+" daily_dt_after="+r.Daily.DTAfter.String()+
		" period_days="+strconv.Itoa(r.Period.Days)+" period_ot_after="+r.Period.OTAfter.String()+
		" ot_factor="+r.OvertimeFactor.String()+" dt_factor="+r.DoubleTimeFactor.String()+
		" rate_decimals="+strconv.Itoa(r.RateDecimals)+" pay_decimals="+strconv.Itoa(r.PayDecimals)+" rounding="+r.Rounding.String())
	return nil
}

func (e *engine) classify() error {
	spans, err := orderSpans(e.ivs)
	if err != nil {
		return err
	}
	if e.segs, err = e.cal.segments(e.ivs, spans); err != nil {
		return err
	}
	worked := make([]Seconds, e.cal.days())
	for _, s := range e.segs {
		worked[s.day] += s.secs()
	}
	e.plans = planDays(e.req, e.rules, worked)
	e.pieces = classify(e.req, e.rules, e.segs, e.plans, e.cal.days())
	if e.req.Alternative != nil {
		e.note("ALTERNATIVE_SCHEDULE", "id="+e.req.Alternative.ID+" dt_after="+e.rules.Alternative.DTAfter.String()+
			" unscheduled_ot_after="+e.rules.Alternative.UnscheduledDay.OTAfter.String())
	}
	if e.req.Method == timeprofile.OvertimePublicCompTime {
		pieces, conv, accrued, err := convertCompTime(e.pieces, e.rules, e.req.CompTimeBefore)
		if err != nil {
			return err
		}
		e.pieces = pieces
		e.comp = CompTimeResult{ConvertedOvertime: conv, Accrued: accrued, BalanceBefore: e.req.CompTimeBefore,
			BalanceAfter: e.req.CompTimeBefore + accrued, Cap: e.rules.CompTime.Cap}
		e.note("COMP_TIME_7O", "factor="+e.rules.CompTime.AccrualFactor.String()+" cap="+e.rules.CompTime.Cap.String()+
			" before="+e.req.CompTimeBefore.String()+" converted_overtime="+conv.String()+" accrued="+accrued.String())
	}
	e.days = make([]DayResult, e.cal.days())
	for d := range e.days {
		e.days[d] = DayResult{Day: d, Date: e.cal.dateOf(d), Rule: e.plans[d].reason}
	}
	for _, p := range e.pieces {
		s := e.segs[p.seg]
		e.days[s.day].Buckets.add(p.cat, p.secs, p.comp)
		e.periods[s.day/e.rules.Period.Days].res.Buckets.add(p.cat, p.secs, p.comp)
	}
	for _, d := range e.days {
		if d.Rule == ReasonSeventhDay && d.Buckets.Worked > 0 {
			e.note("SEVENTH_DAY", "day="+strconv.Itoa(d.Day)+" date="+d.Date+" ot_up_to="+e.rules.SeventhDay.OTUpTo.String()+
				" overtime="+d.Buckets.Overtime.String()+" double_time="+d.Buckets.DoubleTime.String())
		}
	}
	return nil
}

func (e *engine) result() Result {
	r := Result{
		CalculationID: e.req.CalculationID, RuleSetID: e.rules.ID, RuleSetVersion: e.rules.Version,
		Jurisdiction: e.rules.Jurisdiction, Method: e.req.Method, Days: e.days,
		PremiumSeconds: e.premium, CompTime: e.comp, Trace: e.trace,
	}
	r.Periods = make([]PeriodResult, len(e.periods))
	for i, p := range e.periods {
		r.Periods[i] = p.res
		r.Totals = r.Totals.plus(p.res.Buckets)
		for _, l := range p.res.Lines {
			r.GrossPay += l.Amount
		}
	}
	r.Digest = r.digest()
	return r
}

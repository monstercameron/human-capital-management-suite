package timecalc

import (
	"sort"
	"strconv"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
)

// onCallCriterion classifies an on-call period. Engaged-to-wait time is
// hours worked (29 CFR 785.15, 785.17); waiting-to-be-engaged time is not
// (785.16) and earns only standby pay.
func (r OnCallRule) classify(rs Restrictions) (engaged bool, criterion string) {
	switch {
	case r.OnPremisesEngaged && rs.OnPremises:
		return true, "on_premises"
	case r.EngagedResponseMinutes > 0 && rs.ResponseMinutes > 0 && rs.ResponseMinutes <= r.EngagedResponseMinutes:
		return true, "response_minutes=" + strconv.Itoa(rs.ResponseMinutes) + "<=" + strconv.Itoa(r.EngagedResponseMinutes)
	case r.EngagedExpectedCalls > 0 && rs.ExpectedCalls >= r.EngagedExpectedCalls:
		return true, "expected_calls=" + strconv.Itoa(rs.ExpectedCalls) + ">=" + strconv.Itoa(r.EngagedExpectedCalls)
	}
	return false, "free_to_use_time"
}

// unworked returns the parts of [s, e) not covered by ordered worked spans.
func unworked(s, e int64, worked []span) []span {
	var out []span
	cur := s
	for _, w := range worked {
		if w.end <= cur || w.start >= e {
			continue
		}
		if w.start > cur {
			out = append(out, span{start: cur, end: w.start})
		}
		cur = max(cur, w.end)
	}
	if cur < e {
		out = append(out, span{start: cur, end: e})
	}
	return out
}

// onCall turns engaged-to-wait periods into worked intervals (minus any
// recorded work inside them, so nothing counts twice) and totals
// waiting-to-be-engaged time per period for standby pay.
func (e *engine) onCall() error {
	e.ivs = append(make([]Interval, 0, len(e.req.Intervals)+len(e.req.OnCall)), e.req.Intervals...)
	if len(e.req.OnCall) == 0 {
		return nil
	}
	worked, err := orderSpans(e.req.Intervals)
	if err != nil {
		return err
	}
	periods := append([]OnCallPeriod(nil), e.req.OnCall...)
	sort.Slice(periods, func(i, j int) bool {
		if !periods[i].Start.Equal(periods[j].Start) {
			return periods[i].Start.Before(periods[j].Start)
		}
		return periods[i].ID < periods[j].ID
	})
	lo, hi := e.cal.bounds[0], e.cal.bounds[len(e.cal.bounds)-1]
	for _, o := range periods {
		s, end := o.Start.Unix(), o.End.Unix()
		if s < lo || end > hi {
			return reject(ErrOutsidePeriod, "on_call."+o.ID, "falls outside the calculation period")
		}
		engaged, why := e.rules.OnCall.classify(o.Restrictions)
		free := unworked(s, end, worked)
		var secs Seconds
		for _, g := range free {
			secs += Seconds(g.end - g.start)
		}
		state := "WAITING_TO_BE_ENGAGED"
		if engaged {
			state = "ENGAGED_TO_WAIT"
			if err := e.engagedIntervals(o, free); err != nil {
				return err
			}
		} else {
			for _, g := range free {
				for t := g.start; t < g.end; {
					p := e.cal.dayOf(t) / e.rules.Period.Days
					stop := min(g.end, e.cal.bounds[(p+1)*e.rules.Period.Days])
					e.standby[p] += Seconds(stop - t)
					t = stop
				}
			}
		}
		e.note("ON_CALL", "id="+o.ID+" state="+state+" criterion="+why+" unworked_hours="+secs.String())
	}
	return nil
}

func (e *engine) engagedIntervals(o OnCallPeriod, free []span) error {
	rate := o.Rate
	kind := KindProductive
	switch e.req.Method {
	case timeprofile.OvertimeFluctuatingWeek:
		rate = 0
	case timeprofile.OvertimePieceRateAverage:
		kind = KindNonproductive
	}
	if rate == 0 && e.req.Method != timeprofile.OvertimeFluctuatingWeek && len(free) > 0 {
		return reject(ErrInvalidRequest, "on_call."+o.ID, "engaged-to-wait time is hours worked and needs a rate")
	}
	code := o.RateCode
	if code == "" {
		code = "ON_CALL"
	}
	for i, g := range free {
		e.ivs = append(e.ivs, Interval{
			ID: "oncall:" + o.ID + ":" + strconv.Itoa(i), AggregationKey: e.req.AggregationKey,
			Start: time.Unix(g.start, 0).UTC(), End: time.Unix(g.end, 0).UTC(), Zone: e.req.Workweek.Zone,
			RateCode: code, Rate: rate, Kind: kind,
		})
	}
	return nil
}

// splitShift pays the split-shift premium for each workday interrupted by
// an unpaid gap longer than the rule's threshold.
func (e *engine) splitShift() error {
	r := e.rules
	if !r.SplitShift.Enabled {
		return nil
	}
	for i := 0; i < len(e.segs); {
		day := e.segs[i].day
		var gap int64
		j := i + 1
		for ; j < len(e.segs) && e.segs[j].day == day; j++ {
			gap = max(gap, e.segs[j].start-e.segs[j-1].end)
		}
		i = j
		if Seconds(gap) <= r.SplitShift.GapMoreThan {
			continue
		}
		full, err := amountFor(r.MinimumWage, r.SplitShift.PremiumSeconds, r.PayDecimals, r.Rounding)
		if err != nil {
			return err
		}
		amount := full
		if r.SplitShift.OffsetByExcess {
			need, err := mulChecked(int64(r.MinimumWage), int64(e.days[day].Buckets.Worked+r.SplitShift.PremiumSeconds))
			if err != nil {
				return err
			}
			amount = 0
			if short := need - e.dayEarn[day]; short > 0 {
				unit := pow10(moneyScale - r.PayDecimals)
				q, err := mulDiv(short, 1, int64(Hour)*unit, r.Rounding)
				if err != nil {
					return err
				}
				amount = min(full, Money(q*unit))
			}
		}
		e.note("SPLIT_SHIFT", "day="+strconv.Itoa(day)+" date="+e.days[day].Date+" gap="+Seconds(gap).String()+
			" gap_more_than="+r.SplitShift.GapMoreThan.String()+" minimum_wage="+r.MinimumWage.String()+
			" offset_by_excess="+strconv.FormatBool(r.SplitShift.OffsetByExcess)+" premium="+amount.String())
		if amount == 0 {
			continue
		}
		ps := e.periods[day/r.Period.Days]
		in := r.SplitShift.Treatment.Inclusion == Include
		if err := e.addFixed(ps, LineSplitShift, e.days[day].Date, r.SplitShift.PremiumSeconds, r.MinimumWage, amount, in, "SPLIT_SHIFT"); err != nil {
			return err
		}
	}
	return nil
}

// guaranteeRate resolves a minimum-pay guarantee's rate basis.
func (e *engine) guaranteeRate(basis RateBasis, period int, shiftRate Money, field string) (Money, error) {
	switch basis {
	case BasisRegularRate:
		return e.periods[period].res.RegularRate, nil
	case BasisMinimumWage:
		return e.rules.MinimumWage, nil
	}
	if shiftRate <= 0 {
		return 0, reject(ErrInvalidRequest, field, "the SHIFT_RATE basis needs the shift's rate")
	}
	return shiftRate, nil
}

// addGuarantee appends a paid-not-worked guarantee line. Show-up and
// call-back pay is excluded from the regular rate (29 CFR 778.220-221).
func (e *engine) addGuarantee(kind LineKind, code string, period int, secs Seconds, rate Money, rule string) error {
	amount, err := amountFor(rate, secs, e.rules.PayDecimals, e.rules.Rounding)
	if err != nil {
		return err
	}
	ps := e.periods[period]
	ps.res.Lines = append(ps.res.Lines, PayLine{Period: period, Kind: kind, Code: code, Seconds: secs, Rate: rate, Amount: amount, Rule: e.ruleID(rule)})
	e.premium += secs
	return nil
}

func (e *engine) workedWithin(s, end int64) Seconds {
	var total int64
	for _, iv := range e.ivs {
		total += overlap(s, end, iv.Start.Unix(), iv.End.Unix())
	}
	return Seconds(total)
}

func (e *engine) periodOf(t int64, field string) (int, error) {
	if t < e.cal.bounds[0] || t >= e.cal.bounds[len(e.cal.bounds)-1] {
		return 0, reject(ErrOutsidePeriod, field, "falls outside the calculation period")
	}
	return e.cal.dayOf(t) / e.rules.Period.Days, nil
}

// reportingTime compares each reported shift with the work actually done
// inside it and pays the shortfall against the guarantee.
func (e *engine) reportingTime() error {
	rule := e.rules.ReportingTime
	shifts := append([]ScheduledShift(nil), e.req.Shifts...)
	sort.Slice(shifts, func(i, j int) bool {
		if !shifts[i].Start.Equal(shifts[j].Start) {
			return shifts[i].Start.Before(shifts[j].Start)
		}
		return shifts[i].ID < shifts[j].ID
	})
	for _, sh := range shifts {
		s, end := sh.Start.Unix(), sh.End.Unix()
		period, err := e.periodOf(s, "shifts."+sh.ID)
		if err != nil {
			return err
		}
		if !sh.Reported {
			e.note("REPORTING_TIME", "shift="+sh.ID+" reported=false")
			continue
		}
		sched := Seconds(end - s)
		worked := e.workedWithin(s, end)
		g, err := mulDiv(int64(sched), rule.GuaranteeNum, rule.GuaranteeDen, e.rules.Rounding)
		if err != nil {
			return err
		}
		guarantee := max(Seconds(g), rule.Min)
		if rule.Max > 0 {
			guarantee = min(guarantee, rule.Max)
		}
		if rule.CapAtScheduled {
			guarantee = min(guarantee, sched)
		}
		threshold := guarantee
		if rule.TriggerDen > 0 {
			t, err := mulDiv(int64(sched), rule.TriggerNum, rule.TriggerDen, e.rules.Rounding)
			if err != nil {
				return err
			}
			threshold = Seconds(t)
		}
		owed := Seconds(0)
		if worked < threshold && guarantee > worked {
			owed = guarantee - worked
		}
		e.note("REPORTING_TIME", "shift="+sh.ID+" scheduled="+sched.String()+" worked="+worked.String()+
			" trigger_below="+threshold.String()+" guarantee="+guarantee.String()+" basis="+string(rule.Basis)+" premium_hours="+owed.String())
		if owed == 0 {
			continue
		}
		rate, err := e.guaranteeRate(rule.Basis, period, sh.Rate, "shifts."+sh.ID)
		if err != nil {
			return err
		}
		if err := e.addGuarantee(LineReporting, sh.ID, period, owed, rate, "REPORTING_TIME"); err != nil {
			return err
		}
	}
	return nil
}

// callBack pays each call back's shortfall against the minimum.
func (e *engine) callBack() error {
	rule := e.rules.CallBack
	if !rule.Enabled {
		return nil
	}
	type event struct {
		id     string
		first  Interval
		worked Seconds
	}
	var events []event
	for _, iv := range e.req.Intervals {
		if iv.CallBackID == "" {
			continue
		}
		k := -1
		for i := range events {
			if events[i].id == iv.CallBackID {
				k = i
			}
		}
		if k < 0 {
			events = append(events, event{id: iv.CallBackID, first: iv})
			k = len(events) - 1
		}
		ev := &events[k]
		ev.worked += Seconds(iv.End.Unix() - iv.Start.Unix())
		if iv.Start.Before(ev.first.Start) {
			ev.first = iv
		}
	}
	sort.Slice(events, func(i, j int) bool { return events[i].id < events[j].id })
	for _, ev := range events {
		owed := max(0, rule.Minimum-ev.worked)
		e.note("CALL_BACK", "id="+ev.id+" worked="+ev.worked.String()+" minimum="+rule.Minimum.String()+
			" basis="+string(rule.Basis)+" premium_hours="+owed.String())
		if owed == 0 {
			continue
		}
		period, err := e.periodOf(ev.first.Start.Unix(), "intervals."+ev.first.ID)
		if err != nil {
			return err
		}
		rate, err := e.guaranteeRate(rule.Basis, period, ev.first.Rate, "intervals."+ev.first.ID)
		if err != nil {
			return err
		}
		if err := e.addGuarantee(LineCallBack, ev.id, period, owed, rate, "CALL_BACK"); err != nil {
			return err
		}
	}
	return nil
}

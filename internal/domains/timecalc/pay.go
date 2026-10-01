package timecalc

import (
	"sort"
	"strconv"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
)

// periodState accumulates one overtime period. rrNum is the regular-rate
// numerator in money-seconds: every included payment scaled by 3600 plus
// every hourly rate times its seconds, so the rate is divided exactly once.
type periodState struct {
	res      PeriodResult
	rrNum    int64
	straight []PayLine
	fixed    []PayLine
	restSecs Seconds
	byReason [ReasonAlternative + 1][DoubleTime + 1]Seconds
}

// addTimed accumulates seconds at one rate into a line, first-seen order.
func (ps *periodState) addTimed(kind LineKind, code string, rate Money, secs Seconds, inRR bool, rule string) {
	for i := range ps.straight {
		l := &ps.straight[i]
		if l.Kind == kind && l.Code == code && l.Rate == rate {
			l.Seconds += secs
			return
		}
	}
	ps.straight = append(ps.straight, PayLine{Period: ps.res.Index, Kind: kind, Code: code, Rate: rate, Seconds: secs, InRegularRate: inRR, Rule: rule})
}

func (ps *periodState) include(amount Money) error {
	n, err := mulChecked(int64(amount), int64(Hour))
	if err != nil {
		return err
	}
	ps.rrNum, err = addChecked(ps.rrNum, n)
	return err
}

func (ps *periodState) includeTimed(rate Money, secs Seconds) (int64, error) {
	n, err := mulChecked(int64(rate), int64(secs))
	if err != nil {
		return 0, err
	}
	ps.rrNum, err = addChecked(ps.rrNum, n)
	return n, err
}

// straightRate is the hourly rate and line kind for an interval's time.
func (e *engine) straightRate(iv Interval) (LineKind, Money) {
	r := e.rules
	switch {
	case e.req.Method == timeprofile.OvertimeFluctuatingWeek:
		return LineStraightTime, 0
	case e.req.Method == timeprofile.OvertimePieceRateAverage && r.PieceRest.Enabled && iv.Kind == KindRest:
		return LineRest, 0
	case e.req.Method == timeprofile.OvertimePieceRateAverage && r.PieceRest.Enabled && iv.Kind == KindNonproductive:
		return LineNonproductive, max(iv.Rate, r.PieceRest.NonproductiveFloor)
	}
	return LineStraightTime, iv.Rate
}

// accumulate prices every classified piece at straight time and applies
// interval differentials, feeding the regular-rate numerator.
func (e *engine) accumulate() error {
	r := e.rules
	for _, p := range e.pieces {
		s := e.segs[p.seg]
		iv := e.ivs[s.iv]
		ps := e.periods[s.day/r.Period.Days]
		ps.byReason[p.reason][p.cat] += p.secs
		kind, rate := e.straightRate(iv)
		if kind == LineRest {
			ps.restSecs += p.secs // priced once the period's average rate is known
			continue
		}
		n, err := ps.includeTimed(rate, p.secs)
		if err != nil {
			return err
		}
		e.dayEarn[s.day] += n
		if rate > 0 && !p.comp {
			rule := "STRAIGHT_TIME"
			if kind == LineNonproductive {
				rule = "LC_226_2_NONPRODUCTIVE"
			}
			ps.addTimed(kind, iv.RateCode, rate, p.secs, true, e.ruleID(rule))
		}
		for _, tag := range iv.Differentials {
			if err := e.differential(ps, s.day, p, iv, tag); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *engine) differential(ps *periodState, day int, p piece, iv Interval, tag string) error {
	r := e.rules
	d, _ := r.differential(tag)
	secs := p.secs
	if d.Window != nil {
		secs = windowOverlap(p.start, p.start+int64(p.secs), e.z[iv.Zone], *d.Window)
	}
	rate := d.PerHour
	if d.Percent > 0 {
		var err error
		if rate, err = scaleRate(iv.Rate, d.Percent, r.RateDecimals, r.Rounding); err != nil {
			return err
		}
	}
	if secs == 0 || rate == 0 {
		return nil
	}
	inRR := d.Treatment.Inclusion == Include
	ps.addTimed(LineDifferential, tag, rate, secs, inRR, e.ruleID("DIFFERENTIAL:"+tag))
	n, err := mulChecked(int64(rate), int64(secs))
	if err != nil {
		return err
	}
	e.dayEarn[day] += n
	if inRR {
		ps.rrNum, err = addChecked(ps.rrNum, n)
	}
	return err
}

// fixedAmounts adds salary, piece earnings, earning items and standby pay.
func (e *engine) fixedAmounts() error {
	r := e.rules
	piece := make([]Money, len(e.periods))
	for _, iv := range e.ivs {
		if iv.PieceEarnings == 0 {
			continue
		}
		p := e.cal.dayOf(iv.Start.Unix()) / r.Period.Days
		if e.cal.dayOf(iv.End.Unix()-1)/r.Period.Days != p {
			return reject(ErrInvalidRequest, "intervals."+iv.ID, "piece earnings cannot span two overtime periods")
		}
		piece[p] += iv.PieceEarnings
	}
	items := append([]EarningItem(nil), e.req.Earnings...)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Period != items[j].Period {
			return items[i].Period < items[j].Period
		}
		if items[i].Code != items[j].Code {
			return items[i].Code < items[j].Code
		}
		return items[i].Amount < items[j].Amount
	})
	for i, ps := range e.periods {
		if e.req.Method == timeprofile.OvertimeFluctuatingWeek {
			if err := e.addFixed(ps, LineSalary, "WEEKLY_SALARY", ps.res.Buckets.Worked, 0, e.req.WeeklySalary, true, "FWW_SALARY_778_114"); err != nil {
				return err
			}
		}
		if piece[i] > 0 {
			if err := e.addFixed(ps, LinePiece, "PIECE", 0, 0, piece[i], true, "PIECE_RATE_778_111"); err != nil {
				return err
			}
		}
		for _, it := range items {
			if it.Period != i {
				continue
			}
			in := it.Treatment.Inclusion == Include
			if err := e.addFixed(ps, LineEarning, it.Code, 0, 0, it.Amount, in, "EARNING:"+it.Code); err != nil {
				return err
			}
			e.note("EARNING", "period="+strconv.Itoa(i)+" code="+it.Code+" amount="+it.Amount.String()+
				" inclusion="+string(it.Treatment.Inclusion)+" basis="+it.Treatment.Basis)
		}
		if secs := e.standby[i]; secs > 0 && r.OnCall.StandbyPerHour > 0 {
			amount, err := amountFor(r.OnCall.StandbyPerHour, secs, r.PayDecimals, r.Rounding)
			if err != nil {
				return err
			}
			in := r.OnCall.Standby.Inclusion == Include
			if err := e.addFixed(ps, LineStandby, "STANDBY", secs, r.OnCall.StandbyPerHour, amount, in, "ON_CALL_STANDBY"); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *engine) addFixed(ps *periodState, kind LineKind, code string, secs Seconds, rate, amount Money, inRR bool, rule string) error {
	ps.fixed = append(ps.fixed, PayLine{Period: ps.res.Index, Kind: kind, Code: code, Seconds: secs, Rate: rate, Amount: amount, InRegularRate: inRR, Rule: e.ruleID(rule)})
	if inRR {
		return ps.include(amount)
	}
	return nil
}

// regularRate prices 226.2 rest time, computes each period's regular rate
// by the configured method and adds the overtime and double-time premiums.
func (e *engine) regularRate() error {
	r := e.rules
	for _, ps := range e.periods {
		worked := ps.res.Buckets.Worked
		var rest []PayLine
		if ps.restSecs > 0 {
			avg, err := rateFor(ps.rrNum, worked-ps.restSecs, r.RateDecimals, r.Rounding)
			if err != nil {
				return err
			}
			rate := max(avg, r.PieceRest.RestFloor)
			amount, err := amountFor(rate, ps.restSecs, r.PayDecimals, r.Rounding)
			if err != nil {
				return err
			}
			if _, err := ps.includeTimed(rate, ps.restSecs); err != nil {
				return err
			}
			rest = append(rest, PayLine{Period: ps.res.Index, Kind: LineRest, Code: "REST", Seconds: ps.restSecs, Rate: rate, Amount: amount, InRegularRate: true, Rule: e.ruleID("LC_226_2_REST")})
			e.note("LC_226_2_REST", "period="+strconv.Itoa(ps.res.Index)+" average_rate="+avg.String()+" floor="+r.PieceRest.RestFloor.String()+" rate="+rate.String()+" hours="+ps.restSecs.String())
		}
		rr, err := rateFor(ps.rrNum, worked, r.RateDecimals, r.Rounding)
		if err != nil {
			return err
		}
		ps.res.RegularRate = rr
		earnings, err := rateFor(ps.rrNum, Hour, moneyScale, r.Rounding)
		if err != nil {
			return err
		}
		br := ps.byReason
		e.note("CLASSIFY", "period="+strconv.Itoa(ps.res.Index)+" daily_ot="+br[ReasonDaily][Overtime].String()+
			" daily_dt="+br[ReasonDaily][DoubleTime].String()+" period_ot="+br[ReasonPeriod][Overtime].String()+
			" seventh_day_ot="+br[ReasonSeventhDay][Overtime].String()+" seventh_day_dt="+br[ReasonSeventhDay][DoubleTime].String()+
			" alternative_ot="+br[ReasonAlternative][Overtime].String()+" alternative_dt="+br[ReasonAlternative][DoubleTime].String())
		e.note(rateRule(e.req.Method), "period="+strconv.Itoa(ps.res.Index)+" included_earnings="+earnings.String()+
			" hours="+worked.String()+" regular_rate="+rr.String())
		if e.req.Method == timeprofile.OvertimeFluctuatingWeek && r.MinimumWage > 0 && worked > 0 && rr < r.MinimumWage {
			return reject(ErrBelowMinimumWage, "weekly_salary", "period %d regular rate %s is below the minimum wage %s", ps.res.Index, rr, r.MinimumWage)
		}
		var prem []PayLine
		if e.req.Method != timeprofile.OvertimeNone {
			b := ps.res.Buckets
			for _, x := range []struct {
				kind   LineKind
				secs   Seconds
				factor Factor
			}{{LineOvertime, b.Overtime - b.CompTime, r.OvertimeFactor}, {LineDoubleTime, b.DoubleTime, r.DoubleTimeFactor}} {
				if x.secs == 0 {
					continue
				}
				rate, err := scaleRate(rr, x.factor-One, r.RateDecimals, r.Rounding)
				if err != nil {
					return err
				}
				amount, err := amountFor(rate, x.secs, r.PayDecimals, r.Rounding)
				if err != nil {
					return err
				}
				prem = append(prem, PayLine{Period: ps.res.Index, Kind: x.kind, Code: string(x.kind), Seconds: x.secs, Rate: rate, Amount: amount, Rule: e.ruleID(string(x.kind))})
			}
		}
		lines := make([]PayLine, 0, len(ps.straight)+len(ps.fixed)+len(rest)+len(prem))
		for _, l := range ps.straight {
			if l.Amount, err = amountFor(l.Rate, l.Seconds, r.PayDecimals, r.Rounding); err != nil {
				return err
			}
			lines = append(lines, l)
		}
		lines = append(append(append(lines, ps.fixed...), rest...), prem...)
		ps.res.Lines = lines
	}
	return nil
}

func rateRule(m timeprofile.OvertimeMethod) string {
	switch m {
	case timeprofile.OvertimeFluctuatingWeek:
		return "REGULAR_RATE_FWW_778_114"
	case timeprofile.OvertimePieceRateAverage:
		return "REGULAR_RATE_PIECE_778_111"
	case timeprofile.OvertimeSingleRate:
		return "REGULAR_RATE_SINGLE"
	}
	return "REGULAR_RATE_WEIGHTED_778_115"
}

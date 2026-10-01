package timecalc

import (
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Category is the pay bucket of a worked second. Buckets are exclusive:
// a daily overtime hour never also counts toward period overtime.
type Category uint8

// Categories.
const (
	Regular Category = iota
	Overtime
	DoubleTime
)

func (c Category) String() string {
	switch c {
	case Overtime:
		return "OVERTIME"
	case DoubleTime:
		return "DOUBLE_TIME"
	}
	return "REGULAR"
}

// Reason is the rule that put a second into its bucket.
type Reason uint8

// Reasons.
const (
	ReasonNone Reason = iota
	ReasonDaily
	ReasonPeriod
	ReasonSeventhDay
	ReasonAlternative
)

func (r Reason) String() string {
	switch r {
	case ReasonDaily:
		return "DAILY"
	case ReasonPeriod:
		return "PERIOD"
	case ReasonSeventhDay:
		return "SEVENTH_DAY"
	case ReasonAlternative:
		return "ALTERNATIVE_SCHEDULE"
	}
	return "NONE"
}

// piece is a chronological slice of a segment in exactly one bucket.
type piece struct {
	seg    int
	start  int64
	secs   Seconds
	cat    Category
	reason Reason
	comp   bool // converted to 7(o) comp time; no cash straight time or premium
}

// dayPlan is the daily rule in force for one workday.
type dayPlan struct {
	rule    DailyRule
	reason  Reason
	seventh bool
}

func planDays(req Request, rules RuleSet, worked []Seconds) []dayPlan {
	plans := make([]dayPlan, len(worked))
	var sched map[int]Seconds
	if req.Alternative != nil {
		sched = make(map[int]Seconds, len(req.Alternative.Days))
		for _, d := range req.Alternative.Days {
			sched[d.Day] = d.Scheduled
		}
	}
	for d := range plans {
		switch {
		case req.Method == timeprofile.OvertimeNone:
		case rules.SeventhDay.Enabled && d%7 == 6 && workedAll(worked[d-6:d+1]):
			plans[d] = dayPlan{rule: DailyRule{OTAfter: -1, DTAfter: rules.SeventhDay.OTUpTo}, reason: ReasonSeventhDay, seventh: true}
		case sched != nil:
			if s, ok := sched[d]; ok {
				plans[d] = dayPlan{rule: DailyRule{OTAfter: s, DTAfter: rules.Alternative.DTAfter}, reason: ReasonAlternative}
			} else {
				plans[d] = dayPlan{rule: rules.Alternative.UnscheduledDay, reason: ReasonAlternative}
			}
		case rules.Daily != DailyRule{}:
			plans[d] = dayPlan{rule: rules.Daily, reason: ReasonDaily}
		}
	}
	return plans
}

func workedAll(days []Seconds) bool {
	for _, s := range days {
		if s <= 0 {
			return false
		}
	}
	return true
}

// classify walks the segments in time order and cuts each into pieces at
// every daily and period threshold. Only regular seconds advance the period
// counter, which is what keeps daily and period overtime from pyramiding.
func classify(req Request, rules RuleSet, segs []segment, plans []dayPlan, days int) []piece {
	out := make([]piece, 0, len(segs)+len(segs)/2)
	dayUsed := make([]Seconds, days)
	var periodReg Seconds
	period := -1
	periodOT := rules.Period.OTAfter
	if req.Method == timeprofile.OvertimeNone {
		periodOT = 0
	}
	for si, s := range segs {
		if p := s.day / rules.Period.Days; p != period {
			period, periodReg = p, 0
		}
		plan := plans[s.day]
		t, rem := s.start, s.secs()
		for rem > 0 {
			used := dayUsed[s.day]
			cat, reason, limit := Regular, ReasonNone, rem
			switch {
			case plan.seventh:
				// The whole seventh day is premium: overtime up to the band, then double time.
				if used < plan.rule.DTAfter {
					cat, reason, limit = Overtime, ReasonSeventhDay, min(rem, plan.rule.DTAfter-used)
				} else {
					cat, reason = DoubleTime, ReasonSeventhDay
				}
			case plan.rule.DTAfter > 0 && used >= plan.rule.DTAfter:
				cat, reason = DoubleTime, plan.reason
			case plan.rule.OTAfter > 0 && used >= plan.rule.OTAfter:
				cat, reason = Overtime, plan.reason
				if plan.rule.DTAfter > 0 {
					limit = min(rem, plan.rule.DTAfter-used)
				}
			default:
				bandEnd := rem
				if plan.rule.OTAfter > 0 {
					bandEnd = min(rem, plan.rule.OTAfter-used)
				} else if plan.rule.DTAfter > 0 {
					bandEnd = min(rem, plan.rule.DTAfter-used)
				}
				limit = bandEnd
				if periodOT > 0 {
					if periodReg >= periodOT {
						cat, reason = Overtime, ReasonPeriod
					} else {
						limit = min(bandEnd, periodOT-periodReg)
					}
				}
			}
			out = append(out, piece{seg: si, start: t, secs: limit, cat: cat, reason: reason})
			t += int64(limit)
			rem -= limit
			dayUsed[s.day] += limit
			if cat == Regular {
				periodReg += limit
			}
		}
	}
	return out
}

// convertCompTime converts overtime pieces, earliest first, into 7(o)
// compensatory time until the balance cap is reached. It returns the pieces
// (split where the cap falls mid-piece), converted overtime and accrual.
func convertCompTime(pieces []piece, rules RuleSet, before Seconds) ([]piece, Seconds, Seconds, error) {
	room := rules.CompTime.Cap - before
	if room <= 0 {
		return pieces, 0, 0, nil
	}
	maxConv, err := mulDiv(int64(room), int64(One), int64(rules.CompTime.AccrualFactor), values.RoundingTowardZero)
	if err != nil {
		return nil, 0, 0, err
	}
	left := Seconds(maxConv)
	var converted Seconds
	out := make([]piece, 0, len(pieces)+1)
	for _, p := range pieces {
		if p.cat != Overtime || left == 0 {
			out = append(out, p)
			continue
		}
		take := min(p.secs, left)
		conv := p
		conv.secs, conv.comp = take, true
		out = append(out, conv)
		if take < p.secs {
			rest := p
			rest.start += int64(take)
			rest.secs -= take
			out = append(out, rest)
		}
		left -= take
		converted += take
	}
	accrued, err := mulDiv(int64(converted), int64(rules.CompTime.AccrualFactor), int64(One), rules.Rounding)
	return out, converted, Seconds(accrued), err
}

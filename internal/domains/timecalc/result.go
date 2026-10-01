package timecalc

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// LineKind names a pay line.
type LineKind string

// Pay line kinds.
const (
	LineStraightTime  LineKind = "STRAIGHT_TIME"
	LineSalary        LineKind = "SALARY"
	LinePiece         LineKind = "PIECE_EARNINGS"
	LineNonproductive LineKind = "NONPRODUCTIVE_226_2"
	LineRest          LineKind = "REST_226_2"
	LineDifferential  LineKind = "DIFFERENTIAL"
	LineEarning       LineKind = "EARNING"
	LineStandby       LineKind = "ON_CALL_STANDBY"
	LineSplitShift    LineKind = "SPLIT_SHIFT"
	LineOvertime      LineKind = "OVERTIME_PREMIUM"
	LineDoubleTime    LineKind = "DOUBLE_TIME_PREMIUM"
	LineReporting     LineKind = "REPORTING_TIME"
	LineCallBack      LineKind = "CALL_BACK"
)

// PayLine is one priced component. Seconds and Rate are informational for
// fixed amounts; Amount is authoritative.
type PayLine struct {
	Period        int
	Kind          LineKind
	Code          string
	Seconds       Seconds
	Rate          Money
	Amount        Money
	InRegularRate bool
	Rule          string
}

// Buckets are exclusive classified durations. Worked = Regular + Overtime +
// DoubleTime; CompTime is the part of Overtime converted to 7(o) comp time.
type Buckets struct {
	Worked, Regular, Overtime, DoubleTime, CompTime Seconds
}

func (b *Buckets) add(cat Category, s Seconds, comp bool) {
	b.Worked += s
	switch cat {
	case Overtime:
		b.Overtime += s
		if comp {
			b.CompTime += s
		}
	case DoubleTime:
		b.DoubleTime += s
	default:
		b.Regular += s
	}
}

func (b Buckets) plus(o Buckets) Buckets {
	return Buckets{b.Worked + o.Worked, b.Regular + o.Regular, b.Overtime + o.Overtime, b.DoubleTime + o.DoubleTime, b.CompTime + o.CompTime}
}

// DayResult is one workday's classification.
type DayResult struct {
	Day     int
	Date    string
	Rule    Reason
	Buckets Buckets
}

// PeriodResult is one overtime period with its regular rate and lines.
type PeriodResult struct {
	Index       int
	StartDate   string
	Buckets     Buckets
	RegularRate Money
	Lines       []PayLine
}

// CompTimeResult is the 7(o) accrual to post to the comp-time balance.
type CompTimeResult struct {
	ConvertedOvertime, Accrued, BalanceBefore, BalanceAfter, Cap Seconds
}

// TraceEntry records one applied rule and its parameters.
type TraceEntry struct {
	Rule   string
	Params string
}

// Result is the deterministic output of one calculation.
type Result struct {
	CalculationID  string
	RuleSetID      string
	RuleSetVersion string
	Jurisdiction   string
	Method         timeprofile.OvertimeMethod
	Days           []DayResult
	Periods        []PeriodResult
	Totals         Buckets
	PremiumSeconds Seconds // paid, not worked (reporting time, call back)
	GrossPay       Money
	CompTime       CompTimeResult
	Trace          []TraceEntry
	Digest         string
}

// Render is the canonical text of the result; the digest is its SHA-256.
func (r Result) Render() string { return string(r.appendRender(make([]byte, 0, 4096))) }

func (r Result) appendRender(b []byte) []byte {
	str := func(parts ...string) {
		for _, p := range parts {
			b = append(b, p...)
		}
	}
	dur := func(key string, s Seconds) {
		b = append(b, key...)
		b = s.appendTo(b)
	}
	amt := func(key string, m Money) {
		b = append(b, key...)
		b = m.appendTo(b)
	}
	bk := func(x Buckets) {
		dur(" worked=", x.Worked)
		dur(" regular=", x.Regular)
		dur(" overtime=", x.Overtime)
		dur(" double_time=", x.DoubleTime)
		dur(" comp_time=", x.CompTime)
	}
	str("timecalc v", strconv.Itoa(contractVersion), "\n")
	str("calculation ", r.CalculationID, " ruleset=", r.RuleSetID, "/", r.RuleSetVersion, " jurisdiction=", r.Jurisdiction, " method=", string(r.Method), "\n")
	for _, d := range r.Days {
		if d.Buckets.Worked == 0 {
			continue
		}
		b = append(b, "day "...)
		b = strconv.AppendInt(b, int64(d.Day), 10)
		str(" ", d.Date, " rule=", d.Rule.String())
		bk(d.Buckets)
		b = append(b, '\n')
	}
	for _, p := range r.Periods {
		b = append(b, "period "...)
		b = strconv.AppendInt(b, int64(p.Index), 10)
		str(" ", p.StartDate)
		bk(p.Buckets)
		amt(" regular_rate=", p.RegularRate)
		b = append(b, '\n')
		for _, l := range p.Lines {
			str("  line ", string(l.Kind), " code=", l.Code)
			dur(" hours=", l.Seconds)
			amt(" rate=", l.Rate)
			amt(" amount=", l.Amount)
			str(" in_regular_rate=", strconv.FormatBool(l.InRegularRate), " rule=", l.Rule, "\n")
		}
	}
	b = append(b, "totals"...)
	bk(r.Totals)
	dur(" premium_hours=", r.PremiumSeconds)
	amt(" gross=", r.GrossPay)
	c := r.CompTime
	dur("\ncomp_time converted=", c.ConvertedOvertime)
	dur(" accrued=", c.Accrued)
	dur(" before=", c.BalanceBefore)
	dur(" after=", c.BalanceAfter)
	dur(" cap=", c.Cap)
	b = append(b, '\n')
	for _, t := range r.Trace {
		str("trace ", t.Rule, " ", t.Params, "\n")
	}
	return b
}

func (r Result) digest() string {
	sum := sha256.Sum256(r.appendRender(make([]byte, 0, 4096)))
	return hex.EncodeToString(sum[:])
}

// Validate rechecks bucket arithmetic, line sums and the digest binding.
func (r Result) Validate() error {
	var totals Buckets
	var gross Money
	for _, p := range r.Periods {
		if p.Buckets.Worked != p.Buckets.Regular+p.Buckets.Overtime+p.Buckets.DoubleTime || p.Buckets.CompTime > p.Buckets.Overtime {
			return reject(ErrResultTampered, "periods", "period %d buckets do not partition worked time", p.Index)
		}
		totals = totals.plus(p.Buckets)
		for _, l := range p.Lines {
			gross += l.Amount
		}
	}
	if totals != r.Totals {
		return reject(ErrResultTampered, "totals", "period buckets do not sum to totals")
	}
	if gross != r.GrossPay {
		return reject(ErrResultTampered, "gross_pay", "lines sum to %s, gross is %s", gross, r.GrossPay)
	}
	if r.Digest != r.digest() {
		return reject(ErrResultTampered, "digest", "digest does not bind the result")
	}
	return nil
}

// Explain renders an audit summary: rule set, method, buckets and pay.
func (r Result) Explain() string {
	return "timecalc " + r.CalculationID + " ruleset=" + r.RuleSetID + "/" + r.RuleSetVersion +
		" jurisdiction=" + r.Jurisdiction + " method=" + string(r.Method) +
		" regular=" + r.Totals.Regular.String() + " overtime=" + r.Totals.Overtime.String() +
		" double_time=" + r.Totals.DoubleTime.String() + " gross=" + r.GrossPay.String() +
		" trace_steps=" + strconv.Itoa(len(r.Trace)) + " digest=" + r.Digest
}

// WageHours is the classification in the shape labor.CalculateWages
// consumes (LABOR-003 prices pre-classified regular and overtime hours).
type WageHours struct {
	Regular, Overtime, DoubleTime values.Decimal
}

// WageHours converts the total buckets to decimal hours at scale.
func (r Result) WageHours(scale int32, mode values.RoundingMode) (WageHours, error) {
	var out WageHours
	var err error
	if out.Regular, err = HoursDecimal(r.Totals.Regular, scale, mode); err != nil {
		return WageHours{}, err
	}
	if out.Overtime, err = HoursDecimal(r.Totals.Overtime-r.Totals.CompTime, scale, mode); err != nil {
		return WageHours{}, err
	}
	out.DoubleTime, err = HoursDecimal(r.Totals.DoubleTime, scale, mode)
	return out, err
}

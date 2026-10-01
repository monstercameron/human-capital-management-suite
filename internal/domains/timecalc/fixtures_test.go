package timecalc

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Fixture rule sets. Real parameters come from rule packs; these carry the
// published thresholds so the tests exercise realistic shapes.

const tz = "America/Los_Angeles"

func money(t testing.TB, s string) Money {
	t.Helper()
	m, err := ParseMoney(s)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func m(s string) Money {
	v, err := ParseMoney(s)
	if err != nil {
		panic(err)
	}
	return v
}

func base(id, jurisdiction string) RuleSet {
	return RuleSet{
		ID: id, Version: "2026.1", Jurisdiction: jurisdiction, Citation: "fixture",
		Period:         PeriodRule{Days: 7, OTAfter: 40 * Hour},
		OvertimeFactor: 15000, DoubleTimeFactor: 20000,
		RateDecimals: 2, PayDecimals: 2, Rounding: values.RoundingHalfUp,
		MinimumWage: m("7.25"),
	}
}

func fedFLSA() RuleSet {
	r := base("US-FLSA", "US")
	r.Citation = "29 USC 207; 29 CFR 778"
	r.Methods = []timeprofile.OvertimeMethod{timeprofile.OvertimeNone, timeprofile.OvertimeSingleRate,
		timeprofile.OvertimeWeightedAverage, timeprofile.OvertimeFluctuatingWeek, timeprofile.OvertimePieceRateAverage}
	r.OnCall = OnCallRule{Enabled: true, OnPremisesEngaged: true, EngagedResponseMinutes: 15, EngagedExpectedCalls: 4,
		StandbyPerHour: m("3.00"), Standby: Treatment{Include, "29 CFR 778.223"}}
	r.Differentials = []DifferentialRule{
		{Tag: "NIGHT", PerHour: m("2.00"), Window: &Window{StartMinute: 22 * 60, EndMinute: 6 * 60}, Treatment: Treatment{Include, "29 CFR 778.207(b)"}},
	}
	return r
}

func california() RuleSet {
	r := base("US-CA", "US-CA")
	r.Citation = "Cal. Lab. Code 510, 226.2; IWC wage orders 3, 5"
	r.Methods = []timeprofile.OvertimeMethod{timeprofile.OvertimeSingleRate, timeprofile.OvertimeWeightedAverage, timeprofile.OvertimePieceRateAverage}
	r.Daily = DailyRule{OTAfter: 8 * Hour, DTAfter: 12 * Hour}
	r.SeventhDay = SeventhDayRule{Enabled: true, OTUpTo: 8 * Hour}
	r.Alternative = AlternativeRule{Allowed: true, DTAfter: 12 * Hour, UnscheduledDay: DailyRule{OTAfter: 8 * Hour, DTAfter: 12 * Hour}}
	r.MinimumWage = m("16.90")
	r.PieceRest = PieceRestRule{Enabled: true, RestFloor: m("16.90"), NonproductiveFloor: m("16.90")}
	r.Differentials = []DifferentialRule{
		{Tag: "NIGHT", PerHour: m("2.00"), Window: &Window{StartMinute: 22 * 60, EndMinute: 6 * 60}, Treatment: Treatment{Include, "29 CFR 778.207(b)"}},
		{Tag: "LOC_SF", Percent: 500, Treatment: Treatment{Include, "29 CFR 778.207(b)"}},
	}
	r.OnCall = OnCallRule{Enabled: true, OnPremisesEngaged: true, EngagedResponseMinutes: 15}
	r.ReportingTime = ReportingRule{Enabled: true, TriggerNum: 1, TriggerDen: 2, GuaranteeNum: 1, GuaranteeDen: 2,
		Min: 2 * Hour, Max: 4 * Hour, Basis: BasisRegularRate}
	r.CallBack = CallBackRule{Enabled: true, Minimum: 2 * Hour, Basis: BasisRegularRate}
	r.SplitShift = SplitShiftRule{Enabled: true, GapMoreThan: Hour, PremiumSeconds: Hour, OffsetByExcess: true,
		Treatment: Treatment{Include, "IWC wage order 4(C); fixture decision"}}
	return r
}

// dailyEight is an Alaska/Nevada-style daily overtime set (after 8, no DT).
func dailyEight() RuleSet {
	r := base("US-AK", "US-AK")
	r.Methods = []timeprofile.OvertimeMethod{timeprofile.OvertimeSingleRate, timeprofile.OvertimeWeightedAverage}
	r.Daily = DailyRule{OTAfter: 8 * Hour}
	return r
}

// dailyTwelve is a Colorado-style daily overtime set (after 12).
func dailyTwelve() RuleSet {
	r := base("US-CO", "US-CO")
	r.Methods = []timeprofile.OvertimeMethod{timeprofile.OvertimeSingleRate, timeprofile.OvertimeWeightedAverage}
	r.Daily = DailyRule{OTAfter: 12 * Hour}
	return r
}

func healthcare880() RuleSet {
	r := base("US-FLSA-880", "US")
	r.Methods = []timeprofile.OvertimeMethod{timeprofile.OvertimeHealthcare880}
	r.Daily = DailyRule{OTAfter: 8 * Hour}
	r.Period = PeriodRule{Days: 14, OTAfter: 80 * Hour}
	return r
}

func publicSafety7k() RuleSet {
	r := base("US-FLSA-7K-LE", "US")
	r.Methods = []timeprofile.OvertimeMethod{timeprofile.OvertimePublicSafety7k}
	r.Period = PeriodRule{Days: 28, OTAfter: 171 * Hour}
	return r
}

func publicComp7o() RuleSet {
	r := base("US-FLSA-7O", "US")
	r.Methods = []timeprofile.OvertimeMethod{timeprofile.OvertimePublicCompTime}
	r.CompTime = CompTimeRule{Enabled: true, AccrualFactor: 15000, Cap: 240 * Hour}
	return r
}

func newYork() RuleSet {
	r := base("US-NY", "US-NY")
	r.Methods = []timeprofile.OvertimeMethod{timeprofile.OvertimeSingleRate}
	r.MinimumWage = m("16.50")
	r.ReportingTime = ReportingRule{Enabled: true, GuaranteeNum: 1, GuaranteeDen: 1, Min: 4 * Hour, Max: 4 * Hour,
		CapAtScheduled: true, Basis: BasisMinimumWage}
	return r
}

var laLoc = mustLoc(tz)

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

// at parses a Los Angeles wall-clock time "2026-01-05 08:00".
func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, laLoc)
	if err != nil {
		panic(err)
	}
	return t
}

// week1 is Monday 2026-01-05, a workweek starting Monday 00:00 in LA.
var week1 = Date{2026, time.January, 5}

func request(method timeprofile.OvertimeMethod, days int, ivs ...Interval) Request {
	return Request{
		CalculationID: "calc-1", AggregationKey: "worker-1", Method: method,
		Workweek:    Workweek{Zone: tz, StartWeekday: time.Monday},
		PeriodStart: week1, Days: days, Intervals: ivs,
	}
}

func work(id, start, end string, rate Money) Interval {
	return Interval{ID: id, AggregationKey: "worker-1", Assignment: "a1", Start: at(start), End: at(end),
		Zone: tz, RateCode: "R" + rate.String(), Rate: rate, Kind: KindProductive}
}

// dayStr returns the date string of the n-th day after 2026-01-05.
func dayStr(n int) string {
	return time.Date(2026, time.January, 5+n, 0, 0, 0, 0, laLoc).Format("2006-01-02")
}

// daily builds one interval per day starting 07:00 for the given hours.
func daily(rate Money, hours ...int) []Interval {
	var out []Interval
	for i, h := range hours {
		if h == 0 {
			continue
		}
		s := at(dayStr(i) + " 07:00")
		out = append(out, Interval{ID: "d" + dayStr(i), AggregationKey: "worker-1", Assignment: "a1", Start: s, End: s.Add(time.Duration(h) * time.Hour),
			Zone: tz, RateCode: "BASE", Rate: rate, Kind: KindProductive})
	}
	return out
}

func mustCalc(t testing.TB, req Request, rules RuleSet) Result {
	t.Helper()
	res, err := Calculate(req, rules)
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if err := res.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return res
}

func line(r Result, kind LineKind) (PayLine, bool) {
	for _, p := range r.Periods {
		for _, l := range p.Lines {
			if l.Kind == kind {
				return l, true
			}
		}
	}
	return PayLine{}, false
}

func sumKind(r Result, kind LineKind) Money {
	var total Money
	for _, p := range r.Periods {
		for _, l := range p.Lines {
			if l.Kind == kind {
				total += l.Amount
			}
		}
	}
	return total
}

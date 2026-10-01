package timecalc

import (
	"errors"
	"math"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_WTIME_013_ExactArithmetic(t *testing.T) {
	for _, tc := range []struct {
		in, out string
	}{{"13.64", "13.64"}, {"0.5", "0.50"}, {"7", "7.00"}, {"16.6667", "16.6667"}, {"1000000.0001", "1000000.0001"}} {
		got, err := ParseMoney(tc.in)
		if err != nil || got.String() != tc.out {
			t.Fatalf("ParseMoney(%q) = %s, %v; want %s", tc.in, got, err, tc.out)
		}
	}
	for _, bad := range []string{"", "-1.00", "+1", "1.23456", "abc", "1.x", "999999999999999999"} {
		if _, err := ParseMoney(bad); err == nil {
			t.Fatalf("ParseMoney(%q) accepted", bad)
		}
	}
	if Money(-12345).String() != "-1.2345" || Factor(15000).String() != "1.5" || Factor(20000).String() != "2" {
		t.Fatal("formatting drifted")
	}
	if Seconds(-90).String() != "-0:01:30" || (37*Hour+1800).String() != "37:30:00" {
		t.Fatal("duration formatting drifted")
	}

	// 5 / 2 and 7 / 2 exercise ties; 7 / 3 exercises a non-tie.
	for _, tc := range []struct {
		mode    values.RoundingMode
		a, want int64
	}{
		{values.RoundingHalfUp, 5, 3}, {values.RoundingHalfAwayFromZero, 7, 4},
		{values.RoundingHalfEven, 5, 2}, {values.RoundingHalfEven, 7, 4},
		{values.RoundingTowardZero, 7, 3}, {values.RoundingFloor, 7, 3},
		{values.RoundingAwayFromZero, 5, 3}, {values.RoundingCeiling, 5, 3},
	} {
		got, err := mulDiv(tc.a, 1, 2, tc.mode)
		if err != nil || got != tc.want {
			t.Fatalf("mulDiv(%d/2, %s) = %d, %v; want %d", tc.a, tc.mode, got, err, tc.want)
		}
	}
	if got, _ := mulDiv(7, 1, 3, values.RoundingHalfEven); got != 2 {
		t.Fatalf("7/3 half-even = %d", got)
	}
	if got, err := mulDiv(6, 1, 3, values.RoundingExactRequired); err != nil || got != 2 {
		t.Fatalf("exact division = %d, %v", got, err)
	}
	for _, bad := range []func() error{
		func() error { _, err := mulDiv(7, 1, 2, values.RoundingExactRequired); return err },
		func() error { _, err := mulDiv(7, 1, 2, values.RoundingUnspecified); return err },
		func() error { _, err := mulDiv(-1, 1, 2, values.RoundingHalfUp); return err },
		func() error { _, err := mulDiv(math.MaxInt64, math.MaxInt64, 2, values.RoundingHalfUp); return err },
		func() error { _, err := mulDiv(math.MaxInt64, 2, 1, values.RoundingHalfUp); return err },
		func() error { _, err := mulChecked(math.MaxInt64, 2); return err },
		func() error { _, err := mulChecked(-1, 2); return err },
		func() error { _, err := addChecked(math.MaxInt64, 1); return err },
		func() error { _, err := addChecked(math.MinInt64, -1); return err },
	} {
		if err := bad(); !errors.Is(err, ErrArithmetic) {
			t.Fatalf("arithmetic refusal = %v", err)
		}
	}
	// A one-second piece at $15 is exact in money-seconds and rounds once.
	if a, _ := amountFor(m("15.00"), 1, 4, values.RoundingHalfUp); a != m("0.0042") {
		t.Fatalf("one second at $15 = %s", a)
	}
	if r, _ := rateFor(0, 0, 2, values.RoundingHalfUp); r != 0 {
		t.Fatal("rate over zero hours must be zero")
	}

	d, err := ToDecimal(m("13.6364"), 2, values.RoundingHalfUp)
	if err != nil || d.String() != "13.64" {
		t.Fatalf("ToDecimal = %s, %v", d, err)
	}
	h, err := HoursDecimal(37*Hour+1800, 2, values.RoundingHalfUp)
	if err != nil || h.String() != "37.50" {
		t.Fatalf("HoursDecimal = %s, %v", h, err)
	}
	if _, err := HoursDecimal(Hour, 2, values.RoundingUnspecified); err == nil {
		t.Fatal("undeclared rounding accepted")
	}
	if _, err := (Result{}).WageHours(2, values.RoundingUnspecified); err == nil {
		t.Fatal("wage hours with undeclared rounding accepted")
	}
	for c, want := range map[Category]string{Regular: "REGULAR", Overtime: "OVERTIME", DoubleTime: "DOUBLE_TIME"} {
		if c.String() != want {
			t.Fatalf("category %d = %s", c, c)
		}
	}
	for r, want := range map[Reason]string{ReasonNone: "NONE", ReasonDaily: "DAILY", ReasonPeriod: "PERIOD", ReasonSeventhDay: "SEVENTH_DAY", ReasonAlternative: "ALTERNATIVE_SCHEDULE"} {
		if r.String() != want {
			t.Fatalf("reason %d = %s", r, r)
		}
	}
	rej := &Rejection{Field: "f", Reason: "why", Err: ErrInvalidRequest}
	if rej.Error() != "TIMECALC_REQUEST_INVALID: f: why" {
		t.Fatalf("rejection text = %q", rej.Error())
	}
}

func TestTodo_WTIME_013_RuleSetValidation(t *testing.T) {
	for _, rs := range []RuleSet{fedFLSA(), california(), dailyEight(), dailyTwelve(), healthcare880(), publicSafety7k(), publicComp7o(), newYork()} {
		if err := rs.Validate(); err != nil {
			t.Fatalf("fixture %s invalid: %v", rs.ID, err)
		}
	}
	cases := map[string]func(*RuleSet){
		"identity":            func(r *RuleSet) { r.ID = "" },
		"decimals":            func(r *RuleSet) { r.RateDecimals = 5 },
		"period":              func(r *RuleSet) { r.Period.Days = 29 },
		"daily order":         func(r *RuleSet) { r.Daily.DTAfter = 7 * Hour },
		"factors":             func(r *RuleSet) { r.DoubleTimeFactor = 12000 },
		"seventh day":         func(r *RuleSet) { r.SeventhDay.OTUpTo = 0 },
		"alternative":         func(r *RuleSet) { r.Alternative.DTAfter = 0 },
		"alternative days":    func(r *RuleSet) { r.Alternative.UnscheduledDay.OTAfter = -1 },
		"minimum wage":        func(r *RuleSet) { r.MinimumWage = -1 },
		"comp time":           func(r *RuleSet) { r.CompTime = CompTimeRule{Enabled: true} },
		"no methods":          func(r *RuleSet) { r.Methods = nil },
		"unknown method":      func(r *RuleSet) { r.Methods = append(r.Methods, "MOONLIGHT") },
		"8/80 period":         func(r *RuleSet) { r.Methods = append(r.Methods, "HEALTHCARE_8_80") },
		"7(o) without comp":   func(r *RuleSet) { r.Methods = append(r.Methods, "PUBLIC_COMP_TIME") },
		"differential tag":    func(r *RuleSet) { r.Differentials = append(r.Differentials, r.Differentials[0]) },
		"differential amount": func(r *RuleSet) { r.Differentials[0].Percent = 100 },
		"differential window": func(r *RuleSet) { r.Differentials[0].Window = &Window{StartMinute: 60, EndMinute: 60} },
		"on-call negative":    func(r *RuleSet) { r.OnCall.EngagedResponseMinutes = -1 },
		"reporting fraction":  func(r *RuleSet) { r.ReportingTime.GuaranteeDen = 0 },
		"reporting trigger":   func(r *RuleSet) { r.ReportingTime.TriggerNum = 0 },
		"reporting bounds":    func(r *RuleSet) { r.ReportingTime.Max = Hour },
		"reporting basis":     func(r *RuleSet) { r.ReportingTime.Basis = "HOPE" },
		"call back":           func(r *RuleSet) { r.CallBack.Minimum = 0 },
		"split shift":         func(r *RuleSet) { r.SplitShift.GapMoreThan = 0 },
		"minimum wage basis":  func(r *RuleSet) { r.MinimumWage = 0 },
	}
	for name, mut := range cases {
		rs := california()
		mut(&rs)
		if err := rs.Validate(); !errors.Is(err, ErrInvalidRuleSet) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	for name, mut := range map[string]func(*RuleSet){
		"differential treatment": func(r *RuleSet) { r.Differentials[0].Treatment = Treatment{} },
		"standby treatment":      func(r *RuleSet) { r.OnCall.StandbyPerHour = m("1.00") },
		"split treatment":        func(r *RuleSet) { r.SplitShift.Treatment.Basis = " " },
	} {
		rs := california()
		mut(&rs)
		if err := rs.Validate(); !errors.Is(err, ErrTreatmentUndeclared) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}

func TestTodo_WTIME_014_Rejections(t *testing.T) {
	week := daily(m("20.00"), 8)
	cases := []struct {
		name  string
		rules RuleSet
		mut   func(*Request)
		want  error
	}{
		{"shifts without reporting rule", fedFLSA(), func(r *Request) {
			r.Shifts = []ScheduledShift{shift("s", "2026-01-05 07:00", "2026-01-05 15:00", true)}
		}, ErrRuleDisabled},
		{"malformed shift", california(), func(r *Request) {
			r.Shifts = []ScheduledShift{shift("s", "2026-01-05 15:00", "2026-01-05 07:00", true)}
		}, ErrInvalidRequest},
		{"shift outside period", california(), func(r *Request) {
			r.Shifts = []ScheduledShift{shift("s", "2026-01-13 07:00", "2026-01-13 15:00", true)}
		}, ErrOutsidePeriod},
		{"malformed on-call", california(), func(r *Request) { r.OnCall = []OnCallPeriod{{ID: ""}} }, ErrInvalidRequest},
		{"on-call outside period", california(), func(r *Request) {
			r.OnCall = []OnCallPeriod{onCall("x", "2026-01-12 07:00", "2026-01-12 15:00", m("20.00"), Restrictions{})}
		}, ErrOutsidePeriod},
		{"shift-rate basis without rate", func() RuleSet { r := california(); r.CallBack.Basis = BasisShiftRate; return r }(), func(r *Request) {
			cb := work("cb", "2026-01-05 20:00", "2026-01-05 20:30", 0)
			cb.CallBackID = "cb"
			r.Method = "WEIGHTED_AVERAGE"
			r.Intervals = append(r.Intervals, cb)
		}, ErrInvalidRequest},
		{"earning period out of range", california(), func(r *Request) {
			r.Earnings = []EarningItem{{Code: "B", Period: 1, Amount: m("1.00"), Treatment: Treatment{Include, "x"}}}
		}, ErrInvalidRequest},
		{"alternative day out of range", california(), func(r *Request) {
			r.Alternative = &AlternativeSchedule{ID: "a", Days: []ScheduledDay{{Day: 9, Scheduled: 10 * Hour}}}
		}, ErrInvalidRequest},
		{"repeated interval id", california(), func(r *Request) {
			r.Intervals = append(r.Intervals, work(r.Intervals[0].ID, "2026-01-06 07:00", "2026-01-06 08:00", m("20.00")))
		}, ErrInvalidRequest},
		{"negative comp balance", california(), func(r *Request) { r.CompTimeBefore = -1 }, ErrInvalidRequest},
		{"missing calculation id", california(), func(r *Request) { r.CalculationID = "" }, ErrInvalidRequest},
		{"fluctuating week without salary", fedFLSA(), func(r *Request) { r.Method = "FLUCTUATING_WEEK"; r.Intervals[0].Rate = 0 }, ErrInvalidRequest},
		{"fluctuating week with hourly rate", fedFLSA(), func(r *Request) { r.Method = "FLUCTUATING_WEEK"; r.WeeklySalary = m("900.00") }, ErrInvalidRequest},
		{"interval zone", california(), func(r *Request) { r.Intervals[0].Zone = "Nowhere/Here" }, ErrInvalidRequest},
		{"workweek hour", california(), func(r *Request) { r.Workweek.StartHour = 24 }, ErrInvalidRequest},
		{"piece earnings across periods", func() RuleSet { r := fedFLSA(); return r }(), func(r *Request) {
			iv := work("p", "2026-01-11 20:00", "2026-01-12 04:00", 0)
			iv.PieceEarnings = m("10.00")
			r.Method = "PIECE_RATE_AVERAGE"
			r.Days = 14
			r.Intervals = []Interval{iv}
		}, ErrInvalidRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := request("SINGLE_RATE", 7, append([]Interval(nil), week...)...)
			tc.mut(&req)
			if _, err := Calculate(req, tc.rules); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

package timecalc

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func wantBuckets(t *testing.T, got Buckets, worked, reg, ot, dt Seconds) {
	t.Helper()
	if got.Worked != worked || got.Regular != reg || got.Overtime != ot || got.DoubleTime != dt {
		t.Fatalf("buckets = worked %s reg %s ot %s dt %s; want %s %s %s %s",
			got.Worked, got.Regular, got.Overtime, got.DoubleTime, worked, reg, ot, dt)
	}
}

func wantMoney(t *testing.T, what string, got Money, want string) {
	t.Helper()
	if got != m(want) {
		t.Fatalf("%s = %s, want %s", what, got, want)
	}
}

// twoRateWeek is 30 hours at $15 and 15 hours at $20 in one workweek.
func twoRateWeek() Request {
	ivs := daily(m("15.00"), 10, 10, 10)
	for i := range ivs {
		ivs[i].RateCode = "CASHIER"
	}
	ivs = append(ivs,
		Interval{ID: "stock-1", AggregationKey: "worker-1", Assignment: "a2", Start: at(dayStr(3) + " 07:00"), End: at(dayStr(3) + " 15:00"), Zone: tz, RateCode: "STOCK", Rate: m("20.00"), Kind: KindProductive},
		Interval{ID: "stock-2", AggregationKey: "worker-1", Assignment: "a2", Start: at(dayStr(4) + " 07:00"), End: at(dayStr(4) + " 14:00"), Zone: tz, RateCode: "STOCK", Rate: m("20.00"), Kind: KindProductive},
	)
	return request(timeprofile.OvertimeWeightedAverage, 7, ivs...)
}

// TestTodo_WTIME_013 proves each RED condition is closed and each GREEN
// classification and regular-rate method holds.
func TestTodo_WTIME_013(t *testing.T) {
	t.Run("two rates use the weighted average, not the higher or lower rate", func(t *testing.T) {
		res := mustCalc(t, twoRateWeek(), fedFLSA())
		wantBuckets(t, res.Totals, 45*Hour, 40*Hour, 5*Hour, 0)
		// (30 x 15 + 15 x 20) / 45 = 16.666... -> 16.67; premium rate 8.335 -> 8.34.
		wantMoney(t, "regular rate", res.Periods[0].RegularRate, "16.67")
		ot, _ := line(res, LineOvertime)
		wantMoney(t, "premium rate", ot.Rate, "8.34")
		wantMoney(t, "premium", ot.Amount, "41.70")
		wantMoney(t, "straight time", sumKind(res, LineStraightTime), "750.00")
		wantMoney(t, "gross", res.GrossPay, "791.70")
		if !strings.Contains(res.Render(), "REGULAR_RATE_WEIGHTED_778_115") {
			t.Fatal("trace does not cite the weighted-average rule")
		}
	})

	t.Run("nondiscretionary bonus is included in the regular rate", func(t *testing.T) {
		req := request(timeprofile.OvertimeSingleRate, 7, daily(m("20.00"), 9, 9, 9, 9, 9)...)
		req.Earnings = []EarningItem{{Code: "PRODUCTION_BONUS", Amount: m("90.00"), Treatment: Treatment{Include, "29 CFR 778.211(c)"}}}
		res := mustCalc(t, req, fedFLSA())
		wantMoney(t, "regular rate", res.Periods[0].RegularRate, "22.00") // (900 + 90) / 45
		wantMoney(t, "overtime premium", sumKind(res, LineOvertime), "55.00")
		req.Earnings[0].Treatment = Treatment{Exclude, "29 CFR 778.211(b) discretionary"}
		res = mustCalc(t, req, fedFLSA())
		wantMoney(t, "regular rate without bonus", res.Periods[0].RegularRate, "20.00")
		wantMoney(t, "bonus still paid", sumKind(res, LineEarning), "90.00")
		req.Earnings[0].Treatment = Treatment{}
		if _, err := Calculate(req, fedFLSA()); !errors.Is(err, ErrTreatmentUndeclared) {
			t.Fatalf("undeclared bonus treatment = %v", err)
		}
	})

	t.Run("California seventh consecutive day is 1.5x then 2x, not ordinary overtime", func(t *testing.T) {
		res := mustCalc(t, request(timeprofile.OvertimeSingleRate, 7, daily(m("20.00"), 8, 8, 8, 8, 8, 8, 10)...), california())
		wantBuckets(t, res.Totals, 58*Hour, 40*Hour, 16*Hour, 2*Hour)
		wantBuckets(t, res.Days[6].Buckets, 10*Hour, 0, 8*Hour, 2*Hour)
		if res.Days[6].Rule != ReasonSeventhDay {
			t.Fatalf("day 7 rule = %s", res.Days[6].Rule)
		}
		wantMoney(t, "gross", res.GrossPay, "1360.00") // 40x20 + 16x30 + 2x40
		// Six days do not trigger it.
		res = mustCalc(t, request(timeprofile.OvertimeSingleRate, 7, daily(m("20.00"), 8, 8, 8, 8, 8, 0, 10)...), california())
		if res.Totals.DoubleTime != 0 {
			t.Fatalf("seventh-day rule applied without seven consecutive days: %+v", res.Totals)
		}
	})

	t.Run("piece-rate rest and nonproductive time are paid separately (Labor Code 226.2)", func(t *testing.T) {
		res := mustCalc(t, pieceRestWeek(), california())
		wantBuckets(t, res.Totals, 44*Hour, 40*Hour, 4*Hour, 0)
		np, ok := line(res, LineNonproductive)
		if !ok {
			t.Fatal("no nonproductive line")
		}
		wantMoney(t, "nonproductive at floor", np.Amount, "33.80")
		rest, ok := line(res, LineRest)
		if !ok || rest.Amount <= 0 {
			t.Fatalf("rest time unpaid: %+v", rest)
		}
		// Average rate = (800 + 33.80) / 42 = 19.852 -> 19.85.
		wantMoney(t, "rest rate", rest.Rate, "19.85")
		wantMoney(t, "rest pay", rest.Amount, "39.70")
		wantMoney(t, "regular rate", res.Periods[0].RegularRate, "19.85") // 873.50 / 44
		wantMoney(t, "overtime premium", sumKind(res, LineOvertime), "39.72")
	})

	t.Run("daily overtime never also counts toward weekly overtime", func(t *testing.T) {
		res := mustCalc(t, request(timeprofile.OvertimeSingleRate, 7, daily(m("20.00"), 10, 10, 10, 10, 10)...), california())
		wantBuckets(t, res.Totals, 50*Hour, 40*Hour, 10*Hour, 0)
		res = mustCalc(t, request(timeprofile.OvertimeSingleRate, 7, daily(m("20.00"), 10, 10, 10, 10, 10, 6)...), california())
		wantBuckets(t, res.Totals, 56*Hour, 40*Hour, 16*Hour, 0)
		res = mustCalc(t, request(timeprofile.OvertimeSingleRate, 7, daily(m("20.00"), 13)...), california())
		wantBuckets(t, res.Totals, 13*Hour, 8*Hour, 4*Hour, Hour)
	})

	t.Run("daily-overtime states", func(t *testing.T) {
		res := mustCalc(t, request(timeprofile.OvertimeSingleRate, 7, daily(m("20.00"), 10)...), dailyEight())
		wantBuckets(t, res.Totals, 10*Hour, 8*Hour, 2*Hour, 0)
		res = mustCalc(t, request(timeprofile.OvertimeSingleRate, 7, daily(m("20.00"), 10)...), dailyTwelve())
		wantBuckets(t, res.Totals, 10*Hour, 10*Hour, 0, 0)
		res = mustCalc(t, request(timeprofile.OvertimeSingleRate, 7, daily(m("20.00"), 13)...), dailyTwelve())
		wantBuckets(t, res.Totals, 13*Hour, 12*Hour, Hour, 0)
		res = mustCalc(t, request(timeprofile.OvertimeSingleRate, 7, daily(m("20.00"), 10, 10, 10, 10, 10)...), fedFLSA())
		wantBuckets(t, res.Totals, 50*Hour, 40*Hour, 10*Hour, 0)
	})

	t.Run("alternative workweek schedule moves the daily threshold", func(t *testing.T) {
		req := request(timeprofile.OvertimeSingleRate, 7, daily(m("20.00"), 10, 10, 10, 10)...)
		req.Alternative = &AlternativeSchedule{ID: "AWS-4x10", Days: []ScheduledDay{{0, 10 * Hour}, {1, 10 * Hour}, {2, 10 * Hour}, {3, 10 * Hour}}}
		res := mustCalc(t, req, california())
		wantBuckets(t, res.Totals, 40*Hour, 40*Hour, 0, 0)
		req.Intervals = daily(m("20.00"), 11, 10, 10, 10, 9)
		res = mustCalc(t, req, california())
		// Day 1: 1h over schedule; day 5 is unscheduled: 1h past 8; weekly regular capped at 40.
		wantBuckets(t, res.Totals, 50*Hour, 40*Hour, 10*Hour, 0)
		if _, err := Calculate(req, fedFLSA()); !errors.Is(err, ErrRuleDisabled) {
			t.Fatalf("alternative schedule under a set that forbids it = %v", err)
		}
	})

	t.Run("healthcare 8/80 uses the 14-day period and the daily threshold", func(t *testing.T) {
		req := request(timeprofile.OvertimeHealthcare880, 14, daily(m("30.00"), 10, 8, 8, 8, 8, 8, 0, 8, 8, 8, 8)...)
		res := mustCalc(t, req, healthcare880())
		wantBuckets(t, res.Totals, 82*Hour, 80*Hour, 2*Hour, 0)
		if len(res.Periods) != 1 {
			t.Fatalf("8/80 periods = %d", len(res.Periods))
		}
		// The same work under the weekly FLSA rule is 10 hours of overtime.
		req.Method = timeprofile.OvertimeSingleRate
		res = mustCalc(t, req, fedFLSA())
		wantBuckets(t, res.Totals, 82*Hour, 72*Hour, 10*Hour, 0)
	})

	t.Run("7(k) work period", func(t *testing.T) {
		hours := make([]int, 28)
		for i := 0; i < 20; i++ {
			hours[i+i/3] = 9
		}
		res := mustCalc(t, request(timeprofile.OvertimePublicSafety7k, 28, daily(m("40.00"), hours...)...), publicSafety7k())
		wantBuckets(t, res.Totals, 180*Hour, 171*Hour, 9*Hour, 0)
	})

	t.Run("7(o) comp time accrues to the cap and the rest is cash", func(t *testing.T) {
		req := request(timeprofile.OvertimePublicCompTime, 7, daily(m("30.00"), 9, 9, 9, 9, 9)...)
		req.CompTimeBefore = 238 * Hour
		res := mustCalc(t, req, publicComp7o())
		c := res.CompTime
		if c.ConvertedOvertime != 80*60 || c.Accrued != 2*Hour || c.BalanceAfter != 240*Hour {
			t.Fatalf("comp time = %+v", c)
		}
		ot, _ := line(res, LineOvertime)
		if ot.Seconds != 5*Hour-80*60 {
			t.Fatalf("cash overtime = %s", ot.Seconds)
		}
		// Converted hours are not paid straight time in cash.
		wantMoney(t, "straight time", sumKind(res, LineStraightTime), "1310.00") // (45h - 1h20m) x 30
		req.CompTimeBefore = 0
		res = mustCalc(t, req, publicComp7o())
		if res.CompTime.Accrued != 7*Hour+30*60 || sumKind(res, LineOvertime) != 0 {
			t.Fatalf("uncapped comp time = %+v", res.CompTime)
		}
	})

	t.Run("DST days are measured by instants", func(t *testing.T) {
		req := request(timeprofile.OvertimeSingleRate, 7,
			Interval{ID: "fall-back", AggregationKey: "worker-1", Start: at("2026-11-01 00:00"), End: at("2026-11-01 09:00"), Zone: tz, RateCode: "BASE", Rate: m("20.00"), Kind: KindProductive})
		req.PeriodStart = Date{2026, time.October, 26}
		res := mustCalc(t, req, california())
		wantBuckets(t, res.Totals, 10*Hour, 8*Hour, 2*Hour, 0)
		req = request(timeprofile.OvertimeSingleRate, 7,
			Interval{ID: "spring", AggregationKey: "worker-1", Start: at("2026-03-08 00:00"), End: at("2026-03-08 09:00"), Zone: tz, RateCode: "BASE", Rate: m("20.00"), Kind: KindProductive})
		req.PeriodStart = Date{2026, time.March, 2}
		res = mustCalc(t, req, california())
		wantBuckets(t, res.Totals, 8*Hour, 8*Hour, 0, 0)
	})

	t.Run("overnight shift splits at the workday boundary", func(t *testing.T) {
		res := mustCalc(t, request(timeprofile.OvertimeSingleRate, 7, work("night", "2026-01-05 18:00", "2026-01-06 06:00", m("20.00"))), california())
		wantBuckets(t, res.Days[0].Buckets, 6*Hour, 6*Hour, 0, 0)
		wantBuckets(t, res.Days[1].Buckets, 6*Hour, 6*Hour, 0, 0)
	})

	t.Run("concurrent assignments aggregate under one key", func(t *testing.T) {
		req := twoRateWeek()
		res := mustCalc(t, req, fedFLSA())
		if res.Totals.Overtime != 5*Hour {
			t.Fatalf("assignments not aggregated: %+v", res.Totals)
		}
		req.Intervals[3].AggregationKey = "other"
		if _, err := Calculate(req, fedFLSA()); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("foreign aggregation key = %v", err)
		}
	})

	t.Run("CBA override may only be more favorable", func(t *testing.T) {
		cba, err := california().WithOverride(Override{Source: "CBA-7 art. 12", DailyOTAfter: 7 * Hour, OvertimeFactor: 16000})
		if err != nil {
			t.Fatal(err)
		}
		res := mustCalc(t, request(timeprofile.OvertimeSingleRate, 7, daily(m("20.00"), 8)...), cba)
		wantBuckets(t, res.Totals, 8*Hour, 7*Hour, Hour, 0)
		wantMoney(t, "premium at 1.6x", sumKind(res, LineOvertime), "12.00")
		if !strings.Contains(res.RuleSetVersion, "CBA-7") {
			t.Fatalf("override not traced in version %q", res.RuleSetVersion)
		}
		for _, o := range []Override{
			{Source: "x", DailyOTAfter: 9 * Hour},
			{Source: "x", OvertimeFactor: 12000},
			{Source: "x", PeriodOTAfter: -1},
		} {
			if _, err := california().WithOverride(o); !errors.Is(err, ErrLessFavorable) {
				t.Fatalf("override %+v = %v", o, err)
			}
		}
		if _, err := california().WithOverride(Override{}); !errors.Is(err, ErrInvalidRuleSet) {
			t.Fatalf("unsourced override = %v", err)
		}
		// A daily threshold the base set lacks may be added.
		added, err := fedFLSA().WithOverride(Override{Source: "x", DailyOTAfter: 8 * Hour})
		if err != nil || added.Daily.OTAfter != 8*Hour {
			t.Fatalf("adding a daily threshold = %v", err)
		}
	})

	t.Run("exempt method pays no premium", func(t *testing.T) {
		res := mustCalc(t, request(timeprofile.OvertimeNone, 7, daily(m("20.00"), 12, 12, 12, 12)...), fedFLSA())
		wantBuckets(t, res.Totals, 48*Hour, 48*Hour, 0, 0)
	})

	t.Run("labor handoff", func(t *testing.T) {
		res := mustCalc(t, request(timeprofile.OvertimeSingleRate, 7, daily(m("20.00"), 8, 8, 8, 8, 8, 8, 10)...), california())
		wh, err := res.WageHours(2, values.RoundingHalfUp)
		if err != nil {
			t.Fatal(err)
		}
		if wh.Regular.String() != "40.00" || wh.Overtime.String() != "16.00" || wh.DoubleTime.String() != "2.00" {
			t.Fatalf("wage hours = %s %s %s", wh.Regular, wh.Overtime, wh.DoubleTime)
		}
		if !strings.Contains(Explain(res), "double_time=2:00:00") || Version() != 1 {
			t.Fatalf("explain = %s", Explain(res))
		}
	})

	t.Run("rejections", func(t *testing.T) {
		cases := []struct {
			name  string
			mut   func(*Request, *RuleSet)
			want  error
			field string
		}{
			{"overlap", func(r *Request, _ *RuleSet) {
				r.Intervals = append(r.Intervals, work("dup", "2026-01-05 08:00", "2026-01-05 09:00", m("20.00")))
			}, ErrOverlap, "intervals.dup"},
			{"outside period", func(r *Request, _ *RuleSet) {
				r.Intervals = append(r.Intervals, work("late", "2026-01-12 08:00", "2026-01-12 09:00", m("20.00")))
			}, ErrOutsidePeriod, "intervals.late"},
			{"unknown differential", func(r *Request, _ *RuleSet) { r.Intervals[0].Differentials = []string{"MOON"} }, ErrUnknownDifferential, "intervals.d2026-01-05"},
			{"method not allowed", func(r *Request, _ *RuleSet) { r.Method = timeprofile.OvertimeFluctuatingWeek }, ErrMethodNotAllowed, "method"},
			{"single rate with two rates", func(r *Request, _ *RuleSet) {
				r.Intervals = append(r.Intervals, work("x", "2026-01-06 18:00", "2026-01-06 19:00", m("25.00")))
			}, ErrInvalidRequest, "intervals.x"},
			{"fractional-second punch", func(r *Request, _ *RuleSet) { r.Intervals[0].End = r.Intervals[0].End.Add(time.Millisecond) }, ErrInvalidRequest, "intervals.d2026-01-05"},
			{"undeclared kind", func(r *Request, _ *RuleSet) { r.Intervals[0].Kind = "" }, ErrInvalidRequest, "intervals.d2026-01-05"},
			{"bad zone", func(r *Request, _ *RuleSet) { r.Workweek.Zone = "Mars/Olympus" }, ErrInvalidRequest, "zone"},
			{"wrong start day", func(r *Request, _ *RuleSet) { r.PeriodStart = Date{2026, time.January, 6} }, ErrInvalidRequest, "period_start"},
			{"partial period", func(r *Request, _ *RuleSet) { r.Days = 10 }, ErrInvalidRequest, "days"},
			{"rule set incomplete", func(_ *Request, rs *RuleSet) { rs.Rounding = values.RoundingUnspecified }, ErrInvalidRuleSet, "rounding"},
			{"piece earnings without piece method", func(r *Request, _ *RuleSet) { r.Intervals[0].PieceEarnings = m("5.00") }, ErrInvalidRequest, "intervals.d2026-01-05"},
			{"on-call without rule", func(r *Request, rs *RuleSet) {
				rs.OnCall = OnCallRule{}
				r.OnCall = []OnCallPeriod{{ID: "oc", Start: at("2026-01-06 18:00"), End: at("2026-01-06 20:00")}}
			}, ErrRuleDisabled, "on_call"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				req := request(timeprofile.OvertimeSingleRate, 7, daily(m("20.00"), 8)...)
				rules := california()
				tc.mut(&req, &rules)
				_, err := Calculate(req, rules)
				if !errors.Is(err, tc.want) {
					t.Fatalf("err = %v, want %v", err, tc.want)
				}
				var rej *Rejection
				if !errors.As(err, &rej) || rej.Field != tc.field {
					t.Fatalf("rejection field = %+v, want %q", rej, tc.field)
				}
			})
		}
	})

	t.Run("fluctuating workweek below the minimum wage is refused", func(t *testing.T) {
		req := request(timeprofile.OvertimeFluctuatingWeek, 7, daily(0, 12, 12, 12, 12, 12)...)
		req.WeeklySalary = m("400.00") // 400 / 60 = 6.67 < 7.25
		if _, err := Calculate(req, fedFLSA()); !errors.Is(err, ErrBelowMinimumWage) {
			t.Fatalf("err = %v", err)
		}
	})
}

// pieceRestWeek is a California piece-rate week: 40 productive hours
// earning $800, 2 nonproductive hours with no rate and 2 hours of rest.
func pieceRestWeek() Request {
	ivs := daily(0, 8, 8, 8, 8, 8)
	for i := range ivs {
		ivs[i].PieceEarnings = m("160.00")
		ivs[i].PieceUnits = 400
		ivs[i].RateCode = "PIECE"
	}
	ivs = append(ivs,
		Interval{ID: "np", AggregationKey: "worker-1", Start: at(dayStr(5) + " 08:00"), End: at(dayStr(5) + " 10:00"), Zone: tz, RateCode: "NONPROD", Kind: KindNonproductive},
		Interval{ID: "rest", AggregationKey: "worker-1", Start: at(dayStr(5) + " 10:00"), End: at(dayStr(5) + " 12:00"), Zone: tz, RateCode: "REST", Kind: KindRest},
	)
	return request(timeprofile.OvertimePieceRateAverage, 7, ivs...)
}

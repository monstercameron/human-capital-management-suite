package timecalc

import (
	"errors"
	"math/rand/v2"
	"strconv"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
)

var fiveRates = []string{"17.00", "17.25", "18.50", "21.75", "26.40"}

// randomPeriod builds non-overlapping intervals over days at up to five
// rates, sometimes crossing midnight.
func randomPeriod(rng *rand.Rand, days int) (Request, Money, Money) {
	var ivs []Interval
	lo, hi := Money(0), Money(0)
	for d := 0; d < days; d++ {
		if rng.IntN(5) == 0 {
			continue
		}
		cursor := at(dayStr(d) + " 06:00").Add(time.Duration(rng.IntN(120)) * time.Minute)
		count := 1 + rng.IntN(3)
		if d == days-1 {
			count = 1 // nothing may run past the end of the period
		}
		for k := 0; k < count; k++ {
			length := time.Duration(30+rng.IntN(300)) * time.Minute
			rate := m(fiveRates[rng.IntN(len(fiveRates))])
			if lo == 0 || rate < lo {
				lo = rate
			}
			hi = max(hi, rate)
			ivs = append(ivs, Interval{ID: "i" + strconv.Itoa(d) + "-" + strconv.Itoa(k), AggregationKey: "worker-1", Assignment: "a" + strconv.Itoa(k),
				Start: cursor, End: cursor.Add(length), Zone: tz, RateCode: "R" + rate.String(), Rate: rate, Kind: KindProductive})
			cursor = cursor.Add(length + time.Duration(rng.IntN(90))*time.Minute)
		}
	}
	return request(timeprofile.OvertimeWeightedAverage, days, ivs...), lo, hi
}

// TestTodo_WTIME_013_Property checks, over many generated periods and every
// hourly rule set, that classification partitions worked time exactly, that
// the weighted-average regular rate lies between the lowest and highest
// rate, and that the result does not depend on input order.
func TestTodo_WTIME_013_Property(t *testing.T) {
	rng := rand.New(rand.NewPCG(13, 2026))
	ca := california()
	ca.SplitShift.Enabled = false // the premium is an included non-hourly payment; the rate bound is for hourly pay alone
	sets := []RuleSet{fedFLSA(), ca, dailyEight(), dailyTwelve()}
	for n := 0; n < 400; n++ {
		rules := sets[n%len(sets)]
		req, lo, hi := randomPeriod(rng, 14)
		res := mustCalc(t, req, rules)
		var worked Seconds
		for _, iv := range req.Intervals {
			worked += Seconds(iv.End.Unix() - iv.Start.Unix())
		}
		tot := res.Totals
		if tot.Worked != worked || tot.Regular+tot.Overtime+tot.DoubleTime != worked {
			t.Fatalf("case %d (%s): buckets %+v do not partition worked %s", n, rules.ID, tot, worked)
		}
		var days Buckets
		for _, d := range res.Days {
			days = days.plus(d.Buckets)
		}
		if days != tot {
			t.Fatalf("case %d: day buckets %+v differ from totals %+v", n, days, tot)
		}
		for _, p := range res.Periods {
			if p.Buckets.Regular > rules.Period.OTAfter {
				t.Fatalf("case %d: period regular %s exceeds threshold", n, p.Buckets.Regular)
			}
			if p.Buckets.Worked > 0 && (p.RegularRate < lo || p.RegularRate > hi) {
				t.Fatalf("case %d: regular rate %s outside [%s, %s]", n, p.RegularRate, lo, hi)
			}
		}
		for _, d := range res.Days {
			if rules.Daily.OTAfter > 0 && d.Rule == ReasonDaily && d.Buckets.Regular > rules.Daily.OTAfter {
				t.Fatalf("case %d: day %d regular %s exceeds daily threshold", n, d.Day, d.Buckets.Regular)
			}
		}
		shuffled := req
		shuffled.Intervals = append([]Interval(nil), req.Intervals...)
		rng.Shuffle(len(shuffled.Intervals), func(i, j int) {
			shuffled.Intervals[i], shuffled.Intervals[j] = shuffled.Intervals[j], shuffled.Intervals[i]
		})
		if again := mustCalc(t, shuffled, rules); again.Digest != res.Digest {
			t.Fatalf("case %d: result depends on input order", n)
		}
	}
}

// TestTodo_WTIME_013_Mutation kills the RED mutants: each engine behaviour
// the todo names is shown to be load-bearing, and a tampered result is
// detected.
func TestTodo_WTIME_013_Mutation(t *testing.T) {
	two := mustCalc(t, twoRateWeek(), fedFLSA())
	rr := two.Periods[0].RegularRate
	if rr == m("15.00") || rr == m("20.00") {
		t.Fatalf("mutant survived: regular rate %s is one of the base rates", rr)
	}

	bonus := request(timeprofile.OvertimeSingleRate, 7, daily(m("20.00"), 9, 9, 9, 9, 9)...)
	bonus.Earnings = []EarningItem{{Code: "B", Amount: m("90.00"), Treatment: Treatment{Include, "778.211(c)"}}}
	withBonus := mustCalc(t, bonus, fedFLSA())
	bonus.Earnings[0].Treatment.Inclusion = Exclude
	if mustCalc(t, bonus, fedFLSA()).Periods[0].RegularRate == withBonus.Periods[0].RegularRate {
		t.Fatal("mutant survived: bonus inclusion does not move the regular rate")
	}

	seventh := request(timeprofile.OvertimeSingleRate, 7, daily(m("20.00"), 8, 8, 8, 8, 8, 8, 10)...)
	ca := california()
	full := mustCalc(t, seventh, ca)
	ca.SeventhDay.Enabled = false
	plain := mustCalc(t, seventh, ca)
	if plain.GrossPay >= full.GrossPay || plain.Totals.DoubleTime != 0 {
		t.Fatalf("mutant survived: seventh-day rule is not load-bearing (%s vs %s)", plain.GrossPay, full.GrossPay)
	}

	ca = california()
	paid := mustCalc(t, pieceRestWeek(), ca)
	ca.PieceRest.Enabled = false
	unpaid := mustCalc(t, pieceRestWeek(), ca)
	if unpaid.GrossPay >= paid.GrossPay {
		t.Fatal("mutant survived: 226.2 rest pay is not load-bearing")
	}

	ca = california()
	base := mustCalc(t, request(timeprofile.OvertimeSingleRate, 7, daily(m("20.00"), 9)...), ca)
	ca.Daily.OTAfter += 1
	if mustCalc(t, request(timeprofile.OvertimeSingleRate, 7, daily(m("20.00"), 9)...), ca).Totals == base.Totals {
		t.Fatal("mutant survived: daily threshold off by one second is invisible")
	}

	tampered := []func(*Result){
		func(r *Result) { r.Periods[0].Buckets.Regular += 1 },
		func(r *Result) { r.Periods[0].Lines[0].Amount += 1 },
		func(r *Result) { r.Totals.Overtime += 1 },
		func(r *Result) { r.Trace[0].Params = "forged" },
		func(r *Result) { r.Periods[0].RegularRate += 1 },
	}
	for i, mutate := range tampered {
		r := mustCalc(t, twoRateWeek(), fedFLSA())
		r.Periods = append([]PeriodResult(nil), r.Periods...)
		r.Periods[0].Lines = append([]PayLine(nil), r.Periods[0].Lines...)
		r.Trace = append([]TraceEntry(nil), r.Trace...)
		mutate(&r)
		if err := r.Validate(); !errors.Is(err, ErrResultTampered) {
			t.Fatalf("tamper %d undetected: %v", i, err)
		}
	}
}

// BenchmarkTodo_WTIME_013 classifies and prices a two-week period with five
// rates, differentials and a bonus under the California rule set.
func BenchmarkTodo_WTIME_013(b *testing.B) {
	rng := rand.New(rand.NewPCG(7, 7))
	req, _, _ := randomPeriod(rng, 14)
	req.Method = timeprofile.OvertimeWeightedAverage
	for i := range req.Intervals {
		if i%3 == 0 {
			req.Intervals[i].Differentials = []string{"NIGHT"}
		}
	}
	req.Earnings = []EarningItem{{Code: "BONUS", Period: 1, Amount: m("120.00"), Treatment: Treatment{Include, "778.211(c)"}}}
	rules := california()
	if _, err := Calculate(req, rules); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Calculate(req, rules); err != nil {
			b.Fatal(err)
		}
	}
}

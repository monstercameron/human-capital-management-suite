package rules

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func t21Money(t *testing.T, text string) Money {
	t.Helper()
	m, err := ParseMoney(text)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func t21BaseRules() RuleSet {
	return RuleSet{
		ID: "US-FLSA", Version: "2026.1", Jurisdiction: "US", Citation: "29 CFR 778",
		Methods: []timeprofile.OvertimeMethod{timeprofile.OvertimeWeightedAverage, timeprofile.OvertimeSingleRate},
		Period:  PeriodRule{Days: 7, OTAfter: 40 * Hour}, OvertimeFactor: 15000, DoubleTimeFactor: 20000,
		RateDecimals: 2, PayDecimals: 2, Rounding: values.RoundingHalfUp,
	}
}

func t21Request(ivs ...Interval) Request {
	return Request{
		CalculationID: "t21-calc", AggregationKey: "worker-1", Method: timeprofile.OvertimeWeightedAverage,
		Workweek:    Workweek{Zone: "UTC", StartWeekday: time.Monday},
		PeriodStart: Date{Year: 2026, Month: time.January, Day: 5}, Days: 7, Intervals: ivs,
	}
}

// TestTodo_WTIME_013 proves the façade preserves weighted regular-rate
// calculation and exclusive regular/overtime classification.
func TestTodo_WTIME_013(t *testing.T) {
	start := time.Date(2026, time.January, 5, 8, 0, 0, 0, time.UTC)
	firstEnd := start.Add(30 * time.Hour)
	secondEnd := firstEnd.Add(15 * time.Hour)
	result, err := Calculate(t21Request(
		Interval{ID: "low", AggregationKey: "worker-1", Start: start, End: firstEnd, Zone: "UTC", Rate: t21Money(t, "15.00"), Kind: KindProductive},
		Interval{ID: "high", AggregationKey: "worker-1", Start: firstEnd, End: secondEnd, Zone: "UTC", Rate: t21Money(t, "20.00"), Kind: KindProductive},
	), t21BaseRules())
	if err != nil {
		t.Fatal(err)
	}
	if result.Totals.Regular != 40*Hour || result.Totals.Overtime != 5*Hour || result.Totals.DoubleTime != 0 {
		t.Fatalf("buckets = %+v", result.Totals)
	}
	if result.Periods[0].RegularRate != t21Money(t, "16.67") {
		t.Fatalf("regular rate = %s, want 16.67", result.Periods[0].RegularRate)
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("result validation = %v", err)
	}
}

// TestTodo_WTIME_013_Property proves the weighted rate remains bounded by the
// participating hourly rates while the classified buckets partition work.
func TestTodo_WTIME_013_Property(t *testing.T) {
	for _, rates := range [][2]string{{"12.00", "18.00"}, {"17.25", "26.40"}, {"8.00", "8.01"}} {
		start := time.Date(2026, time.January, 5, 8, 0, 0, 0, time.UTC)
		mid := start.Add(2 * time.Hour)
		end := mid.Add(2 * time.Hour)
		result, err := Calculate(t21Request(
			Interval{ID: "a", AggregationKey: "worker-1", Start: start, End: mid, Zone: "UTC", Rate: t21Money(t, rates[0]), Kind: KindProductive},
			Interval{ID: "b", AggregationKey: "worker-1", Start: mid, End: end, Zone: "UTC", Rate: t21Money(t, rates[1]), Kind: KindProductive},
		), t21BaseRules())
		if err != nil {
			t.Fatal(err)
		}
		if result.Totals.Worked != result.Totals.Regular+result.Totals.Overtime+result.Totals.DoubleTime {
			t.Fatalf("rates %v do not partition work: %+v", rates, result.Totals)
		}
		if result.Periods[0].RegularRate < t21Money(t, rates[0]) || result.Periods[0].RegularRate > t21Money(t, rates[1]) {
			t.Fatalf("rates %v produced out-of-range regular rate %s", rates, result.Periods[0].RegularRate)
		}
	}
}

// TestTodo_WTIME_013_Golden pins the façade's canonical audit prefix and
// digest-bearing rendering contract.
func TestTodo_WTIME_013_Golden(t *testing.T) {
	start := time.Date(2026, time.January, 5, 8, 0, 0, 0, time.UTC)
	result, err := Calculate(t21Request(Interval{ID: "one", AggregationKey: "worker-1", Start: start, End: start.Add(time.Hour), Zone: "UTC", Rate: t21Money(t, "20.00"), Kind: KindProductive}), t21BaseRules())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.Render(), "timecalc v1\ncalculation t21-calc ruleset=US-FLSA/2026.1 jurisdiction=US method=WEIGHTED_AVERAGE\n") {
		t.Fatalf("unexpected canonical rendering:\n%s", result.Render())
	}
	if result.Digest == "" {
		t.Fatal("result digest is empty")
	}
}

// TestTodo_WTIME_013_Mutation proves the result digest detects a changed
// classified bucket after calculation.
func TestTodo_WTIME_013_Mutation(t *testing.T) {
	start := time.Date(2026, time.January, 5, 8, 0, 0, 0, time.UTC)
	result, err := Calculate(t21Request(Interval{ID: "one", AggregationKey: "worker-1", Start: start, End: start.Add(time.Hour), Zone: "UTC", Rate: t21Money(t, "20.00"), Kind: KindProductive}), t21BaseRules())
	if err != nil {
		t.Fatal(err)
	}
	result.Totals.Regular++
	if err := result.Validate(); !errors.Is(err, ErrResultTampered) {
		t.Fatalf("tampered result error = %v", err)
	}
}

// TestTodo_WTIME_014 proves engaged-to-wait on-call time is classified as
// worked and a shift differential contributes a distinct pay line.
func TestTodo_WTIME_014(t *testing.T) {
	rules := t21BaseRules()
	rules.OnCall = OnCallRule{Enabled: true, OnPremisesEngaged: true}
	rules.Differentials = []DifferentialRule{{Tag: "NIGHT", PerHour: t21Money(t, "2.00"), Treatment: Treatment{Inclusion: Include, Basis: "29 CFR 778.207"}}}
	start := time.Date(2026, time.January, 5, 22, 0, 0, 0, time.UTC)
	workEnd := start.Add(time.Hour)
	result, err := Calculate(Request{
		CalculationID: "t21-premium", AggregationKey: "worker-1", Method: timeprofile.OvertimeSingleRate,
		Workweek: Workweek{Zone: "UTC", StartWeekday: time.Monday}, PeriodStart: Date{Year: 2026, Month: time.January, Day: 5}, Days: 7,
		Intervals: []Interval{{ID: "work", AggregationKey: "worker-1", Start: start, End: workEnd, Zone: "UTC", Rate: t21Money(t, "20.00"), Kind: KindProductive, Differentials: []string{"NIGHT"}}},
		OnCall:    []OnCallPeriod{{ID: "oc", Start: workEnd, End: workEnd.Add(time.Hour), Rate: t21Money(t, "20.00"), Restrictions: Restrictions{OnPremises: true}}},
	}, rules)
	if err != nil {
		t.Fatal(err)
	}
	if result.Totals.Worked != 2*Hour {
		t.Fatalf("worked = %s, want 2:00:00", result.Totals.Worked)
	}
	var differential, straight bool
	for _, line := range result.Periods[0].Lines {
		differential = differential || line.Kind == LineDifferential
		straight = straight || line.Kind == LineStraightTime
	}
	if !differential || !straight {
		t.Fatalf("premium lines missing differential=%t straight=%t: %+v", differential, straight, result.Periods[0].Lines)
	}
}

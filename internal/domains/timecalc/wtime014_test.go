package timecalc

import (
	"errors"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
)

func onCall(id, start, end string, rate Money, rs Restrictions) OnCallPeriod {
	return OnCallPeriod{ID: id, Start: at(start), End: at(end), RateCode: "ON_CALL", Rate: rate, Restrictions: rs}
}

func shift(id, start, end string, reported bool) ScheduledShift {
	return ScheduledShift{ID: id, Start: at(start), End: at(end), Reported: reported, Rate: m("20.00")}
}

// TestTodo_WTIME_014 proves each premium: on-call classification,
// reporting-time and call-back minimums, split shift and differentials
// flowing into the regular rate.
func TestTodo_WTIME_014(t *testing.T) {
	week := func(rate string) []Interval { return daily(m(rate), 8, 8, 8, 8, 8) }

	t.Run("restricted on-call time is paid as hours worked", func(t *testing.T) {
		req := request(timeprofile.OvertimeSingleRate, 7, week("20.00")...)
		req.OnCall = []OnCallPeriod{onCall("sat", dayStr(5)+" 08:00", dayStr(5)+" 12:00", m("20.00"), Restrictions{OnPremises: true})}
		res := mustCalc(t, req, california())
		wantBuckets(t, res.Totals, 44*Hour, 40*Hour, 4*Hour, 0)
		if !strings.Contains(res.Render(), "state=ENGAGED_TO_WAIT criterion=on_premises") {
			t.Fatal("on-call classification not traced")
		}
		req.OnCall[0].Restrictions = Restrictions{ResponseMinutes: 10}
		if res = mustCalc(t, req, california()); res.Totals.Worked != 44*Hour {
			t.Fatalf("10-minute response not engaged: %+v", res.Totals)
		}
		req.OnCall[0].Rate = 0
		if _, err := Calculate(req, california()); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("engaged on-call with no rate = %v", err)
		}
	})

	t.Run("free on-call time is not worked but standby pay enters the regular rate", func(t *testing.T) {
		req := request(timeprofile.OvertimeSingleRate, 7, week("20.00")...)
		req.OnCall = []OnCallPeriod{onCall("home", dayStr(5)+" 08:00", dayStr(5)+" 12:00", m("20.00"), Restrictions{ResponseMinutes: 60})}
		res := mustCalc(t, req, fedFLSA())
		wantBuckets(t, res.Totals, 40*Hour, 40*Hour, 0, 0)
		wantMoney(t, "standby", sumKind(res, LineStandby), "12.00")
		wantMoney(t, "regular rate", res.Periods[0].RegularRate, "20.30") // (800 + 12) / 40
		req.OnCall[0].Restrictions.ExpectedCalls = 4
		if res = mustCalc(t, req, fedFLSA()); res.Totals.Worked != 44*Hour {
			t.Fatalf("frequent calls not engaged: %+v", res.Totals)
		}
	})

	t.Run("work inside an on-call period is counted once", func(t *testing.T) {
		req := request(timeprofile.OvertimeWeightedAverage, 7, work("callout", "2026-01-06 19:00", "2026-01-06 20:00", m("25.00")))
		req.OnCall = []OnCallPeriod{onCall("eve", "2026-01-06 18:00", "2026-01-06 22:00", m("20.00"), Restrictions{OnPremises: true})}
		res := mustCalc(t, req, fedFLSA())
		wantBuckets(t, res.Totals, 4*Hour, 4*Hour, 0, 0)
		wantMoney(t, "straight time", sumKind(res, LineStraightTime), "85.00") // 3 x 20 + 1 x 25
		req.OnCall[0].Restrictions = Restrictions{}
		res = mustCalc(t, req, fedFLSA())
		wantBuckets(t, res.Totals, Hour, Hour, 0, 0)
		if l, _ := line(res, LineStandby); l.Seconds != 3*Hour {
			t.Fatalf("standby hours = %s", l.Seconds)
		}
	})

	t.Run("California reporting time pays half the scheduled day, 2 to 4 hours", func(t *testing.T) {
		req := request(timeprofile.OvertimeSingleRate, 7, work("w", "2026-01-05 08:00", "2026-01-05 09:00", m("20.00")))
		req.Shifts = []ScheduledShift{shift("s8", "2026-01-05 08:00", "2026-01-05 16:00", true)}
		res := mustCalc(t, req, california())
		l, ok := line(res, LineReporting)
		if !ok || l.Seconds != 3*Hour {
			t.Fatalf("sent home after one hour of eight: %+v", l)
		}
		wantMoney(t, "reporting pay", l.Amount, "60.00")
		if res.Totals.Worked != Hour || res.PremiumSeconds != 3*Hour || l.InRegularRate {
			t.Fatalf("reporting time counted as worked or included: %+v", res.Totals)
		}
		req.Shifts = []ScheduledShift{shift("s3", "2026-01-05 08:00", "2026-01-05 11:00", true)}
		if l, _ = line(mustCalc(t, req, california()), LineReporting); l.Seconds != Hour {
			t.Fatalf("3-hour shift guarantee: %+v", l)
		}
		req.Intervals[0].End = at("2026-01-05 09:36") // 1.6h >= half of 3h
		if _, ok = line(mustCalc(t, req, california()), LineReporting); ok {
			t.Fatal("reporting pay owed after working half the shift")
		}
		req.Shifts[0].Reported = false
		req.Intervals[0].End = at("2026-01-05 09:00")
		if _, ok = line(mustCalc(t, req, california()), LineReporting); ok {
			t.Fatal("reporting pay owed for an unreported shift")
		}
	})

	t.Run("New York call-in pay is four hours or the shift, at minimum wage", func(t *testing.T) {
		req := request(timeprofile.OvertimeSingleRate, 7, work("w", "2026-01-05 08:00", "2026-01-05 09:00", m("20.00")))
		req.Shifts = []ScheduledShift{shift("s3", "2026-01-05 08:00", "2026-01-05 11:00", true)}
		l, _ := line(mustCalc(t, req, newYork()), LineReporting)
		if l.Seconds != 2*Hour || l.Amount != m("33.00") {
			t.Fatalf("NY call-in = %+v", l)
		}
		req.Shifts = []ScheduledShift{shift("s8", "2026-01-05 08:00", "2026-01-05 16:00", true)}
		req.Intervals[0].End = at("2026-01-05 13:00")
		if _, ok := line(mustCalc(t, req, newYork()), LineReporting); ok {
			t.Fatal("NY call-in owed after four hours")
		}
	})

	t.Run("call back pays the minimum", func(t *testing.T) {
		cb := work("cb", "2026-01-05 20:00", "2026-01-05 20:30", m("20.00"))
		cb.CallBackID = "cb-1"
		res := mustCalc(t, request(timeprofile.OvertimeSingleRate, 7, append(week("20.00"), cb)...), california())
		l, ok := line(res, LineCallBack)
		if !ok || l.Seconds != 90*60 || l.Amount != m("30.00") {
			t.Fatalf("call back = %+v", l)
		}
		if _, err := Calculate(request(timeprofile.OvertimeSingleRate, 7, cb), newYork()); !errors.Is(err, ErrRuleDisabled) {
			t.Fatalf("call back without a rule = %v", err)
		}
	})

	t.Run("split shift premium with the minimum-wage offset", func(t *testing.T) {
		split := func(rate string) Request {
			return request(timeprofile.OvertimeSingleRate, 7,
				work("am", "2026-01-05 08:00", "2026-01-05 12:00", m(rate)),
				work("pm", "2026-01-05 14:00", "2026-01-05 18:00", m(rate)))
		}
		res := mustCalc(t, split("16.90"), california())
		wantMoney(t, "split premium at minimum wage", sumKind(res, LineSplitShift), "16.90")
		res = mustCalc(t, split("17.50"), california())
		wantMoney(t, "split premium offset", sumKind(res, LineSplitShift), "12.10") // 16.90 x 9 - 17.50 x 8
		res = mustCalc(t, split("30.00"), california())
		if _, ok := line(res, LineSplitShift); ok || !strings.Contains(res.Render(), "SPLIT_SHIFT day=0") {
			t.Fatal("well-paid split shift should be traced but not paid")
		}
		meal := request(timeprofile.OvertimeSingleRate, 7,
			work("am", "2026-01-05 08:00", "2026-01-05 12:00", m("16.90")),
			work("pm", "2026-01-05 13:00", "2026-01-05 17:00", m("16.90")))
		if _, ok := line(mustCalc(t, meal, california()), LineSplitShift); ok {
			t.Fatal("a one-hour meal is not a split shift")
		}
	})

	t.Run("shift differential flows into the regular rate", func(t *testing.T) {
		var ivs []Interval
		for d := 0; d < 5; d++ {
			iv := work("n"+dayStr(d), dayStr(d)+" 16:00", dayStr(d+1)+" 01:00", m("20.00"))
			iv.Differentials = []string{"NIGHT"}
			ivs = append(ivs, iv)
		}
		res := mustCalc(t, request(timeprofile.OvertimeSingleRate, 7, ivs...), fedFLSA())
		diff, _ := line(res, LineDifferential)
		if diff.Seconds != 15*Hour || diff.Amount != m("30.00") || !diff.InRegularRate {
			t.Fatalf("night differential = %+v", diff)
		}
		wantMoney(t, "regular rate", res.Periods[0].RegularRate, "20.67") // (900 + 30) / 45
		wantMoney(t, "overtime premium", sumKind(res, LineOvertime), "51.70")

		loc := request(timeprofile.OvertimeSingleRate, 7, daily(m("20.00"), 8)...)
		loc.Intervals[0].Differentials = []string{"LOC_SF"}
		l, _ := line(mustCalc(t, loc, california()), LineDifferential)
		if l.Rate != m("1.00") || l.Amount != m("8.00") {
			t.Fatalf("percent differential = %+v", l)
		}
	})
}

// TestTodo_WTIME_014_Property checks premium invariants over generated
// schedules: reporting pay tops worked time up to exactly the guarantee and
// never counts as worked; on-call time is counted once; a differential
// never lowers the regular rate.
func TestTodo_WTIME_014_Property(t *testing.T) {
	rng := rand.New(rand.NewPCG(14, 2026))
	start := at("2026-01-05 08:00")
	for n := 0; n < 300; n++ {
		sched := Seconds(4+rng.IntN(37)) * 15 * 60 // 1h..10h
		worked := Seconds(1+rng.IntN(int(sched/60))) * 60
		req := request(timeprofile.OvertimeSingleRate, 7, Interval{ID: "w", AggregationKey: "worker-1", Start: start,
			End: start.Add(time.Duration(worked) * time.Second), Zone: tz, RateCode: "BASE", Rate: m("20.00"), Kind: KindProductive})
		req.Shifts = []ScheduledShift{{ID: "s", Start: start, End: start.Add(time.Duration(sched) * time.Second), Reported: true}}
		ca := mustCalc(t, req, california())
		guarantee := min(max(sched/2, 2*Hour), 4*Hour)
		l, paid := line(ca, LineReporting)
		switch {
		case ca.Totals.Worked != worked:
			t.Fatalf("case %d: reporting time changed worked hours", n)
		case worked*2 < sched && worked < guarantee && (!paid || worked+l.Seconds != guarantee):
			t.Fatalf("case %d: sched %s worked %s paid %s, guarantee %s", n, sched, worked, l.Seconds, guarantee)
		case (worked*2 >= sched || worked >= guarantee) && paid:
			t.Fatalf("case %d: paid reporting time with worked %s of %s", n, worked, sched)
		}
		ny := mustCalc(t, req, newYork())
		nyG := min(4*Hour, sched)
		l, paid = line(ny, LineReporting)
		if (worked < nyG) != paid || (paid && worked+l.Seconds != nyG) {
			t.Fatalf("case %d: NY sched %s worked %s paid %+v", n, sched, worked, l)
		}
	}
	for n := 0; n < 300; n++ {
		ocStart := start.Add(time.Duration(rng.IntN(600)) * time.Minute)
		ocEnd := ocStart.Add(time.Duration(30+rng.IntN(600)) * time.Minute)
		wStart := start.Add(time.Duration(rng.IntN(900)) * time.Minute)
		wEnd := wStart.Add(time.Duration(15+rng.IntN(300)) * time.Minute)
		req := request(timeprofile.OvertimeWeightedAverage, 7, Interval{ID: "w", AggregationKey: "worker-1", Start: wStart, End: wEnd, Zone: tz, RateCode: "W", Rate: m("25.00"), Kind: KindProductive})
		engaged := rng.IntN(2) == 0
		req.OnCall = []OnCallPeriod{{ID: "oc", Start: ocStart, End: ocEnd, Rate: m("20.00"), Restrictions: Restrictions{OnPremises: engaged}}}
		res := mustCalc(t, req, fedFLSA())
		wLen, ocLen := wEnd.Unix()-wStart.Unix(), ocEnd.Unix()-ocStart.Unix()
		both := overlap(wStart.Unix(), wEnd.Unix(), ocStart.Unix(), ocEnd.Unix())
		standby, _ := line(res, LineStandby)
		if engaged && (res.Totals.Worked != Seconds(wLen+ocLen-both) || standby.Seconds != 0) {
			t.Fatalf("case %d: engaged worked %s, want union %d", n, res.Totals.Worked, wLen+ocLen-both)
		}
		if !engaged && (res.Totals.Worked != Seconds(wLen) || standby.Seconds != Seconds(ocLen-both)) {
			t.Fatalf("case %d: waiting worked %s standby %s", n, res.Totals.Worked, standby.Seconds)
		}
	}
	for n := 0; n < 100; n++ {
		req, _, _ := randomPeriod(rng, 7)
		plain := mustCalc(t, req, fedFLSA())
		for i := range req.Intervals {
			req.Intervals[i].Differentials = []string{"NIGHT"}
		}
		tagged := mustCalc(t, req, fedFLSA())
		if tagged.Periods[0].RegularRate < plain.Periods[0].RegularRate || tagged.GrossPay < plain.GrossPay {
			t.Fatalf("case %d: differential lowered pay", n)
		}
	}
}

// premiumWeek exercises every WTIME-014 premium in one California week.
func premiumWeek() Request {
	ivs := daily(m("20.00"), 8, 8, 8, 8)
	ivs[1].Differentials = []string{"LOC_SF"}
	ivs = append(ivs,
		work("fri-am", dayStr(4)+" 06:00", dayStr(4)+" 10:00", m("16.90")),
		work("fri-pm", dayStr(4)+" 15:00", dayStr(4)+" 23:00", m("16.90")),
		work("sat", dayStr(5)+" 09:00", dayStr(5)+" 10:00", m("20.00")),
	)
	ivs[5].Differentials = []string{"NIGHT"}
	cb := work("cb", dayStr(2)+" 21:00", dayStr(2)+" 21:45", m("20.00"))
	cb.CallBackID = "wed-callback"
	ivs = append(ivs, cb)
	req := request(timeprofile.OvertimeWeightedAverage, 7, ivs...)
	req.OnCall = []OnCallPeriod{
		onCall("sun-site", dayStr(6)+" 08:00", dayStr(6)+" 11:00", m("18.00"), Restrictions{OnPremises: true}),
		onCall("sun-home", dayStr(6)+" 12:00", dayStr(6)+" 20:00", m("18.00"), Restrictions{ResponseMinutes: 45}),
	}
	req.Shifts = []ScheduledShift{shift("sat-shift", dayStr(5)+" 09:00", dayStr(5)+" 17:00", true)}
	return req
}

// TestTodo_WTIME_014_Golden pins a week with every premium kind.
func TestTodo_WTIME_014_Golden(t *testing.T) {
	res := mustCalc(t, premiumWeek(), california())
	for _, k := range []LineKind{LineReporting, LineCallBack, LineSplitShift, LineDifferential, LineOvertime} {
		if _, ok := line(res, k); !ok {
			t.Fatalf("golden week lacks a %s line", k)
		}
	}
	checkGolden(t, "wtime014_premium_week", res)
}

package timecalc

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
)

var update = flag.Bool("update", false, "rewrite the timecalc golden files")

// checkGolden pins the canonical rendering of a result byte for byte.
func checkGolden(t *testing.T, name string, res Result) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	got := []byte(res.Render())
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s drifted from golden:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// fwwMonth is the 29 CFR 778.114(b) example: a $600 salary for weeks of
// 37.5, 44, 50 and 48 hours.
func fwwMonth() Request {
	var ivs []Interval
	weeks := [][]int{{8, 8, 8, 8, 5}, {9, 9, 9, 9, 8}, {10, 10, 10, 10, 10}, {10, 10, 10, 10, 8}}
	for w, days := range weeks {
		for d, h := range days {
			s := at(dayStr(w*7+d) + " 07:00")
			end := s.Add(time.Duration(h) * time.Hour)
			if w == 0 && d == 4 {
				end = end.Add(30 * time.Minute) // 37.5 hours
			}
			ivs = append(ivs, Interval{ID: "fww-" + dayStr(w*7+d), AggregationKey: "worker-1", Start: s, End: end, Zone: tz, RateCode: "SALARY", Kind: KindProductive})
		}
	}
	req := request(timeprofile.OvertimeFluctuatingWeek, 28, ivs...)
	req.WeeklySalary = m("600.00")
	return req
}

// piece778111 is the 29 CFR 778.111(a) example: $491 of piece earnings for
// 46 productive hours plus 4 hours of waiting time at $8.00.
func piece778111() Request {
	var ivs []Interval
	hours := []int{10, 10, 10, 10, 6}
	for d, h := range hours {
		s := at(dayStr(d) + " 07:00")
		ivs = append(ivs, Interval{ID: "p" + dayStr(d), AggregationKey: "worker-1", Start: s, End: s.Add(time.Duration(h) * time.Hour),
			Zone: tz, RateCode: "PIECE", Kind: KindProductive, PieceUnits: 100, PieceEarnings: m("98.20")})
	}
	s := at(dayStr(5) + " 07:00")
	ivs = append(ivs, Interval{ID: "wait", AggregationKey: "worker-1", Start: s, End: s.Add(4 * time.Hour), Zone: tz, RateCode: "WAITING", Rate: m("8.00"), Kind: KindNonproductive})
	return request(timeprofile.OvertimePieceRateAverage, 7, ivs...)
}

// TestTodo_WTIME_013_Golden pins the worked regulatory examples: the
// figures are asserted directly and the full rendering (buckets, lines,
// trace) is pinned in testdata.
func TestTodo_WTIME_013_Golden(t *testing.T) {
	t.Run("778.114 fluctuating workweek", func(t *testing.T) {
		res := mustCalc(t, fwwMonth(), fedFLSA())
		wantRates := []string{"16.00", "13.64", "12.00", "12.50"}
		wantPay := []string{"600.00", "627.28", "660.00", "650.00"}
		for i, p := range res.Periods {
			wantMoney(t, "regular rate", p.RegularRate, wantRates[i])
			var pay Money
			for _, l := range p.Lines {
				pay += l.Amount
			}
			wantMoney(t, "weekly pay", pay, wantPay[i])
		}
		checkGolden(t, "wtime013_fww_778_114", res)
	})
	t.Run("778.111 piece rate", func(t *testing.T) {
		res := mustCalc(t, piece778111(), fedFLSA())
		wantMoney(t, "regular rate", res.Periods[0].RegularRate, "10.46")
		ot, _ := line(res, LineOvertime)
		wantMoney(t, "half-time rate", ot.Rate, "5.23")
		wantMoney(t, "overtime", ot.Amount, "52.30")
		wantMoney(t, "total", res.GrossPay, "575.30")
		checkGolden(t, "wtime013_piece_778_111", res)
	})
	t.Run("778.115 weighted average", func(t *testing.T) {
		checkGolden(t, "wtime013_weighted_778_115", mustCalc(t, twoRateWeek(), fedFLSA()))
	})
	t.Run("California seventh day", func(t *testing.T) {
		res := mustCalc(t, request(timeprofile.OvertimeSingleRate, 7, daily(m("20.00"), 8, 8, 8, 8, 8, 8, 10)...), california())
		checkGolden(t, "wtime013_ca_seventh_day", res)
	})
}

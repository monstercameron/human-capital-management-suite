package worktimerules

import (
	"fmt"
	"testing"
	"time"
)

func euDirectiveDefaults() EURuleSet {
	return EURuleSet{
		Name:                    "EU-2003-88-EC",
		DailyRestMinutes:        11 * 60,
		WeeklyRestMinutes:       24 * 60,
		BreakAfterMinutes:       6 * 60,
		NightWorkLimitMinutes:   8 * 60,
		WeeklyAverageCapMinutes: 48 * 60,
		ReferencePeriod:         17 * 7 * 24 * time.Hour,
	}
}

func TestTodo_WTIME_017(t *testing.T) {
	rules := euDirectiveDefaults()

	// A shift published with only 9 hours' rest after the previous one
	// breaches the 11-hour daily rest requirement.
	windows := Windows{DayMinutes: 8 * 60, ReferencePeriodMinutes: 40 * 17 * 60, HasLastRestEnd: true}
	minutesSinceRest := 15 * 60 // worked or awake 15h since the rest ended, leaving only 9h until the next shift in a 24h day
	eval, err := EvaluateRest(windows, minutesSinceRest, rules)
	if err != nil {
		t.Fatalf("EvaluateRest: %v", err)
	}
	hasBreach := false
	for _, f := range eval.Findings {
		if f == FindingDailyRestBreach {
			hasBreach = true
		}
	}
	if !hasBreach {
		t.Fatalf("expected FindingDailyRestBreach, got %v", eval.Findings)
	}

	// The 48-hour weekly average is computed over the configured
	// reference period, not a single week: a single heavy week inside a
	// long, otherwise light reference period must not breach.
	lightAverage := Windows{ReferencePeriodMinutes: 40 * 17 * 60, HasLastRestEnd: true} // 40h/week average
	evalLight, err := EvaluateRest(lightAverage, 8*60, rules)
	if err != nil {
		t.Fatalf("EvaluateRest light: %v", err)
	}
	for _, f := range evalLight.Findings {
		if f == FindingWeeklyAverageCap {
			t.Fatalf("did not expect a weekly-average breach at a 40h/week average, got %v", evalLight.Findings)
		}
	}

	heavyAverage := Windows{ReferencePeriodMinutes: 55 * 17 * 60, HasLastRestEnd: true} // 55h/week average
	evalHeavy, err := EvaluateRest(heavyAverage, 8*60, rules)
	if err != nil {
		t.Fatalf("EvaluateRest heavy: %v", err)
	}
	heavyBreach := false
	for _, f := range evalHeavy.Findings {
		if f == FindingWeeklyAverageCap {
			heavyBreach = true
		}
	}
	if !heavyBreach {
		t.Fatalf("expected FindingWeeklyAverageCap at a 55h/week average, got %v", evalHeavy.Findings)
	}

	// A withdrawn UK opt-out keeps applying during its notice period and
	// stops applying once the notice period elapses.
	withdrawnAt := mustTime(t, "2026-09-01T00:00:00Z")
	optOut := OptOutDocument{
		WorkerID: "w1", SignedAt: mustTime(t, "2026-01-01T00:00:00Z"),
		NoticePeriod: 30 * 24 * time.Hour, WithdrawnAt: &withdrawnAt,
	}
	stillActive, err := optOut.IsActive(mustTime(t, "2026-09-15T00:00:00Z"))
	if err != nil {
		t.Fatalf("IsActive during notice: %v", err)
	}
	if !stillActive {
		t.Fatal("expected the opt-out to still apply during its notice period")
	}
	noLongerActive, err := optOut.IsActive(mustTime(t, "2026-10-05T00:00:00Z"))
	if err != nil {
		t.Fatalf("IsActive after notice: %v", err)
	}
	if noLongerActive {
		t.Fatal("expected the opt-out to stop applying once the notice period elapsed")
	}

	// Daily recording applies to every EU worker regardless of exemption.
	if !RequiresDailyRecording(true) {
		t.Fatal("expected the EU daily recording duty to apply")
	}

	// Right-to-disconnect: a non-urgent shift-change message sent outside
	// the allowed window is suppressed; an urgent one is never suppressed.
	policy := DisconnectionPolicy{Jurisdiction: "FR", WindowStartMinute: 8 * 60, WindowEndMinute: 19 * 60}
	lateNight := mustTime(t, "2026-09-22T22:00:00Z")
	if !SuppressNotification(policy, lateNight, false) {
		t.Fatal("expected a non-urgent late-night notification to be suppressed")
	}
	if SuppressNotification(policy, lateNight, true) {
		t.Fatal("expected an urgent notification to never be suppressed")
	}
}

func TestTodo_WTIME_017_Golden(t *testing.T) {
	rules := euDirectiveDefaults()
	var got string
	for _, avgPerWeek := range []int{35, 48, 55} {
		w := Windows{ReferencePeriodMinutes: avgPerWeek * 17 * 60, HasLastRestEnd: true}
		eval, err := EvaluateRest(w, 8*60, rules)
		if err != nil {
			t.Fatalf("EvaluateRest %d: %v", avgPerWeek, err)
		}
		got += fmt.Sprintf("avg_per_week=%d weekly_average_minutes=%d findings=%v\n", avgPerWeek, eval.WeeklyAverageMinutes, eval.Findings)
	}
	accrual, err := RolledUpHolidayAccrualMinutes(40*60, 1207)
	if err != nil {
		t.Fatalf("RolledUpHolidayAccrualMinutes: %v", err)
	}
	got += fmt.Sprintf("rolled_up_accrual_minutes=%d\n", accrual)
	compareGolden(t, "eu_working_time.golden", got)
}

func TestTodo_WTIME_017_Property(t *testing.T) {
	// Property: RolledUpHolidayAccrualMinutes is monotonic in worked
	// minutes for a fixed permille, and never exceeds the worked minutes
	// themselves for any permille at or below 10000 (100%).
	prev := -1
	for mins := 0; mins <= 5000; mins += 137 {
		got, err := RolledUpHolidayAccrualMinutes(mins, 1207)
		if err != nil {
			t.Fatalf("mins=%d: %v", mins, err)
		}
		if got < prev {
			t.Fatalf("accrual not monotonic: at mins=%d got %d, previous was %d", mins, got, prev)
		}
		if got > mins {
			t.Fatalf("accrual %d exceeds worked minutes %d at 12.07%%", got, mins)
		}
		prev = got
	}
}

func TestTodo_WTIME_017_Security(t *testing.T) {
	// An invalid rule set (missing thresholds) is rejected rather than
	// silently evaluated against zero limits, which would report no
	// findings for arbitrarily large working time.
	if _, err := EvaluateRest(Windows{}, 0, EURuleSet{}); err == nil {
		t.Fatal("expected EvaluateRest to reject an empty rule set")
	}

	// An opt-out asserted for no worker is rejected.
	forged := OptOutDocument{SignedAt: mustTime(t, "2026-01-01T00:00:00Z")}
	if _, err := forged.IsActive(mustTime(t, "2026-02-01T00:00:00Z")); err == nil {
		t.Fatal("expected an opt-out with no worker_id to be rejected")
	}

	// A withdrawn opt-out with no notice period (unsourced withdrawal) is
	// rejected rather than treated as an immediate, ungoverned withdrawal.
	withdrawnAt := mustTime(t, "2026-02-01T00:00:00Z")
	badWithdrawal := OptOutDocument{WorkerID: "w1", SignedAt: mustTime(t, "2026-01-01T00:00:00Z"), WithdrawnAt: &withdrawnAt}
	if _, err := badWithdrawal.IsActive(mustTime(t, "2026-03-01T00:00:00Z")); err == nil {
		t.Fatal("expected a withdrawn opt-out with no notice period to be rejected")
	}

	// Negative worked minutes cannot be laundered into a negative accrual.
	if _, err := RolledUpHolidayAccrualMinutes(-10, 1207); err == nil {
		t.Fatal("expected negative worked minutes to be rejected")
	}
}

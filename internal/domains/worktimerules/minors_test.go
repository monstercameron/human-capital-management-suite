package worktimerules

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files instead of comparing against them")

// flsa570 is a fixture rule pack for 14-15 year olds under 29 CFR 570.35,
// used only in tests per the lane brief.
func flsa570() MinorRuleSet {
	return MinorRuleSet{
		Name:             "FLSA-570.35-14-15",
		Bands:            []AgeBandRule{{Band: "14-15", MinAgeYears: 14, MaxAgeYears: 15}, {Band: "16-17", MinAgeYears: 16, MaxAgeYears: 17}},
		DailyCapMinutes:  map[string]int{"14-15": 3 * 60, "16-17": 8 * 60},
		WeeklyCapMinutes: map[string]int{"14-15": 18 * 60, "16-17": 40 * 60},
		SchoolSessions:   []DateRange{{From: mustDate("2026-08-01"), To: mustDate("2026-12-20")}},
		Windows: []TimeWindowRule{
			{Band: "14-15", SchoolPeriod: true, EarliestMinute: 7 * 60, LatestMinute: 19 * 60},
			{Band: "14-15", SchoolPeriod: false, EarliestMinute: 7 * 60, LatestMinute: 21 * 60},
			{Band: "16-17", SchoolPeriod: true, EarliestMinute: 6 * 60, LatestMinute: 22 * 60},
			{Band: "16-17", SchoolPeriod: false, EarliestMinute: 6 * 60, LatestMinute: 23 * 60},
		},
		Hazardous:              []HazardousTaskRestriction{{Band: "14-15", TaskCode: "MEAT_SLICER"}, {Band: "16-17", TaskCode: "FORKLIFT"}},
		PermitRequired:         map[string]bool{"14-15": true, "16-17": true},
		ApproachingCapFraction: 0.9,
	}
}

func mustDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestTodo_WTIME_015(t *testing.T) {
	rules := flsa570()
	worker := MinorWorker{
		TenantID: "t1", WorkerID: "minor-1", BirthDate: mustDate("2011-03-01"), // 15 at shift date below
		Permit: &PermitEvidence{WorkerID: "minor-1", ID: "permit-1", Source: "state-labor-board", IssuedAt: mustDate("2026-01-01")},
	}
	// A school night shift running until 8pm violates the 7pm window.
	shiftStart := mustTime(t, "2026-09-22T17:00:00Z") // Tuesday, in session
	shift := ScheduledShift{Start: shiftStart, End: shiftStart.Add(3 * time.Hour)}
	decision, err := EvaluateSchedulePublication(worker, shift, rules, Windows{})
	if err != nil {
		t.Fatalf("EvaluateSchedulePublication: %v", err)
	}
	if !decision.Blocked {
		t.Fatal("expected the 8pm-ending school-night shift to be blocked")
	}
	found := false
	for _, v := range decision.Violations {
		if v == ViolationTimeWindow {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected ViolationTimeWindow, got %v", decision.Violations)
	}

	// A compliant shift within the window and caps publishes clean.
	goodShift := ScheduledShift{Start: mustTime(t, "2026-09-22T14:00:00Z"), End: mustTime(t, "2026-09-22T17:00:00Z")}
	clean, err := EvaluateSchedulePublication(worker, goodShift, rules, Windows{})
	if err != nil {
		t.Fatalf("EvaluateSchedulePublication clean: %v", err)
	}
	if clean.Blocked {
		t.Fatalf("expected a compliant shift to publish, got violations %v", clean.Violations)
	}

	// Missing permit blocks scheduling where required.
	unpermitted := worker
	unpermitted.Permit = nil
	blocked, err := EvaluateSchedulePublication(unpermitted, goodShift, rules, Windows{})
	if err != nil {
		t.Fatalf("EvaluateSchedulePublication unpermitted: %v", err)
	}
	if !blocked.Blocked {
		t.Fatal("expected missing permit to block scheduling")
	}

	// Age is read at the shift date, not "today": a worker born such that
	// they are 16 by the shift date resolves to the 16-17 band even if a
	// different band would apply on some other date.
	olderWorker := worker
	olderWorker.BirthDate = mustDate("2010-09-20") // turns 16 on 2026-09-20, before the shift
	band, err := AgeBandAt(olderWorker.BirthDate, shiftStart, rules.Bands)
	if err != nil {
		t.Fatalf("AgeBandAt: %v", err)
	}
	if band != "16-17" {
		t.Fatalf("age band at shift date = %s, want 16-17", band)
	}

	// Clock-in outside the window is recorded, never rejected, and raises
	// an immediate supervisor exception.
	outcome, err := EvaluateClockIn(worker, mustTime(t, "2026-09-22T19:30:00Z"), rules, Windows{})
	if err != nil {
		t.Fatalf("EvaluateClockIn: %v", err)
	}
	if !outcome.Recorded {
		t.Fatal("a minor's out-of-window punch must still be Recorded")
	}
	if !outcome.SupervisorException {
		t.Fatal("expected an immediate supervisor exception for the out-of-window punch")
	}

	// Approaching-cap warning: worker already near the daily cap.
	near, err := EvaluateClockIn(worker, mustTime(t, "2026-09-22T14:00:00Z"), rules, Windows{DayMinutes: 165}) // 2h45 of 3h cap = 91.7%
	if err != nil {
		t.Fatalf("EvaluateClockIn near cap: %v", err)
	}
	if !near.ApproachingDailyCap {
		t.Fatal("expected an approaching-daily-cap warning")
	}

	// Hazardous task is blocked outright regardless of hours.
	hazardShift := ScheduledShift{Start: goodShift.Start, End: goodShift.End, TaskCodes: []string{"MEAT_SLICER"}}
	hazard, err := EvaluateSchedulePublication(worker, hazardShift, rules, Windows{})
	if err != nil {
		t.Fatalf("EvaluateSchedulePublication hazard: %v", err)
	}
	if !hazard.Blocked {
		t.Fatal("expected a hazardous task assignment to block publication")
	}
}

func TestTodo_WTIME_015_Golden(t *testing.T) {
	rules := flsa570()
	worker := MinorWorker{
		TenantID: "t1", WorkerID: "minor-1", BirthDate: mustDate("2011-03-01"),
		Permit: &PermitEvidence{WorkerID: "minor-1", ID: "permit-1", Source: "state-labor-board", IssuedAt: mustDate("2026-01-01")},
	}
	shift := ScheduledShift{Start: mustTime(t, "2026-09-22T17:00:00Z"), End: mustTime(t, "2026-09-22T20:00:00Z")}
	decision, err := EvaluateSchedulePublication(worker, shift, rules, Windows{})
	if err != nil {
		t.Fatalf("EvaluateSchedulePublication: %v", err)
	}
	got := fmt.Sprintf("band=%s blocked=%v violations=%v\n", decision.Band, decision.Blocked, decision.Violations)
	compareGolden(t, "minor_publication_decision.golden", got)
}

func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with -update to create it): %v", path, err)
	}
	if !bytes.Equal(want, []byte(got)) {
		t.Fatalf("golden mismatch for %s:\n got:  %q\n want: %q", name, got, want)
	}
}

func TestTodo_WTIME_015_Property(t *testing.T) {
	// Property: for any shift whose minutes plus the existing day window
	// exceed the daily cap, publication is always blocked with
	// ViolationDailyCap present, regardless of the other fields varied.
	rules := flsa570()
	worker := MinorWorker{
		TenantID: "t1", WorkerID: "minor-1", BirthDate: mustDate("2011-03-01"),
		Permit: &PermitEvidence{WorkerID: "minor-1", ID: "permit-1", Source: "state-labor-board", IssuedAt: mustDate("2026-01-01")},
	}
	for trial := 0; trial < 20; trial++ {
		mins := 30 + trial*10 // grows past the 180-minute cap
		start := mustTime(t, "2026-09-22T10:00:00Z")
		shift := ScheduledShift{Start: start, End: start.Add(time.Duration(mins) * time.Minute)}
		decision, err := EvaluateSchedulePublication(worker, shift, rules, Windows{})
		if err != nil {
			t.Fatalf("trial %d: %v", trial, err)
		}
		if mins > 180 {
			hasDailyCap := false
			for _, v := range decision.Violations {
				if v == ViolationDailyCap {
					hasDailyCap = true
				}
			}
			if !decision.Blocked || !hasDailyCap {
				t.Fatalf("trial %d: shift of %d minutes should breach the 180-minute daily cap; got blocked=%v violations=%v", trial, mins, decision.Blocked, decision.Violations)
			}
		}
	}
}

func TestTodo_WTIME_015_Security(t *testing.T) {
	rules := flsa570()
	// Forged permit: evidence recorded for a different worker is rejected.
	forged := MinorWorker{
		TenantID: "t1", WorkerID: "minor-1", BirthDate: mustDate("2011-03-01"),
		Permit: &PermitEvidence{WorkerID: "someone-else", ID: "permit-1", Source: "state-labor-board", IssuedAt: mustDate("2026-01-01")},
	}
	shift := ScheduledShift{Start: mustTime(t, "2026-09-22T14:00:00Z"), End: mustTime(t, "2026-09-22T16:00:00Z")}
	if _, err := EvaluateSchedulePublication(forged, shift, rules, Windows{}); err == nil {
		t.Fatal("expected forged permit evidence (mismatched worker_id) to be rejected")
	}

	// Unsourced permit (no Source) is rejected too.
	unsourced := forged
	unsourced.Permit = &PermitEvidence{WorkerID: "minor-1", ID: "permit-1", IssuedAt: mustDate("2026-01-01")}
	if _, err := EvaluateSchedulePublication(unsourced, shift, rules, Windows{}); err == nil {
		t.Fatal("expected unsourced permit evidence to be rejected")
	}

	// Age must not be forgeable through a today-relative computation: two
	// evaluations of the same worker at two different shift dates must be
	// able to resolve to two different bands, and AgeYearsAt never reads
	// the wall clock.
	worker := MinorWorker{TenantID: "t1", WorkerID: "minor-2", BirthDate: mustDate("2012-01-01")}
	youngShift := mustTime(t, "2026-09-22T14:00:00Z") // 14 years old
	oldShift := mustTime(t, "2028-09-22T14:00:00Z")   // 16 years old
	bandYoung, err := AgeBandAt(worker.BirthDate, youngShift, rules.Bands)
	if err != nil {
		t.Fatalf("AgeBandAt young: %v", err)
	}
	bandOld, err := AgeBandAt(worker.BirthDate, oldShift, rules.Bands)
	if err != nil {
		t.Fatalf("AgeBandAt old: %v", err)
	}
	if bandYoung == bandOld {
		t.Fatalf("expected different age bands at different shift dates, got %s both times", bandYoung)
	}
}

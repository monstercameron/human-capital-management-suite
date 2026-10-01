package timecompliance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func t22Time(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return parsed
}

func minorRules() MinorRulePack {
	return MinorRulePack{
		Name: "US-CA-2026",
		Bands: []AgeBand{{
			Name: "14-15", MinAge: 14, MaxAge: 15, DailyCapMinutes: 180, WeeklyCapMinutes: 1080,
			SchoolWindow: MinuteWindow{Start: 7 * 60, End: 19 * 60}, NonSchoolWindow: MinuteWindow{Start: 7 * 60, End: 21 * 60},
			HazardousTaskCode: []string{"MEAT_SLICER"}, PermitRequired: true,
		}, {
			Name: "16-17", MinAge: 16, MaxAge: 17, DailyCapMinutes: 480, WeeklyCapMinutes: 2400,
			SchoolWindow: MinuteWindow{Start: 6 * 60, End: 22 * 60}, NonSchoolWindow: MinuteWindow{Start: 6 * 60, End: 23 * 60},
		}},
		SchoolSessions: []SchoolSession{{From: t22TimeNoPanic("2026-08-01"), To: t22TimeNoPanic("2026-12-20")}}, ApproachingCapFraction: .9,
	}
}

func t22TimeNoPanic(value string) time.Time {
	parsed, _ := time.Parse("2006-01-02", value)
	return parsed
}

func permit(worker string) *PermitEvidence {
	return &PermitEvidence{WorkerID: worker, EvidenceID: "permit-1", Source: "state-labor-board", IssuedAt: t22TimeNoPanic("2026-01-01")}
}

func TestTodo_WTIME_015(t *testing.T) {
	rules := minorRules()
	worker := MinorWorker{TenantID: "tenant-1", WorkerID: "minor-1", BirthDate: t22TimeNoPanic("2011-03-01"), Permit: permit("minor-1")}
	late := ScheduledShift{Interval: Interval{Start: t22Time(t, "2026-09-22T17:00:00Z"), End: t22Time(t, "2026-09-22T20:00:00Z")}}
	decision, err := EvaluateMinorSchedule(worker, late, rules, MinorScheduleEvidence{})
	if err != nil || !decision.Blocked || !hasMinorViolation(decision.Violations, ViolationTimeWindow) {
		t.Fatalf("school-night publication = %+v, err=%v; want blocked window violation", decision, err)
	}
	good := ScheduledShift{Interval: Interval{Start: t22Time(t, "2026-09-22T14:00:00Z"), End: t22Time(t, "2026-09-22T17:00:00Z")}}
	clean, err := EvaluateMinorSchedule(worker, good, rules, MinorScheduleEvidence{})
	if err != nil || clean.Blocked {
		t.Fatalf("compliant publication = %+v, err=%v; want allowed", clean, err)
	}
	missingPermit := worker
	missingPermit.Permit = nil
	blocked, err := EvaluateMinorSchedule(missingPermit, good, rules, MinorScheduleEvidence{})
	if err != nil || !hasMinorViolation(blocked.Violations, ViolationPermit) {
		t.Fatalf("missing permit = %+v, err=%v; want permit violation", blocked, err)
	}
	// The worker turns 16 before the shift, even if today's date were later.
	older := worker
	older.BirthDate = t22TimeNoPanic("2010-09-20")
	aged, err := EvaluateMinorSchedule(older, good, rules, MinorScheduleEvidence{})
	if err != nil || aged.AgeBand != "16-17" {
		t.Fatalf("age at shift date = %+v, err=%v; want 16-17", aged, err)
	}
	out, err := EvaluateMinorClockIn(worker, MinorClockInInput{PunchAt: t22Time(t, "2026-09-22T19:30:00Z")}, rules)
	if err != nil || !out.Recorded || !out.OutsideWindow || !out.SupervisorException {
		t.Fatalf("out-of-window clock-in = %+v, err=%v; want recorded exception", out, err)
	}
	near, err := EvaluateMinorClockIn(worker, MinorClockInInput{PunchAt: t22Time(t, "2026-09-22T14:00:00Z"), WorkedMinutesToday: 165}, rules)
	if err != nil || !near.ApproachingDailyCap {
		t.Fatalf("near-cap clock-in = %+v, err=%v; want warning", near, err)
	}
}

func hasMinorViolation(got []MinorViolation, want MinorViolation) bool {
	for _, item := range got {
		if item == want {
			return true
		}
	}
	return false
}

func TestTodo_WTIME_015_Golden(t *testing.T) {
	rules := minorRules()
	worker := MinorWorker{TenantID: "tenant-1", WorkerID: "minor-1", BirthDate: t22TimeNoPanic("2011-03-01"), Permit: permit("minor-1")}
	shift := ScheduledShift{Interval: Interval{Start: t22Time(t, "2026-09-22T17:00:00Z"), End: t22Time(t, "2026-09-22T20:00:00Z")}, TaskCodes: []string{"MEAT_SLICER"}}
	decision, err := EvaluateMinorSchedule(worker, shift, rules, MinorScheduleEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprintf("band=%s blocked=%v violations=%v\n", decision.AgeBand, decision.Blocked, decision.Violations)
	want := readT22Golden(t, "wtime015.golden")
	if got != want {
		t.Fatalf("golden mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestTodo_WTIME_015_Property(t *testing.T) {
	rules := minorRules()
	worker := MinorWorker{TenantID: "tenant-1", WorkerID: "minor-1", BirthDate: t22TimeNoPanic("2011-03-01"), Permit: permit("minor-1")}
	for minutes := 1; minutes <= 240; minutes++ {
		start := t22Time(t, "2026-09-22T14:00:00Z")
		decision, err := EvaluateMinorSchedule(worker, ScheduledShift{Interval: Interval{Start: start, End: start.Add(time.Duration(minutes) * time.Minute)}}, rules, MinorScheduleEvidence{})
		if err != nil {
			t.Fatal(err)
		}
		if minutes > 180 && !hasMinorViolation(decision.Violations, ViolationDailyCap) {
			t.Fatalf("%d-minute shift omitted daily-cap violation: %v", minutes, decision.Violations)
		}
	}
}

func TestTodo_WTIME_015_Security(t *testing.T) {
	rules := minorRules()
	shift := ScheduledShift{Interval: Interval{Start: t22Time(t, "2026-09-22T14:00:00Z"), End: t22Time(t, "2026-09-22T16:00:00Z")}}
	for _, worker := range []MinorWorker{
		{WorkerID: "minor-1", BirthDate: t22TimeNoPanic("2011-03-01")},
		{TenantID: "tenant-1", BirthDate: t22TimeNoPanic("2011-03-01")},
		{TenantID: "tenant-1", WorkerID: "minor-1", BirthDate: t22TimeNoPanic("2011-03-01"), Permit: &PermitEvidence{WorkerID: "other", EvidenceID: "p", Source: "x", IssuedAt: t22TimeNoPanic("2026-01-01")}},
	} {
		if _, err := EvaluateMinorSchedule(worker, shift, rules, MinorScheduleEvidence{}); err == nil {
			t.Fatal("expected missing scope or forged permit to fail closed")
		} else if !errors.Is(err, ErrMissingScope) && !errors.Is(err, ErrInvalidEvidence) {
			t.Fatalf("unexpected security error: %v", err)
		}
	}
}

func TestTodo_WTIME_016(t *testing.T) {
	base := func() TravelSegment {
		return TravelSegment{TenantID: "tenant-1", WorkerID: "worker-1", Interval: Interval{Start: t22Time(t, "2026-09-22T08:00:00Z"), End: t22Time(t, "2026-09-22T08:30:00Z")}}
	}
	commute := base()
	commute.HomeCommute = true
	c, err := ClassifyTravel(commute, TravelPolicy{})
	if err != nil || c.Paid || c.CountsTowardHours || c.Kind != TravelHomeCommute {
		t.Fatalf("ordinary commute = %+v, err=%v", c, err)
	}
	controlled := commute
	controlled.EmployerRequiredTransport = true
	cc, err := ClassifyTravel(controlled, TravelPolicy{Jurisdiction: "US-CA", CaliforniaEmployerControlledTravel: true})
	if err != nil || !cc.Paid || cc.Kind != TravelEmployerControlled {
		t.Fatalf("California controlled travel = %+v, err=%v", cc, err)
	}
	inter := base()
	inter.BetweenSites = true
	is, err := ClassifyTravel(inter, TravelPolicy{})
	if err != nil || !is.Paid || is.Minutes != 30 || is.Kind != TravelBetweenSites {
		t.Fatalf("inter-site travel = %+v, err=%v", is, err)
	}
	mileage := MileageExpense{TenantID: "tenant-1", WorkerID: "worker-1", SegmentStart: commute.Interval.Start, SegmentEnd: commute.Interval.End, MilesHundredths: 1250}
	if err := mileage.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_WTIME_016_Property(t *testing.T) {
	for _, paid := range []TravelSegment{
		{TenantID: "t", WorkerID: "w", Interval: Interval{Start: t22Time(t, "2026-09-22T08:00:00Z"), End: t22Time(t, "2026-09-22T08:20:00Z")}, BetweenSites: true},
		{TenantID: "t", WorkerID: "w", Interval: Interval{Start: t22Time(t, "2026-09-22T08:00:00Z"), End: t22Time(t, "2026-09-22T08:20:00Z")}, WorkPerformed: true},
	} {
		result, err := ClassifyTravel(paid, TravelPolicy{})
		if err != nil || !result.Paid || !result.CountsTowardHours {
			t.Fatalf("paid segment = %+v, err=%v", result, err)
		}
	}
}

func TestTodo_WTIME_016_Golden(t *testing.T) {
	segments := []TravelSegment{
		{TenantID: "t", WorkerID: "w", Interval: Interval{Start: t22Time(t, "2026-09-22T08:00:00Z"), End: t22Time(t, "2026-09-22T08:30:00Z")}, HomeCommute: true},
		{TenantID: "t", WorkerID: "w", Interval: Interval{Start: t22Time(t, "2026-09-22T12:00:00Z"), End: t22Time(t, "2026-09-22T12:45:00Z")}, BetweenSites: true},
		{TenantID: "t", WorkerID: "w", Interval: Interval{Start: t22Time(t, "2026-09-22T20:00:00Z"), End: t22Time(t, "2026-09-23T02:00:00Z")}, Overnight: true, NormalWorkStartMinute: 0, NormalWorkEndMinute: 24 * 60},
	}
	var lines []string
	for _, segment := range segments {
		result, err := ClassifyTravel(segment, TravelPolicy{})
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, fmt.Sprintf("kind=%s paid=%v hours=%v minutes=%d", result.Kind, result.Paid, result.CountsTowardHours, result.Minutes))
	}
	got := strings.Join(lines, "\n") + "\n"
	if want := readT22Golden(t, "wtime016.golden"); got != want {
		t.Fatalf("golden mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestTodo_WTIME_016_Security(t *testing.T) {
	segment := TravelSegment{WorkerID: "w", Interval: Interval{Start: t22Time(t, "2026-09-22T08:00:00Z"), End: t22Time(t, "2026-09-22T08:30:00Z")}}
	if _, err := ClassifyTravel(segment, TravelPolicy{}); !errors.Is(err, ErrMissingScope) {
		t.Fatalf("missing tenant should fail closed, got %v", err)
	}
	if err := (MileageExpense{TenantID: "t", WorkerID: "w", SegmentStart: segment.Interval.Start, SegmentEnd: segment.Interval.End, MilesHundredths: -1}).Validate(); err == nil {
		t.Fatal("negative mileage should be rejected")
	}
}

func euRules() WorkingTimeRules {
	return WorkingTimeRules{Name: "EU-2003-88-EC", DailyRestMinutes: 11 * 60, WeeklyRestMinutes: 24 * 60, BreakAfterMinutes: 6 * 60, NightWorkLimitMinutes: 8 * 60, WeeklyAverageCapMinutes: 48 * 60, ReferencePeriodWeeks: 17, NightWindow: MinuteWindow{Start: 22 * 60, End: 30 * 60}, UKIrregularHours: true, RolledUpHolidayPermille: 1207}
}

func TestTodo_WTIME_017(t *testing.T) {
	rules := euRules()
	previous := t22Time(t, "2026-09-22T23:00:00Z")
	current := t22Time(t, "2026-09-23T08:00:00Z")
	decision, err := EvaluateWorkingTime(WorkingTimeInput{TenantID: "t", WorkerID: "w", AsOf: current, PreviousShiftEnd: &previous, CurrentShiftStart: &current, WeeklyRestMinutesAvailable: 24 * 60, WorkedSinceBreakMinutes: 7 * 60, NightWorkMinutes: 9 * 60, ReferencePeriodWorkedMinutes: 55 * 17 * 60}, rules, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []WorkingTimeFinding{FindingDailyRestBreach, FindingBreakDue, FindingNightLimitBreach, FindingWeeklyAverageBreach} {
		if !hasFinding(decision.Findings, want) {
			t.Fatalf("findings=%v, missing %s", decision.Findings, want)
		}
	}
	if !decision.DailyRecordingRequired || decision.RolledUpHolidayMinutes != 0 {
		t.Fatalf("EU decision = %+v, want daily recording and no UK accrual without irregular-hours flag", decision)
	}
	// A signed UK opt-out suppresses only the average-cap finding.
	signed := t22Time(t, "2026-01-01T00:00:00Z")
	decision, err = EvaluateWorkingTime(WorkingTimeInput{TenantID: "t", WorkerID: "w", AsOf: current, WeeklyRestMinutesAvailable: 24 * 60, ReferencePeriodWorkedMinutes: 55 * 17 * 60, OptOut: &OptOutDocument{WorkerID: "w", SignedAt: signed}}, rules, true)
	if err != nil || !decision.OptOutActive || hasFinding(decision.Findings, FindingWeeklyAverageBreach) {
		t.Fatalf("active opt-out decision = %+v, err=%v", decision, err)
	}
	policy := DisconnectionPolicy{Jurisdiction: "FR", AllowedWindow: MinuteWindow{Start: 8 * 60, End: 19 * 60}}
	suppressed, err := SuppressNonUrgentNotification(policy, t22Time(t, "2026-09-23T22:00:00Z"), false)
	if err != nil || !suppressed {
		t.Fatalf("late non-urgent notification: suppressed=%v err=%v", suppressed, err)
	}
	urgent, err := SuppressNonUrgentNotification(policy, t22Time(t, "2026-09-23T22:00:00Z"), true)
	if err != nil || urgent {
		t.Fatalf("urgent notification: suppressed=%v err=%v", urgent, err)
	}
}

func hasFinding(got []WorkingTimeFinding, want WorkingTimeFinding) bool {
	for _, item := range got {
		if item == want {
			return true
		}
	}
	return false
}

func TestTodo_WTIME_017_Golden(t *testing.T) {
	rules := euRules()
	decision, err := EvaluateWorkingTime(WorkingTimeInput{TenantID: "t", WorkerID: "w", AsOf: t22Time(t, "2026-09-23T08:00:00Z"), WeeklyRestMinutesAvailable: 24 * 60, ReferencePeriodWorkedMinutes: 55 * 17 * 60, IrregularHoursWorker: true}, rules, true)
	if err != nil {
		t.Fatal(err)
	}
	accrual, err := RolledUpHolidayAccrualMinutes(40*60, 1207)
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprintf("average=%d findings=%v recording=%v accrual=%d\n", decision.WeeklyAverageMinutes, decision.Findings, decision.DailyRecordingRequired, accrual)
	if want := readT22Golden(t, "wtime017.golden"); got != want {
		t.Fatalf("golden mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestTodo_WTIME_017_Property(t *testing.T) {
	for minutes := 0; minutes <= 10000; minutes += 137 {
		accrual, err := RolledUpHolidayAccrualMinutes(minutes, 1207)
		if err != nil || accrual < 0 || accrual > minutes {
			t.Fatalf("minutes=%d accrual=%d err=%v", minutes, accrual, err)
		}
	}
}

func TestTodo_WTIME_017_Security(t *testing.T) {
	if _, err := EvaluateWorkingTime(WorkingTimeInput{TenantID: "t", WorkerID: "w", AsOf: t22Time(t, "2026-09-23T08:00:00Z")}, WorkingTimeRules{}, true); !errors.Is(err, ErrInvalidRules) {
		t.Fatalf("empty rules should fail closed, got %v", err)
	}
	withdrawn := t22Time(t, "2026-02-01T00:00:00Z")
	bad := OptOutDocument{WorkerID: "w", SignedAt: t22Time(t, "2026-01-01T00:00:00Z"), WithdrawnAt: &withdrawn}
	if _, err := OptOutActive(bad, "w", t22Time(t, "2026-03-01T00:00:00Z")); err == nil {
		t.Fatal("withdrawal without notice period should fail closed")
	}
	doc := OptOutDocument{WorkerID: "w", SignedAt: t22Time(t, "2026-01-01T00:00:00Z"), WithdrawnAt: &withdrawn, NoticePeriod: 30 * 24 * time.Hour}
	active, err := OptOutActive(doc, "w", t22Time(t, "2026-02-15T00:00:00Z"))
	if err != nil || !active {
		t.Fatalf("opt-out should remain active during notice: active=%v err=%v", active, err)
	}
	active, err = OptOutActive(doc, "w", t22Time(t, "2026-03-05T00:00:00Z"))
	if err != nil || active {
		t.Fatalf("opt-out should end after notice: active=%v err=%v", active, err)
	}
}

func readT22Golden(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	return string(data)
}

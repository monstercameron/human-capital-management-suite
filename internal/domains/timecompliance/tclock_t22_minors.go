// Package timecompliance contains pure, data-driven working-time compliance
// decisions. It does not persist punches or schedule changes: callers record
// the returned decision and its evidence in their owning workflow.
package timecompliance

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

var (
	ErrInvalidEvidence = errors.New("timecompliance: invalid evidence")
	ErrInvalidRules    = errors.New("timecompliance: invalid rule pack")
	ErrMissingScope    = errors.New("timecompliance: tenant and worker scope are required")
)

// Interval is an immutable sourced period of work or travel.
type Interval struct {
	Start time.Time
	End   time.Time
}

func (i Interval) validate() error {
	if i.Start.IsZero() || i.End.IsZero() || !i.End.After(i.Start) {
		return fmt.Errorf("%w: interval must be non-empty", ErrInvalidEvidence)
	}
	return nil
}

func (i Interval) minutes() int {
	return int(i.End.Sub(i.Start) / time.Minute)
}

func calendarDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func ageAt(birthDate, onDate time.Time) int {
	age := onDate.Year() - birthDate.Year()
	anniversary := time.Date(onDate.Year(), birthDate.Month(), birthDate.Day(), 0, 0, 0, 0, onDate.Location())
	if onDate.Before(anniversary) {
		age--
	}
	return age
}

// AgeYearsAt is the explicit-date age calculation used by schedule and
// clock-in callers. It never consults the wall clock.
func AgeYearsAt(birthDate, onDate time.Time) int { return ageAt(birthDate, onDate) }

// AgeBand is a rule-pack age range. MaxAge is inclusive; zero means no upper
// bound. All limits are supplied by the rule pack, never inferred here.
type AgeBand struct {
	Name              string
	MinAge            int
	MaxAge            int
	DailyCapMinutes   int
	WeeklyCapMinutes  int
	SchoolWindow      MinuteWindow
	NonSchoolWindow   MinuteWindow
	HazardousTaskCode []string
	PermitRequired    bool
}

func (b AgeBand) matches(age int) bool {
	return age >= b.MinAge && (b.MaxAge == 0 || age <= b.MaxAge)
}

// MinuteWindow is inclusive at the start and exclusive at the end. End may
// exceed 24 hours for a permitted window crossing midnight.
type MinuteWindow struct {
	Start int
	End   int
}

func (w MinuteWindow) validate() error {
	if w.Start < 0 || w.End <= w.Start || w.End > 48*60 {
		return fmt.Errorf("%w: invalid minute window %d-%d", ErrInvalidRules, w.Start, w.End)
	}
	return nil
}

// SchoolSession is an inclusive local-date range from a sourced school
// calendar. Seasonal variants are represented by separate ranges and rule
// windows selected by the session flag.
type SchoolSession struct {
	From time.Time
	To   time.Time
}

func (s SchoolSession) contains(day time.Time) bool {
	d := calendarDay(day).UTC()
	from := calendarDay(s.From).UTC()
	to := calendarDay(s.To).UTC()
	return !d.Before(from) && !d.After(to)
}

// MinorRulePack supplies the legal thresholds for one jurisdiction and
// effective period. It can hold a federal pack and a stricter state pack
// without changing the evaluator.
type MinorRulePack struct {
	Name                   string
	Bands                  []AgeBand
	SchoolSessions         []SchoolSession
	ApproachingCapFraction float64
}

func (p MinorRulePack) validate() error {
	if p.Name == "" || len(p.Bands) == 0 || p.ApproachingCapFraction <= 0 || p.ApproachingCapFraction > 1 {
		return fmt.Errorf("%w: name, bands and approaching_cap_fraction are required", ErrInvalidRules)
	}
	for i, b := range p.Bands {
		if b.Name == "" || b.MinAge < 0 || (b.MaxAge != 0 && b.MaxAge < b.MinAge) || b.DailyCapMinutes <= 0 || b.WeeklyCapMinutes <= 0 {
			return fmt.Errorf("%w: invalid age band %d", ErrInvalidRules, i)
		}
		if err := b.SchoolWindow.validate(); err != nil {
			return err
		}
		if err := b.NonSchoolWindow.validate(); err != nil {
			return err
		}
	}
	return nil
}

// MinorWorker is the tenant-scoped identity and permit evidence used by the
// schedule evaluator. A permit is deliberately checked by identity and
// source, not merely by presence.
type MinorWorker struct {
	TenantID  string
	WorkerID  string
	BirthDate time.Time
	Permit    *PermitEvidence
}

type PermitEvidence struct {
	WorkerID   string
	EvidenceID string
	Source     string
	IssuedAt   time.Time
}

func (w MinorWorker) validate() error {
	if w.TenantID == "" || w.WorkerID == "" {
		return ErrMissingScope
	}
	if w.BirthDate.IsZero() {
		return fmt.Errorf("%w: birth date is required", ErrInvalidEvidence)
	}
	if w.Permit != nil && (w.Permit.WorkerID != w.WorkerID || w.Permit.EvidenceID == "" || w.Permit.Source == "" || w.Permit.IssuedAt.IsZero()) {
		return fmt.Errorf("%w: permit does not bind the worker", ErrInvalidEvidence)
	}
	return nil
}

type ScheduledShift struct {
	Interval  Interval
	TaskCodes []string
}

type MinorScheduleEvidence struct {
	WorkedIntervals []Interval
}

type MinorViolation string

const (
	ViolationDailyCap      MinorViolation = "DAILY_CAP_EXCEEDED"
	ViolationWeeklyCap     MinorViolation = "WEEKLY_CAP_EXCEEDED"
	ViolationTimeWindow    MinorViolation = "OUTSIDE_TIME_WINDOW"
	ViolationHazardousTask MinorViolation = "HAZARDOUS_TASK"
	ViolationPermit        MinorViolation = "PERMIT_REQUIRED_MISSING"
)

type MinorScheduleDecision struct {
	AgeBand    string
	Blocked    bool
	Violations []MinorViolation
}

func applicableWindow(b AgeBand, shiftDate time.Time, sessions []SchoolSession) MinuteWindow {
	inSession := false
	for _, session := range sessions {
		if session.contains(shiftDate) {
			inSession = true
			break
		}
	}
	if inSession {
		return b.SchoolWindow
	}
	return b.NonSchoolWindow
}

func minuteFromDayStart(t, day time.Time) int {
	return int(t.Sub(day) / time.Minute)
}

func containsWholeShift(window MinuteWindow, shift Interval) bool {
	day := calendarDay(shift.Start)
	start := minuteFromDayStart(shift.Start, day)
	end := minuteFromDayStart(shift.End, day)
	if end <= start {
		end += 24 * 60
	}
	return start >= window.Start && end <= window.End
}

func ageBandFor(worker MinorWorker, shiftDate time.Time, rules MinorRulePack) (AgeBand, error) {
	age := ageAt(worker.BirthDate, shiftDate)
	for _, band := range rules.Bands {
		if band.matches(age) {
			return band, nil
		}
	}
	return AgeBand{}, fmt.Errorf("%w: no age band for age %d on %s", ErrInvalidRules, age, shiftDate.Format("2006-01-02"))
}

// AgeBandAt resolves a rule-pack band at the supplied shift date.
func AgeBandAt(birthDate, shiftDate time.Time, bands []AgeBand) (string, error) {
	age := ageAt(birthDate, shiftDate)
	for _, band := range bands {
		if band.matches(age) {
			return band.Name, nil
		}
	}
	return "", fmt.Errorf("%w: no age band for age %d on %s", ErrInvalidRules, age, shiftDate.Format("2006-01-02"))
}

func sumOnDay(intervals []Interval, day time.Time) (int, error) {
	dayStart := calendarDay(day)
	dayEnd := dayStart.Add(24 * time.Hour)
	total := 0
	for _, interval := range intervals {
		if err := interval.validate(); err != nil {
			return 0, err
		}
		start, end := interval.Start, interval.End
		if start.Before(dayStart) {
			start = dayStart
		}
		if end.After(dayEnd) {
			end = dayEnd
		}
		if end.After(start) {
			total += int(end.Sub(start) / time.Minute)
		}
	}
	return total, nil
}

func sumInWeek(intervals []Interval, day time.Time) (int, error) {
	day = calendarDay(day)
	// ISO-week-like Monday boundary is deterministic for local scheduling.
	for day.Weekday() != time.Monday {
		day = day.Add(-24 * time.Hour)
	}
	weekEnd := day.Add(7 * 24 * time.Hour)
	total := 0
	for _, interval := range intervals {
		if err := interval.validate(); err != nil {
			return 0, err
		}
		start, end := interval.Start, interval.End
		if start.Before(day) {
			start = day
		}
		if end.After(weekEnd) {
			end = weekEnd
		}
		if end.After(start) {
			total += int(end.Sub(start) / time.Minute)
		}
	}
	return total, nil
}

// EvaluateMinorSchedule blocks publication violations but never mutates or
// discards evidence. Existing intervals are counted in the shift's local day
// and calendar week, and age is evaluated at the shift date.
func EvaluateMinorSchedule(worker MinorWorker, shift ScheduledShift, rules MinorRulePack, evidence MinorScheduleEvidence) (MinorScheduleDecision, error) {
	if err := worker.validate(); err != nil {
		return MinorScheduleDecision{}, err
	}
	if err := rules.validate(); err != nil {
		return MinorScheduleDecision{}, err
	}
	if err := shift.Interval.validate(); err != nil {
		return MinorScheduleDecision{}, err
	}
	band, err := ageBandFor(worker, shift.Interval.Start, rules)
	if err != nil {
		return MinorScheduleDecision{}, err
	}
	dayMinutes, err := sumOnDay(evidence.WorkedIntervals, shift.Interval.Start)
	if err != nil {
		return MinorScheduleDecision{}, err
	}
	weekMinutes, err := sumInWeek(evidence.WorkedIntervals, shift.Interval.Start)
	if err != nil {
		return MinorScheduleDecision{}, err
	}
	decision := MinorScheduleDecision{AgeBand: band.Name}
	if dayMinutes+shift.Interval.minutes() > band.DailyCapMinutes {
		decision.Violations = append(decision.Violations, ViolationDailyCap)
	}
	if weekMinutes+shift.Interval.minutes() > band.WeeklyCapMinutes {
		decision.Violations = append(decision.Violations, ViolationWeeklyCap)
	}
	if !containsWholeShift(applicableWindow(band, shift.Interval.Start, rules.SchoolSessions), shift.Interval) {
		decision.Violations = append(decision.Violations, ViolationTimeWindow)
	}
	for _, task := range shift.TaskCodes {
		for _, forbidden := range band.HazardousTaskCode {
			if task == forbidden {
				decision.Violations = append(decision.Violations, ViolationHazardousTask)
				break
			}
		}
	}
	if band.PermitRequired && worker.Permit == nil {
		decision.Violations = append(decision.Violations, ViolationPermit)
	}
	sort.Slice(decision.Violations, func(i, j int) bool { return decision.Violations[i] < decision.Violations[j] })
	decision.Blocked = len(decision.Violations) > 0
	return decision, nil
}

// EvaluateSchedulePublication is the workflow-facing spelling of
// EvaluateMinorSchedule.
func EvaluateSchedulePublication(worker MinorWorker, shift ScheduledShift, rules MinorRulePack, evidence MinorScheduleEvidence) (MinorScheduleDecision, error) {
	return EvaluateMinorSchedule(worker, shift, rules, evidence)
}

type MinorClockInInput struct {
	PunchAt                  time.Time
	WorkedMinutesToday       int
	WorkedMinutesThisWeek    int
	ExpectedRemainingMinutes int
}

type MinorClockInDecision struct {
	AgeBand              string
	Recorded             bool
	OutsideWindow        bool
	SupervisorException  bool
	EndOfWindowNoticeAt  time.Time
	HasEndOfWindowNotice bool
	ApproachingDailyCap  bool
	ApproachingWeeklyCap bool
}

// EvaluateMinorClockIn always returns Recorded=true for a valid minor. An
// out-of-window punch becomes an immediate exception; it is never rejected,
// because rejecting it would lose payable work evidence.
func EvaluateMinorClockIn(worker MinorWorker, input MinorClockInInput, rules MinorRulePack) (MinorClockInDecision, error) {
	if err := worker.validate(); err != nil {
		return MinorClockInDecision{}, err
	}
	if err := rules.validate(); err != nil {
		return MinorClockInDecision{}, err
	}
	if input.PunchAt.IsZero() || input.WorkedMinutesToday < 0 || input.WorkedMinutesThisWeek < 0 || input.ExpectedRemainingMinutes < 0 {
		return MinorClockInDecision{}, fmt.Errorf("%w: invalid clock-in input", ErrInvalidEvidence)
	}
	band, err := ageBandFor(worker, input.PunchAt, rules)
	if err != nil {
		return MinorClockInDecision{}, err
	}
	window := applicableWindow(band, input.PunchAt, rules.SchoolSessions)
	day := calendarDay(input.PunchAt)
	minute := minuteFromDayStart(input.PunchAt, day)
	out := MinorClockInDecision{AgeBand: band.Name, Recorded: true}
	if minute < window.Start || minute >= window.End {
		out.OutsideWindow = true
		out.SupervisorException = true
	} else {
		out.EndOfWindowNoticeAt = day.Add(time.Duration(window.End) * time.Minute)
		out.HasEndOfWindowNotice = true
	}
	threshold := func(current, cap int) bool {
		return float64(current+input.ExpectedRemainingMinutes) >= float64(cap)*rules.ApproachingCapFraction
	}
	out.ApproachingDailyCap = threshold(input.WorkedMinutesToday, band.DailyCapMinutes)
	out.ApproachingWeeklyCap = threshold(input.WorkedMinutesThisWeek, band.WeeklyCapMinutes)
	return out, nil
}

// EvaluateClockIn is the workflow-facing spelling of EvaluateMinorClockIn.
func EvaluateClockIn(worker MinorWorker, input MinorClockInInput, rules MinorRulePack) (MinorClockInDecision, error) {
	return EvaluateMinorClockIn(worker, input, rules)
}

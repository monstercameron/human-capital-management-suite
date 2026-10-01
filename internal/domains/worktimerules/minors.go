// minors.go implements WTIME-015: minors' working-hour limits at schedule
// publication and at clock-in. Every threshold — the age bands themselves,
// daily and weekly caps, time-of-day windows (with a school-session and a
// seasonal variant), the school calendar, hazardous-task restrictions and
// the permit requirement — is data on MinorRuleSet; this package hardcodes
// none of it, so a 29 CFR 570.35 pack and a stricter state pack both run
// through the same evaluator.
//
// Publication blocks a violation outright. Clock-in never rejects a punch:
// a minor who is on the clock outside the allowed window is recorded, and
// an immediate supervisor exception is raised alongside it, because the
// alternative — discarding the punch — discards pay for work the minor
// actually did.
package worktimerules

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

var (
	// ErrInvalidRuleSet reports a minor rule set that cannot be evaluated:
	// an age band with no name, an unordered or overlapping band, or a
	// window with an unknown band reference.
	ErrInvalidRuleSet = errors.New("worktimerules: invalid minor rule set")
	// ErrForgedPermit reports permit evidence that fails its own identity
	// checks: no source, no evidence ID, or evidence recorded for a
	// different worker than the one it is being asserted for.
	ErrForgedPermit = errors.New("worktimerules: forged or unsourced permit evidence")
	// ErrNoAgeBand reports a shift date for which no rule-set band matches
	// the worker's age. The caller should treat this the same as any other
	// missing evidence: fail closed, not silently unrestricted.
	ErrNoAgeBand = errors.New("worktimerules: no age band matches this worker at the shift date")
)

// AgeBandRule is one rule-pack age band, e.g. 14 CFR 570.35's 14-15 band.
// MaxAgeYears is exclusive-or-equal at the top: a worker turning MaxAgeYears
// on the shift date is still in-band; MaxAgeYears of 0 means unbounded
// above (used for the oldest minor band, e.g. 16-17).
type AgeBandRule struct {
	Band        string
	MinAgeYears int
	MaxAgeYears int
}

func (b AgeBandRule) matches(ageYears int) bool {
	if ageYears < b.MinAgeYears {
		return false
	}
	if b.MaxAgeYears > 0 && ageYears > b.MaxAgeYears {
		return false
	}
	return true
}

// AgeYearsAt returns the completed-years age at asOf. It never reads a
// clock; asOf is always supplied by the caller (the shift date, not today).
func AgeYearsAt(birthDate, asOf time.Time) int {
	years := asOf.Year() - birthDate.Year()
	anniversary := time.Date(asOf.Year(), birthDate.Month(), birthDate.Day(), 0, 0, 0, 0, asOf.Location())
	if asOf.Before(anniversary) {
		years--
	}
	return years
}

// AgeBandAt selects the band that matches the worker's age at the shift
// date, deliberately taking the shift date rather than the evaluation
// instant so a birthday during an open schedule is handled correctly.
func AgeBandAt(birthDate, shiftDate time.Time, bands []AgeBandRule) (string, error) {
	age := AgeYearsAt(birthDate, shiftDate)
	for _, b := range bands {
		if b.matches(age) {
			return b.Band, nil
		}
	}
	return "", fmt.Errorf("%w: age %d at %s", ErrNoAgeBand, age, shiftDate.Format("2006-01-02"))
}

// DateRange is an inclusive calendar range in UTC calendar days.
type DateRange struct{ From, To time.Time }

func (r DateRange) contains(d time.Time) bool {
	day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	from := time.Date(r.From.Year(), r.From.Month(), r.From.Day(), 0, 0, 0, 0, time.UTC)
	to := time.Date(r.To.Year(), r.To.Month(), r.To.Day(), 0, 0, 0, 0, time.UTC)
	return !day.Before(from) && !day.After(to)
}

// InSchoolSession reports whether d falls inside any session range.
func InSchoolSession(d time.Time, sessions []DateRange) bool {
	for _, s := range sessions {
		if s.contains(d) {
			return true
		}
	}
	return false
}

// TimeWindowRule is one allowed clock window for a band, with a seasonal
// variant selected by whether the shift date is in school session.
type TimeWindowRule struct {
	Band         string
	SchoolPeriod bool // true: applies when the shift date is in session; false: out of session
	// EarliestMinute/LatestMinute are minutes since local midnight of the
	// shift date. LatestMinute may exceed 1440 to express a window that
	// runs past midnight (e.g. 7pm-9pm the next calendar instant is still
	// expressed as 1260 for clarity, never negative).
	EarliestMinute int
	LatestMinute   int
}

func (w TimeWindowRule) allows(minuteOfDay int) bool {
	return minuteOfDay >= w.EarliestMinute && minuteOfDay <= w.LatestMinute
}

func minuteOfDay(t time.Time) int {
	return t.Hour()*60 + t.Minute()
}

func selectWindow(band string, shiftDate time.Time, sessions []DateRange, windows []TimeWindowRule) (TimeWindowRule, bool) {
	inSession := InSchoolSession(shiftDate, sessions)
	for _, w := range windows {
		if w.Band == band && w.SchoolPeriod == inSession {
			return w, true
		}
	}
	return TimeWindowRule{}, false
}

// HazardousTaskRestriction forbids a task code for a band outright,
// independent of hours.
type HazardousTaskRestriction struct {
	Band     string
	TaskCode string
}

// MinorRuleSet packs every threshold this evaluator needs. All of it is
// data: ship a 29 CFR 570.35 pack and a stricter state pack side by side in
// tests, never as constants in this file.
type MinorRuleSet struct {
	Name             string
	Bands            []AgeBandRule
	DailyCapMinutes  map[string]int
	WeeklyCapMinutes map[string]int
	SchoolSessions   []DateRange
	Windows          []TimeWindowRule
	Hazardous        []HazardousTaskRestriction
	PermitRequired   map[string]bool
	// ApproachingCapFraction triggers the warning once worked minutes reach
	// this fraction of the applicable cap (e.g. 0.9 for a 90% warning).
	ApproachingCapFraction float64
}

func (r MinorRuleSet) validate() error {
	if len(r.Bands) == 0 {
		return fmt.Errorf("%w: %s: at least one age band is required", ErrInvalidRuleSet, r.Name)
	}
	if r.ApproachingCapFraction <= 0 || r.ApproachingCapFraction > 1 {
		return fmt.Errorf("%w: %s: approaching_cap_fraction must be in (0,1]", ErrInvalidRuleSet, r.Name)
	}
	return nil
}

func (r MinorRuleSet) hazardous(band, taskCode string) bool {
	for _, h := range r.Hazardous {
		if h.Band == band && h.TaskCode == taskCode {
			return true
		}
	}
	return false
}

// PermitEvidence is a sourced work permit. WorkerID must equal the worker
// it is asserted for; evidence sourced for a different worker, or with no
// source or ID, is a forgery and is rejected rather than silently trusted.
type PermitEvidence struct {
	WorkerID string
	ID       string
	Source   string
	IssuedAt time.Time
}

func (p PermitEvidence) validateFor(workerID string) error {
	if p.ID == "" || p.Source == "" {
		return fmt.Errorf("%w: permit evidence requires id and source", ErrForgedPermit)
	}
	if p.WorkerID == "" || p.WorkerID != workerID {
		return fmt.Errorf("%w: permit evidence worker_id does not match", ErrForgedPermit)
	}
	if p.IssuedAt.IsZero() {
		return fmt.Errorf("%w: permit evidence requires issued_at", ErrForgedPermit)
	}
	return nil
}

// MinorWorker is the sourced identity and age evidence for one worker.
type MinorWorker struct {
	TenantID  string
	WorkerID  string
	BirthDate time.Time
	Permit    *PermitEvidence
}

func (w MinorWorker) validate() error {
	if w.TenantID == "" || w.WorkerID == "" {
		return ErrMissingScope
	}
	if w.BirthDate.IsZero() {
		return fmt.Errorf("%w: birth_date is required", ErrInvalidLedger)
	}
	return nil
}

// ScheduledShift is one candidate or published shift being evaluated.
type ScheduledShift struct {
	Start     time.Time
	End       time.Time
	TaskCodes []string
}

func (s ScheduledShift) minutes() int { return int(s.End.Sub(s.Start).Minutes()) }

// PublicationViolation names one reason a shift cannot publish.
type PublicationViolation string

const (
	ViolationDailyCap   PublicationViolation = "DAILY_CAP_EXCEEDED"
	ViolationWeeklyCap  PublicationViolation = "WEEKLY_CAP_EXCEEDED"
	ViolationTimeWindow PublicationViolation = "OUTSIDE_TIME_WINDOW"
	ViolationHazardous  PublicationViolation = "HAZARDOUS_TASK"
	ViolationNoPermit   PublicationViolation = "PERMIT_REQUIRED_MISSING"
	ViolationNoWindow   PublicationViolation = "NO_APPLICABLE_WINDOW"
)

// PublicationDecision is the blocking answer for one candidate shift.
type PublicationDecision struct {
	Band       string
	Blocked    bool
	Violations []PublicationViolation
}

// EvaluateSchedulePublication decides whether a candidate shift for a minor
// may publish. windows.DayMinutes/WeekMinutes are the worker's minutes
// already on the ledger at the shift's day/week, read at the shift start;
// the shift's own minutes are added before comparing against the caps.
func EvaluateSchedulePublication(worker MinorWorker, shift ScheduledShift, rules MinorRuleSet, windows Windows) (PublicationDecision, error) {
	if err := worker.validate(); err != nil {
		return PublicationDecision{}, err
	}
	if err := rules.validate(); err != nil {
		return PublicationDecision{}, err
	}
	if !shift.End.After(shift.Start) {
		return PublicationDecision{}, fmt.Errorf("%w: shift interval must be non-empty", ErrInvalidLedger)
	}
	band, err := AgeBandAt(worker.BirthDate, shift.Start, rules.Bands)
	if err != nil {
		return PublicationDecision{}, err
	}
	d := PublicationDecision{Band: band}

	if cap, ok := rules.DailyCapMinutes[band]; ok {
		if windows.DayMinutes+shift.minutes() > cap {
			d.Violations = append(d.Violations, ViolationDailyCap)
		}
	}
	if cap, ok := rules.WeeklyCapMinutes[band]; ok {
		if windows.WeekMinutes+shift.minutes() > cap {
			d.Violations = append(d.Violations, ViolationWeeklyCap)
		}
	}
	if win, ok := selectWindow(band, shift.Start, rules.SchoolSessions, rules.Windows); ok {
		if !win.allows(minuteOfDay(shift.Start)) || !win.allows(minuteOfDay(shift.End)) {
			d.Violations = append(d.Violations, ViolationTimeWindow)
		}
	} else {
		d.Violations = append(d.Violations, ViolationNoWindow)
	}
	for _, task := range shift.TaskCodes {
		if rules.hazardous(band, task) {
			d.Violations = append(d.Violations, ViolationHazardous)
			break
		}
	}
	if rules.PermitRequired[band] {
		if worker.Permit == nil {
			d.Violations = append(d.Violations, ViolationNoPermit)
		} else if err := worker.Permit.validateFor(worker.WorkerID); err != nil {
			return PublicationDecision{}, err
		}
	} else if worker.Permit != nil {
		if err := worker.Permit.validateFor(worker.WorkerID); err != nil {
			return PublicationDecision{}, err
		}
	}
	sort.Slice(d.Violations, func(i, j int) bool { return d.Violations[i] < d.Violations[j] })
	d.Blocked = len(d.Violations) > 0
	return d, nil
}

// ClockInOutcome is the never-rejecting answer at clock-in.
type ClockInOutcome struct {
	Band                 string
	Recorded             bool
	OutsideWindow        bool
	SupervisorException  bool
	EndOfWindowNoticeAt  time.Time
	HasEndOfWindowNotice bool
	ApproachingDailyCap  bool
	ApproachingWeeklyCap bool
}

// EvaluateClockIn records a punch for a minor and reports whether it falls
// outside the allowed window or is approaching a cap. It never returns a
// rejection: the punch is always Recorded, because discarding it discards
// pay for work performed.
func EvaluateClockIn(worker MinorWorker, punchAt time.Time, rules MinorRuleSet, windows Windows) (ClockInOutcome, error) {
	if err := worker.validate(); err != nil {
		return ClockInOutcome{}, err
	}
	if err := rules.validate(); err != nil {
		return ClockInOutcome{}, err
	}
	band, err := AgeBandAt(worker.BirthDate, punchAt, rules.Bands)
	if err != nil {
		return ClockInOutcome{}, err
	}
	out := ClockInOutcome{Band: band, Recorded: true}

	if win, ok := selectWindow(band, punchAt, rules.SchoolSessions, rules.Windows); ok {
		if !win.allows(minuteOfDay(punchAt)) {
			out.OutsideWindow = true
			out.SupervisorException = true
		} else {
			dayStart := time.Date(punchAt.Year(), punchAt.Month(), punchAt.Day(), 0, 0, 0, 0, punchAt.Location())
			out.EndOfWindowNoticeAt = dayStart.Add(time.Duration(win.LatestMinute) * time.Minute)
			out.HasEndOfWindowNotice = true
		}
	} else {
		out.OutsideWindow = true
		out.SupervisorException = true
	}

	if cap, ok := rules.DailyCapMinutes[band]; ok && cap > 0 {
		if float64(windows.DayMinutes) >= float64(cap)*rules.ApproachingCapFraction {
			out.ApproachingDailyCap = true
		}
	}
	if cap, ok := rules.WeeklyCapMinutes[band]; ok && cap > 0 {
		if float64(windows.WeekMinutes) >= float64(cap)*rules.ApproachingCapFraction {
			out.ApproachingWeeklyCap = true
		}
	}
	return out, nil
}

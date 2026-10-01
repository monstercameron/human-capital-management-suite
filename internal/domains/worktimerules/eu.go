// eu.go implements WTIME-017: EU/UK working-time limits, opt-outs and the
// right-to-disconnect suppression decision. Every numeric limit is data on
// EURuleSet (Directive 2003/88/EC defaults and UK WTR 1998 both run
// through the same evaluator); this file hosts no jurisdiction constant.
package worktimerules

import (
	"errors"
	"fmt"
	"time"
)

// ErrOptOutInvalid reports an opt-out document that cannot be evaluated:
// unsigned, or withdrawn with no notice period.
var ErrOptOutInvalid = errors.New("worktimerules: invalid opt-out document")

// EURuleSet packs the WTIME-017 thresholds. ReferencePeriod pairs with
// WindowOptions.ReferencePeriod when computing WeeklyAverageCapMinutes.
type EURuleSet struct {
	Name                    string
	DailyRestMinutes        int // e.g. 11h = 660
	WeeklyRestMinutes       int
	BreakAfterMinutes       int // a break is due after this many worked minutes
	NightWorkLimitMinutes   int // per 24h period
	WeeklyAverageCapMinutes int // e.g. 48h = 2880, averaged over the reference period
	ReferencePeriod         time.Duration
	RolledUpHolidayPermille int // e.g. 12.07% expressed as 1207 parts per 10,000; 0 disables
}

func (r EURuleSet) validate() error {
	if r.DailyRestMinutes <= 0 || r.WeeklyRestMinutes <= 0 || r.WeeklyAverageCapMinutes <= 0 || r.ReferencePeriod <= 0 {
		return fmt.Errorf("%w: %s: daily rest, weekly rest, weekly average cap and reference period are required", ErrOptOutInvalid, r.Name)
	}
	return nil
}

// RestFinding names one WTIME-017 rest or limit breach.
type RestFinding string

const (
	FindingDailyRestBreach  RestFinding = "DAILY_REST_BREACH"
	FindingWeeklyAverageCap RestFinding = "WEEKLY_AVERAGE_CAP_BREACH"
	FindingBreakOwed        RestFinding = "BREAK_AFTER_HOURS_OWED"
)

// RestEvaluation is the read-only answer for one worker as of one Windows
// read; it never blocks or writes anything on its own — scheduling and
// clock-in DECISIONs read it and decide what to do.
type RestEvaluation struct {
	Findings []RestFinding
	// WeeklyAverageMinutes is ReferencePeriodMinutes divided by the number
	// of weeks in ReferencePeriod, for reporting alongside the cap.
	WeeklyAverageMinutes int
}

// EvaluateRest reads a WTIME-007 Windows and reports breaches against an
// EURuleSet. shiftMinutesSinceLastRest is the minutes worked since
// windows.LastRestEnd (the caller supplies it because "since rest" depends
// on the instant being evaluated, which may be a candidate shift end, not
// just AsOf).
func EvaluateRest(windows Windows, minutesSinceLastRest int, rules EURuleSet) (RestEvaluation, error) {
	if err := rules.validate(); err != nil {
		return RestEvaluation{}, err
	}
	if !windows.HasLastRestEnd {
		// No rest on record is itself evidence of a potential breach, not
		// a pass; the caller decides how to treat missing evidence, this
		// function only reports what it can compute.
		minutesSinceLastRest = windows.DayMinutes
	}
	var eval RestEvaluation
	restMinutesAvailable := windows.WeekMinutes // placeholder not used directly; kept for clarity of intent
	_ = restMinutesAvailable
	if minutesSinceLastRest > 0 && (24*60-minutesSinceLastRest) < rules.DailyRestMinutes {
		eval.Findings = append(eval.Findings, FindingDailyRestBreach)
	}
	weeks := int(rules.ReferencePeriod / (7 * 24 * time.Hour))
	if weeks <= 0 {
		weeks = 1
	}
	eval.WeeklyAverageMinutes = windows.ReferencePeriodMinutes / weeks
	if eval.WeeklyAverageMinutes > rules.WeeklyAverageCapMinutes {
		eval.Findings = append(eval.Findings, FindingWeeklyAverageCap)
	}
	if rules.BreakAfterMinutes > 0 && windows.DayMinutes > rules.BreakAfterMinutes {
		eval.Findings = append(eval.Findings, FindingBreakOwed)
	}
	return eval, nil
}

// OptOutDocument is a signed UK WTR opt-out from the 48-hour average, with
// a notice-period withdrawal: withdrawing takes effect NoticePeriod after
// WithdrawnAt, not immediately.
type OptOutDocument struct {
	WorkerID     string
	SignedAt     time.Time
	NoticePeriod time.Duration
	WithdrawnAt  *time.Time
}

func (d OptOutDocument) validate() error {
	if d.WorkerID == "" || d.SignedAt.IsZero() {
		return fmt.Errorf("%w: worker_id and signed_at are required", ErrOptOutInvalid)
	}
	if d.WithdrawnAt != nil && d.NoticePeriod <= 0 {
		return fmt.Errorf("%w: a withdrawn opt-out requires a notice period", ErrOptOutInvalid)
	}
	return nil
}

// IsActive reports whether the opt-out still suppresses the weekly average
// cap at asOf: signed, and either not withdrawn or still inside the notice
// period.
func (d OptOutDocument) IsActive(asOf time.Time) (bool, error) {
	if err := d.validate(); err != nil {
		return false, err
	}
	if asOf.Before(d.SignedAt) {
		return false, nil
	}
	if d.WithdrawnAt == nil {
		return true, nil
	}
	effectiveEnd := d.WithdrawnAt.Add(d.NoticePeriod)
	return asOf.Before(effectiveEnd), nil
}

// RequiresDailyRecording reports the EU daily recording duty (CJEU
// C-55/18): it applies to every EU worker regardless of any overtime
// exemption. euJurisdiction is a caller-supplied flag (this package makes
// no jurisdiction determination of its own).
func RequiresDailyRecording(euJurisdiction bool) bool { return euJurisdiction }

// RolledUpHolidayAccrualMinutes computes UK rolled-up holiday pay accrual
// for an irregular-hours worker who has elected it, as integer minutes
// using an integer permille (parts-per-10,000) rate rather than float64.
// 12.07% is expressed by the caller as permille=1207.
func RolledUpHolidayAccrualMinutes(workedMinutes int, permille int) (int, error) {
	if permille <= 0 {
		return 0, fmt.Errorf("%w: rolled-up holiday permille must be positive", ErrOptOutInvalid)
	}
	if workedMinutes < 0 {
		return 0, fmt.Errorf("%w: worked minutes must not be negative", ErrOptOutInvalid)
	}
	// Round half up: (workedMinutes*permille + 5000) / 10000.
	return int((int64(workedMinutes)*int64(permille) + 5000) / 10000), nil
}

// DisconnectionPolicy is one right-to-disconnect rule: outside
// [WindowStartMinute, WindowEndMinute) local minutes-of-day, non-urgent
// notifications are suppressed. Urgent messages are never suppressed by
// this function; the caller marks urgency.
type DisconnectionPolicy struct {
	Jurisdiction      string
	WindowStartMinute int
	WindowEndMinute   int
}

// SuppressNotification decides whether a non-urgent message sent at
// messageAt should be suppressed under policy.
func SuppressNotification(policy DisconnectionPolicy, messageAt time.Time, urgent bool) bool {
	if urgent {
		return false
	}
	minute := minuteOfDay(messageAt)
	if policy.WindowStartMinute <= policy.WindowEndMinute {
		inWindow := minute >= policy.WindowStartMinute && minute < policy.WindowEndMinute
		return !inWindow
	}
	// Window spans midnight (e.g. 22:00-06:00 is the *disconnect* window in
	// some configurations, but WindowStart/End here name the *allowed*
	// send window; keep the same spanning semantics for either shape).
	inWindow := minute >= policy.WindowStartMinute || minute < policy.WindowEndMinute
	return !inWindow
}

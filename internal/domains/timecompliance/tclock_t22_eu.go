package timecompliance

import (
	"fmt"
	"time"
)

// WorkingTimeRules is a jurisdiction/effective-period rule pack. The
// defaults for Directive 2003/88/EC or UK WTR are supplied by configuration;
// this domain does not hard-code a jurisdiction's legal thresholds.
type WorkingTimeRules struct {
	Name                    string
	DailyRestMinutes        int
	WeeklyRestMinutes       int
	BreakAfterMinutes       int
	NightWorkLimitMinutes   int
	WeeklyAverageCapMinutes int
	ReferencePeriodWeeks    int
	NightWindow             MinuteWindow
	UKIrregularHours        bool
	RolledUpHolidayPermille int
}

func (r WorkingTimeRules) validate() error {
	if r.Name == "" || r.DailyRestMinutes <= 0 || r.WeeklyRestMinutes <= 0 || r.BreakAfterMinutes <= 0 || r.NightWorkLimitMinutes <= 0 || r.WeeklyAverageCapMinutes <= 0 || r.ReferencePeriodWeeks <= 0 {
		return fmt.Errorf("%w: incomplete EU/UK working-time rule pack", ErrInvalidRules)
	}
	if err := r.NightWindow.validate(); err != nil {
		return err
	}
	if r.RolledUpHolidayPermille < 0 || r.RolledUpHolidayPermille > 10000 {
		return fmt.Errorf("%w: rolled-up holiday rate is outside 0-100%%", ErrInvalidRules)
	}
	return nil
}

type OptOutDocument struct {
	WorkerID     string
	SignedAt     time.Time
	WithdrawnAt  *time.Time
	NoticePeriod time.Duration
}

func (d OptOutDocument) validate() error {
	if d.WorkerID == "" || d.SignedAt.IsZero() {
		return fmt.Errorf("%w: opt-out needs worker and signed_at", ErrInvalidEvidence)
	}
	if d.WithdrawnAt != nil && d.NoticePeriod <= 0 {
		return fmt.Errorf("%w: withdrawal needs a positive notice period", ErrInvalidEvidence)
	}
	return nil
}

// OptOutActive reports the status at the supplied evaluation instant. A
// withdrawn opt-out remains active through its notice period.
func OptOutActive(document OptOutDocument, workerID string, asOf time.Time) (bool, error) {
	if err := document.validate(); err != nil {
		return false, err
	}
	if workerID == "" || workerID != document.WorkerID || asOf.IsZero() {
		return false, fmt.Errorf("%w: opt-out identity or evaluation instant does not match", ErrInvalidEvidence)
	}
	if asOf.Before(document.SignedAt) {
		return false, nil
	}
	if document.WithdrawnAt == nil {
		return true, nil
	}
	return asOf.Before(document.WithdrawnAt.Add(document.NoticePeriod)), nil
}

type WorkingTimeInput struct {
	TenantID                     string
	WorkerID                     string
	AsOf                         time.Time
	PreviousShiftEnd             *time.Time
	CurrentShiftStart            *time.Time
	WorkedSinceBreakMinutes      int
	WorkedSinceWeeklyRestMinutes int
	WeeklyRestMinutesAvailable   int
	NightWorkMinutes             int
	ReferencePeriodWorkedMinutes int
	OptOut                       *OptOutDocument
	IrregularHoursWorker         bool
}

type WorkingTimeFinding string

const (
	FindingDailyRestBreach     WorkingTimeFinding = "DAILY_REST_BREACH"
	FindingWeeklyRestBreach    WorkingTimeFinding = "WEEKLY_REST_BREACH"
	FindingBreakDue            WorkingTimeFinding = "BREAK_AFTER_SIX_HOURS_DUE"
	FindingNightLimitBreach    WorkingTimeFinding = "NIGHT_WORK_LIMIT_BREACH"
	FindingWeeklyAverageBreach WorkingTimeFinding = "WEEKLY_AVERAGE_CAP_BREACH"
)

type WorkingTimeDecision struct {
	Findings               []WorkingTimeFinding
	WeeklyAverageMinutes   int
	DailyRecordingRequired bool
	OptOutActive           bool
	RolledUpHolidayMinutes int
}

func (in WorkingTimeInput) validate() error {
	if in.TenantID == "" || in.WorkerID == "" {
		return ErrMissingScope
	}
	if in.AsOf.IsZero() {
		return fmt.Errorf("%w: as_of is required", ErrInvalidEvidence)
	}
	for _, minutes := range []int{in.WorkedSinceBreakMinutes, in.WorkedSinceWeeklyRestMinutes, in.WeeklyRestMinutesAvailable, in.NightWorkMinutes, in.ReferencePeriodWorkedMinutes} {
		if minutes < 0 {
			return fmt.Errorf("%w: working minutes cannot be negative", ErrInvalidEvidence)
		}
	}
	if (in.PreviousShiftEnd == nil) != (in.CurrentShiftStart == nil) {
		return fmt.Errorf("%w: previous shift end and current shift start must be paired", ErrInvalidEvidence)
	}
	if in.PreviousShiftEnd != nil && !in.CurrentShiftStart.After(*in.PreviousShiftEnd) {
		return fmt.Errorf("%w: current shift must follow previous shift", ErrInvalidEvidence)
	}
	return nil
}

func nightMinutes(interval Interval, window MinuteWindow) int {
	if err := interval.validate(); err != nil {
		return 0
	}
	total := 0
	for day := calendarDay(interval.Start).Add(-24 * time.Hour); !day.After(interval.End); day = day.Add(24 * time.Hour) {
		windowStart := day.Add(time.Duration(window.Start) * time.Minute)
		windowEnd := day.Add(time.Duration(window.End) * time.Minute)
		start, end := interval.Start, interval.End
		if start.Before(windowStart) {
			start = windowStart
		}
		if end.After(windowEnd) {
			end = windowEnd
		}
		if end.After(start) {
			total += int(end.Sub(start) / time.Minute)
		}
	}
	return total
}

// EvaluateWorkingTime evaluates scheduling/clock-in evidence. It reports
// findings rather than rejecting or deleting recorded work. The caller may
// use an active opt-out only for the weekly-average finding; rest, breaks and
// daily recording remain applicable.
func EvaluateWorkingTime(in WorkingTimeInput, rules WorkingTimeRules, euJurisdiction bool) (WorkingTimeDecision, error) {
	if err := in.validate(); err != nil {
		return WorkingTimeDecision{}, err
	}
	if err := rules.validate(); err != nil {
		return WorkingTimeDecision{}, err
	}
	out := WorkingTimeDecision{DailyRecordingRequired: euJurisdiction}
	if in.PreviousShiftEnd != nil {
		rest := int(in.CurrentShiftStart.Sub(*in.PreviousShiftEnd) / time.Minute)
		if rest < rules.DailyRestMinutes {
			out.Findings = append(out.Findings, FindingDailyRestBreach)
		}
	}
	if in.WeeklyRestMinutesAvailable < rules.WeeklyRestMinutes {
		out.Findings = append(out.Findings, FindingWeeklyRestBreach)
	}
	if in.WorkedSinceBreakMinutes > rules.BreakAfterMinutes {
		out.Findings = append(out.Findings, FindingBreakDue)
	}
	if in.NightWorkMinutes > rules.NightWorkLimitMinutes {
		out.Findings = append(out.Findings, FindingNightLimitBreach)
	}
	out.WeeklyAverageMinutes = (in.ReferencePeriodWorkedMinutes + rules.ReferencePeriodWeeks - 1) / rules.ReferencePeriodWeeks
	if in.OptOut != nil {
		active, err := OptOutActive(*in.OptOut, in.WorkerID, in.AsOf)
		if err != nil {
			return WorkingTimeDecision{}, err
		}
		out.OptOutActive = active
	}
	if out.WeeklyAverageMinutes > rules.WeeklyAverageCapMinutes && !out.OptOutActive {
		out.Findings = append(out.Findings, FindingWeeklyAverageBreach)
	}
	if rules.UKIrregularHours && in.IrregularHoursWorker && rules.RolledUpHolidayPermille > 0 {
		out.RolledUpHolidayMinutes = rolledUpHolidayMinutes(in.ReferencePeriodWorkedMinutes, rules.RolledUpHolidayPermille)
	}
	return out, nil
}

// RequiresDailyRecording keeps the jurisdiction decision explicit at the
// boundary; exemptions do not alter the EU recording duty.
func RequiresDailyRecording(euJurisdiction bool) bool { return euJurisdiction }

func rolledUpHolidayMinutes(workedMinutes, permille int) int {
	return int((int64(workedMinutes)*int64(permille) + 5000) / 10000)
}

// RolledUpHolidayAccrualMinutes is exported for payroll projections that
// need the same exact integer rounding as EvaluateWorkingTime.
func RolledUpHolidayAccrualMinutes(workedMinutes, permille int) (int, error) {
	if workedMinutes < 0 || permille <= 0 || permille > 10000 {
		return 0, fmt.Errorf("%w: invalid rolled-up holiday inputs", ErrInvalidEvidence)
	}
	return rolledUpHolidayMinutes(workedMinutes, permille), nil
}

type DisconnectionPolicy struct {
	Jurisdiction  string
	AllowedWindow MinuteWindow
}

// SuppressNonUrgentNotification suppresses after-hours notices while always
// permitting urgent messages. Allowed windows may cross midnight.
func SuppressNonUrgentNotification(policy DisconnectionPolicy, messageAt time.Time, urgent bool) (bool, error) {
	if messageAt.IsZero() {
		return false, fmt.Errorf("%w: notification time is required", ErrInvalidEvidence)
	}
	if err := policy.AllowedWindow.validate(); err != nil {
		return false, err
	}
	if urgent {
		return false, nil
	}
	minute := messageAt.Hour()*60 + messageAt.Minute()
	if policy.AllowedWindow.End <= 24*60 {
		return minute < policy.AllowedWindow.Start || minute >= policy.AllowedWindow.End, nil
	}
	// A window ending after midnight is allowed from Start through midnight,
	// then from 00:00 through End-24h.
	return minute < policy.AllowedWindow.Start && minute >= policy.AllowedWindow.End-24*60, nil
}

// SuppressNotification is the concise workflow-facing spelling.
func SuppressNotification(policy DisconnectionPolicy, messageAt time.Time, urgent bool) (bool, error) {
	return SuppressNonUrgentNotification(policy, messageAt, urgent)
}

// WTIME-010: exception-only time for genuinely exempt staff, and the
// forced fallback to real daily recording where the law imposes an
// affirmative recording duty. Salary-basis deduction rules come from the
// rule pack as data, never as a hard-coded per-jurisdiction exception.
package timecard

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
)

// ErrExceptionPeriodRejected is the WTIME-010 seeded-defect sentinel.
var ErrExceptionPeriodRejected = errors.New("WTIME_010_REJECTED")

// ErrDisallowedDeduction reports a partial-day deduction the rule pack does
// not allow for an exempt worker's salary basis.
var ErrDisallowedDeduction = errors.New("timecard: salary-basis rules forbid this deduction")

// RecordingDutyProfile pins whether a jurisdiction imposes an affirmative
// daily-recording duty (CJEU C-55/18, Spain RD-ley 8/2019, Germany BAG 1
// ABR 22/21 and similar). It is data from the rule pack, never a hard-coded
// per-country switch.
type RecordingDutyProfile struct {
	JurisdictionCode           string
	DailyRecordingDutyRequired bool
	Ref                        string // rule-pack citation/version
}

// WorkerClassification is the pair of axes that decide whether
// exception-only reporting is lawful for one assignment.
type WorkerClassification struct {
	ExemptionStatus timeprofile.ExemptionStatus
	PayBasis        timeprofile.PayBasis
}

// SelectCaptureMode resolves the lawful capture mode for one assignment. A
// jurisdiction with a daily recording duty always forces a real record
// with actual start and end -- punch for non-exempt/hourly workers,
// duration otherwise. Short of that duty, only a genuinely exempt worker
// may report by exception; a salaried non-exempt worker, like everyone
// else, records actuals.
func SelectCaptureMode(worker WorkerClassification, jurisdiction RecordingDutyProfile) (timeprofile.CaptureMode, error) {
	if !worker.ExemptionStatus.Valid() || !worker.PayBasis.Valid() {
		return "", fmt.Errorf("%w: worker classification is not declared", ErrExceptionPeriodRejected)
	}
	if strings.TrimSpace(jurisdiction.JurisdictionCode) == "" {
		return "", fmt.Errorf("%w: jurisdiction is required", ErrExceptionPeriodRejected)
	}
	if jurisdiction.DailyRecordingDutyRequired {
		if worker.PayBasis == timeprofile.PayHourly || worker.ExemptionStatus == timeprofile.NonExempt {
			return timeprofile.CapturePunch, nil
		}
		return timeprofile.CaptureDuration, nil
	}
	if worker.ExemptionStatus != timeprofile.Exempt {
		return timeprofile.CaptureDuration, nil
	}
	return timeprofile.CaptureException, nil
}

// DeviationKind is the closed vocabulary of an exception-only deviation
// from the assumed scheduled pattern.
type DeviationKind string

const (
	DeviationFullDayAbsence DeviationKind = "FULL_DAY_ABSENCE"
	DeviationPartialDay     DeviationKind = "PARTIAL_DAY"
	DeviationOther          DeviationKind = "OTHER"
)

func (k DeviationKind) Valid() bool {
	switch k {
	case DeviationFullDayAbsence, DeviationPartialDay, DeviationOther:
		return true
	default:
		return false
	}
}

// Deviation is one departure from the assumed pattern for one day.
type Deviation struct {
	Date    time.Time
	Kind    DeviationKind
	Minutes int
	Reason  string
}

func (d Deviation) Validate() error {
	if d.Date.IsZero() {
		return fmt.Errorf("%w: deviation date is required", ErrExceptionPeriodRejected)
	}
	if !d.Kind.Valid() {
		return fmt.Errorf("%w: deviation kind is not declared", ErrExceptionPeriodRejected)
	}
	if d.Minutes < 0 {
		return fmt.Errorf("%w: deviation minutes cannot be negative", ErrExceptionPeriodRejected)
	}
	if strings.TrimSpace(d.Reason) == "" {
		return fmt.Errorf("%w: deviation reason is required", ErrExceptionPeriodRejected)
	}
	return nil
}

// ExceptionPeriod is the WTIME-010 exception-only period: the assumed
// pattern plus captured deviations, confirmed by the worker.
type ExceptionPeriod struct {
	WorkerID    string
	Mode        timeprofile.CaptureMode
	PatternRef  string
	Deviations  []Deviation
	Confirmed   bool
	ConfirmedAt time.Time
	ConfirmedBy string
}

// NewExceptionPeriod builds an unconfirmed exception-only period. Mode must
// already be the lawful mode from SelectCaptureMode: this constructor
// refuses any mode other than EXCEPTION_ONLY, so a daily-recording
// jurisdiction can never end up modeled as one by a caller's mistake.
func NewExceptionPeriod(workerID, patternRef string, mode timeprofile.CaptureMode, deviations []Deviation) (ExceptionPeriod, error) {
	if strings.TrimSpace(workerID) == "" || strings.TrimSpace(patternRef) == "" {
		return ExceptionPeriod{}, fmt.Errorf("%w: worker and pattern reference are required", ErrExceptionPeriodRejected)
	}
	if mode != timeprofile.CaptureException {
		return ExceptionPeriod{}, fmt.Errorf("%w: exception period requires EXCEPTION_ONLY capture mode, got %q", ErrExceptionPeriodRejected, mode)
	}
	for i, d := range deviations {
		if err := d.Validate(); err != nil {
			return ExceptionPeriod{}, fmt.Errorf("%w: deviation %d: %v", ErrExceptionPeriodRejected, i, err)
		}
	}
	return ExceptionPeriod{WorkerID: workerID, Mode: mode, PatternRef: patternRef, Deviations: append([]Deviation(nil), deviations...)}, nil
}

// ConfirmExceptionPeriod records the worker's confirmation of the assumed
// pattern for this run. An already-confirmed period is refused: it is not
// silently repeatable evidence.
func ConfirmExceptionPeriod(period ExceptionPeriod, by string, now time.Time) (ExceptionPeriod, error) {
	if period.Confirmed {
		return ExceptionPeriod{}, fmt.Errorf("%w: period is already confirmed", ErrExceptionPeriodRejected)
	}
	if strings.TrimSpace(by) == "" || now.IsZero() {
		return ExceptionPeriod{}, fmt.Errorf("%w: confirming party and time are required", ErrExceptionPeriodRejected)
	}
	if by != period.WorkerID {
		return ExceptionPeriod{}, fmt.Errorf("%w: only the worker confirms their own pattern", ErrExceptionPeriodRejected)
	}
	out := period
	out.Confirmed, out.ConfirmedAt, out.ConfirmedBy = true, now, by
	return out, nil
}

// SalaryBasisRule declares which partial-day deviation kinds a
// jurisdiction or company policy allows to deduct from an exempt salary.
// FLSA salary-basis rules forbid most partial-day deductions; the allowed
// set is data from the rule pack, never a hard-coded exception list.
type SalaryBasisRule struct {
	RuleRef                     string
	AllowedPartialDayDeductions map[DeviationKind]bool
}

// ValidateSalaryDeduction blocks a partial-day deduction the rule pack does
// not allow for an exempt worker's salary basis. A full-day absence is
// always a lawful deduction; the rule pack governs only partial days.
func ValidateSalaryDeduction(dev Deviation, worker WorkerClassification, rule SalaryBasisRule) error {
	if err := dev.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(rule.RuleRef) == "" {
		return fmt.Errorf("%w: salary-basis rule reference is required", ErrExceptionPeriodRejected)
	}
	if worker.ExemptionStatus != timeprofile.Exempt {
		return nil // salary-basis deduction rules apply only to exempt pay
	}
	if dev.Kind != DeviationPartialDay {
		return nil
	}
	if !rule.AllowedPartialDayDeductions[dev.Kind] {
		return fmt.Errorf("%w: partial-day deduction is not an allowed safe harbor under %s", ErrDisallowedDeduction, rule.RuleRef)
	}
	return nil
}

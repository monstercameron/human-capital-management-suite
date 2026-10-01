package timecard

import (
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
)

func germanRecordingDuty() RecordingDutyProfile {
	return RecordingDutyProfile{JurisdictionCode: "DE", DailyRecordingDutyRequired: true, Ref: "BAG-1-ABR-22-21"}
}

func noRecordingDuty() RecordingDutyProfile {
	return RecordingDutyProfile{JurisdictionCode: "US-TX", DailyRecordingDutyRequired: false, Ref: "none"}
}

// TestTodo_WTIME_010 is the PRIMARY test: a German exempt worker falls back
// to a real duration record instead of exception-only, a salaried
// non-exempt worker never gets exception-only even without a recording
// duty, a genuinely exempt worker in a jurisdiction without that duty may
// use exception-only, and a disallowed partial-day deduction is blocked.
func TestTodo_WTIME_010(t *testing.T) {
	exempt := WorkerClassification{ExemptionStatus: timeprofile.Exempt, PayBasis: timeprofile.PaySalary}
	salariedNonExempt := WorkerClassification{ExemptionStatus: timeprofile.SalariedNonExempt, PayBasis: timeprofile.PaySalary}

	// RED: a German exempt employee's record must never default to the
	// scheduled pattern alone -- the daily recording duty forces an actual
	// record.
	mode, err := SelectCaptureMode(exempt, germanRecordingDuty())
	if err != nil {
		t.Fatalf("SelectCaptureMode german exempt: %v", err)
	}
	if mode == timeprofile.CaptureException {
		t.Fatalf("mode = %s, want a real record under a daily recording duty", mode)
	}

	// RED: a salaried non-exempt worker must never get an exception-only
	// profile, even without a recording duty.
	mode, err = SelectCaptureMode(salariedNonExempt, noRecordingDuty())
	if err != nil {
		t.Fatalf("SelectCaptureMode salaried non-exempt: %v", err)
	}
	if mode == timeprofile.CaptureException {
		t.Fatalf("mode = %s, want a salaried non-exempt worker to record actuals", mode)
	}

	// GREEN: a genuinely exempt worker with no recording duty may use
	// exception-only.
	mode, err = SelectCaptureMode(exempt, noRecordingDuty())
	if err != nil {
		t.Fatalf("SelectCaptureMode exempt no duty: %v", err)
	}
	if mode != timeprofile.CaptureException {
		t.Fatalf("mode = %s, want EXCEPTION_ONLY", mode)
	}

	period, err := NewExceptionPeriod("worker-1", "pattern-1", mode, []Deviation{
		{Date: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC), Kind: DeviationFullDayAbsence, Minutes: 480, Reason: "sick day"},
	})
	if err != nil {
		t.Fatalf("NewExceptionPeriod: %v", err)
	}
	if _, err := NewExceptionPeriod("worker-1", "pattern-1", timeprofile.CaptureDuration, nil); !errors.Is(err, ErrExceptionPeriodRejected) {
		t.Fatalf("NewExceptionPeriod with a non-exception mode: got %v, want ErrExceptionPeriodRejected", err)
	}

	confirmed, err := ConfirmExceptionPeriod(period, "worker-1", time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ConfirmExceptionPeriod: %v", err)
	}
	if !confirmed.Confirmed {
		t.Fatalf("period not confirmed")
	}
	if _, err := ConfirmExceptionPeriod(confirmed, "worker-1", time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC)); !errors.Is(err, ErrExceptionPeriodRejected) {
		t.Fatalf("ConfirmExceptionPeriod twice: got %v, want ErrExceptionPeriodRejected", err)
	}
	if _, err := ConfirmExceptionPeriod(period, "supervisor-1", time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC)); !errors.Is(err, ErrExceptionPeriodRejected) {
		t.Fatalf("ConfirmExceptionPeriod by someone other than the worker: got %v, want ErrExceptionPeriodRejected", err)
	}

	// RED: salary-basis deduction rules must block a disallowed
	// partial-day deduction.
	rule := SalaryBasisRule{RuleRef: "flsa-541", AllowedPartialDayDeductions: map[DeviationKind]bool{}}
	partial := Deviation{Date: time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC), Kind: DeviationPartialDay, Minutes: 120, Reason: "left early"}
	if err := ValidateSalaryDeduction(partial, exempt, rule); !errors.Is(err, ErrDisallowedDeduction) {
		t.Fatalf("ValidateSalaryDeduction disallowed partial day: got %v, want ErrDisallowedDeduction", err)
	}
	allowedRule := SalaryBasisRule{RuleRef: "flsa-541", AllowedPartialDayDeductions: map[DeviationKind]bool{DeviationPartialDay: true}}
	if err := ValidateSalaryDeduction(partial, exempt, allowedRule); err != nil {
		t.Fatalf("ValidateSalaryDeduction allowed partial day: %v", err)
	}
	fullDay := Deviation{Date: partial.Date, Kind: DeviationFullDayAbsence, Minutes: 480, Reason: "sick day"}
	if err := ValidateSalaryDeduction(fullDay, exempt, rule); err != nil {
		t.Fatalf("ValidateSalaryDeduction full-day absence must always be allowed: %v", err)
	}
	if err := ValidateSalaryDeduction(partial, salariedNonExempt, rule); err != nil {
		t.Fatalf("ValidateSalaryDeduction for a non-exempt worker must not apply salary-basis rules: %v", err)
	}
}

// TestTodo_WTIME_010_Golden pins the exact capture-mode outcome for a
// fixed matrix of jurisdictions and worker classifications.
func TestTodo_WTIME_010_Golden(t *testing.T) {
	cases := []struct {
		name   string
		worker WorkerClassification
		juris  RecordingDutyProfile
		want   timeprofile.CaptureMode
	}{
		{"DE_exempt", WorkerClassification{ExemptionStatus: timeprofile.Exempt, PayBasis: timeprofile.PaySalary}, germanRecordingDuty(), timeprofile.CaptureDuration},
		{"DE_hourly", WorkerClassification{ExemptionStatus: timeprofile.NonExempt, PayBasis: timeprofile.PayHourly}, germanRecordingDuty(), timeprofile.CapturePunch},
		{"US_exempt", WorkerClassification{ExemptionStatus: timeprofile.Exempt, PayBasis: timeprofile.PaySalary}, noRecordingDuty(), timeprofile.CaptureException},
		{"US_salaried_nonexempt", WorkerClassification{ExemptionStatus: timeprofile.SalariedNonExempt, PayBasis: timeprofile.PaySalary}, noRecordingDuty(), timeprofile.CaptureDuration},
	}
	for _, c := range cases {
		got, err := SelectCaptureMode(c.worker, c.juris)
		if err != nil {
			t.Fatalf("%s: SelectCaptureMode: %v", c.name, err)
		}
		if got != c.want {
			t.Fatalf("%s: mode = %s, want %s", c.name, got, c.want)
		}
	}
}

// TestTodo_WTIME_010_Property proves exception-only is never selected when
// either the jurisdiction imposes a daily recording duty or the worker is
// not genuinely exempt, across many generated combinations.
func TestTodo_WTIME_010_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(77))
	exemptions := []timeprofile.ExemptionStatus{timeprofile.NonExempt, timeprofile.Exempt, timeprofile.SalariedNonExempt, timeprofile.NotApplicable}
	payBases := []timeprofile.PayBasis{timeprofile.PayHourly, timeprofile.PaySalary, timeprofile.PayPieceRate}
	for trial := 0; trial < 200; trial++ {
		worker := WorkerClassification{ExemptionStatus: exemptions[rng.Intn(len(exemptions))], PayBasis: payBases[rng.Intn(len(payBases))]}
		duty := rng.Intn(2) == 0
		juris := RecordingDutyProfile{JurisdictionCode: "X", DailyRecordingDutyRequired: duty}
		mode, err := SelectCaptureMode(worker, juris)
		if err != nil {
			t.Fatalf("trial %d: SelectCaptureMode: %v", trial, err)
		}
		if duty && mode == timeprofile.CaptureException {
			t.Fatalf("trial %d: exception-only selected under a daily recording duty: %+v", trial, worker)
		}
		if worker.ExemptionStatus != timeprofile.Exempt && mode == timeprofile.CaptureException {
			t.Fatalf("trial %d: exception-only selected for a non-exempt classification: %+v", trial, worker)
		}
	}
}

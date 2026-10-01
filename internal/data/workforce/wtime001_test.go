package workforce_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
)

// TestTodo_WTIME_001 proves the backend half of the worker-record time
// profile: journey_worker's exemption status and time capture mode carry the
// closed tokens internal/domains/timeprofile/vocabulary.go declares, a
// contractor cannot assert an exemption status other than NOT_APPLICABLE, a
// SALARIED_NON_EXEMPT worker must actually be paid a salary, and worker_type
// maps onto a WorkerCategory including AGENCY_TEMP and PLATFORM_WORKER, which
// journey_worker's worker_type column has never enumerated.
func TestTodo_WTIME_001(t *testing.T) {
	t.Parallel()

	t.Run("a complete row with every new field set validates", func(t *testing.T) {
		row := newRow(uuid.New(), "wtime-complete")
		row.ExemptionStatus = string(timeprofile.NonExempt)
		row.TimeCaptureMode = string(timeprofile.CapturePunch)
		row.TimeProfileRef = "time-profile.hourly-punch/2026.1"
		if err := row.Validate(); err != nil {
			t.Fatalf("Validate(): %v", err)
		}
	})

	t.Run("a row asserting none of the three fields still validates", func(t *testing.T) {
		if err := newRow(uuid.New(), "wtime-unstated").Validate(); err != nil {
			t.Fatalf("Validate(): %v", err)
		}
	})

	for name, tc := range map[string]struct {
		mutate func(*workforce.WorkerRow)
	}{
		"exemption status outside the vocabulary": {func(r *workforce.WorkerRow) {
			r.ExemptionStatus = "SOMETIMES_EXEMPT"
		}},
		"time capture mode outside the vocabulary": {func(r *workforce.WorkerRow) {
			r.TimeCaptureMode = "BADGE_SWIPE"
		}},
		"time profile ref that is only whitespace": {func(r *workforce.WorkerRow) {
			r.TimeProfileRef = "   "
		}},
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			row := newRow(uuid.New(), "wtime-invalid")
			tc.mutate(&row)
			if err := row.Validate(); !errors.Is(err, workforce.ErrInvalidRow) {
				t.Fatalf("Validate() = %v, want %v", err, workforce.ErrInvalidRow)
			}
		})
	}

	t.Run("a contractor cannot be EXEMPT or NON_EXEMPT", func(t *testing.T) {
		for _, status := range []string{string(timeprofile.Exempt), string(timeprofile.NonExempt), string(timeprofile.SalariedNonExempt)} {
			row := newRow(uuid.New(), "wtime-contractor")
			row.WorkerType = "CONTRACTOR"
			row.ExemptionStatus = status
			if err := row.Validate(); !errors.Is(err, workforce.ErrInvalidRow) {
				t.Errorf("Validate() for contractor exemption %s = %v, want %v", status, err, workforce.ErrInvalidRow)
			}
		}
	})

	t.Run("a contractor asserting NOT_APPLICABLE validates", func(t *testing.T) {
		row := newRow(uuid.New(), "wtime-contractor-ok")
		row.WorkerType = "CONTRACTOR"
		row.ExemptionStatus = string(timeprofile.NotApplicable)
		if err := row.Validate(); err != nil {
			t.Fatalf("Validate(): %v", err)
		}
	})

	t.Run("worker_type matching is case-insensitive for the contractor rule", func(t *testing.T) {
		row := newRow(uuid.New(), "wtime-contractor-case")
		row.WorkerType = "contractor"
		row.ExemptionStatus = string(timeprofile.Exempt)
		if err := row.Validate(); !errors.Is(err, workforce.ErrInvalidRow) {
			t.Fatalf("Validate() = %v, want %v", err, workforce.ErrInvalidRow)
		}
	})

	t.Run("an unrecognised worker_type does not trip the contractor rule", func(t *testing.T) {
		row := newRow(uuid.New(), "wtime-unknown-type")
		row.WorkerType = "VOLUNTEER"
		row.ExemptionStatus = string(timeprofile.Exempt)
		if err := row.Validate(); err != nil {
			t.Fatalf("Validate() for an unrecognised worker_type: %v", err)
		}
	})

	t.Run("SALARIED_NON_EXEMPT requires a salaried pay basis", func(t *testing.T) {
		row := newRow(uuid.New(), "wtime-snexempt-bad")
		row.PayBasis = "HOURLY_RATE"
		row.ExemptionStatus = string(timeprofile.SalariedNonExempt)
		if err := row.Validate(); !errors.Is(err, workforce.ErrInvalidRow) {
			t.Fatalf("Validate() = %v, want %v", err, workforce.ErrInvalidRow)
		}
	})

	t.Run("SALARIED_NON_EXEMPT with a salaried pay basis validates", func(t *testing.T) {
		row := newRow(uuid.New(), "wtime-snexempt-ok")
		row.PayBasis = "ANNUAL_SALARY"
		row.ExemptionStatus = string(timeprofile.SalariedNonExempt)
		if err := row.Validate(); err != nil {
			t.Fatalf("Validate(): %v", err)
		}
	})
}

// TestTodo_WTIME_001_Property exercises WorkerCategoryFor across every input
// it must resolve deterministically: the four worker_type tokens the doc
// comment on migrations/00023 names, AGENCY_TEMP and PLATFORM_WORKER (neither
// of which any CHECK constraint enumerates, so nothing stops a caller writing
// them), arbitrary case and padding, and a token nobody has defined the
// meaning of.
func TestTodo_WTIME_001_Property(t *testing.T) {
	t.Parallel()
	cases := []struct {
		workerType string
		want       timeprofile.WorkerCategory
		ok         bool
	}{
		{"EMPLOYEE", timeprofile.CategoryEmployee, true},
		{"employee", timeprofile.CategoryEmployee, true},
		{"  Employee  ", timeprofile.CategoryEmployee, true},
		{"INTERN", timeprofile.CategoryEmployee, true},
		{"TEMPORARY", timeprofile.CategoryEmployee, true},
		{"CONTRACTOR", timeprofile.CategoryContractor, true},
		{"contractor", timeprofile.CategoryContractor, true},
		{"AGENCY_TEMP", timeprofile.CategoryAgencyTemp, true},
		{"agency_temp", timeprofile.CategoryAgencyTemp, true},
		{"PLATFORM_WORKER", timeprofile.CategoryPlatform, true},
		{"", "", false},
		{"VOLUNTEER", "", false},
		{"EMPLOYEE_CONTRACTOR", "", false},
	}
	for _, tc := range cases {
		got, ok := workforce.WorkerCategoryFor(tc.workerType)
		if ok != tc.ok || got != tc.want {
			t.Errorf("WorkerCategoryFor(%q) = (%q, %v), want (%q, %v)", tc.workerType, got, ok, tc.want, tc.ok)
		}
		// Every category WorkerCategoryFor can return is itself a valid
		// timeprofile.WorkerCategory token, so a caller that trusts the
		// mapping never receives a category the vocabulary would reject.
		if ok && !got.Valid() {
			t.Errorf("WorkerCategoryFor(%q) returned %q, which is not a valid WorkerCategory", tc.workerType, got)
		}
	}
}

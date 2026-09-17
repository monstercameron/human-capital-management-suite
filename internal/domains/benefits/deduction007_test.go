package benefits

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func ben007Input() DeductionInput {
	return DeductionInput{
		Tenant: "acme", WorkerRef: "worker-1", ElectionDigest: "sha256:election-head",
		RateVersion:    "rates-2026.01",
		AnnualEmployee: values.MustDecimal("2880.00", 2, values.RoundingHalfUp),
		AnnualEmployer: values.MustDecimal("9120.00", 2, values.RoundingHalfUp),
		Frequency:      FrequencyMonthly, PeriodStart: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		Arrears:     values.MustDecimal("0.00", 2, values.RoundingHalfUp),
		RetroAmount: values.MustDecimal("0.00", 2, values.RoundingHalfUp),
	}
}

// TestTodo_BEN_007 is the PRIMARY contract: plan rate, frequency,
// coverage, arrears and retro inputs yield exact employee/employer
// amounts with a trace; retro corrections are deltas, never rewrites.
func TestTodo_BEN_007(t *testing.T) {
	got, err := ComputeDeduction(ben007Input())
	if err != nil {
		t.Fatalf("ComputeDeduction: %v", err)
	}
	if got.EmployeeBase.String() != "240.00" || got.EmployerBase.String() != "760.00" {
		t.Fatalf("monthly bases must be 240.00/760.00: %+v", got)
	}
	if got.EmployeeTotal.String() != "240.00" || len(got.Trace) != 4 {
		t.Fatalf("clean period must total the base with a full trace: %+v", got)
	}
	if got.Digest == "" {
		t.Fatalf("result must seal a digest")
	}

	t.Run("retro correction is a delta, not a rewrite", func(t *testing.T) {
		in := ben007Input()
		in.RetroAmount = values.MustDecimal("120.00", 2, values.RoundingHalfUp)
		in.RetroCorrects = "2026-02"
		got, err := ComputeDeduction(in)
		if err != nil {
			t.Fatal(err)
		}
		if got.EmployeeBase.String() != "240.00" {
			t.Fatalf("retro must not rewrite the base: %+v", got)
		}
		if got.RetroDelta.String() != "120.00" || got.EmployeeTotal.String() != "360.00" {
			t.Fatalf("retro must ride as a delta: %+v", got)
		}
		nameless := in
		nameless.RetroCorrects = ""
		if _, err := ComputeDeduction(nameless); !errors.Is(err, ErrDeductionRejected) {
			t.Fatalf("unnamed retro must be BEN_007_REJECTED, got %v", err)
		}
	})

	t.Run("arrears apply to the employee share", func(t *testing.T) {
		in := ben007Input()
		in.Arrears = values.MustDecimal("60.00", 2, values.RoundingHalfUp)
		got, err := ComputeDeduction(in)
		if err != nil {
			t.Fatal(err)
		}
		if got.EmployeeTotal.String() != "300.00" || got.EmployerTotal.String() != "760.00" {
			t.Fatalf("arrears must land on the employee share only: %+v", got)
		}
	})
}

func TestTodo_BEN_007_Property(t *testing.T) {
	// Twelve monthly bases reconstruct the annual rate exactly here.
	var sum = values.MustDecimal("0.00", 2, values.RoundingHalfUp)
	for i := 0; i < 12; i++ {
		in := ben007Input()
		in.PeriodStart = time.Date(2026, time.January+time.Month(i), 1, 0, 0, 0, 0, time.UTC)
		got, err := ComputeDeduction(in)
		if err != nil {
			t.Fatal(err)
		}
		sum, err = sum.Add(got.EmployeeBase)
		if err != nil {
			t.Fatal(err)
		}
	}
	if sum.String() != "2880.00" {
		t.Fatalf("period bases must reconstruct the annual rate, got %s", sum.String())
	}
	// Frequency is exact: biweekly splits the same annual into 26ths.
	in := ben007Input()
	in.Frequency = FrequencyBiweekly
	got, err := ComputeDeduction(in)
	if err != nil {
		t.Fatal(err)
	}
	want, err := in.AnnualEmployee.Div(values.MustDecimal("26.00", 2, values.RoundingHalfUp), 2, values.RoundingHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	if !got.EmployeeBase.Equal(want) {
		t.Fatalf("biweekly base = %s, want %s", got.EmployeeBase.String(), want.String())
	}
	// Identical inputs digest identically.
	a, _ := ComputeDeduction(ben007Input())
	b, _ := ComputeDeduction(ben007Input())
	if a.Digest != b.Digest {
		t.Fatalf("identical deductions must digest identically")
	}
}

func TestTodo_BEN_007_Race(t *testing.T) {
	var wg sync.WaitGroup
	digests := make([]string, 8)
	errs := make([]error, 8)
	for i := range digests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := ComputeDeduction(ben007Input())
			if err == nil {
				digests[i] = got.Digest
			}
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("concurrent ComputeDeduction: %v", err)
		}
	}
	for i := 1; i < len(digests); i++ {
		if digests[i] != digests[0] {
			t.Fatalf("concurrent runs must agree: %q vs %q", digests[i], digests[0])
		}
	}
}

func TestTodo_BEN_007_Mutation(t *testing.T) {
	for name, mutate := range map[string]func(*DeductionInput){
		"election":  func(in *DeductionInput) { in.ElectionDigest = "" },
		"rate":      func(in *DeductionInput) { in.RateVersion = "" },
		"frequency": func(in *DeductionInput) { in.Frequency = "FORTNIGHTLY" },
	} {
		in := ben007Input()
		mutate(&in)
		if _, err := ComputeDeduction(in); !errors.Is(err, ErrDeductionRejected) {
			t.Fatalf("bad %s must be BEN_007_REJECTED", name)
		}
	}
	neg := ben007Input()
	neg.AnnualEmployee = values.MustDecimal("-1.00", 2, values.RoundingHalfUp)
	if _, err := ComputeDeduction(neg); !errors.Is(err, ErrDeductionRejected) {
		t.Fatalf("negative rate must be BEN_007_REJECTED")
	}
	over := ben007Input()
	over.RetroAmount = values.MustDecimal("-1000.00", 2, values.RoundingHalfUp)
	over.RetroCorrects = "2026-02"
	if _, err := ComputeDeduction(over); !errors.Is(err, ErrDeductionRejected) {
		t.Fatalf("retro driving the total negative must be BEN_007_REJECTED")
	}
}

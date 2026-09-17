package payroll

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func wageDecimal(t *testing.T, s string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(s, 4, values.RoundingHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func wageInput(t *testing.T) WageInput {
	t.Helper()
	monday := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	return WageInput{
		Tenant: "acme", WorkerRef: "worker-1",
		WorkweekStart: monday, Timezone: "America/New_York",
		DailyHours: []values.Decimal{
			wageDecimal(t, "8"), wageDecimal(t, "8"), wageDecimal(t, "8"),
			wageDecimal(t, "8"), wageDecimal(t, "8"), wageDecimal(t, "10"),
			wageDecimal(t, "0"),
		},
		Approved: true, ApprovedBy: "manager-2",
		Exemption:          ExemptionNonexempt,
		RegularRate:        wageDecimal(t, "20"),
		MinimumWage:        wageDecimal(t, "16"),
		OvertimeMultiple:   wageDecimal(t, "1.5"),
		DoubleTimeMultiple: wageDecimal(t, "2"),
		Rounding:           values.RoundingHalfUp,
		RuleVersion:        "wage-rules/2026.1",
	}
}

// TestTodo_WAGE_001 is the primary WAGE-001 contract test: exact
// regular/overtime/double-time/premium/break/minimum-wage results with a
// rule trace, while missing inputs never produce payable output.
func TestTodo_WAGE_001(t *testing.T) {
	t.Run("golden week classifies regular overtime and premium", func(t *testing.T) {
		got, err := EvaluateWages(wageInput(t))
		if err != nil {
			t.Fatalf("EvaluateWages: %v", err)
		}
		// 5x8 regular plus a 10-hour Saturday: 40 regular, 10 overtime at
		// 1.5x (2 daily excess + 8 weekly excess over a 40-hour base).
		if got.RegularHours.String() != "40.00" {
			t.Fatalf("regular hours = %s, want 40.00", got.RegularHours)
		}
		if got.OvertimeHours.String() != "10.00" {
			t.Fatalf("overtime hours = %s, want 10.00", got.OvertimeHours)
		}
		if got.GrossPay.String() != "1100.00" {
			t.Fatalf("gross pay = %s, want 1100.00", got.GrossPay)
		}
		if got.Status != WageOK {
			t.Fatalf("status = %v, want OK", got.Status)
		}
		if len(got.RuleTrace) == 0 || got.Digest == "" {
			t.Fatalf("result must carry a rule trace and digest: %+v", got)
		}
	})

	t.Run("double time prices hours past twelve", func(t *testing.T) {
		input := wageInput(t)
		input.DailyHours[5] = wageDecimal(t, "14")
		got, err := EvaluateWages(input)
		if err != nil {
			t.Fatal(err)
		}
		if got.DoubleTimeHours.String() != "2.00" {
			t.Fatalf("double-time hours = %s, want 2.00", got.DoubleTimeHours)
		}
		// 40 regular + 12 overtime (4 daily + 8 weekly) + 2 double-time.
		if got.GrossPay.String() != "1240.00" {
			t.Fatalf("gross pay = %s, want 1240.00", got.GrossPay)
		}
	})

	t.Run("exempt workers earn straight time only", func(t *testing.T) {
		input := wageInput(t)
		input.Exemption = ExemptionExempt
		got, err := EvaluateWages(input)
		if err != nil {
			t.Fatal(err)
		}
		if !got.OvertimeHours.IsZero() || !got.DoubleTimeHours.IsZero() {
			t.Fatalf("exempt week must have no premium hours: %+v", got)
		}
		if got.GrossPay.String() != "1000.00" {
			t.Fatalf("gross pay = %s, want 1000.00", got.GrossPay)
		}
	})

	t.Run("minimum wage floors low rates for review", func(t *testing.T) {
		input := wageInput(t)
		input.RegularRate = wageDecimal(t, "10")
		got, err := EvaluateWages(input)
		if err != nil {
			t.Fatal(err)
		}
		if got.MinimumWageTopUp.Sign() <= 0 {
			t.Fatalf("sub-minimum rate must accrue a top-up: %+v", got)
		}
		if got.Status != WageReviewRequired {
			t.Fatalf("status = %v, want REVIEW_REQUIRED", got.Status)
		}
		// 50 payable hours at the 16.00 floor.
		if got.GrossPay.String() != "800.00" {
			t.Fatalf("gross pay = %s, want 800.00", got.GrossPay)
		}
	})

	t.Run("missed meal periods accrue premium hours", func(t *testing.T) {
		input := wageInput(t)
		input.MissedMealPeriods = 2
		got, err := EvaluateWages(input)
		if err != nil {
			t.Fatal(err)
		}
		if got.PremiumHours.String() != "2.00" {
			t.Fatalf("premium hours = %s, want 2.00", got.PremiumHours)
		}
		if got.GrossPay.String() != "1140.00" {
			t.Fatalf("gross pay = %s, want 1140.00", got.GrossPay)
		}
	})

	t.Run("breaks reduce payable hours", func(t *testing.T) {
		input := wageInput(t)
		input.UnpaidBreakHours = wageDecimal(t, "2.5")
		input.BreaksSet = true
		got, err := EvaluateWages(input)
		if err != nil {
			t.Fatal(err)
		}
		if got.RegularHours.String() != "37.50" {
			t.Fatalf("regular hours = %s, want 37.50", got.RegularHours)
		}
	})

	t.Run("missing inputs never produce payable output", func(t *testing.T) {
		base := wageInput(t)
		cases := map[string]func(*WageInput){
			"workweek":  func(i *WageInput) { i.WorkweekStart = time.Time{} },
			"timezone":  func(i *WageInput) { i.Timezone = "" },
			"approval":  func(i *WageInput) { i.Approved = false },
			"exemption": func(i *WageInput) { i.Exemption = "" },
			"rate":      func(i *WageInput) { i.RegularRate = values.Decimal{} },
			"minimum":   func(i *WageInput) { i.MinimumWage = values.Decimal{} },
			"overtime":  func(i *WageInput) { i.OvertimeMultiple = values.Decimal{} },
			"rounding":  func(i *WageInput) { i.Rounding = values.RoundingUnspecified },
			"rules":     func(i *WageInput) { i.RuleVersion = "" },
			"days":      func(i *WageInput) { i.DailyHours = nil },
		}
		for name, mutate := range cases {
			input := base
			mutate(&input)
			if _, err := EvaluateWages(input); err == nil {
				t.Fatalf("%s: missing input must not produce payable output", name)
			}
		}
	})

	t.Run("undeclared exemption is unknown with zero pay", func(t *testing.T) {
		input := wageInput(t)
		input.Exemption = ExemptionUndeclared
		got, err := EvaluateWages(input)
		if err != nil {
			t.Fatalf("undeclared exemption must return UNKNOWN, not an error: %v", err)
		}
		if got.Status != WageUnknown || !got.GrossPay.IsZero() {
			t.Fatalf("undeclared exemption must be UNKNOWN with zero pay: %+v", got)
		}
	})
}

// TestTodo_WAGE_001_Property holds the wage algebra: payable hours are
// conserved, gross equals its components, and evaluation is pure.
func TestTodo_WAGE_001_Property(t *testing.T) {
	t.Run("hours conserved and gross decomposes", func(t *testing.T) {
		input := wageInput(t)
		input.DailyHours[0] = wageDecimal(t, "13")
		input.MissedMealPeriods = 1
		got, err := EvaluateWages(input)
		if err != nil {
			t.Fatal(err)
		}
		total := wageDecimal(t, "0")
		for _, day := range input.DailyHours {
			var err error
			total, err = total.Add(day)
			if err != nil {
				t.Fatal(err)
			}
		}
		classified := values.MustDecimal("0", 2, values.RoundingHalfUp)
		for _, part := range []values.Decimal{got.RegularHours, got.OvertimeHours, got.DoubleTimeHours} {
			var err error
			classified, err = classified.Add(part)
			if err != nil {
				t.Fatal(err)
			}
		}
		if !total.Equal(classified) {
			t.Fatalf("classified %s != reported %s", classified, total)
		}
		rate := input.RegularRate
		otRate, err := rate.Mul(input.OvertimeMultiple, 4, values.RoundingHalfUp)
		if err != nil {
			t.Fatal(err)
		}
		dtRate, err := rate.Mul(input.DoubleTimeMultiple, 4, values.RoundingHalfUp)
		if err != nil {
			t.Fatal(err)
		}
		recomputed := wageDecimal(t, "0")
		for _, leg := range []struct {
			hours values.Decimal
			rate  values.Decimal
		}{
			{got.RegularHours, rate}, {got.OvertimeHours, otRate},
			{got.DoubleTimeHours, dtRate}, {got.PremiumHours, rate},
		} {
			hours4, err := leg.hours.Quantize(4, values.RoundingHalfUp)
			if err != nil {
				t.Fatal(err)
			}
			legPay, err := hours4.Mul(leg.rate, 4, values.RoundingHalfUp)
			if err != nil {
				t.Fatal(err)
			}
			recomputed, err = recomputed.Add(legPay)
			if err != nil {
				t.Fatal(err)
			}
		}
		topUp4, err := got.MinimumWageTopUp.Quantize(4, values.RoundingHalfUp)
		if err != nil {
			t.Fatal(err)
		}
		recomputed, err = recomputed.Add(topUp4)
		if err != nil {
			t.Fatal(err)
		}
		recomputed, err = recomputed.Quantize(2, values.RoundingHalfUp)
		if err != nil {
			t.Fatal(err)
		}
		if !recomputed.Equal(got.GrossPay) {
			t.Fatalf("gross %s != components %s", got.GrossPay, recomputed)
		}
	})

	t.Run("evaluation is deterministic", func(t *testing.T) {
		first, err := EvaluateWages(wageInput(t))
		if err != nil {
			t.Fatal(err)
		}
		second, err := EvaluateWages(wageInput(t))
		if err != nil {
			t.Fatal(err)
		}
		if first.Digest != second.Digest || !first.GrossPay.Equal(second.GrossPay) {
			t.Fatal("same input must evaluate identically")
		}
	})
}

// TestTodo_WAGE_001_Race runs concurrent evaluations of shared input: the
// pure engine must report no interference.
func TestTodo_WAGE_001_Race(t *testing.T) {
	input := wageInput(t)
	done := make(chan WageResult, 16)
	for i := 0; i < 8; i++ {
		go func() {
			got, err := EvaluateWages(input)
			if err != nil {
				t.Errorf("EvaluateWages: %v", err)
				return
			}
			done <- got
		}()
	}
	first := <-done
	for i := 1; i < 8; i++ {
		if got := <-done; got.Digest != first.Digest {
			t.Fatalf("concurrent evaluations diverged: %q vs %q", got.Digest, first.Digest)
		}
	}
}

// TestTodo_WAGE_001_Security proves self-approved time is never payable and
// rule versions are bound to every result.
func TestTodo_WAGE_001_Security(t *testing.T) {
	input := wageInput(t)
	input.ApprovedBy = "worker-1"
	if _, err := EvaluateWages(input); err == nil {
		t.Fatal("self-approved time must never be payable")
	}
	got, err := EvaluateWages(wageInput(t))
	if err != nil {
		t.Fatal(err)
	}
	if got.RuleVersion != "wage-rules/2026.1" {
		t.Fatalf("result must bind its rule version: %+v", got)
	}
	var _ = errors.Is
}

// TestTodo_WAGE_001_Mutation kills the arithmetic mutants: halved
// multipliers, dropped top-ups and skipped approvals must each fail.
func TestTodo_WAGE_001_Mutation(t *testing.T) {
	got, err := EvaluateWages(wageInput(t))
	if err != nil {
		t.Fatal(err)
	}
	if got.GrossPay.String() != "1100.00" {
		t.Fatalf("multiplier mutant survived: gross = %s", got.GrossPay)
	}
	low := wageInput(t)
	low.RegularRate = wageDecimal(t, "10")
	topped, err := EvaluateWages(low)
	if err != nil {
		t.Fatal(err)
	}
	if topped.MinimumWageTopUp.IsZero() {
		t.Fatal("top-up mutant survived: sub-minimum week has no top-up")
	}
	unapproved := wageInput(t)
	unapproved.Approved = false
	if _, err := EvaluateWages(unapproved); err == nil {
		t.Fatal("approval mutant survived: unapproved time is payable")
	}
}

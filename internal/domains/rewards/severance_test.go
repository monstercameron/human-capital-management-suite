package rewards

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// severanceTestPlan is the published plan under test: two weeks per year of
// tenure, floored at four weeks and capped at twenty-six, in USD cents with
// half-even rounding.
func severanceTestPlan(t *testing.T) SeverancePlan {
	t.Helper()
	wpy, err := values.NewDecimal("2", 0, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	min, err := values.NewDecimal("4", 0, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	max, err := values.NewDecimal("26", 0, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return SeverancePlan{
		Version:       "acme-severance/2026-01-01",
		WeeksPerYear:  wpy,
		MinWeeks:      min,
		MaxWeeks:      max,
		Currency:      "USD",
		MoneyScale:    2,
		MoneyRounding: values.RoundingHalfEven,
	}
}

func severanceTestInput(t *testing.T, weeklyPay, tenureYears string) SeveranceInput {
	t.Helper()
	pay, err := values.NewMoney(weeklyPay, "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	tenure, err := values.NewDecimal(tenureYears, 1, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return SeveranceInput{WeeklyPay: pay, TenureYears: tenure}
}

// TestTodo_WF_CAP_0010... PRIMARY for WF-CAP-010: raw, floored and capped
// tenure price exactly, and invalid plans, inputs and currencies fail closed
// against the sentinel errors.
func TestTodo_WF_CAP_010(t *testing.T) {
	plan := severanceTestPlan(t)

	t.Run("raw tenure prices exactly", func(t *testing.T) {
		// 5.0 years x 2 weeks/year = 10 weeks; 10 x $1,500.00 = $15,000.00.
		got, err := CalculateSeverance(plan, severanceTestInput(t, "1500.00", "5.0"))
		if err != nil {
			t.Fatal(err)
		}
		if !got.CreditedWeeks.Equal(mustTestDecimal(t, "10.0000")) {
			t.Fatalf("credited weeks = %s, want 10.0000", got.CreditedWeeks.String())
		}
		if got.Amount.String() != "15000.00 USD" {
			t.Fatalf("amount = %s, want 15000.00 USD", got.Amount.String())
		}
		if got.PlanVersion != plan.Version {
			t.Fatalf("plan version = %q, want %q", got.PlanVersion, plan.Version)
		}
		if !strings.Contains(got.Explanation, "15000.00 USD") {
			t.Fatalf("explanation does not derive the amount: %q", got.Explanation)
		}
	})

	t.Run("short tenure takes the floor", func(t *testing.T) {
		// 0.5 years x 2 = 1 week, floored to 4; 4 x $1,000.00 = $4,000.00.
		got, err := CalculateSeverance(plan, severanceTestInput(t, "1000.00", "0.5"))
		if err != nil {
			t.Fatal(err)
		}
		if !got.CreditedWeeks.Equal(mustTestDecimal(t, "4.0000")) {
			t.Fatalf("credited weeks = %s, want 4.0000", got.CreditedWeeks.String())
		}
		if got.Amount.String() != "4000.00 USD" {
			t.Fatalf("amount = %s, want 4000.00 USD", got.Amount.String())
		}
		if !strings.Contains(got.Explanation, "floor applied") {
			t.Fatalf("explanation does not name the floor: %q", got.Explanation)
		}
	})

	t.Run("long tenure takes the cap", func(t *testing.T) {
		// 20.0 years x 2 = 40 weeks, capped to 26; 26 x $2,000.00 = $52,000.00.
		got, err := CalculateSeverance(plan, severanceTestInput(t, "2000.00", "20.0"))
		if err != nil {
			t.Fatal(err)
		}
		if !got.CreditedWeeks.Equal(mustTestDecimal(t, "26.0000")) {
			t.Fatalf("credited weeks = %s, want 26.0000", got.CreditedWeeks.String())
		}
		if got.Amount.String() != "52000.00 USD" {
			t.Fatalf("amount = %s, want 52000.00 USD", got.Amount.String())
		}
		if !strings.Contains(got.Explanation, "cap applied") {
			t.Fatalf("explanation does not name the cap: %q", got.Explanation)
		}
	})

	t.Run("zero tenure still pays the floor", func(t *testing.T) {
		got, err := CalculateSeverance(plan, severanceTestInput(t, "1000.00", "0.0"))
		if err != nil {
			t.Fatal(err)
		}
		if got.Amount.String() != "4000.00 USD" {
			t.Fatalf("amount = %s, want 4000.00 USD", got.Amount.String())
		}
	})

	t.Run("currency mismatch makes no FX decision", func(t *testing.T) {
		pay, err := values.NewMoney("1000.00", "EUR", 2, values.RoundingHalfEven)
		if err != nil {
			t.Fatal(err)
		}
		tenure, err := values.NewDecimal("5.0", 1, values.RoundingHalfEven)
		if err != nil {
			t.Fatal(err)
		}
		_, err = CalculateSeverance(plan, SeveranceInput{WeeklyPay: pay, TenureYears: tenure})
		if !errors.Is(err, ErrSeveranceCurrencyMismatch) {
			t.Fatalf("err = %v, want ErrSeveranceCurrencyMismatch", err)
		}
	})

	t.Run("negative tenure is invalid", func(t *testing.T) {
		pay, err := values.NewMoney("1000.00", "USD", 2, values.RoundingHalfEven)
		if err != nil {
			t.Fatal(err)
		}
		tenure, err := values.NewDecimal("-1.0", 1, values.RoundingHalfEven)
		if err != nil {
			t.Fatal(err)
		}
		_, err = CalculateSeverance(plan, SeveranceInput{WeeklyPay: pay, TenureYears: tenure})
		if !errors.Is(err, ErrSeveranceInvalid) {
			t.Fatalf("err = %v, want ErrSeveranceInvalid", err)
		}
	})

	t.Run("inverted bounds are an invalid plan", func(t *testing.T) {
		bad := plan
		bad.MinWeeks, bad.MaxWeeks = bad.MaxWeeks, bad.MinWeeks
		_, err := CalculateSeverance(bad, severanceTestInput(t, "1000.00", "5.0"))
		if !errors.Is(err, ErrSeveranceInvalid) {
			t.Fatalf("err = %v, want ErrSeveranceInvalid", err)
		}
	})

	t.Run("missing version is an invalid plan", func(t *testing.T) {
		bad := plan
		bad.Version = ""
		_, err := CalculateSeverance(bad, severanceTestInput(t, "1000.00", "5.0"))
		if !errors.Is(err, ErrSeveranceInvalid) {
			t.Fatalf("err = %v, want ErrSeveranceInvalid", err)
		}
	})
}

// TestTodo_WF_CAP_010_Golden pins the exact derivation: one worked example
// whose weeks, amount and explanation bytes must never drift.
func TestTodo_WF_CAP_010_Golden(t *testing.T) {
	plan := severanceTestPlan(t)
	got, err := CalculateSeverance(plan, severanceTestInput(t, "1500.00", "5.0"))
	if err != nil {
		t.Fatal(err)
	}
	want := "severance plan acme-severance/2026-01-01: 5.0 years x 2 weeks/year = 10.0000 weeks; " +
		"bounds [4, 26] -> 10.0000 weeks (raw); 10.0000 weeks x 1500.00 USD/week = 15000.00 USD"
	if got.Explanation != want {
		t.Fatalf("explanation = %q, want %q", got.Explanation, want)
	}
	if got.Amount.String() != "15000.00 USD" {
		t.Fatalf("amount = %s, want 15000.00 USD", got.Amount.String())
	}
}

func mustTestDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 4, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

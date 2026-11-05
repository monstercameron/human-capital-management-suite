package app

import (
	"context"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// severanceParityPlan is the published plan the served severance binding
// answers with: two weeks per year of tenure, floored at four weeks and
// capped at twenty-six, in USD cents with half-even rounding.
func severanceParityPlan(t *testing.T) rewards.SeverancePlan {
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
	return rewards.SeverancePlan{
		Version:       "acme-severance/2026-01-01",
		WeeksPerYear:  wpy,
		MinWeeks:      min,
		MaxWeeks:      max,
		Currency:      "USD",
		MoneyScale:    2,
		MoneyRounding: values.RoundingHalfEven,
	}
}

func severanceParityInput(t *testing.T) SeveranceInputs {
	t.Helper()
	pay, err := values.NewMoney("1500.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	tenure, err := values.NewDecimal("5.0", 1, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return SeveranceInputs{
		Plan:  severanceParityPlan(t),
		Query: rewards.SeveranceInput{WeeklyPay: pay, TenureYears: tenure},
	}
}

// TestTodo_WF_CAP_010_ServedSeveranceParity proves the calculate_severance
// binding answers with the real rewards engine, not a stub or an echo: the
// capability-table handler from handlerFor, driven with the engine's
// canonical input, returns a result identical to calling the engine
// directly, with the same pinned versions, result digest, and zero-effect
// evidence.
func TestTodo_WF_CAP_010_ServedSeveranceParity(t *testing.T) {
	ctx := context.Background()
	served := &domainHandlers{}

	handle := served.handlerFor(rewards.CalculateSeveranceIntentType)
	if handle == nil {
		t.Fatal("no handler bound for calculate_severance on the serve path")
	}
	in := severanceParityInput(t)
	got, err := handle(ctx, in)
	if err != nil {
		t.Fatalf("served calculate_severance: %v", err)
	}
	servedResult, ok := got.(rewards.SeveranceResult)
	if !ok {
		t.Fatalf("served calculate_severance returned %T, want rewards.SeveranceResult", got)
	}

	direct, err := rewards.CalculateSeverance(in.Plan, in.Query)
	if err != nil {
		t.Fatalf("direct engine severance: %v", err)
	}

	if servedResult.ResultDigest == "" || servedResult.ResultDigest != direct.ResultDigest {
		t.Fatalf("served digest %q != engine digest %q: the serve path is not running the engine's answer",
			servedResult.ResultDigest, direct.ResultDigest)
	}
	if !reflect.DeepEqual(servedResult, direct) {
		t.Fatal("served result differs from the direct engine result beyond its digest")
	}
	if got := servedResult.Amount.String(); got != "15000.00 USD" {
		t.Fatalf("served amount = %s, want 15000.00 USD for 5 years at $1,500/week and 2 weeks/year", got)
	}
	if !servedResult.Effects.IsZero() || servedResult.InputsDigest == "" {
		t.Fatalf("served simulation violated zero-effect evidence: effects=%v inputs=%q",
			servedResult.Effects.NonZero(), servedResult.InputsDigest)
	}
}

// TestTodo_WF_CAP_010_ServedSeveranceRejectsBadInput proves the served
// binding does not launder invalid input into a plausible answer: what the
// engine refuses, the serve path refuses.
func TestTodo_WF_CAP_010_ServedSeveranceRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	served := &domainHandlers{}
	handle := served.handlerFor(rewards.CalculateSeveranceIntentType)
	in := severanceParityInput(t)
	in.Plan.MaxWeeks = in.Plan.MinWeeks
	// Min above max is an unusable plan; keep the suite honest by forcing
	// the inversion through the plan the engine validates.
	zero, err := values.NewDecimal("0", 0, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	in.Plan.WeeksPerYear = zero
	if _, err := handle(ctx, in); err == nil {
		t.Fatal("served calculate_severance accepted an unusable plan the engine must refuse")
	}
}

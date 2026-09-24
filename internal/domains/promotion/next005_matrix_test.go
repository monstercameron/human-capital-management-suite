package promotion_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
)

// TestTodo_NEXT_005_Property varies the promotion's decision inputs and checks
// that every computed answer remains a simulation: a blocked or incomplete
// candidate may change its verdict, but it never gains write authority.
func TestTodo_NEXT_005_Property(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*promotion.PreflightRequest)
	}{
		{name: "ready"},
		{name: "retroactive", mutate: func(r *promotion.PreflightRequest) { r.EffectiveDate = date(t, "2025-10-01") }},
		{name: "missing-band", mutate: func(r *promotion.PreflightRequest) { r.Target.JobCode = "NOT-A-JOB" }},
		{name: "currency-mismatch", mutate: func(r *promotion.PreflightRequest) { r.Proposed = snapshot(t, "98000.00", "EUR", "0.0500", 11) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := baseRequest(t)
			if tc.mutate != nil {
				tc.mutate(&req)
			}
			result, err := promotion.SimulatePromotion(context.Background(), catalog(t), req)
			if err != nil {
				t.Fatalf("SimulatePromotion: %v", err)
			}
			if !result.Effects.IsZero() || !result.Preflight.Effects.IsZero() || !result.Compensation.Effects.IsZero() {
				t.Fatalf("case %s counted a prohibited effect: result=%v preflight=%v compensation=%v",
					tc.name, result.Effects.NonZero(), result.Preflight.Effects.NonZero(), result.Compensation.Effects.NonZero())
			}
			if err := result.Receipt.Validate(); err != nil {
				t.Fatalf("case %s receipt: %v", tc.name, err)
			}
			if string(result.Receipt.Mode) != "SIMULATE" || result.Receipt.ExecutionState != "NOT_PLANNED" {
				t.Fatalf("case %s receipt grants an unexpected lifecycle: mode=%s execution=%s",
					tc.name, result.Receipt.Mode, result.Receipt.ExecutionState)
			}
			if tc.name != "ready" && result.Executable {
				t.Fatalf("case %s is not a ready candidate but is marked executable", tc.name)
			}
		})
	}
}

// TestTodo_NEXT_005_Golden pins the caller-visible business result for the
// canonical P1A promotion fixture. The shared golden covers status, every
// changed before/after value, compensation, budget authority, findings, and
// the zero-effect result in a stable reviewable form.
func TestTodo_NEXT_005_Golden(t *testing.T) {
	result, err := promotion.SimulatePromotion(context.Background(), catalog(t), baseRequest(t))
	if err != nil {
		t.Fatalf("SimulatePromotion: %v", err)
	}
	compareGolden(t, filepath.Join("testdata", "golden", "ready-promotion.txt"), renderSimulation(result))
}

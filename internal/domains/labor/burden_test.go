package labor

import (
	"errors"
	"math/rand"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func burdenRequest(t *testing.T) BurdenRequest {
	t.Helper()
	rule := validLaborRule(t)
	return BurdenRequest{
		CalculationID: "burden-week-26",
		Rule:          rule,
		RuleID:        rule.ID,
		RuleVersion:   rule.Version,
		Currency:      "USD",
		Wages:         laborDecimal(t, "1920.00"),
		AmountScale:   2,
		Rounding:      values.RoundingHalfUp,
		Items: []BurdenItem{
			{Kind: BurdenTax, Name: "fica", Basis: laborDecimal(t, "1920.00"), Rate: laborDecimal(t, "0.08"), Capped: true, Cap: laborDecimal(t, "2000.00"), Period: "2026-W26"},
			{Kind: BurdenBenefit, Name: "health", Basis: laborDecimal(t, "1920.00"), Rate: laborDecimal(t, "0.12"), Period: "2026-W26"},
			{Kind: BurdenAllowance, Name: "meal", Basis: laborDecimal(t, "500.00"), Rate: laborDecimal(t, "0.10"), Period: "2026-W26"},
			{Kind: BurdenOverhead, Name: "facilities", Basis: laborDecimal(t, "1920.00"), Rate: laborDecimal(t, "0.05"), Period: "2026-W26"},
		},
	}
}

// TestTodo_LABOR_004 is the primary acceptance case: every burden component
// names basis, rate, cap and period and yields exact totals, while an omitted
// currency, component or cap binding is rejected instead of passing on
// totals alone.
func TestTodo_LABOR_004(t *testing.T) {
	got, err := CalculateBurden(burdenRequest(t))
	if err != nil {
		t.Fatalf("CalculateBurden: %v", err)
	}
	// fica 1920*.08=153.60; health 1920*.12=230.40; meal 500*.10=50.00;
	// facilities 1920*.05=96.00; burden=530.00; cost=2450.00.
	want := map[string]string{
		"fica": "153.60", "health": "230.40", "meal": "50.00", "facilities": "96.00",
	}
	for _, item := range got.Items {
		if want[item.Name] != item.Amount.String() {
			t.Fatalf("%s = %s, want %s", item.Name, item.Amount, want[item.Name])
		}
	}
	if got.TotalBurden.String() != "530.00" {
		t.Fatalf("burden = %s, want 530.00", got.TotalBurden)
	}
	if got.TotalCost.String() != "2450.00" {
		t.Fatalf("cost = %s, want 2450.00", got.TotalCost)
	}
	rejectEmptyDigest(t, got.Digest)
	if err := got.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	// The uncapped health item is exact but records an unknown range.
	if got.Complete {
		t.Fatal("uncapped exposure reported complete")
	}
	if len(got.UnknownRanges) == 0 {
		t.Fatal("no unknown range recorded for uncapped item")
	}

	cases := map[string]func(*BurdenRequest){
		"omitted currency": func(r *BurdenRequest) {
			r.Currency = ""
		},
		"currency mismatch": func(r *BurdenRequest) {
			r.Currency = "EUR"
		},
		"omitted component": func(r *BurdenRequest) {
			r.Items = r.Items[:3]
		},
		"unknown kind": func(r *BurdenRequest) {
			r.Items[0].Kind = "BONUS"
		},
		"missing period": func(r *BurdenRequest) {
			r.Items[1].Period = ""
		},
		"missing basis": func(r *BurdenRequest) {
			r.Items[2].Basis = values.Decimal{}
		},
		"capped without cap": func(r *BurdenRequest) {
			r.Items[0].Capped = true
			r.Items[0].Cap = values.Decimal{}
		},
		"stale rule version": func(r *BurdenRequest) {
			r.RuleVersion = "v0"
		},
	}
	for name, mutate := range cases {
		req := burdenRequest(t)
		mutate(&req)
		if _, err := CalculateBurden(req); !errors.Is(err, ErrBurdenRejected) {
			t.Fatalf("%s: err = %v, want LABOR_004_REJECTED", name, err)
		}
	}
	// A cap below the basis binds the capped basis exactly.
	capped := burdenRequest(t)
	capped.Items[0].Cap = laborDecimal(t, "1000.00")
	cappedGot, err := CalculateBurden(capped)
	if err != nil {
		t.Fatalf("capped: %v", err)
	}
	if cappedGot.Items[0].Amount.String() != "80.00" {
		t.Fatalf("capped fica = %s, want 80.00", cappedGot.Items[0].Amount)
	}
}

// TestTodo_LABOR_004_Property proves totals always equal wages plus the
// exact component sum, and capped assays never exceed rate times cap.
func TestTodo_LABOR_004_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(20260917))
	for i := 0; i < 48; i++ {
		req := burdenRequest(t)
		req.Wages = laborDecimal(t, itoa2(rng.Intn(5000), rng.Intn(100)))
		for n := range req.Items {
			req.Items[n].Basis = laborDecimal(t, itoa2(rng.Intn(5000), rng.Intn(100)))
			req.Items[n].Capped = rng.Intn(2) == 0
			if req.Items[n].Capped {
				req.Items[n].Cap = laborDecimal(t, itoa2(rng.Intn(5000), rng.Intn(100)))
			}
		}
		got, err := CalculateBurden(req)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		sum := laborDecimal(t, "0.00")
		for _, item := range got.Items {
			var err error
			sum, err = sum.Add(item.Amount)
			if err != nil {
				t.Fatal(err)
			}
			if item.Capped {
				bound, err := item.Cap.Mul(item.Rate, 2, values.RoundingHalfUp)
				if err != nil {
					t.Fatal(err)
				}
				if item.Amount.Cmp(bound) > 0 {
					t.Fatalf("case %d %s: %s exceeds capped bound %s", i, item.Name, item.Amount, bound)
				}
			}
		}
		if !sum.Equal(got.TotalBurden) {
			t.Fatalf("case %d: components sum %s, burden %s", i, sum, got.TotalBurden)
		}
		total, err := req.Wages.Add(got.TotalBurden)
		if err != nil {
			t.Fatal(err)
		}
		if !total.Equal(got.TotalCost) {
			t.Fatalf("case %d: wages plus burden %s, cost %s", i, total, got.TotalCost)
		}
	}
}

// TestTodo_LABOR_004_Mutation proves the digest binds currency and every
// component: any change yields a new digest and tampering fails validation.
func TestTodo_LABOR_004_Mutation(t *testing.T) {
	base, err := CalculateBurden(burdenRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*BurdenRequest){
		func(r *BurdenRequest) { r.Wages = laborDecimal(t, "1920.01") },
		func(r *BurdenRequest) { r.Items[0].Basis = laborDecimal(t, "1921.00") },
		func(r *BurdenRequest) { r.Items[1].Rate = laborDecimal(t, "0.13") },
		func(r *BurdenRequest) { r.Items[0].Cap = laborDecimal(t, "1500.00") },
		func(r *BurdenRequest) { r.Items[2].Period = "2026-W27" },
		func(r *BurdenRequest) { r.Currency = "USD"; r.Items[3].Name = "rent" },
	}
	for i, mutate := range mutations {
		req := burdenRequest(t)
		mutate(&req)
		mutated, err := CalculateBurden(req)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if mutated.Digest == base.Digest {
			t.Fatalf("case %d did not change the digest", i)
		}
	}
	dropped := base
	dropped.Items = dropped.Items[:3]
	if err := dropped.Validate(); err == nil {
		t.Fatal("dropped component passed validation")
	}
	forged := base
	forged.Digest = "sha256:forged"
	if err := forged.Validate(); err == nil {
		t.Fatal("forged digest passed validation")
	}
}

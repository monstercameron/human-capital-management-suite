package labor

import (
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/attendance"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func wageRequest(t *testing.T) WageRequest {
	t.Helper()
	rule := validLaborRule(t)
	return WageRequest{
		CalculationID:     "wage-week-26",
		Rule:              rule,
		RuleID:            rule.ID,
		RuleVersion:       rule.Version,
		Jurisdiction:      "US-CA",
		JurisdictionRules: laborRef("jurisdiction-pack"),
		RegularHours:      laborDecimal(t, "40.00"),
		OvertimeHours:     laborDecimal(t, "5.00"),
		DifferentialHours: laborDecimal(t, "8.00"),
		PremiumHours:      laborDecimal(t, "0.00"),
		PremiumRate:       laborDecimal(t, "40.00"),
		PremiumRuleRef:    laborRef("meal-rest-premium"),
		AmountScale:       2,
		Rounding:          values.RoundingHalfUp,
	}
}

func TestTodo_REV_045_01(t *testing.T) {
	req := wageRequest(t)
	req.PremiumHours = laborDecimal(t, "3.00")
	req.PremiumRate = laborDecimal(t, "45.00")
	got, err := CalculateWages(req)
	if err != nil {
		t.Fatal(err)
	}
	if got.PremiumPay.String() != "135.00" || got.TotalPay.String() != "2055.00" {
		t.Fatalf("premium=%s total=%s", got.PremiumPay, got.TotalPay)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	got.PremiumPay = laborDecimal(t, "0.00")
	if err := got.Validate(); !errors.Is(err, ErrWageRejected) {
		t.Fatalf("tampered premium err=%v", err)
	}
	missingRate := wageRequest(t)
	missingRate.PremiumHours = laborDecimal(t, "1.00")
	missingRate.PremiumRate = values.Decimal{}
	if _, err := CalculateWages(missingRate); !errors.Is(err, ErrWageRejected) {
		t.Fatalf("missing regular premium rate err=%v", err)
	}
}

func TestTodo_REV_045_01_AttendanceToLabor(t *testing.T) {
	findings := []attendance.Finding{{Kind: attendance.MealException, ShiftID: "s1"}, {Kind: attendance.MealException, ShiftID: "s2"}, {Kind: attendance.BreakException, ShiftID: "s1"}}
	lines, state, err := attendance.CalculateBreakPremiums(attendance.Result{Outcome: attendance.Exception, Exceptions: findings}, "US-CA", attendance.PremiumRule{JurisdictionCode: "US-CA", RuleRef: attendance.VersionedRef{ID: "ca-premium", Version: "2026"}, MealHours: 1, RestHours: 1, MaxMealPerWorkday: 1, MaxRestPerWorkday: 1}, map[string]string{"s1": "d1", "s2": "d2"})
	if err != nil || state != attendance.Exception || len(lines) != 3 {
		t.Fatalf("attendance lines=%+v state=%s err=%v", lines, state, err)
	}
	hours := 0
	for _, line := range lines {
		hours += line.Hours
	}
	req := wageRequest(t)
	req.PremiumHours = laborDecimal(t, fmt.Sprintf("%d.00", hours))
	req.PremiumRate = laborDecimal(t, "45.00")
	req.PremiumRuleRef = laborRef("ca-premium")
	got, err := CalculateWages(req)
	if err != nil {
		t.Fatal(err)
	}
	if hours != 3 || got.PremiumPay.String() != "135.00" || got.PremiumRuleRef.ID != "ca-premium" {
		t.Fatalf("hours=%d wage=%+v", hours, got)
	}
}

// TestTodo_LABOR_003 is the primary acceptance case: exact time, rate and
// rule inputs return a component trace, while unknown jurisdiction, rate or
// rule blocks authoritative cost instead of defaulting to zero.
func TestTodo_LABOR_003(t *testing.T) {
	got, err := CalculateWages(wageRequest(t))
	if err != nil {
		t.Fatalf("CalculateWages: %v", err)
	}
	// 40*40.00 = 1600.00; 5*40.00*1.50 = 300.00; 8*2.50 = 20.00.
	if got.RegularPay.String() != "1600.00" {
		t.Fatalf("regular = %s, want 1600.00", got.RegularPay)
	}
	if got.OvertimePay.String() != "300.00" {
		t.Fatalf("overtime = %s, want 300.00", got.OvertimePay)
	}
	if got.DifferentialPay.String() != "20.00" {
		t.Fatalf("differential = %s, want 20.00", got.DifferentialPay)
	}
	if got.TotalPay.String() != "1920.00" {
		t.Fatalf("total = %s, want 1920.00", got.TotalPay)
	}
	rejectEmptyDigest(t, got.Digest)
	if err := got.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	cases := map[string]func(*WageRequest){
		"unknown jurisdiction": func(r *WageRequest) {
			r.Jurisdiction = ""
		},
		"unknown jurisdiction rules": func(r *WageRequest) {
			r.JurisdictionRules = RuleRef{}
		},
		"stale rule version": func(r *WageRequest) {
			r.Rule.Version = "v99"
		},
		"negative hours": func(r *WageRequest) {
			d, err := values.NewDecimal("-1.00", 2, values.RoundingExactRequired)
			if err != nil {
				t.Fatal(err)
			}
			r.RegularHours = d
		},
		"hour precision differs": func(r *WageRequest) {
			d, err := values.NewDecimal("40.000", 3, values.RoundingExactRequired)
			if err != nil {
				t.Fatal(err)
			}
			r.RegularHours = d
		},
		"missing rounding": func(r *WageRequest) {
			r.Rounding = values.RoundingUnspecified
		},
		"missing calculation id": func(r *WageRequest) {
			r.CalculationID = ""
		},
	}
	for name, mutate := range cases {
		req := wageRequest(t)
		mutate(&req)
		if _, err := CalculateWages(req); !errors.Is(err, ErrWageRejected) {
			t.Fatalf("%s: err = %v, want LABOR_003_REJECTED", name, err)
		}
	}
	// A zero-hour week is an exact zero, not a blocked unknown.
	zero := wageRequest(t)
	zero.RegularHours = laborDecimal(t, "0.00")
	zero.OvertimeHours = laborDecimal(t, "0.00")
	zero.DifferentialHours = laborDecimal(t, "0.00")
	zeroGot, err := CalculateWages(zero)
	if err != nil {
		t.Fatalf("zero-hour week: %v", err)
	}
	if !zeroGot.TotalPay.IsZero() {
		t.Fatalf("zero-hour total = %s, want zero", zeroGot.TotalPay)
	}
}

// TestTodo_LABOR_003_Property proves linearity: doubling every hour input
// doubles every pay component, and totals always equal the component sum.
func TestTodo_LABOR_003_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(20260917))
	for i := 0; i < 48; i++ {
		req := wageRequest(t)
		req.RegularHours = laborDecimal(t, itoa2(rng.Intn(60), rng.Intn(100)))
		req.OvertimeHours = laborDecimal(t, itoa2(rng.Intn(20), rng.Intn(100)))
		req.DifferentialHours = laborDecimal(t, itoa2(rng.Intn(40), rng.Intn(100)))
		got, err := CalculateWages(req)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		sum, err := got.RegularPay.Add(got.OvertimePay)
		if err != nil {
			t.Fatal(err)
		}
		sum, err = sum.Add(got.DifferentialPay)
		if err != nil {
			t.Fatal(err)
		}
		if !sum.Equal(got.TotalPay) {
			t.Fatalf("case %d: components sum %s, total %s", i, sum, got.TotalPay)
		}
		// Doubling is asserted on whole-hour inputs, where every product
		// is exact and rounding cannot introduce a cent of drift.
		whole := wageRequest(t)
		whole.RegularHours = laborDecimal(t, itoa2(rng.Intn(60), 0))
		whole.OvertimeHours = laborDecimal(t, itoa2(rng.Intn(20), 0))
		whole.DifferentialHours = laborDecimal(t, itoa2(rng.Intn(40), 0))
		base, err := CalculateWages(whole)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		doubled := whole
		for _, h := range []*values.Decimal{&doubled.RegularHours, &doubled.OvertimeHours, &doubled.DifferentialHours} {
			d, err := h.Add(*h)
			if err != nil {
				t.Fatal(err)
			}
			*h = d
		}
		again, err := CalculateWages(doubled)
		if err != nil {
			t.Fatalf("case %d doubled: %v", i, err)
		}
		for name, pair := range map[string][2]values.Decimal{
			"regular":      {base.RegularPay, again.RegularPay},
			"overtime":     {base.OvertimePay, again.OvertimePay},
			"differential": {base.DifferentialPay, again.DifferentialPay},
			"total":        {base.TotalPay, again.TotalPay},
		} {
			want, err := pair[0].Add(pair[0])
			if err != nil {
				t.Fatal(err)
			}
			if !want.Equal(pair[1]) {
				t.Fatalf("case %d %s: doubled %s, want %s", i, name, pair[1], want)
			}
		}
		// Same request twice is digest-identical.
		repeat, err := CalculateWages(req)
		if err != nil {
			t.Fatalf("case %d repeat: %v", i, err)
		}
		if repeat.Digest != got.Digest {
			t.Fatalf("case %d: digest not deterministic", i)
		}
	}
}

// TestTodo_LABOR_003_Race proves concurrent calculation is race-free and
// deterministic.
func TestTodo_LABOR_003_Race(t *testing.T) {
	req := wageRequest(t)
	const workers = 8
	results := make([]WageCalculation, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			results[w], errs[w] = CalculateWages(req)
		}(w)
	}
	wg.Wait()
	for w := 0; w < workers; w++ {
		if errs[w] != nil {
			t.Fatalf("worker %d: %v", w, errs[w])
		}
		if results[w].Digest != results[0].Digest {
			t.Fatalf("worker %d digest differs", w)
		}
	}
}

// TestTodo_LABOR_003_Security proves blocked unknowns never yield amounts
// and the explanation carries rule references, never raw worker identity.
func TestTodo_LABOR_003_Security(t *testing.T) {
	req := wageRequest(t)
	req.Jurisdiction = "XX-UNKNOWN"
	req.JurisdictionRules = RuleRef{}
	if _, err := CalculateWages(req); !errors.Is(err, ErrWageRejected) {
		t.Fatalf("unknown jurisdiction rules: err = %v, want LABOR_003_REJECTED", err)
	}
	got, err := CalculateWages(wageRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	expl := got.Explain()
	for _, leak := range []string{"SSN", "ssn", "bank", "password"} {
		if containsFold(expl, leak) {
			t.Fatalf("explanation leaks %q", leak)
		}
	}
	if !containsFold(expl, "US-CA") || !containsFold(expl, got.Digest) {
		t.Fatal("explanation omits jurisdiction or digest lineage")
	}
}

// TestTodo_LABOR_003_Mutation proves the digest binds every input: any
// change yields a new digest and a tampered result fails validation.
func TestTodo_LABOR_003_Mutation(t *testing.T) {
	base, err := CalculateWages(wageRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*WageRequest){
		func(r *WageRequest) { r.RegularHours = laborDecimal(t, "41.00") },
		func(r *WageRequest) { r.OvertimeHours = laborDecimal(t, "6.00") },
		func(r *WageRequest) { r.DifferentialHours = laborDecimal(t, "9.00") },
		func(r *WageRequest) { r.Jurisdiction = "US-NY" },
		func(r *WageRequest) {
			next := r.Rule
			next.Version = "v2"
			rebased, err := NewLaborRule(next)
			if err != nil {
				t.Fatal(err)
			}
			r.Rule = rebased
			r.RuleVersion = "v2"
		},
	}
	for i, mutate := range mutations {
		req := wageRequest(t)
		mutate(&req)
		mutated, err := CalculateWages(req)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if mutated.Digest == base.Digest {
			t.Fatalf("case %d did not change the digest", i)
		}
	}
	tampered := base
	tampered.TotalPay = laborDecimal(t, "0.01")
	if err := tampered.Validate(); err == nil {
		t.Fatal("tampered total passed validation")
	}
	forged := base
	forged.Digest = "sha256:forged"
	if err := forged.Validate(); err == nil {
		t.Fatal("forged digest passed validation")
	}
}

func containsFold(haystack, needle string) bool {
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			a, b := haystack[i+j], needle[j]
			if 'A' <= a && a <= 'Z' {
				a += 'a' - 'A'
			}
			if 'A' <= b && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

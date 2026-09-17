package garnishment

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func garn003Input() EarningsInput {
	return EarningsInput{
		Tenant: "acme", WorkerRef: "worker-1", OrderRef: "order:creditor-1",
		OrderType: OrderCreditor, Jurisdiction: "US-CA",
		RulePackRef: "rulepack:garnish-limits", RulePackVersion: "v2026.01", RulePackDigest: "sha256:pack",
		Gross: values.MustDecimal("2000.00", 2, values.RoundingHalfUp),
		Deductions: []MandatoryDeduction{
			{Kind: "FEDERAL_TAX", Amount: values.MustDecimal("300.00", 2, values.RoundingHalfUp), BasisRef: "statute:26-3401"},
			{Kind: "FICA", Amount: values.MustDecimal("153.00", 2, values.RoundingHalfUp), BasisRef: "statute:26-3101"},
		},
		LimitBps:       2500,
		ProtectedFloor: values.MustDecimal("217.50", 2, values.RoundingHalfUp),
		OrderedAmount:  values.MustDecimal("500.00", 2, values.RoundingHalfUp),
	}
}

// TestTodo_GARN_003 is the PRIMARY contract: exact earnings, deductions,
// jurisdiction and rules produce a bounded withholding trace, and a
// missing fact or rule blocks instead of resolving to zero or a default.
func TestTodo_GARN_003(t *testing.T) {
	got, err := ComputeWithholdingLimit(garn003Input())
	if err != nil {
		t.Fatalf("ComputeWithholdingLimit: %v", err)
	}
	if got.Disposable.String() != "1547.00" {
		t.Fatalf("disposable = %s, want 1547.00", got.Disposable.String())
	}
	if got.MaxWithholding.String() != "386.75" {
		t.Fatalf("max = %s, want 386.75", got.MaxWithholding.String())
	}
	if got.OrderedCapped.String() != "386.75" {
		t.Fatalf("ordered 500.00 must cap to 386.75, got %s", got.OrderedCapped.String())
	}
	if len(got.Trace) == 0 || got.CanonicalDigest == "" {
		t.Fatalf("result must carry a trace and digest: %+v", got)
	}

	t.Run("missing fact or rule blocks, never zero", func(t *testing.T) {
		for name, mutate := range map[string]func(*EarningsInput){
			"jurisdiction": func(in *EarningsInput) { in.Jurisdiction = "" },
			"rule pack":    func(in *EarningsInput) { in.RulePackDigest = "" },
			"gross": func(in *EarningsInput) {
				in.Gross = values.MustDecimal("0.00", 2, values.RoundingHalfUp)
				in.Deductions = []MandatoryDeduction{{Kind: "FEDERAL_TAX", Amount: values.MustDecimal("1.00", 2, values.RoundingHalfUp), BasisRef: "statute:x"}}
			},
			"order type": func(in *EarningsInput) { in.OrderType = "LOTTERY" },
		} {
			in := garn003Input()
			mutate(&in)
			res, err := ComputeWithholdingLimit(in)
			if !errors.Is(err, ErrLimitBlocked) {
				t.Fatalf("missing %s must block, got %+v, %v", name, res, err)
			}
			if res.CanonicalDigest != "" {
				t.Fatalf("blocked calculation must seal nothing")
			}
		}
		if _, err := ComputeWithholdingLimit(EarningsInput{}); !errors.Is(err, ErrLimitBlocked) {
			t.Fatalf("empty input must block, got %v", err)
		}
	})

	t.Run("floor breach caps to zero without defaulting", func(t *testing.T) {
		in := garn003Input()
		in.Gross = values.MustDecimal("400.00", 2, values.RoundingHalfUp)
		in.Deductions = []MandatoryDeduction{
			{Kind: "FEDERAL_TAX", Amount: values.MustDecimal("100.00", 2, values.RoundingHalfUp), BasisRef: "statute:26-3401"},
			{Kind: "FICA", Amount: values.MustDecimal("100.00", 2, values.RoundingHalfUp), BasisRef: "statute:26-3101"},
		}
		got, err := ComputeWithholdingLimit(in)
		if err != nil {
			t.Fatal(err)
		}
		if got.Disposable.String() != "200.00" || !got.MaxWithholding.IsZero() || !got.OrderedCapped.IsZero() {
			t.Fatalf("below-floor disposable must cap to zero: %+v", got)
		}
	})
}

func TestTodo_GARN_003_Race(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := ComputeWithholdingLimit(garn003Input())
			if err != nil {
				t.Error(err)
				return
			}
			if got.Disposable.String() != "1547.00" {
				t.Errorf("concurrent compute diverged: %s", got.Disposable.String())
			}
		}()
	}
	wg.Wait()
}

func TestTodo_GARN_003_Fault(t *testing.T) {
	for name, mutate := range map[string]func(*EarningsInput){
		"limit zero":    func(in *EarningsInput) { in.LimitBps = 0 },
		"limit overrun": func(in *EarningsInput) { in.LimitBps = 10001 },
		"unbased":       func(in *EarningsInput) { in.Deductions[0].BasisRef = "" },
		"negative": func(in *EarningsInput) {
			in.Deductions[1].Amount = values.MustDecimal("-1.00", 2, values.RoundingHalfUp)
		},
	} {
		in := garn003Input()
		mutate(&in)
		if _, err := ComputeWithholdingLimit(in); !errors.Is(err, ErrLimitBlocked) {
			t.Fatalf("fault %s must block", name)
		}
	}
	in := garn003Input()
	in.Deductions = append(in.Deductions, MandatoryDeduction{Kind: "STATE_TAX", Amount: values.MustDecimal("5000.00", 2, values.RoundingHalfUp), BasisRef: "statute:ca-17071"})
	if _, err := ComputeWithholdingLimit(in); !errors.Is(err, ErrLimitBlocked) {
		t.Fatalf("deductions exceeding gross must block")
	}
}

func TestTodo_GARN_003_Security(t *testing.T) {
	// The calculation consumes references, never bank payload: there is
	// no account, routing or credential field to smuggle one through.
	in := garn003Input()
	got, err := ComputeWithholdingLimit(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range got.Trace {
		if len(line) > 0 && (containsSecret(line)) {
			t.Fatalf("trace must not carry bank payload: %q", line)
		}
	}
	if _, err := ComputeWithholdingLimit(EarningsInput{Tenant: "acme"}); !errors.Is(err, ErrLimitBlocked) {
		t.Fatalf("tenant-only input must block without disclosing pack state")
	}
}

func containsSecret(s string) bool {
	for _, token := range []string{"acct:", "routing:", "iban:", "swift:", "card:"} {
		if len(s) >= len(token) {
			for i := 0; i+len(token) <= len(s); i++ {
				if s[i:i+len(token)] == token {
					return true
				}
			}
		}
	}
	return false
}

func TestTodo_GARN_003_Mutation(t *testing.T) {
	a, err := ComputeWithholdingLimit(garn003Input())
	if err != nil {
		t.Fatal(err)
	}
	for field, mutate := range map[string]func(*EarningsInput){
		"gross": func(in *EarningsInput) { in.Gross = values.MustDecimal("2000.01", 2, values.RoundingHalfUp) },
		"deduction": func(in *EarningsInput) {
			in.Deductions[0].Amount = values.MustDecimal("300.01", 2, values.RoundingHalfUp)
		},
		"limit":        func(in *EarningsInput) { in.LimitBps = 2501 },
		"jurisdiction": func(in *EarningsInput) { in.Jurisdiction = "US-NV" },
		"ordered":      func(in *EarningsInput) { in.OrderedAmount = values.MustDecimal("10.00", 2, values.RoundingHalfUp) },
	} {
		in := garn003Input()
		mutate(&in)
		b, err := ComputeWithholdingLimit(in)
		if err != nil {
			t.Fatalf("mutation %s must compute: %v", field, err)
		}
		if b.CanonicalDigest == a.CanonicalDigest {
			t.Fatalf("mutation %s must move the digest", field)
		}
	}
}

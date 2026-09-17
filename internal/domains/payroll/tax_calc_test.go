package payroll

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func taxMoney(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingHalfUp)
	if err != nil {
		t.Fatalf("taxMoney %q: %v", text, err)
	}
	return d
}

func taxRate(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 4, values.RoundingHalfUp)
	if err != nil {
		t.Fatalf("taxRate %q: %v", text, err)
	}
	return d
}

func taxInputFixture(t *testing.T) TaxInput {
	t.Helper()
	return TaxInput{
		Tenant: "acme", WorkerRef: "worker-1",
		PeriodStart: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
		GrossWages:  taxMoney(t, "5000.00"), PreTaxDeductions: taxMoney(t, "500.00"),
		TaxProfileRef: "profile-1", RegistrationRef: "registration-1",
		ElectionRef: "election-1", WageBasisRef: "wagebasis-1",
		RuleVersion: "2026-v1",
	}
}

func taxPackFixture(t *testing.T, version string) TaxRulePack {
	t.Helper()
	return TaxRulePack{
		Version: version, Authority: "irs",
		EmployeeRate: taxRate(t, "0.0765"), EmployerRate: taxRate(t, "0.0765"),
		WageBaseCap: taxMoney(t, "100000.00"), CapApplies: true,
		Rounding: values.RoundingHalfUp,
	}
}

// TestTodo_TAX_001 is the primary TAX-001 contract test: the exact golden
// vector prices version-pinned worker and employer taxes, and any missing
// tax profile, registration, wage basis, election or rule version refuses net
// pay instead of computing it.
func TestTodo_TAX_001(t *testing.T) {
	t.Run("golden vector prices exactly", func(t *testing.T) {
		got, err := CalculateTax(taxInputFixture(t), taxPackFixture(t, "2026-v1"), taxMoney(t, "1200.00"))
		if err != nil {
			t.Fatalf("CalculateTax: %v", err)
		}
		if got.TaxableWages.String() != "4500.00" {
			t.Fatalf("taxable wages = %s, want 4500.00", got.TaxableWages)
		}
		if got.EmployeeTax.String() != "344.25" || got.EmployerTax.String() != "344.25" {
			t.Fatalf("employee/employer tax = %s/%s, want 344.25/344.25", got.EmployeeTax, got.EmployerTax)
		}
		if got.TotalLiability.String() != "688.50" {
			t.Fatalf("total liability = %s, want 688.50", got.TotalLiability)
		}
		if got.TaxBase.String() != "4500.00" {
			t.Fatalf("tax base = %s, want 4500.00", got.TaxBase)
		}
		if got.NewAccumulator.String() != "1888.50" {
			t.Fatalf("new accumulator = %s, want 1888.50", got.NewAccumulator)
		}
		if got.Authority != "irs" || got.RuleVersion != "2026-v1" {
			t.Fatalf("result must carry authority and pinned rule version: %+v", got)
		}
		if len(got.RuleTrace) == 0 || !strings.HasPrefix(got.RuleTrace[0], "2026-v1:") {
			t.Fatalf("result must carry a version-pinned rule trace: %+v", got.RuleTrace)
		}
		if !got.Delta.Balanced || got.Delta.Amount.String() != "688.50" {
			t.Fatalf("accounting delta must balance at the total liability: %+v", got.Delta)
		}
		if got.Digest == "" {
			t.Fatal("result must carry a calculation digest")
		}
	})

	t.Run("missing governed input refuses net pay", func(t *testing.T) {
		base := taxInputFixture(t)
		pack := taxPackFixture(t, "2026-v1")
		cases := map[string]func(*TaxInput){
			"tax profile":   func(i *TaxInput) { i.TaxProfileRef = "" },
			"registration":  func(i *TaxInput) { i.RegistrationRef = "" },
			"election":      func(i *TaxInput) { i.ElectionRef = "" },
			"wage basis":    func(i *TaxInput) { i.WageBasisRef = "" },
			"rule version":  func(i *TaxInput) { i.RuleVersion = "" },
			"tenant":        func(i *TaxInput) { i.Tenant = "" },
			"worker":        func(i *TaxInput) { i.WorkerRef = "" },
			"gross wages":   func(i *TaxInput) { i.GrossWages = values.Decimal{} },
			"pretax amount": func(i *TaxInput) { i.PreTaxDeductions = values.Decimal{} },
		}
		for name, mutate := range cases {
			input := base
			mutate(&input)
			if _, err := CalculateTax(input, pack, taxMoney(t, "0.00")); !errors.Is(err, ErrTaxRejected) {
				t.Fatalf("%s: missing input must be TAX_001_REJECTED, got %v", name, err)
			}
		}
	})

	t.Run("current rules never reprice a historical fixture", func(t *testing.T) {
		input := taxInputFixture(t)
		current := taxPackFixture(t, "2026-v2")
		current.EmployeeRate = taxRate(t, "0.0800")
		if _, err := CalculateTax(input, current, taxMoney(t, "0.00")); !errors.Is(err, ErrTaxRejected) {
			t.Fatalf("historical input under current rules must be TAX_001_REJECTED, got %v", current.Version)
		}
		var refusal *TaxError
		_, err := CalculateTax(input, current, taxMoney(t, "0.00"))
		if !errors.As(err, &refusal) || refusal.Field != "rule_version" {
			t.Fatalf("version refusal must name field=rule_version, got %v", err)
		}
	})

	t.Run("correction appends a new result and delta", func(t *testing.T) {
		prior, err := CalculateTax(taxInputFixture(t), taxPackFixture(t, "2026-v1"), taxMoney(t, "0.00"))
		if err != nil {
			t.Fatalf("CalculateTax: %v", err)
		}
		fixed := taxInputFixture(t)
		fixed.GrossWages = taxMoney(t, "5200.00")
		recomputed, err := CalculateTax(fixed, taxPackFixture(t, "2026-v1"), taxMoney(t, "0.00"))
		if err != nil {
			t.Fatalf("CalculateTax: %v", err)
		}
		chained := recomputed
		chained.SupersedesDigest = prior.Digest
		correction, err := AppendTaxCorrection(prior, chained, "overtime was omitted", "payroll-lead", fixed.PeriodEnd)
		if err != nil {
			t.Fatalf("AppendTaxCorrection: %v", err)
		}
		if correction.DeltaTotal.String() != "30.60" {
			t.Fatalf("correction total delta = %s, want 30.60", correction.DeltaTotal)
		}
		if correction.Original.Digest != prior.Digest || prior.SupersedesDigest != "" {
			t.Fatal("correction must append: the original result is immutable")
		}
		if correction.Corrected.SupersedesDigest != prior.Digest {
			t.Fatal("corrected result must chain the prior digest")
		}
	})
}

// TestTodo_TAX_001_Property holds the tax algebra over a table of governed
// cases: the wage identity closes, the liability splits exactly, the books
// balance, outputs stay at cent scale, and execution is deterministic.
func TestTodo_TAX_001_Property(t *testing.T) {
	cases := []struct {
		name                  string
		gross, pretax, cap    string
		capApplies            bool
		employeeRate          string
		employerRate          string
		wantTaxable, wantBase string
	}{
		{"below cap", "5000.00", "500.00", "100000.00", true, "0.0765", "0.0765", "4500.00", "4500.00"},
		{"above cap", "200000.00", "10000.00", "100000.00", true, "0.0620", "0.0620", "190000.00", "100000.00"},
		{"no cap", "8000.00", "0.00", "0.00", false, "0.1000", "0.0500", "8000.00", "8000.00"},
		{"zero pretax", "3120.44", "0.00", "100000.00", true, "0.0765", "0.0765", "3120.44", "3120.44"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := taxInputFixture(t)
			input.GrossWages = taxMoney(t, tc.gross)
			input.PreTaxDeductions = taxMoney(t, tc.pretax)
			pack := taxPackFixture(t, "2026-v1")
			pack.EmployeeRate = taxRate(t, tc.employeeRate)
			pack.EmployerRate = taxRate(t, tc.employerRate)
			pack.WageBaseCap = taxMoney(t, tc.cap)
			pack.CapApplies = tc.capApplies
			prior := taxMoney(t, "250.00")
			first, err := CalculateTax(input, pack, prior)
			if err != nil {
				t.Fatalf("CalculateTax: %v", err)
			}
			second, err := CalculateTax(input, pack, prior)
			if err != nil {
				t.Fatalf("CalculateTax: %v", err)
			}
			if first.Digest != second.Digest {
				t.Fatal("same governed input must calculate identically")
			}
			if first.TaxableWages.String() != tc.wantTaxable || first.TaxBase.String() != tc.wantBase {
				t.Fatalf("taxable/base = %s/%s, want %s/%s", first.TaxableWages, first.TaxBase, tc.wantTaxable, tc.wantBase)
			}
			total, err := first.EmployeeTax.Add(first.EmployerTax)
			if err != nil {
				t.Fatalf("Add: %v", err)
			}
			if total.String() != first.TotalLiability.String() {
				t.Fatalf("total %s must equal employee+employer %s", first.TotalLiability, total)
			}
			if !first.Delta.Balanced || first.Delta.Amount.String() != first.TotalLiability.String() {
				t.Fatalf("delta must balance at the total liability: %+v", first.Delta)
			}
			accrued, err := first.NewAccumulator.Sub(prior)
			if err != nil {
				t.Fatalf("Sub: %v", err)
			}
			if accrued.String() != first.TotalLiability.String() {
				t.Fatalf("accumulator movement %s must equal the liability %s", accrued, first.TotalLiability)
			}
			for name, amount := range map[string]values.Decimal{
				"taxable": first.TaxableWages, "employee": first.EmployeeTax,
				"employer": first.EmployerTax, "total": first.TotalLiability,
			} {
				if amount.Scale() != 2 {
					t.Fatalf("%s must stay at cent scale, got scale %d", name, amount.Scale())
				}
			}
		})
	}
}

// TestTodo_TAX_001_Race proves concurrent calculations over one shared rule
// pack never race and always agree.
func TestTodo_TAX_001_Race(t *testing.T) {
	input := taxInputFixture(t)
	pack := taxPackFixture(t, "2026-v1")
	prior := taxMoney(t, "0.00")
	const workers = 8
	results := make([]TaxResult, workers)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := CalculateTax(input, pack, prior)
			if err != nil {
				t.Errorf("CalculateTax: %v", err)
				return
			}
			results[i] = got
		}(i)
	}
	wg.Wait()
	for i := 1; i < len(results); i++ {
		if results[i].Digest != results[0].Digest || results[i].TotalLiability.String() != results[0].TotalLiability.String() {
			t.Fatalf("concurrent calculations must agree: %+v vs %+v", results[0], results[i])
		}
	}
}

// TestTodo_TAX_001_Security proves ungoverned tax input is refused before any
// payable output exists, with field-named refusals that echo no amounts.
func TestTodo_TAX_001_Security(t *testing.T) {
	t.Run("unverified election is refused before computation", func(t *testing.T) {
		input := taxInputFixture(t)
		input.ElectionRef = ""
		_, err := CalculateTax(input, taxPackFixture(t, "2026-v1"), taxMoney(t, "0.00"))
		if !errors.Is(err, ErrTaxRejected) {
			t.Fatalf("unverified election must be TAX_001_REJECTED, got %v", err)
		}
		var refusal *TaxError
		if !errors.As(err, &refusal) || refusal.Field != "election_ref" {
			t.Fatalf("refusal must name field=election_ref, got %v", err)
		}
		if strings.Contains(err.Error(), "5000") {
			t.Fatalf("refusal must not echo governed amounts: %v", err)
		}
	})

	t.Run("unregistered jurisdiction is refused before computation", func(t *testing.T) {
		input := taxInputFixture(t)
		input.RegistrationRef = ""
		if _, err := CalculateTax(input, taxPackFixture(t, "2026-v1"), taxMoney(t, "0.00")); !errors.Is(err, ErrTaxRejected) {
			t.Fatal("unregistered jurisdiction must be TAX_001_REJECTED")
		}
	})

	t.Run("pretax above gross is refused", func(t *testing.T) {
		input := taxInputFixture(t)
		input.PreTaxDeductions = taxMoney(t, "6000.00")
		if _, err := CalculateTax(input, taxPackFixture(t, "2026-v1"), taxMoney(t, "0.00")); !errors.Is(err, ErrTaxRejected) {
			t.Fatal("pretax above gross must be TAX_001_REJECTED")
		}
	})

	t.Run("unspecified rounding is refused", func(t *testing.T) {
		pack := taxPackFixture(t, "2026-v1")
		pack.Rounding = values.RoundingUnspecified
		if _, err := CalculateTax(taxInputFixture(t), pack, taxMoney(t, "0.00")); !errors.Is(err, ErrTaxRejected) {
			t.Fatal("unspecified rounding must be TAX_001_REJECTED")
		}
	})
}

// TestTodo_TAX_001_Mutation kills the seeded semantic mutants that would
// misprice the golden vector.
func TestTodo_TAX_001_Mutation(t *testing.T) {
	t.Run("mutant: gross taxed instead of taxable wages", func(t *testing.T) {
		got, err := CalculateTax(taxInputFixture(t), taxPackFixture(t, "2026-v1"), taxMoney(t, "0.00"))
		if err != nil {
			t.Fatalf("CalculateTax: %v", err)
		}
		if got.EmployeeTax.String() == "382.50" {
			t.Fatal("mutant survived: 5000.00 x 0.0765 prices gross, not taxable wages")
		}
		if got.EmployeeTax.String() != "344.25" {
			t.Fatalf("employee tax = %s, want 344.25", got.EmployeeTax)
		}
	})

	t.Run("mutant: employer share dropped", func(t *testing.T) {
		got, err := CalculateTax(taxInputFixture(t), taxPackFixture(t, "2026-v1"), taxMoney(t, "0.00"))
		if err != nil {
			t.Fatalf("CalculateTax: %v", err)
		}
		if got.EmployerTax.IsZero() || got.TotalLiability.String() == got.EmployeeTax.String() {
			t.Fatal("mutant survived: employer tax must be priced alongside employee tax")
		}
	})

	t.Run("mutant: rounding mode swapped", func(t *testing.T) {
		input := taxInputFixture(t)
		input.GrossWages = taxMoney(t, "1000.05")
		input.PreTaxDeductions = taxMoney(t, "900.00")
		pack := taxPackFixture(t, "2026-v1")
		pack.EmployeeRate = taxRate(t, "0.1000")
		pack.EmployerRate = taxRate(t, "0.1000")
		got, err := CalculateTax(input, pack, taxMoney(t, "0.00"))
		if err != nil {
			t.Fatalf("CalculateTax: %v", err)
		}
		if got.EmployeeTax.String() != "10.01" {
			t.Fatalf("mutant survived: 10.005 under HALF_UP is 10.01, got %s", got.EmployeeTax)
		}
	})

	t.Run("mutant: version pin ignored", func(t *testing.T) {
		pack := taxPackFixture(t, "2026-v2")
		if _, err := CalculateTax(taxInputFixture(t), pack, taxMoney(t, "0.00")); !errors.Is(err, ErrTaxRejected) {
			t.Fatal("mutant survived: unpinned rule version must be refused")
		}
	})
}

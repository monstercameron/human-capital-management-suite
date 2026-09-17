package paygl

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/labor"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func mappingRule(t *testing.T, id, code, debit, credit string, dimension labor.Dimension) AccountingRule {
	t.Helper()
	rule, err := NewAccountingRule(AccountingRule{
		ID: id, Version: "v1", Effective: glInterval(t), ComponentKind: ComponentEarning, ComponentCode: code,
		Dimension: dimension, DebitAccount: debit, CreditAccount: credit, Currency: "USD",
		Rounding: values.RoundingExactRequired, SuspensePolicy: SuspensePolicyReject,
	})
	if err != nil {
		t.Fatal(err)
	}
	return rule
}

func mappingPercent(t *testing.T, text string) values.Decimal {
	t.Helper()
	return decimalForMapping(t, text, 4)
}

func decimalForMapping(t *testing.T, text string, scale int32) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, scale, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func mappingDimension(value string) labor.Dimension {
	return labor.Dimension{Kind: labor.DimensionCostCenter, Value: value, Version: "v1"}
}

func mappingComponent(t *testing.T, id string, ordinal int, amount string, entries ...labor.AllocationEntry) PayrollComponent {
	t.Helper()
	return PayrollComponent{
		ID: id, Ordinal: ordinal, Kind: ComponentEarning, Code: "SALARY", Amount: glDecimal(t, amount), Currency: "USD",
		Allocation: labor.Allocation{Entries: entries}, SourceDigest: "sha256:component-" + id,
	}
}

func mappingFixture(t *testing.T) (payrollRunFixture struct {
	run        payroll.PayrollRun
	rules      []AccountingRule
	components []PayrollComponent
}) {
	t.Helper()
	cc1, cc2, cc3 := mappingDimension("cc-1"), mappingDimension("cc-2"), mappingDimension("cc-3")
	payrollRunFixture.run = glRun(t)
	payrollRunFixture.rules = []AccountingRule{
		mappingRule(t, "salary-cc1", "SALARY", "6001", "2101", cc1),
		mappingRule(t, "salary-cc2", "SALARY", "6002", "2102", cc2),
		mappingRule(t, "salary-cc3", "SALARY", "6003", "2103", cc3),
	}
	payrollRunFixture.components = []PayrollComponent{mappingComponent(t, "component-1", 1, "100.01",
		labor.AllocationEntry{Dimension: cc1, Percent: mappingPercent(t, "33.33")},
		labor.AllocationEntry{Dimension: cc2, Percent: mappingPercent(t, "33.33")},
		labor.AllocationEntry{Dimension: cc3, Percent: mappingPercent(t, "33.34")},
	)}
	return
}

// TestTodo_PAYGL_002 is the primary acceptance case and golden-pins the
// account pairs, exact remainder allocation, lineage, and result digest.
func TestTodo_PAYGL_002(t *testing.T) {
	fixture := mappingFixture(t)
	result, err := MapPayrollComponents(fixture.run, fixture.rules, fixture.components)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Mappings) != 3 || result.TotalComponentAmount.String() != "100.01" || result.TotalSplitAmount.String() != "100.01" {
		t.Fatalf("result totals/mappings = %+v", result)
	}
	wantAmounts := []string{"33.33", "33.33", "33.35"}
	wantDebits := []string{"6001", "6002", "6003"}
	wantCredits := []string{"2101", "2102", "2103"}
	for i, mapping := range result.Mappings {
		if mapping.Amount.String() != wantAmounts[i] || mapping.DebitAccount != wantDebits[i] || mapping.CreditAccount != wantCredits[i] {
			t.Fatalf("mapping[%d] = %+v", i, mapping)
		}
		if mapping.RuleDigest == "" || mapping.SourceDigest == "" || mapping.ComponentAmount.String() != "100.01" {
			t.Fatalf("mapping[%d] lost lineage = %+v", i, mapping)
		}
	}
	if result.Digest != "sha256:3dfafea0e237f68db97296322ffd6afa8c64e003e5c16f42d3f3e69592a62744" {
		t.Fatalf("golden mapping digest = %q", result.Digest)
	}
	repeated, err := MapComponents(fixture.run, fixture.components, fixture.rules)
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Canonical()) != string(repeated.Canonical()) || result.Digest != repeated.Digest {
		t.Fatalf("mapping changed across equivalent calls: %q/%q", result.Digest, repeated.Digest)
	}
	reordered := fixture.components[0]
	reordered.Allocation.Entries = append([]labor.AllocationEntry(nil), reordered.Allocation.Entries...)
	for left, right := 0, len(reordered.Allocation.Entries)-1; left < right; left, right = left+1, right-1 {
		reordered.Allocation.Entries[left], reordered.Allocation.Entries[right] = reordered.Allocation.Entries[right], reordered.Allocation.Entries[left]
	}
	canonicalReordered, err := MapPayrollComponents(fixture.run, fixture.rules, []PayrollComponent{reordered})
	if err != nil {
		t.Fatal(err)
	}
	if canonicalReordered.Digest != result.Digest {
		t.Fatalf("allocation input order changed digest: %q != %q", canonicalReordered.Digest, result.Digest)
	}
	t.Logf("golden mapping digest: %s", result.Digest)
}

// TestTodo_PAYGL_002_Property proves that the exact sum of every mapped split
// equals the exact sum of all source components over representative amounts.
func TestTodo_PAYGL_002_Property(t *testing.T) {
	fixture := mappingFixture(t)
	for _, amount := range []string{"0.01", "1.00", "100.01", "1000000.00"} {
		fixture.components[0].Amount = glDecimal(t, amount)
		result, err := MapPayrollComponents(fixture.run, fixture.rules, fixture.components)
		if err != nil {
			t.Fatalf("amount %s: %v", amount, err)
		}
		if !result.TotalComponentAmount.Equal(result.TotalSplitAmount) {
			t.Fatalf("amount %s is unbalanced: %s != %s", amount, result.TotalComponentAmount, result.TotalSplitAmount)
		}
		var sum values.Decimal
		for i, mapping := range result.Mappings {
			if i == 0 {
				sum = mapping.Amount
				continue
			}
			sum, err = sum.Add(mapping.Amount)
			if err != nil {
				t.Fatal(err)
			}
		}
		if !sum.Equal(result.TotalComponentAmount) {
			t.Fatalf("amount %s split sum = %s, want %s", amount, sum, result.TotalComponentAmount)
		}
	}

	// The pure mapper has no shared state: concurrent replays have the same
	// canonical bytes and digest.
	const workers = 8
	digests := make(chan string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := MapPayrollComponents(fixture.run, fixture.rules, fixture.components)
			if err != nil {
				t.Errorf("concurrent map: %v", err)
				return
			}
			digests <- result.Digest
		}()
	}
	wg.Wait()
	close(digests)
	var first string
	for digest := range digests {
		if first == "" {
			first = digest
		} else if digest != first {
			t.Fatalf("concurrent digest = %q, want %q", digest, first)
		}
	}
}

// TestTodo_PAYGL_002_Fault proves unmapped, ambiguous, non-calculated, and
// non-total allocations refuse before a mapping result is returned.
func TestTodo_PAYGL_002_Fault(t *testing.T) {
	fixture := mappingFixture(t)
	tests := []struct {
		name  string
		check func() error
		want  error
	}{
		{"unmapped", func() error {
			component := fixture.components[0]
			component.Code = "UNKNOWN"
			_, err := MapPayrollComponents(fixture.run, fixture.rules, []PayrollComponent{component})
			return err
		}, ErrComponentUnmapped},
		{"ambiguous", func() error {
			rules := append([]AccountingRule(nil), fixture.rules...)
			rules = append(rules, fixture.rules[0])
			_, err := MapPayrollComponents(fixture.run, rules, fixture.components)
			return err
		}, ErrComponentAmbiguous},
		{"unbalanced allocation", func() error {
			component := fixture.components[0]
			component.Allocation.Entries[2].Percent = mappingPercent(t, "33.33")
			_, err := MapPayrollComponents(fixture.run, fixture.rules, []PayrollComponent{component})
			return err
		}, labor.ErrAllocationUnbalanced},
		{"not calculated", func() error {
			draft, err := payroll.NewPayrollRun("draft", "monthly", fixture.run.Period, fixture.run.Population, "sha256:inputs")
			if err != nil {
				return err
			}
			_, err = MapPayrollComponents(draft, fixture.rules, fixture.components)
			return err
		}, ErrMappingRejected},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.check(); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

// TestTodo_PAYGL_002_Mutation proves that account, component, and source
// lineage mutations cannot reuse a mapping digest.
func TestTodo_PAYGL_002_Mutation(t *testing.T) {
	fixture := mappingFixture(t)
	base, err := MapPayrollComponents(fixture.run, fixture.rules, fixture.components)
	if err != nil {
		t.Fatal(err)
	}
	changedRules := append([]AccountingRule(nil), fixture.rules...)
	changedRules[0].DebitAccount = "6099"
	changedRules[0].CanonicalDigest = ""
	changed, err := MapPayrollComponents(fixture.run, changedRules, fixture.components)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Digest == base.Digest {
		t.Fatal("account mutation reused mapping digest")
	}
	changedComponent := fixture.components[0]
	changedComponent.SourceDigest = "sha256:other-component"
	changedSource, err := MapPayrollComponents(fixture.run, fixture.rules, []PayrollComponent{changedComponent})
	if err != nil {
		t.Fatal(err)
	}
	if changedSource.Digest == base.Digest {
		t.Fatal("source lineage mutation reused mapping digest")
	}
}

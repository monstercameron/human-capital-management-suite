package payroll

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func trialRulesFixture() TrialRuleSet {
	return TrialRuleSet{
		Version:      "rules.payroll/2026.1",
		Digest:       "sha256:rules",
		Jurisdiction: "US-CA",
		DeductionBps: 1500,
		TaxBps:       2000,
	}
}

func trialRequestFixture() TrialCalcRequest {
	return TrialCalcRequest{
		Tenant:         "tenant-acme",
		CalculationKey: "trial-key-1",
		RunID:          "run-2026-11-b",
		PeriodDigest:   "sha256:period",
		Rules:          trialRulesFixture(),
		KnownCodes:     []string{"BASE", "OT"},
		Workers: []TrialWorkerInput{
			{
				WorkerRef:     "worker-a",
				BalanceDigest: "sha256:balance-a",
				Lines: []TrialEarningLine{
					{Code: "BASE", Amount: values.MustDecimal("2000.00", 2, values.RoundingHalfUp)},
					{Code: "OT", Amount: values.MustDecimal("500.00", 2, values.RoundingHalfUp)},
				},
			},
			{
				WorkerRef:     "worker-b",
				BalanceDigest: "sha256:balance-b",
				Lines: []TrialEarningLine{
					{Code: "BASE", Amount: values.MustDecimal("1000.00", 2, values.RoundingHalfUp)},
				},
			},
		},
	}
}

func trialWorkerByRef(t *testing.T, calc TrialCalculation, ref string) TrialWorkerTotal {
	t.Helper()
	for _, w := range calc.Workers {
		if w.WorkerRef == ref {
			return w
		}
	}
	t.Fatalf("worker %q missing from calculation: %+v", ref, calc.Workers)
	return TrialWorkerTotal{}
}

// TestTodo_PAYRUN_004 is the primary acceptance case: the same
// input/rule/period returns exact worker and run totals with traces and an
// empty exception set, bound by a stable digest.
func TestTodo_PAYRUN_004(t *testing.T) {
	calc, err := ExecuteTrialCalculation(trialRequestFixture())
	if err != nil {
		t.Fatalf("ExecuteTrialCalculation: %v", err)
	}
	a := trialWorkerByRef(t, calc, "worker-a")
	if a.Gross.String() != "2500.00" || a.Deduction.String() != "375.00" || a.Tax.String() != "500.00" || a.Net.String() != "1625.00" {
		t.Fatalf("worker-a totals wrong: gross=%s deduction=%s tax=%s net=%s", a.Gross, a.Deduction, a.Tax, a.Net)
	}
	if a.LineCount != 2 {
		t.Fatalf("worker-a line count = %d, want 2", a.LineCount)
	}
	if !strings.Contains(a.Trace, "gross=2500.00") || !strings.Contains(a.Trace, "net=1625.00") {
		t.Fatalf("worker-a trace is missing exact amounts: %q", a.Trace)
	}
	b := trialWorkerByRef(t, calc, "worker-b")
	if b.Gross.String() != "1000.00" || b.Deduction.String() != "150.00" || b.Tax.String() != "200.00" || b.Net.String() != "650.00" {
		t.Fatalf("worker-b totals wrong: gross=%s deduction=%s tax=%s net=%s", b.Gross, b.Deduction, b.Tax, b.Net)
	}
	if calc.RunGross.String() != "3500.00" || calc.RunDeduction.String() != "525.00" || calc.RunTax.String() != "700.00" || calc.RunNet.String() != "2275.00" {
		t.Fatalf("run totals wrong: gross=%s deduction=%s tax=%s net=%s", calc.RunGross, calc.RunDeduction, calc.RunTax, calc.RunNet)
	}
	if len(calc.Exceptions) != 0 {
		t.Fatalf("clean inputs must yield zero exceptions, got %+v", calc.Exceptions)
	}
	if calc.CalculationID != "trial-calculation/trial-key-1" || calc.CalculationDigest == "" {
		t.Fatalf("calculation identity is incomplete: %+v", calc)
	}
	if err := calc.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	explanation, err := calc.Explain()
	if err != nil || explanation.Revision != 1 || explanation.Workers != 2 || explanation.Digest != calc.CalculationDigest {
		t.Fatalf("explanation = %+v, err = %v", explanation, err)
	}
	digest, err := calc.Digest()
	if err != nil || digest != calc.CalculationDigest {
		t.Fatalf("Digest() = %q, err = %v", digest, err)
	}
}

// TestTodo_PAYRUN_004_Property proves determinism and exact-decimal refusal:
// reordered workers and lines produce the identical digest, and every
// malformed decimal or rate is refused without totals.
func TestTodo_PAYRUN_004_Property(t *testing.T) {
	base, err := ExecuteTrialCalculation(trialRequestFixture())
	if err != nil {
		t.Fatal(err)
	}
	shuffled := trialRequestFixture()
	shuffled.Workers[0], shuffled.Workers[1] = shuffled.Workers[1], shuffled.Workers[0]
	lines := shuffled.Workers[1].Lines
	lines[0], lines[1] = lines[1], lines[0]
	again, err := ExecuteTrialCalculation(shuffled)
	if err != nil {
		t.Fatal(err)
	}
	if again.CalculationDigest != base.CalculationDigest || !bytes.Equal(again.Canonical(), base.Canonical()) {
		t.Fatal("reordered inputs did not produce identical calculation evidence")
	}
	if again.Workers[0].WorkerRef != "worker-a" || again.Workers[1].WorkerRef != "worker-b" {
		t.Fatalf("worker order is not canonical: %+v", again.Workers)
	}
	refusals := []func(*TrialCalcRequest){
		func(r *TrialCalcRequest) {
			r.Workers[0].Lines[0].Amount = values.MustDecimal("2000.0000", 4, values.RoundingHalfUp)
		},
		func(r *TrialCalcRequest) {
			r.Workers[0].Lines[0].Amount = values.MustDecimal("-2000.00", 2, values.RoundingHalfUp)
		},
		func(r *TrialCalcRequest) { r.Workers[0].Lines[0].Code = "" },
		func(r *TrialCalcRequest) { r.Rules.DeductionBps = 10001 },
		func(r *TrialCalcRequest) { r.Rules.TaxBps = -1 },
		func(r *TrialCalcRequest) { r.Workers = nil },
		func(r *TrialCalcRequest) { r.KnownCodes = nil },
		func(r *TrialCalcRequest) {
			r.Workers = append(r.Workers, r.Workers[0])
		},
	}
	for i, mutate := range refusals {
		req := trialRequestFixture()
		mutate(&req)
		calc, err := ExecuteTrialCalculation(req)
		if !errors.Is(err, ErrTrialRejected) {
			t.Fatalf("case %d error = %v, want ErrTrialRejected", i, err)
		}
		if calc.CalculationDigest != "" || len(calc.Workers) != 0 {
			t.Fatalf("case %d produced totals from refused input: %+v", i, calc)
		}
	}
}

// TestTodo_PAYRUN_004_Race proves concurrent calculations agree exactly and
// never mutate the shared request.
func TestTodo_PAYRUN_004_Race(t *testing.T) {
	base := trialRequestFixture()
	var wg sync.WaitGroup
	digests := make([]string, 8)
	errs := make([]error, 8)
	for i := range digests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			calc, err := ExecuteTrialCalculation(base)
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = calc.CalculationDigest
			_ = calc.Validate()
			_ = calc.Canonical()
			_, _ = calc.Explain()
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
		if digests[i] != digests[0] {
			t.Fatal("concurrent calculations diverged")
		}
	}
	if base.Workers[0].WorkerRef != "worker-a" || base.CalculationKey != "trial-key-1" {
		t.Fatal("calculation mutated the caller request")
	}
}

// TestTodo_PAYRUN_004_Integration proves the trial result plugs into the
// governed run lifecycle: its digest serves as calculation evidence and its
// net and rules digests bind the approval context.
func TestTodo_PAYRUN_004_Integration(t *testing.T) {
	calc, err := ExecuteTrialCalculation(trialRequestFixture())
	if err != nil {
		t.Fatal(err)
	}
	calculated, err := validPayrollRun(t).Calculate(calc.CalculationDigest)
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	context, err := NewApprovalContext(calculated, calc.RunNet.String(), "sha256:exceptions", calc.Rules.Digest)
	if err != nil {
		t.Fatalf("NewApprovalContext: %v", err)
	}
	if err := context.Validate(); err != nil {
		t.Fatalf("approval context: %v", err)
	}
	if calculated.State != Calculated {
		t.Fatalf("run state = %s, want CALCULATED", calculated.State)
	}
}

// TestTodo_PAYRUN_004_Fault proves unknown jurisdiction, rules, and balances
// block with typed refusals and yield zero totals, never zeros-as-answers.
func TestTodo_PAYRUN_004_Fault(t *testing.T) {
	unknowns := []struct {
		name   string
		mutate func(*TrialCalcRequest)
	}{
		{"jurisdiction", func(r *TrialCalcRequest) { r.Rules.Jurisdiction = "" }},
		{"rules version", func(r *TrialCalcRequest) { r.Rules.Version = "" }},
		{"rules digest", func(r *TrialCalcRequest) { r.Rules.Digest = "" }},
		{"period", func(r *TrialCalcRequest) { r.PeriodDigest = "" }},
		{"all balances", func(r *TrialCalcRequest) {
			r.Workers[0].BalanceDigest = ""
			r.Workers[1].BalanceDigest = ""
		}},
	}
	for _, tc := range unknowns {
		req := trialRequestFixture()
		tc.mutate(&req)
		calc, err := ExecuteTrialCalculation(req)
		if !errors.Is(err, ErrTrialUnknown) {
			t.Fatalf("%s error = %v, want ErrTrialUnknown", tc.name, err)
		}
		if !errors.Is(err, ErrTrialRejected) {
			t.Fatalf("%s error = %v, want ErrTrialRejected boundary", tc.name, err)
		}
		if calc.CalculationDigest != "" || !calc.RunNet.IsZero() && calc.RunNet.String() != "" {
			t.Fatalf("%s produced an answer from unknown input: %+v", tc.name, calc)
		}
		var refusal *TrialCalcError
		if !errors.As(err, &refusal) || refusal.Field == "" || refusal.Reason == "" {
			t.Fatalf("%s refusal = %+v, want field and reason", tc.name, err)
		}
	}
	// One unknown balance excludes only that worker: the known worker still
	// totals exactly while the unknown worker raises a blocking exception.
	partial := trialRequestFixture()
	partial.Workers[1].BalanceDigest = ""
	calc, err := ExecuteTrialCalculation(partial)
	if err != nil {
		t.Fatalf("partial ExecuteTrialCalculation: %v", err)
	}
	if len(calc.Workers) != 1 || calc.RunGross.String() != "2500.00" {
		t.Fatalf("partial totals wrong: %+v", calc)
	}
	if len(calc.Exceptions) != 1 || calc.Exceptions[0].Severity != TrialExceptionBlocking {
		t.Fatalf("partial exceptions wrong: %+v", calc.Exceptions)
	}
	// An unknown earning code excludes only that worker with an exception.
	coded := trialRequestFixture()
	coded.Workers[1].Lines[0].Code = "BONUS"
	calc, err = ExecuteTrialCalculation(coded)
	if err != nil {
		t.Fatalf("coded ExecuteTrialCalculation: %v", err)
	}
	if len(calc.Workers) != 1 || len(calc.Exceptions) != 1 {
		t.Fatalf("coded result wrong: %+v", calc)
	}
	if calc.Exceptions[0].Code != TrialExceptionUnknownCode {
		t.Fatalf("coded exception = %+v", calc.Exceptions[0])
	}
}

// TestTodo_PAYRUN_004_Security proves tenant binding: a missing tenant is
// refused, and the same payroll under another tenant never shares a digest.
func TestTodo_PAYRUN_004_Security(t *testing.T) {
	tenantless := trialRequestFixture()
	tenantless.Tenant = "  "
	if _, err := ExecuteTrialCalculation(tenantless); !errors.Is(err, ErrTrialRejected) {
		t.Fatalf("tenantless error = %v, want ErrTrialRejected", err)
	}
	other := trialRequestFixture()
	other.Tenant = "tenant-other"
	otherCalc, err := ExecuteTrialCalculation(other)
	if err != nil {
		t.Fatal(err)
	}
	base, err := ExecuteTrialCalculation(trialRequestFixture())
	if err != nil {
		t.Fatal(err)
	}
	if otherCalc.CalculationDigest == base.CalculationDigest {
		t.Fatal("cross-tenant calculations share a digest")
	}
}

// TestTodo_PAYRUN_004_Mutation proves the digest binds every amount, code,
// rule, and jurisdiction: any change yields a new digest and a tampered
// result fails validation.
func TestTodo_PAYRUN_004_Mutation(t *testing.T) {
	base, err := ExecuteTrialCalculation(trialRequestFixture())
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*TrialCalcRequest){
		func(r *TrialCalcRequest) {
			r.Workers[0].Lines[0].Amount = values.MustDecimal("2000.01", 2, values.RoundingHalfUp)
		},
		func(r *TrialCalcRequest) { r.Workers[0].Lines[0].Code = "OT" },
		func(r *TrialCalcRequest) { r.Rules.Version = "rules.payroll/2026.2" },
		func(r *TrialCalcRequest) { r.Rules.Jurisdiction = "US-NY" },
		func(r *TrialCalcRequest) { r.Rules.DeductionBps = 1600 },
		func(r *TrialCalcRequest) { r.PeriodDigest = "sha256:other-period" },
	}
	for i, mutate := range mutations {
		req := trialRequestFixture()
		mutate(&req)
		mutated, err := ExecuteTrialCalculation(req)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if mutated.CalculationDigest == base.CalculationDigest {
			t.Fatalf("case %d did not change the digest", i)
		}
	}
	tampered := base
	tampered.RunNet = values.MustDecimal("0.01", 2, values.RoundingHalfUp)
	if err := tampered.Validate(); err == nil {
		t.Fatal("tampered totals passed validation")
	}
	forged := base
	forged.CalculationDigest = "sha256:forged"
	if err := forged.Validate(); err == nil {
		t.Fatal("forged digest passed validation")
	}
}

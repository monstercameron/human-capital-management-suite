package payroll

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func exceptionCalcFixture(t *testing.T) TrialCalculation {
	t.Helper()
	req := trialRequestFixture()
	req.CalculationKey = "trial-exceptions-1"
	req.Workers = append(req.Workers,
		TrialWorkerInput{
			WorkerRef:     "worker-c",
			BalanceDigest: "sha256:balance-c",
			Lines: []TrialEarningLine{
				{Code: "BONUS", Amount: values.MustDecimal("300.00", 2, values.RoundingHalfUp)},
			},
		},
		TrialWorkerInput{
			WorkerRef: "worker-d",
			Lines: []TrialEarningLine{
				{Code: "BASE", Amount: values.MustDecimal("800.00", 2, values.RoundingHalfUp)},
			},
		},
	)
	calc, err := ExecuteTrialCalculation(req)
	if err != nil {
		t.Fatalf("ExecuteTrialCalculation: %v", err)
	}
	return calc
}

func exceptionID(calc TrialCalculation, worker string) string {
	for _, e := range calc.Exceptions {
		if e.WorkerRef == worker {
			return e.ExceptionID
		}
	}
	return ""
}

func fullResolutionFixture(calc TrialCalculation) []ExceptionResolution {
	return []ExceptionResolution{
		{
			ExceptionID: exceptionID(calc, "worker-c"),
			Action:      ResolutionRemapCode,
			FromCode:    "BONUS",
			ToCode:      "BASE",
		},
		{
			ExceptionID:   exceptionID(calc, "worker-d"),
			Action:        ResolutionBindBalance,
			BalanceDigest: "sha256:balance-d",
		},
	}
}

// TestTodo_PAYRUN_005 is the primary acceptance case: every exception carries
// owner, severity, affected worker, and repair action; resolving all of them
// recalculates only the affected scope while preserving every prior attempt.
func TestTodo_PAYRUN_005(t *testing.T) {
	prior := exceptionCalcFixture(t)
	if len(prior.Exceptions) != 2 {
		t.Fatalf("exceptions = %+v, want 2", prior.Exceptions)
	}
	for _, e := range prior.Exceptions {
		if e.Owner == "" || e.Severity == "" || e.WorkerRef == "" || e.RepairAction == "" {
			t.Fatalf("exception is missing owner/severity/worker/action: %+v", e)
		}
		if e.Status != TrialExceptionOpen {
			t.Fatalf("exception status = %s, want OPEN", e.Status)
		}
	}
	resolved, err := ResolveTrialExceptions(prior, fullResolutionFixture(prior))
	if err != nil {
		t.Fatalf("ResolveTrialExceptions: %v", err)
	}
	if resolved.Revision != 2 || resolved.SupersedesDigest != prior.CalculationDigest {
		t.Fatalf("revision linkage wrong: revision=%d supersedes=%q", resolved.Revision, resolved.SupersedesDigest)
	}
	if len(resolved.Attempts) != 1 || len(resolved.Exceptions) != 2 {
		t.Fatalf("attempts/exceptions wrong: %+v", resolved)
	}
	for _, e := range resolved.Exceptions {
		if e.Status != TrialExceptionResolved {
			t.Fatalf("exception %s status = %s, want RESOLVED", e.ExceptionID, e.Status)
		}
	}
	byRef := map[string]TrialWorkerTotal{}
	for _, w := range resolved.Workers {
		byRef[w.WorkerRef] = w
	}
	if len(resolved.Workers) != 4 {
		t.Fatalf("workers = %+v, want 4 recalculated", resolved.Workers)
	}
	if got := byRef["worker-c"]; got.Gross.String() != "300.00" || got.Deduction.String() != "45.00" || got.Tax.String() != "60.00" || got.Net.String() != "195.00" {
		t.Fatalf("worker-c wrong: %+v", got)
	}
	if got := byRef["worker-d"]; got.Gross.String() != "800.00" || got.Deduction.String() != "120.00" || got.Tax.String() != "160.00" || got.Net.String() != "520.00" {
		t.Fatalf("worker-d wrong: %+v", got)
	}
	// Unaffected workers are byte-identical to the prior attempt.
	priorByRef := map[string]TrialWorkerTotal{}
	for _, w := range prior.Workers {
		priorByRef[w.WorkerRef] = w
	}
	for _, ref := range []string{"worker-a", "worker-b"} {
		if !byRef[ref].Gross.Equal(priorByRef[ref].Gross) || !byRef[ref].Net.Equal(priorByRef[ref].Net) || byRef[ref].Trace != priorByRef[ref].Trace {
			t.Fatalf("unaffected worker %s changed: %+v vs %+v", ref, byRef[ref], priorByRef[ref])
		}
	}
	if resolved.RunGross.String() != "4600.00" || resolved.RunDeduction.String() != "690.00" || resolved.RunTax.String() != "920.00" || resolved.RunNet.String() != "2990.00" {
		t.Fatalf("run totals wrong: gross=%s deduction=%s tax=%s net=%s", resolved.RunGross, resolved.RunDeduction, resolved.RunTax, resolved.RunNet)
	}
	if err := resolved.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if resolved.CalculationDigest == prior.CalculationDigest {
		t.Fatal("recalculation did not change the digest")
	}
}

// TestTodo_PAYRUN_005_Property proves scoped invalidation: the prior attempt
// is never mutated, the attempt history binds the superseded digest, and a
// resolution aimed at the wrong reason class is refused.
func TestTodo_PAYRUN_005_Property(t *testing.T) {
	prior := exceptionCalcFixture(t)
	priorDigest := prior.CalculationDigest
	resolved, err := ResolveTrialExceptions(prior, fullResolutionFixture(prior))
	if err != nil {
		t.Fatal(err)
	}
	if prior.CalculationDigest != priorDigest || len(prior.Attempts) != 0 {
		t.Fatal("resolution mutated the prior attempt")
	}
	attempt := resolved.Attempts[0]
	if attempt.SupersedesDigest != priorDigest || attempt.CalculationDigest != resolved.CalculationDigest || len(attempt.ResolvedExceptions) != 2 {
		t.Fatalf("attempt record wrong: %+v", attempt)
	}
	// A balance binding aimed at a code exception is refused, and vice versa.
	swapped := fullResolutionFixture(prior)
	swapped[0].Action = ResolutionBindBalance
	swapped[0].BalanceDigest = "sha256:balance-c"
	swapped[0].ToCode = ""
	if _, err := ResolveTrialExceptions(prior, swapped[:1]); !errors.Is(err, ErrInvalidResolution) {
		t.Fatalf("swapped action error = %v", err)
	}
	swappedBack := fullResolutionFixture(prior)
	swappedBack[1].Action = ResolutionRemapCode
	swappedBack[1].FromCode = "BASE"
	swappedBack[1].ToCode = "OT"
	if _, err := ResolveTrialExceptions(prior, swappedBack[1:]); !errors.Is(err, ErrInvalidResolution) {
		t.Fatalf("swapped balance error = %v", err)
	}
	// Remapping onto an unknown code is refused.
	badTarget := fullResolutionFixture(prior)
	badTarget[0].ToCode = "BONUS"
	if _, err := ResolveTrialExceptions(prior, badTarget[:1]); !errors.Is(err, ErrInvalidResolution) {
		t.Fatalf("unknown target error = %v", err)
	}
}

// TestTodo_PAYRUN_005_Race proves concurrent resolutions agree exactly.
func TestTodo_PAYRUN_005_Race(t *testing.T) {
	prior := exceptionCalcFixture(t)
	resolutions := fullResolutionFixture(prior)
	var wg sync.WaitGroup
	digests := make([]string, 8)
	errs := make([]error, 8)
	for i := range digests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resolved, err := ResolveTrialExceptions(prior, resolutions)
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = resolved.CalculationDigest
			_ = resolved.Validate()
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
		if digests[i] != digests[0] {
			t.Fatal("concurrent resolutions diverged")
		}
	}
}

// TestTodo_PAYRUN_005_Integration proves the resolved calculation feeds the
// governed approval path with its new revision digest.
func TestTodo_PAYRUN_005_Integration(t *testing.T) {
	prior := exceptionCalcFixture(t)
	resolved, err := ResolveTrialExceptions(prior, fullResolutionFixture(prior))
	if err != nil {
		t.Fatal(err)
	}
	calculated, err := validPayrollRun(t).Calculate(resolved.CalculationDigest)
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	context, err := NewApprovalContext(calculated, resolved.RunNet.String(), "sha256:exceptions", resolved.Rules.Digest)
	if err != nil {
		t.Fatalf("NewApprovalContext: %v", err)
	}
	if err := context.Validate(); err != nil {
		t.Fatalf("approval context: %v", err)
	}
	if resolved.Attempts[0].SupersedesDigest != prior.CalculationDigest {
		t.Fatal("approval binds a recalculation that lost its prior attempt")
	}
}

// TestTodo_PAYRUN_005_Fault proves every repair failure is typed and yields
// no recalculation: unknown IDs, duplicates, partial repairs, stale retries,
// and empty repair sets.
func TestTodo_PAYRUN_005_Fault(t *testing.T) {
	prior := exceptionCalcFixture(t)
	unknown := fullResolutionFixture(prior)
	unknown[0].ExceptionID = "trial-exception/run-2026-11-b/ghost/UNKNOWN_EARNING_CODE"
	if _, err := ResolveTrialExceptions(prior, unknown[:1]); !errors.Is(err, ErrResolutionUnknown) {
		t.Fatalf("unknown id error = %v", err)
	}
	duplicated := fullResolutionFixture(prior)
	duplicated = append(duplicated, duplicated[0])
	if _, err := ResolveTrialExceptions(prior, duplicated); !errors.Is(err, ErrInvalidResolution) {
		t.Fatalf("duplicate error = %v", err)
	}
	partial := fullResolutionFixture(prior)
	leaked, err := ResolveTrialExceptions(prior, partial[:1])
	if !errors.Is(err, ErrExceptionsBlocking) {
		t.Fatalf("partial repair error = %v", err)
	}
	if leaked.CalculationDigest != "" || len(leaked.Workers) != 0 {
		t.Fatalf("blocked repair leaked a recalculation: %+v", leaked)
	}
	if _, err := ResolveTrialExceptions(prior, nil); !errors.Is(err, ErrInvalidResolution) {
		t.Fatalf("empty repair error = %v", err)
	}
	resolved, err := ResolveTrialExceptions(prior, fullResolutionFixture(prior))
	if err != nil {
		t.Fatal(err)
	}
	stale := fullResolutionFixture(prior)
	if _, err := ResolveTrialExceptions(resolved, stale); !errors.Is(err, ErrResolutionUnknown) {
		t.Fatalf("stale retry error = %v, want ErrResolutionUnknown", err)
	}
	var refusal *ResolveError
	if _, err := ResolveTrialExceptions(prior, partial[:1]); !errors.As(err, &refusal) || refusal.Field == "" || refusal.Reason == "" {
		t.Fatalf("refusal = %+v, want field and reason", err)
	}
}

// TestTodo_PAYRUN_005_Mutation proves the recalculation digest binds the
// repaired scope: a different repair target changes the digest and a tampered
// recalculation fails validation.
func TestTodo_PAYRUN_005_Mutation(t *testing.T) {
	prior := exceptionCalcFixture(t)
	resolved, err := ResolveTrialExceptions(prior, fullResolutionFixture(prior))
	if err != nil {
		t.Fatal(err)
	}
	alt := fullResolutionFixture(prior)
	alt[0].ToCode = "OT"
	alt[0].FromCode = "BONUS"
	// OT rate equals BASE rate in the fixture rules, so totals match but the
	// code binding still changes the digest.
	altResolved, err := ResolveTrialExceptions(prior, []ExceptionResolution{alt[0], fullResolutionFixture(prior)[1]})
	if err != nil {
		t.Fatal(err)
	}
	if altResolved.CalculationDigest == resolved.CalculationDigest {
		t.Fatal("repaired-code change did not change the digest")
	}
	tampered := resolved
	tampered.RunNet = values.MustDecimal("0.01", 2, values.RoundingHalfUp)
	if err := tampered.Validate(); err == nil {
		t.Fatal("tampered recalculation passed validation")
	}
}

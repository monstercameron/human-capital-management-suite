package scenario

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_SCENARIO_007_Property: reconciliation is deterministic, an
// empty report leaves every intent unknown, ungoverned keys and
// mismatched revisions are rejected, and any status change moves the
// digest.
func TestTodo_SCENARIO_007_Property(t *testing.T) {
	compiled := compiledSix(t)
	headcount := values.MustDecimal("12", 0, values.RoundingHalfEven)
	report := report007(t, compiled, []ExecutionEntry{
		entry007("headcount.target", ExecutionExecuted, DecimalValue(headcount), mustDec007(t, "1500"), mustDec007(t, "1500")),
	})

	first, err := ReconcileExecution(compiled, report)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ReconcileExecution(compiled, report)
	if err != nil {
		t.Fatal(err)
	}
	if first.CanonicalDigest != second.CanonicalDigest {
		t.Fatal("identical reconciliation inputs replayed to different digests")
	}
	if first.ExecutedCount != 1 || first.UnknownCount != 5 {
		t.Fatalf("counts = %+v", first)
	}
	for _, delta := range first.Deltas {
		if delta.Key == "headcount.target" {
			if delta.Status != ExecutionExecuted || delta.OutcomeVariance || delta.CostVariance {
				t.Fatalf("headcount delta = %+v", delta)
			}
			continue
		}
		if delta.Status != ExecutionUnknown || delta.OutcomeVariance || delta.CostVariance || delta.CostDelta != "" {
			t.Fatalf("unreported delta = %+v", delta)
		}
	}

	// An empty report leaves every intent unknown with zero variance.
	empty, err := ReconcileExecution(compiled, report007(t, compiled, nil))
	if err != nil {
		t.Fatal(err)
	}
	if empty.UnknownCount != len(compiled.Intents) || empty.ExecutedCount+empty.DeferredCount+empty.RejectedCount+empty.FailedCount != 0 {
		t.Fatalf("empty counts = %+v", empty)
	}

	// Any status change moves the digest.
	moved := report007(t, compiled, []ExecutionEntry{
		entry007("headcount.target", ExecutionFailed, TypedValue{}, mustDec007(t, "1500"), mustDec007(t, "1500")),
	})
	failed, err := ReconcileExecution(compiled, moved)
	if err != nil {
		t.Fatal(err)
	}
	if failed.CanonicalDigest == first.CanonicalDigest {
		t.Fatal("status change did not move the reconciliation digest")
	}
	if failed.FailedCount != 1 || failed.ExecutedCount != 0 {
		t.Fatalf("moved counts = %+v", failed)
	}

	// An entry for a key the compilation never governed is rejected.
	rogue := report007(t, compiled, []ExecutionEntry{
		entry007("shadow.plan", ExecutionExecuted, DecimalValue(headcount), mustDec007(t, "1"), mustDec007(t, "1")),
	})
	if _, err := ReconcileExecution(compiled, rogue); !errors.Is(err, ErrReconciliationRejected) {
		t.Fatalf("ungoverned key err = %v", err)
	}
	// Duplicate entries for one key are rejected.
	dupe := report007(t, compiled, []ExecutionEntry{
		entry007("headcount.target", ExecutionExecuted, DecimalValue(headcount), mustDec007(t, "1500"), mustDec007(t, "1500")),
		entry007("headcount.target", ExecutionDeferred, TypedValue{}, mustDec007(t, "1500"), mustDec007(t, "0")),
	})
	if _, err := ReconcileExecution(compiled, dupe); !errors.Is(err, ErrReconciliationRejected) {
		t.Fatalf("duplicate entry err = %v", err)
	}
	// A report for another revision is rejected.
	other := report
	other.Revision++
	if _, err := ReconcileExecution(compiled, other); !errors.Is(err, ErrReconciliationRejected) {
		t.Fatalf("revision mismatch err = %v", err)
	}
	// An executed entry without an actual outcome is invalid input, not
	// a variance.
	blank := report007(t, compiled, []ExecutionEntry{
		entry007("headcount.target", ExecutionExecuted, TypedValue{}, mustDec007(t, "1500"), mustDec007(t, "1500")),
	})
	if _, err := ReconcileExecution(compiled, blank); err == nil {
		t.Fatal("executed entry without an actual outcome accepted")
	}
}

// TestTodo_SCENARIO_007_Golden: a fixed two-intent completion pins its
// exact status counts and reconciliation digest.
func TestTodo_SCENARIO_007_Golden(t *testing.T) {
	plan := planWithTwoDeltas(t)
	compiled, err := CompilePlan(plan, spec006())
	if err != nil {
		t.Fatal(err)
	}
	headcount := values.MustDecimal("12", 0, values.RoundingHalfEven)
	report := report007(t, compiled, []ExecutionEntry{
		entry007("headcount.target", ExecutionExecuted, DecimalValue(headcount), mustDec007(t, "1500"), mustDec007(t, "1500")),
		entry007("hiring.freeze", ExecutionDeferred, TypedValue{}, mustDec007(t, "200"), mustDec007(t, "0")),
	})
	reconciled, err := ReconcileExecution(compiled, report)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.ExecutedCount != 1 || reconciled.DeferredCount != 1 || reconciled.UnknownCount != 0 {
		t.Fatalf("counts = %+v", reconciled)
	}
	if len(reconciled.Deltas) != 2 || reconciled.Deltas[0].Key != "headcount.target" || reconciled.Deltas[1].Key != "hiring.freeze" {
		t.Fatalf("deltas = %+v", reconciled.Deltas)
	}
	if reconciled.Deltas[0].OutcomeVariance || reconciled.Deltas[0].CostVariance {
		t.Fatalf("headcount delta = %+v", reconciled.Deltas[0])
	}
	if reconciled.Deltas[1].Status != ExecutionDeferred || !reconciled.Deltas[1].CostVariance {
		t.Fatalf("freeze delta = %+v", reconciled.Deltas[1])
	}
	const wantDigest = "sha256:732c7938adaa9f3796ab6b58ec79c2f93adc19b4eb99bcf2a1ecaae34020f9c7"
	if reconciled.CanonicalDigest != wantDigest {
		t.Fatalf("reconciliation digest = %q want %q", reconciled.CanonicalDigest, wantDigest)
	}
}

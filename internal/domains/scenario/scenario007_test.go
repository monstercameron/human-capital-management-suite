package scenario

import (
	"errors"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func mustDec007(t *testing.T, text string) values.Decimal {
	t.Helper()
	dec, err := values.NewDecimal(text, 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return dec
}

// compiledSix freezes a six-assumption plan and compiles it: one intent
// per assumption key, so the completion report can exercise every
// execution status plus an unreported key.
func compiledSix(t *testing.T) IntentCompilation {
	t.Helper()
	revision := baseScenario(t)
	grown := revision
	for _, assumption := range []Assumption{
		{Key: "hiring.freeze", Value: BooleanValue(true), Unit: "FLAG", ProvenanceRefs: []string{"policy:2026-03"}},
		{Key: "comp.budget", Value: DecimalValue(values.MustDecimal("500", 0, values.RoundingHalfEven)), Unit: "HEAD", ProvenanceRefs: []string{"finance:2026"}},
		{Key: "travel.cap", Value: DecimalValue(values.MustDecimal("50", 0, values.RoundingHalfEven)), Unit: "TRIP", ProvenanceRefs: []string{"finance:2026"}},
		{Key: "training.seats", Value: DecimalValue(values.MustDecimal("30", 0, values.RoundingHalfEven)), Unit: "SEAT", ProvenanceRefs: []string{"lms:2026"}},
		{Key: "attrition.guard", Value: TextValue("watch"), Unit: "NOTE", ProvenanceRefs: []string{"hr:2026"}},
	} {
		next, err := grown.Fork(assumption)
		if err != nil {
			t.Fatal(err)
		}
		grown = next
	}
	approval := approval005(revision)
	approval.ExpectedDigest = grown.CanonicalDigest
	plan := mustApprove005(t, grown, approval, planInstant("2026-01-15T12:00:00Z"))
	compiled, err := CompilePlan(plan, CompileSpec{
		GovernanceRef: "governance:scenario-compile", GovernanceVersion: "v1",
		AllowedKeys: []string{"headcount.target", "hiring.freeze", "comp.budget", "travel.cap", "training.seats", "attrition.guard"},
		WriteTargets: map[string][]string{
			"headcount.target": {"workforce.plan.headcount"},
			"hiring.freeze":    {"workforce.plan.freeze"},
			"comp.budget":      {"workforce.plan.budget"},
			"travel.cap":       {"workforce.plan.travel"},
			"training.seats":   {"workforce.plan.training"},
			"attrition.guard":  {"workforce.plan.watch"},
		},
		Dependencies:  map[string][]string{"hiring.freeze": {"headcount.target"}},
		SimulationRef: "simulation:run-7",
		MaxIntents:    8,
	})
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func entry007(key string, status ExecutionStatus, actual TypedValue, plannedCost, actualCost values.Decimal) ExecutionEntry {
	return ExecutionEntry{
		Key: key, Status: status, ActualValue: actual,
		PlannedCost: plannedCost, ActualCost: actualCost,
		Note: string(status) + " " + key,
	}
}

// compiledValues snapshots the assumption values a compilation froze,
// so the no-rewrite assertion can prove the reconciliation left them
// byte-identical.
func compiledValues(compiled IntentCompilation) map[string]string {
	out := make(map[string]string, len(compiled.Intents))
	for _, intent := range compiled.Intents {
		out[intent.Key] = string(intent.Value.Canonical())
	}
	return out
}

func report007(t *testing.T, compiled IntentCompilation, entries []ExecutionEntry) ExecutionReport {
	t.Helper()
	return ExecutionReport{
		ScenarioID: compiled.ScenarioID, Revision: compiled.Revision,
		RevisionDigest: compiled.RevisionDigest,
		ReportedAt:     planInstant("2026-02-01T12:00:00Z"),
		Entries:        entries,
	}
}

// TestTodo_SCENARIO_007: reconciling the executed Plan against its
// approved compilation reports executed, deferred, rejected, failed and
// unknown deltas with actual outcome and cost variance — and never
// rewrites the scenario assumptions.
func TestTodo_SCENARIO_007(t *testing.T) {
	compiled := compiledSix(t)
	headcount := values.MustDecimal("12", 0, values.RoundingHalfEven)
	report := report007(t, compiled, []ExecutionEntry{
		entry007("headcount.target", ExecutionExecuted, DecimalValue(headcount), mustDec007(t, "1500"), mustDec007(t, "1500")),
		entry007("hiring.freeze", ExecutionExecuted, BooleanValue(false), mustDec007(t, "200"), mustDec007(t, "260")),
		entry007("comp.budget", ExecutionDeferred, TypedValue{}, mustDec007(t, "500"), mustDec007(t, "0")),
		entry007("travel.cap", ExecutionRejected, TypedValue{}, mustDec007(t, "50"), mustDec007(t, "50")),
		entry007("training.seats", ExecutionFailed, TypedValue{}, mustDec007(t, "30"), mustDec007(t, "30")),
	})

	beforeCompiled := compiled.CanonicalDigest
	beforeValues := compiledValues(compiled)
	beforeReport := append([]ExecutionEntry(nil), report.Entries...)

	reconciled, err := ReconcileExecution(compiled, report)
	if err != nil {
		t.Fatal(err)
	}
	if len(reconciled.Deltas) != 6 {
		t.Fatalf("deltas = %+v", reconciled.Deltas)
	}
	byKey := make(map[string]IntentDelta, len(reconciled.Deltas))
	previous := ""
	for _, delta := range reconciled.Deltas {
		byKey[delta.Key] = delta
		if delta.Key < previous {
			t.Fatalf("deltas are not ordered by key: %+v", reconciled.Deltas)
		}
		previous = delta.Key
	}
	cases := []struct {
		key             string
		status          ExecutionStatus
		outcomeVariance bool
		costVariance    bool
	}{
		{"headcount.target", ExecutionExecuted, false, false},
		{"hiring.freeze", ExecutionExecuted, true, true},
		{"comp.budget", ExecutionDeferred, false, true},
		{"travel.cap", ExecutionRejected, false, false},
		{"training.seats", ExecutionFailed, false, false},
		{"attrition.guard", ExecutionUnknown, false, false},
	}
	for _, want := range cases {
		got, ok := byKey[want.key]
		if !ok {
			t.Fatalf("missing delta for %q", want.key)
		}
		if got.Status != want.status || got.OutcomeVariance != want.outcomeVariance || got.CostVariance != want.costVariance {
			t.Fatalf("delta %q = %+v", want.key, got)
		}
	}
	if reconciled.ExecutedCount != 2 || reconciled.DeferredCount != 1 || reconciled.RejectedCount != 1 ||
		reconciled.FailedCount != 1 || reconciled.UnknownCount != 1 {
		t.Fatalf("counts = %+v", reconciled)
	}
	if reconciled.ScenarioID != compiled.ScenarioID || reconciled.RevisionDigest != compiled.RevisionDigest ||
		reconciled.CompilationDigest != compiled.CanonicalDigest {
		t.Fatalf("reconciliation does not bind the compilation: %+v", reconciled)
	}
	if reconciled.CanonicalDigest == "" || reconciled.Explain() == "" {
		t.Fatal("reconciliation carries no digest or summary")
	}
	if err := reconciled.Validate(); err != nil {
		t.Fatal(err)
	}

	// The reconciliation never rewrites scenario assumptions: inputs
	// are byte-identical before and after.
	if compiled.CanonicalDigest != beforeCompiled {
		t.Fatal("reconciliation mutated the compilation")
	}
	if got := compiledValues(compiled); !reflect.DeepEqual(got, beforeValues) {
		t.Fatalf("reconciliation rewrote assumptions: %+v", got)
	}
	if !reflect.DeepEqual(report.Entries, beforeReport) {
		t.Fatal("reconciliation mutated the execution report")
	}

	// A report bound to a different revision digest is rejected without
	// a reconciliation.
	tampered := report
	tampered.RevisionDigest = "digest:other-revision"
	_, err = ReconcileExecution(compiled, tampered)
	if !errors.Is(err, ErrReconciliationRejected) {
		t.Fatalf("tampered digest err = %v", err)
	}
	if rejected, ok := AsReconcileRejected(err); !ok || rejected.Code != ReconciliationRejectedCode {
		t.Fatalf("tampered digest err = %v", err)
	}
}

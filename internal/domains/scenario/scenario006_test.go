package scenario

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// planWithTwoDeltas freezes an approved plan carrying two compilable
// deltas: a headcount target and a hiring freeze that depends on it.
func planWithTwoDeltas(t *testing.T) Plan {
	t.Helper()
	revision := baseScenario(t)
	grown, err := revision.Fork(Assumption{
		Key: "hiring.freeze", Value: BooleanValue(true), Unit: "FLAG", ProvenanceRefs: []string{"policy:2026-03"},
	})
	if err != nil {
		t.Fatal(err)
	}
	approval := approval005(revision)
	approval.ExpectedDigest = grown.CanonicalDigest
	return mustApprove005(t, grown, approval, planInstant("2026-01-15T12:00:00Z"))
}

// spec006 governs both deltas: the freeze depends on the headcount
// target, each has a declared write set, and nothing conflicts.
func spec006() CompileSpec {
	return CompileSpec{
		GovernanceRef: "governance:scenario-compile", GovernanceVersion: "v1",
		AllowedKeys: []string{"headcount.target", "hiring.freeze"},
		WriteTargets: map[string][]string{
			"headcount.target": {"workforce.plan.headcount"},
			"hiring.freeze":    {"workforce.plan.freeze"},
		},
		Dependencies:  map[string][]string{"hiring.freeze": {"headcount.target"}},
		SimulationRef: "simulation:run-7",
		MaxIntents:    8,
	}
}

func mustCompile006(t *testing.T, plan Plan, spec CompileSpec) IntentCompilation {
	t.Helper()
	compiled, err := CompilePlan(plan, spec)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

// TestTodo_SCENARIO_006: compiling an approved Plan emits bounded,
// ordered intents carrying proposal and write sets, dependencies and
// simulation links. An unapproved plan, an ungoverned delta and
// conflicting deltas all refuse with a typed rejection.
func TestTodo_SCENARIO_006(t *testing.T) {
	plan := planWithTwoDeltas(t)
	spec := spec006()

	compiled := mustCompile006(t, plan, spec)
	if len(compiled.Intents) != 2 {
		t.Fatalf("intents = %+v", compiled.Intents)
	}
	first, second := compiled.Intents[0], compiled.Intents[1]
	if first.Key != "headcount.target" || second.Key != "hiring.freeze" {
		t.Fatalf("intents are not ordered by key: %q, %q", first.Key, second.Key)
	}
	for _, intent := range compiled.Intents {
		if !strings.Contains(intent.Proposal, intent.Key) || len(intent.WriteSet) == 0 {
			t.Fatalf("intent carries no proposal/write set: %+v", intent)
		}
		if intent.SimulationLink != spec.SimulationRef+"#"+intent.Key {
			t.Fatalf("simulation link = %q", intent.SimulationLink)
		}
		if len(intent.ProvenanceRefs) == 0 {
			t.Fatalf("intent lost its provenance: %+v", intent)
		}
	}
	if len(first.WriteSet) != 1 || first.WriteSet[0] != "workforce.plan.headcount" {
		t.Fatalf("headcount write set = %v", first.WriteSet)
	}
	if len(second.DependsOn) != 1 || second.DependsOn[0] != "headcount.target" {
		t.Fatalf("freeze dependencies = %v", second.DependsOn)
	}
	if len(first.DependsOn) != 0 {
		t.Fatalf("headcount dependencies = %v", first.DependsOn)
	}
	if compiled.ScenarioID != plan.ScenarioID || compiled.Revision != plan.Revision ||
		compiled.RevisionDigest != plan.CanonicalDigest {
		t.Fatalf("compilation does not bind the plan: %+v", compiled)
	}
	if compiled.GovernanceRef != spec.GovernanceRef || compiled.GovernanceVersion != spec.GovernanceVersion {
		t.Fatalf("compilation does not bind governance: %+v", compiled)
	}
	if compiled.CanonicalDigest == "" || compiled.Explain() == "" {
		t.Fatal("compilation carries no digest or summary")
	}
	if err := compiled.Validate(); err != nil {
		t.Fatal(err)
	}

	// Governance cannot be bypassed: a recorded rejection compiles to
	// nothing.
	revision := baseScenario(t)
	deniedApproval := approval005(revision)
	deniedApproval.Decision = PlanRejected
	denied := mustApprove005(t, revision, deniedApproval, planInstant("2026-01-15T12:00:00Z"))
	_, err := CompilePlan(denied, CompileSpec{
		GovernanceRef: "governance:scenario-compile", GovernanceVersion: "v1",
		AllowedKeys: []string{"headcount.target"},
		WriteTargets: map[string][]string{
			"headcount.target": {"workforce.plan.headcount"},
		},
		SimulationRef: "simulation:run-7",
		MaxIntents:    8,
	})
	rejected, ok := AsCompilationRejected(err)
	if !ok {
		t.Fatalf("unapproved plan err = %v, want SCENARIO_006_REJECTED", err)
	}
	if rejected.Code != CompilationRejectedCode || rejected.Field != "plan" || rejected.State != "not-approved" || rejected.Version != Version() {
		t.Fatalf("rejected = %+v", rejected)
	}
	if !errors.Is(err, ErrCompilationRejected) {
		t.Fatal("rejection does not match ErrCompilationRejected with errors.Is")
	}

	// An ungoverned delta refuses instead of compiling past its authority.
	extra, err := revision.Fork(
		Assumption{Key: "bonus.target", Value: DecimalValue(values.MustDecimal("5", 0, values.RoundingHalfEven)), Unit: "HEAD", ProvenanceRefs: []string{"forecast:2026"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	extraApproval := approval005(revision)
	extraApproval.ExpectedDigest = extra.CanonicalDigest
	extraPlan := mustApprove005(t, extra, extraApproval, planInstant("2026-01-15T12:00:00Z"))
	_, err = CompilePlan(extraPlan, CompileSpec{
		GovernanceRef: "governance:scenario-compile", GovernanceVersion: "v1",
		AllowedKeys: []string{"headcount.target"},
		WriteTargets: map[string][]string{
			"headcount.target": {"workforce.plan.headcount"},
		},
		SimulationRef: "simulation:run-7",
		MaxIntents:    8,
	})
	if rejected, ok := AsCompilationRejected(err); !ok || rejected.Field != "deltas" || rejected.State != "ungoverned-key" {
		t.Fatalf("ungoverned delta err = %v, rejected = %+v", err, rejected)
	}

	// Conflicting deltas cannot be combined into one compilation.
	conflicted := spec006()
	conflicted.ConflictGroups = [][]string{{"headcount.target", "hiring.freeze"}}
	_, err = CompilePlan(plan, conflicted)
	if rejected, ok := AsCompilationRejected(err); !ok || rejected.Field != "deltas" || rejected.State != "conflicting-deltas" {
		t.Fatalf("conflicting deltas err = %v, rejected = %+v", err, rejected)
	}
}

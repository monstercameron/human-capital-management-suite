package replay

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func TestRecomputableAdmitsOnlyDeterministicPureSteps(t *testing.T) {
	for _, tc := range []struct {
		step   workflow.StepType
		effect capability.EffectClass
		want   bool
	}{
		{workflow.StepDecision, capability.EffectPure, true},
		{workflow.StepTransform, capability.EffectPure, true},
		{workflow.StepCapability, capability.EffectReadOnly, true},
		{workflow.StepCapability, capability.EffectInternalMutation, false},
		{workflow.StepCapability, capability.EffectIrreversibleExternalMutation, false},
		{workflow.StepObserve, capability.EffectReadOnly, false},
		{workflow.StepApproval, capability.EffectPure, false},
		{workflow.StepTask, capability.EffectPure, false},
		{workflow.StepWait, capability.EffectPure, false},
		{workflow.StepEnd, capability.EffectPure, false},
	} {
		got := Recomputable(workflow.CompiledNode{Type: tc.step, EffectClass: tc.effect})
		if got != tc.want {
			t.Errorf("%s/%s recomputable = %v, want %v", tc.step, tc.effect, got, tc.want)
		}
	}
}

func TestCandidatesOverlayDefaultsAndPreferNodeBindings(t *testing.T) {
	plan := promotionPlan(t)
	decision, _ := plan.Node(workflow.PromotionNodeRaiseThreshold)
	transform, _ := plan.Node(workflow.PromotionNodeBuildProposal)

	defaults := Candidates{}.merged()
	if _, ok := defaults.lookup(decision); !ok {
		t.Fatal("the default set does not recompute DECISION nodes")
	}
	if _, ok := defaults.lookup(transform); ok {
		t.Fatal("the default set recomputes a TRANSFORM it has no implementation for")
	}

	byNode := divergingCandidate{route: "node"}
	byType := divergingCandidate{route: "type"}
	merged := Candidates{
		ByNode:     map[string]Candidate{workflow.PromotionNodeRaiseThreshold: byNode},
		ByStepType: map[workflow.StepType]Candidate{workflow.StepDecision: byType, workflow.StepTransform: byType},
	}.merged()
	if err := merged.validate(plan); err != nil {
		t.Fatalf("valid bindings refused: %v", err)
	}
	if got, _ := merged.lookup(decision); got != byNode {
		t.Fatalf("a node binding did not win over the step-type binding: %#v", got)
	}
	if got, _ := merged.lookup(transform); got != byType {
		t.Fatalf("the TRANSFORM step-type binding was not used: %#v", got)
	}
	if _, isRules := DefaultCandidates().ByStepType[workflow.StepDecision].(RulesDecisionCandidate); !isRules {
		t.Fatal("overlaying a caller set replaced the package defaults")
	}
}

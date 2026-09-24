package time

import (
	"strings"
	"testing"
)

func TestReferenceDefinition_CompilesUnderP1A(t *testing.T) {
	registry, err := GoldenEnvironment().Registry()
	if err != nil {
		t.Fatalf("Registry: %v", err)
	}
	plan, err := Compile(registry)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if plan.WorkflowID != WorkflowID || plan.Version != Version {
		t.Fatalf("compiled identity = %s/%d, want %s/%d", plan.WorkflowID, plan.Version, WorkflowID, Version)
	}
}

func TestTodo_REV_041_02_DefinitionPinsExecutionGate(t *testing.T) {
	def := ReferenceDefinition()
	for _, obligation := range def.Obligations {
		if obligation.ID == ObligationAttestation {
			if !strings.Contains(obligation.SatisfactionCondition, "before the dependent effect") {
				t.Fatalf("attestation condition does not require pre-effect acceptance: %q", obligation.SatisfactionCondition)
			}
			return
		}
	}
	t.Fatal("attestation obligation missing")
}

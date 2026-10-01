package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type missingPunchGovernanceForTest struct{}

func (missingPunchGovernanceForTest) Evaluate(context.Context, MissingPunchGovernanceRequest) (runtime.GovernanceRefs, error) {
	return runtime.GovernanceRefs{AuthorizationDecisionID: "authz:1", PolicyRef: "policy/time/v1"}, nil
}

func TestTodo_TCLOCK011_MissingPunchEffects_RequireDurableDependencies(t *testing.T) {
	var effects *PostgresMissingPunchWorkflowEffects
	if !errors.Is(effects.configured(), errMissingPunchEffectsConfig) {
		t.Fatal("nil effects should fail closed")
	}
	if !errors.Is((&PostgresMissingPunchWorkflowEffects{Governance: missingPunchGovernanceForTest{}}).configured(), errMissingPunchEffectsConfig) {
		t.Fatal("missing store should fail closed")
	}
}

func TestTodo_TCLOCK011_MissingPunchEffects_RejectForgedGovernanceRefs(t *testing.T) {
	effects := &PostgresMissingPunchWorkflowEffects{Governance: missingPunchGovernanceForTest{}}
	if _, err := effects.refs(context.Background(), clockservice.MissingPunchWorkflowRequest{}, "validate_request"); !errors.Is(err, errMissingPunchEffectsConfig) {
		t.Fatalf("missing store must fail before governance: %v", err)
	}
}

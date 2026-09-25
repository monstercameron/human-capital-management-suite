package execution

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type registrationStepRunner struct {
	called bool
}

func (r *registrationStepRunner) Run(context.Context, execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	r.called = true
	return frontier.NodeOutcome{NodeID: "work_order_step", OutputDigest: "digest"}, runtime.GovernanceRefs{AuthorizationDecisionID: "decision"}, nil
}

func TestRegistrationStepHandlerAcceptsDomainNeutralRunner(t *testing.T) {
	runner := &registrationStepRunner{}
	request := execute.StepRequest{}
	handler := StepHandler{CapabilityID: "fieldwork.record_progress", Run: defaultStepRun}

	outcome, refs, err := handler.Run(context.Background(), runner, request)
	if err != nil {
		t.Fatalf("handler.Run: %v", err)
	}
	if !runner.called {
		t.Fatal("handler did not invoke the supplied execute.StepRunner")
	}
	if outcome.NodeID != "work_order_step" || outcome.OutputDigest != "digest" {
		t.Fatalf("outcome = %+v, want runner outcome", outcome)
	}
	if refs.AuthorizationDecisionID != "decision" {
		t.Fatalf("governance refs = %+v, want forwarded authorization decision", refs)
	}
}

func TestDefaultStepRunRejectsMissingRunner(t *testing.T) {
	_, _, err := defaultStepRun(context.Background(), nil, execute.StepRequest{Node: workflow.CompiledNode{ID: "missing_runner"}})
	if err == nil {
		t.Fatal("defaultStepRun with nil runner returned no error")
	}
}

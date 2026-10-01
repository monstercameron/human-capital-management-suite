package app

import (
	"context"
	"errors"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// AgentWorkflowTarget is resolved from an existing admitted intent through
// the owner's current authorization, simulation and approval contract.
type AgentWorkflowTarget struct {
	Selection     runtime.WorkflowSelection
	CorrelationID string
}

// AuthorizeAgentWorkflowObservation checks the current intent owner and the
// correlation the durable workflow pinned. Observing an execution does not
// re-submit its approval or require the intent's old start revision.
func (s *IntentService) AuthorizeAgentWorkflowObservation(ctx context.Context, intentID, correlationID string) error {
	if s == nil || intentID == "" || correlationID == "" {
		return errors.New("app: observed intent identity is required")
	}
	principal, _, ownedErr := caller(ctx)
	if ownedErr != nil {
		return ownedErr
	}
	instance, _, ownedErr := s.loadInstance(ctx, principal.Tenant().String(), intentID)
	if ownedErr != nil {
		return ownedErr
	}
	definition, err := s.defs.Resolve(instance.Definition)
	if err != nil {
		return err
	}
	if ownedErr = s.authorizeExecution(principal, definition); ownedErr != nil {
		return ownedErr
	}
	if instance.CorrelationID != correlationID {
		return errors.New("app: workflow does not belong to the admitted intent")
	}
	return nil
}

func (s *IntentService) ResolveAgentWorkflowTarget(ctx context.Context, req *intentsv1.ExecuteIntentRequest) (AgentWorkflowTarget, error) {
	if s == nil || req == nil {
		return AgentWorkflowTarget{}, errors.New("app: approved intent is required")
	}
	principal, inv, ownedErr := caller(ctx)
	if ownedErr != nil {
		return AgentWorkflowTarget{}, ownedErr
	}
	instance, record, ownedErr := s.loadInstance(ctx, principal.Tenant().String(), req.GetIntentId())
	if ownedErr != nil {
		return AgentWorkflowTarget{}, ownedErr
	}
	if req.GetExpectedInstanceVersion() == 0 || req.GetExpectedInstanceVersion() != record.InstanceVersion {
		return AgentWorkflowTarget{}, errors.New("app: admitted intent revision is not current")
	}
	definition, err := s.defs.Resolve(instance.Definition)
	if err != nil {
		return AgentWorkflowTarget{}, err
	}
	if ownedErr = s.authorizeExecution(principal, definition); ownedErr != nil {
		return AgentWorkflowTarget{}, ownedErr
	}
	simulated, ownedErr := s.simulateDetailed(ctx, principal, purposeOf(principal, inv), instance, definition)
	if ownedErr != nil {
		return AgentWorkflowTarget{}, ownedErr
	}
	if ownedErr = checkApproval(req.GetApproval(), simulated.Artifact); ownedErr != nil {
		return AgentWorkflowTarget{}, ownedErr
	}
	if simulated.Revision == nil || s.executionResolver == nil {
		return AgentWorkflowTarget{}, executionUnavailable()
	}
	start, ownedErr := s.executionStart(instance, simulated.Artifact, req.GetApproval().GetApprovalRef(), *simulated.Revision)
	if ownedErr != nil {
		return AgentWorkflowTarget{}, ownedErr
	}
	if pin := s.highPerformerPin(ctx, instance.Tenant, employmentSubject(instance.Subjects)); pin != "" {
		start.PinnedCompiledPlanDigest = pin
	}
	selection, err := s.executionResolver.ResolveWorkflow(ctx, start)
	if err != nil {
		return AgentWorkflowTarget{}, err
	}
	if selection.Plan == nil || selection.WorkflowID == "" {
		return AgentWorkflowTarget{}, executionUnavailable()
	}
	return AgentWorkflowTarget{Selection: selection, CorrelationID: instance.CorrelationID}, nil
}

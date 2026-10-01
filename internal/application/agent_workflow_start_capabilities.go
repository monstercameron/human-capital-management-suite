package application

import (
	"context"

	"github.com/google/uuid"
	agentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/agent/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

// RegisterAgentGovernedWorkflowCapabilities binds the typed start and observed
// state operations to their actual owner. The mutation still requires the
// sanctioned governing dispatcher and the exact execution authority amendment.
func RegisterAgentGovernedWorkflowCapabilities(registry *capability.Registry, service AgentGovernedWorkflowService) error {
	if registry == nil || service.Evidence == nil || service.Current == nil || service.Agents == nil || service.ResolveTenant == nil || service.Gate == nil || service.Context == nil || service.Intents == nil || service.Controls == nil || service.Versions == nil || service.Now == nil {
		return ErrAgentWorkflowStartDenied
	}
	start := capability.Definition{ID: "workflows.request_start", Version: 1, OwnerDomain: "workflows", RequestSchema: capability.SchemaRef{SchemaID: "agents.workflow_start.request", Version: 1, ProtobufFullName: "hcmnext.agent.v1.AgentWorkflowStartRequest"}, ResponseSchema: capability.SchemaRef{SchemaID: "agents.workflow_start.result", Version: 1, ProtobufFullName: "hcmnext.agent.v1.AgentWorkflowStartResult"}, ErrorSchema: capability.SchemaRef{SchemaID: "agents.error", Version: 1, ProtobufFullName: "hcmnext.agent.v1.AgentError"}, EffectClass: capability.EffectInternalMutation, WriteData: capability.DataDomainFieldSet{DataDomains: []string{"workflows", "intents"}}, RiskClass: "T3_APPROVED_WORKFLOW", IdempotencyPolicyRef: "intents.execute.proposal", AgentEligible: true, AuthZScopeRef: "workflows.request_start", LegalBasisRef: "intents.approved-purpose", EntitlementRef: "workflows.runtime", SLOClassRef: "workflows.execution", TestRef: "TestTodo_AGENT_034"}
	if err := registry.Register(start, func(ctx context.Context, payload any) (any, error) {
		wire, ok := payload.(*agentv1.AgentWorkflowStartRequest)
		if !ok {
			return nil, ErrAgentWorkflowStartDenied
		}
		request, err := agentWorkflowStartFromProto(wire)
		if err != nil {
			return nil, err
		}
		receipt, err := service.RequestStart(ctx, request)
		if err != nil {
			return nil, err
		}
		return &agentv1.AgentWorkflowStartResult{Execution: receipt}, nil
	}); err != nil {
		return err
	}
	status := capability.Definition{ID: "workflows.read_status", Version: 1, OwnerDomain: "workflows", RequestSchema: capability.SchemaRef{SchemaID: "agents.workflow_status.request", Version: 1, ProtobufFullName: "hcmnext.agent.v1.AgentWorkflowStatusRequest"}, ResponseSchema: capability.SchemaRef{SchemaID: "agents.workflow_status.result", Version: 1, ProtobufFullName: "hcmnext.agent.v1.AgentWorkflowObservedState"}, ErrorSchema: start.ErrorSchema, EffectClass: capability.EffectReadOnly, ReadData: capability.DataDomainFieldSet{DataDomains: []string{"workflows"}}, RiskClass: "BOUNDED_WORKFLOW_OBSERVATION", IdempotencyPolicyRef: "workflows.observed-version", AgentEligible: true, AuthZScopeRef: "workflows.read_status", LegalBasisRef: start.LegalBasisRef, EntitlementRef: start.EntitlementRef, SLOClassRef: "workflows.observation", TestRef: start.TestRef}
	return registry.Register(status, func(ctx context.Context, payload any) (any, error) {
		wire, ok := payload.(*agentv1.AgentWorkflowStatusRequest)
		if !ok || wire == nil {
			return nil, ErrAgentWorkflowStartDenied
		}
		request, err := agentWorkflowStartFromProto(wire.Authority)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(wire.InstanceId)
		if err != nil {
			return nil, ErrAgentWorkflowStartDenied
		}
		observed, err := service.Inspect(ctx, request, id)
		if err != nil {
			return nil, err
		}
		instance := observed.Instance
		return &agentv1.AgentWorkflowObservedState{InstanceId: instance.InstanceID.String(), WorkflowId: instance.WorkflowID, WorkflowVersion: instance.WorkflowVersion, CompiledPlanDigest: instance.CompiledPlanHash, RuntimeStatus: string(instance.RuntimeStatus), InstanceVersion: instance.InstanceVersion, CurrentNodeIds: append([]string(nil), instance.CurrentNodeIDs...)}, nil
	})
}

func agentWorkflowStartFromProto(wire *agentv1.AgentWorkflowStartRequest) (AgentWorkflowStartRequest, error) {
	if wire == nil || wire.Intent == nil || wire.Skill == nil || wire.Skill.Version == 0 || wire.Skill.Version > uint64(^uint32(0)) {
		return AgentWorkflowStartRequest{}, ErrAgentWorkflowStartDenied
	}
	return AgentWorkflowStartRequest{RunID: wire.RunId, Intent: wire.Intent, Skill: agentskills.SkillPin{ID: wire.Skill.Id, Version: uint32(wire.Skill.Version), Digest: wire.Skill.Digest}, StepID: wire.StepId}, nil
}

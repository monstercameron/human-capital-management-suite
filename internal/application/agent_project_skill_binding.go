package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

const agentProjectCurrentCapabilityID = "project.tasks.current"

var errAgentProjectSkillRegistration = errors.New("application: project skill registration unavailable")

// BindAgentProjectSkill publishes the read capability and exact skill pin.
// Draft and commit remain explicit application methods because their write
// path requires the proposal/reviewer contract and cannot be a generic tool.
func BindAgentProjectSkill(caps *capability.Registry, skills *agentskills.Registry, service *AgentProjectSkill) (agentskills.SkillPin, error) {
	if caps == nil || skills == nil || service == nil {
		return agentskills.SkillPin{}, errAgentProjectSkillRegistration
	}
	def := capability.Definition{ID: agentProjectCurrentCapabilityID, Version: 1, OwnerDomain: "project",
		RequestSchema:  capability.SchemaRef{SchemaID: "hcmnext.project.AgentProjectTaskRequest", Version: 1, ProtobufFullName: "google.protobuf.Struct"},
		ResponseSchema: capability.SchemaRef{SchemaID: "hcmnext.project.AgentProjectTaskResult", Version: 1, ProtobufFullName: "google.protobuf.Struct"},
		ErrorSchema:    capability.SchemaRef{SchemaID: "google.rpc.Status", Version: 1, ProtobufFullName: "google.rpc.Status"},
		EffectClass:    capability.EffectReadOnly, ReadData: capability.DataDomainFieldSet{DataDomains: []string{"ordinary_project_task"}},
		RiskClass: "LOW", IdempotencyPolicyRef: "idempotency.read-safe.v1", AgentEligible: true,
		AuthZScopeRef: "project:task:read", LegalBasisRef: "legal.project.execution.v1", EntitlementRef: "entitlement.project.v1", SLOClassRef: "slo.interactive.v1", TestRef: "test:TestTodo_AGENT_049"}
	if err := caps.Register(def, func(ctx context.Context, payload any) (any, error) {
		req, ok := payload.(AgentProjectTaskRequest)
		if !ok {
			return nil, errAgentProjectSkillRegistration
		}
		return service.CurrentTask(ctx, req)
	}); err != nil {
		return agentskills.SkillPin{}, fmt.Errorf("%w: capability: %v", errAgentProjectSkillRegistration, err)
	}
	skill := agentskills.SkillDefinition{ID: AgentProjectSkillID, Version: 1, Owner: "project",
		Description:  "Read an ordinary project task within the current agent grant and revision.",
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"project_id":{"type":"string","minLength":1},"task_id":{"type":"string","minLength":1}},"required":["project_id","task_id"],"additionalProperties":false}`),
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"task":{"type":"object"},"scope":{"type":"object"}},"required":["task","scope"],"additionalProperties":false}`),
		Operations:   []agentskills.OperationRef{{Kind: agentskills.OperationCapability, Capability: capability.Key{ID: agentProjectCurrentCapabilityID, Version: 1}}}, SideEffectTier: agentskills.TierRead,
		RequiredPurposes: []string{"project-agent"}, DataClassesRead: []string{"ORDINARY_PROJECT"}, IdempotencyRule: "read-only", CostClass: "LOW", EvalRefs: []string{"AGENT-049"}}
	if err := skills.Publish(skill); err != nil {
		return agentskills.SkillPin{}, fmt.Errorf("%w: skill: %v", errAgentProjectSkillRegistration, err)
	}
	pin, err := skills.Pin(skill.Key())
	if err != nil {
		return agentskills.SkillPin{}, fmt.Errorf("%w: pin: %v", errAgentProjectSkillRegistration, err)
	}
	return pin, nil
}

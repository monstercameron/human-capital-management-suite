package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	agentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/agent/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/capabilityrunner"
)

var ErrAgentWorkflowInput = errors.New("agent workflow: invalid invocation")

// AgentWorkflowAdmission is the common durable admission owner. Implementations
// persist accepted READY execution before returning and dispatch it separately.
type AgentWorkflowAdmission interface {
	Admit(context.Context, agentrun.Request) (agentrun.Record, bool, error)
}

// AgentWorkflowInvokeRequest pins the deterministic workflow occurrence and the
// bounded shared admission request. Current workflow authority verifies both.
type AgentWorkflowInvokeRequest struct {
	InstanceID uuid.UUID
	NodeID     string
	Admission  agentrun.Request
}

type AgentWorkflowInvokeResult struct {
	RunID       string
	Decision    agentrun.Decision
	RefusalCode string
	Replay      bool
}

type AgentWorkflowService struct{ Admission AgentWorkflowAdmission }

// Invoke starts no model work and holds no workflow lease after admission.
func (s AgentWorkflowService) Invoke(ctx context.Context, request AgentWorkflowInvokeRequest) (AgentWorkflowInvokeResult, error) {
	if s.Admission == nil || ctx == nil || request.InstanceID == uuid.Nil || strings.TrimSpace(request.NodeID) == "" ||
		request.NodeID != strings.TrimSpace(request.NodeID) || len(request.NodeID) > 128 || strings.Contains(request.NodeID, ":") {
		return AgentWorkflowInvokeResult{}, ErrAgentWorkflowInput
	}
	request.Admission.Source.Kind = agentrun.SourceWorkflow
	request.Admission.Source.Key = ""
	request.Admission.Source.Ref = "workflow:" + request.InstanceID.String() + ":" + request.NodeID
	source, err := (agentrun.CanonicalSourceConverter{}).ConvertSource(ctx, request.Admission)
	if err != nil {
		return AgentWorkflowInvokeResult{}, err
	}
	request.Admission.Source = source
	record, created, err := s.Admission.Admit(ctx, request.Admission)
	if err != nil {
		return AgentWorkflowInvokeResult{}, err
	}
	if err := agentrun.ValidateAdmissionRecord(record); err != nil || record.Request.Source.Ref != request.Admission.Source.Ref ||
		record.Request.Source.Kind != agentrun.SourceWorkflow || record.Request.Source.TenantID != request.Admission.Source.TenantID {
		return AgentWorkflowInvokeResult{}, fmt.Errorf("%w: admission returned unrelated evidence", ErrAgentWorkflowInput)
	}
	return AgentWorkflowInvokeResult{RunID: record.ID, Decision: record.Decision, RefusalCode: record.RefusalCode, Replay: !created}, nil
}

// RegisterAgentWorkflowCapabilities publishes the real asynchronous owner
// binding. Its mutation class must be admitted by the governing dispatcher.
func RegisterAgentWorkflowCapabilities(registry *capability.Registry, service AgentWorkflowService) error {
	if registry == nil || service.Admission == nil {
		return ErrAgentWorkflowInput
	}
	return registry.Register(capability.Definition{
		ID: "agents.invoke", Version: 1, OwnerDomain: "agents",
		RequestSchema:  capability.SchemaRef{SchemaID: "agents.invoke.request", Version: 1, ProtobufFullName: "hcmnext.agent.v1.WorkflowInvokeRequest"},
		ResponseSchema: capability.SchemaRef{SchemaID: "agents.invoke.result", Version: 1, ProtobufFullName: "hcmnext.agent.v1.WorkflowInvokeResult"},
		ErrorSchema:    capability.SchemaRef{SchemaID: "agents.error", Version: 1, ProtobufFullName: "hcmnext.agent.v1.AgentError"},
		EffectClass:    capability.EffectInternalMutation, RiskClass: "BOUNDED_ANALYSIS",
		WriteData:            capability.DataDomainFieldSet{DataDomains: []string{"agents"}},
		IdempotencyPolicyRef: "agents.workflow.occurrence", AuthZScopeRef: "agents.invoke", LegalBasisRef: "agents.admitted-purpose",
		EntitlementRef: "agents.runtime", SLOClassRef: "agents.async-admission", TestRef: "TestTodo_AGENT_033",
	}, func(ctx context.Context, payload any) (any, error) {
		if wire, ok := payload.(*agentv1.WorkflowInvokeRequest); ok {
			request, err := agentWorkflowInvokeFromProto(wire)
			if err != nil {
				return nil, err
			}
			result, err := service.Invoke(ctx, request)
			if err != nil {
				return nil, err
			}
			return &agentv1.WorkflowInvokeResult{RunId: result.RunID, Decision: string(result.Decision), RefusalCode: result.RefusalCode, Replay: result.Replay}, nil
		}
		if call, ok := payload.(capabilityrunner.CapabilityCall); ok {
			if call.Capability != (capability.Key{ID: "agents.invoke", Version: 1}) {
				return nil, ErrAgentWorkflowInput
			}
			instance, ok := call.Inputs["instance_id"]
			if !ok || instance.Type.Kind != workflow.KindString {
				return nil, ErrAgentWorkflowInput
			}
			input, ok := call.Inputs["request_json"]
			if !ok || input.Type.Kind != workflow.KindString {
				return nil, ErrAgentWorkflowInput
			}
			id, err := uuid.Parse(instance.Text)
			if err != nil {
				return nil, ErrAgentWorkflowInput
			}
			var admission agentrun.Request
			if agentWorkflowDecodeRequest(input.Text, &admission) != nil {
				return nil, ErrAgentWorkflowInput
			}
			result, err := service.Invoke(ctx, AgentWorkflowInvokeRequest{InstanceID: id, NodeID: call.NodeID, Admission: admission})
			if err != nil {
				return nil, err
			}
			return capabilityrunner.CapabilityAnswer{Outcome: workflow.OutcomeSucceeded, Outputs: map[string]capabilityrunner.ResolvedValue{
				"run_id":   {Type: workflow.ValueType{Kind: workflow.KindString}, Text: result.RunID},
				"decision": {Type: workflow.ValueType{Kind: workflow.KindString}, Text: string(result.Decision)},
			}}, nil
		}
		request, ok := payload.(AgentWorkflowInvokeRequest)
		if !ok {
			return nil, ErrAgentWorkflowInput
		}
		return service.Invoke(ctx, request)
	})
}

func agentWorkflowDecodeRequest(text string, request *agentrun.Request) error {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(request); err != nil {
		return ErrAgentWorkflowInput
	}
	if decoder.Decode(new(any)) != io.EOF {
		return ErrAgentWorkflowInput
	}
	return nil
}

func agentWorkflowInvokeFromProto(wire *agentv1.WorkflowInvokeRequest) (AgentWorkflowInvokeRequest, error) {
	if wire == nil || wire.Admission == nil || wire.Admission.Agent == nil || wire.Admission.Principal == nil || wire.Admission.Audience == nil || wire.Admission.ContextScope == nil || wire.Admission.Budget == nil || wire.Admission.Deadline == nil || wire.Admission.Deadline.CheckValid() != nil {
		return AgentWorkflowInvokeRequest{}, ErrAgentWorkflowInput
	}
	id, err := uuid.Parse(wire.InstanceId)
	if err != nil {
		return AgentWorkflowInvokeRequest{}, ErrAgentWorkflowInput
	}
	a := wire.Admission
	return AgentWorkflowInvokeRequest{InstanceID: id, NodeID: wire.NodeId, Admission: agentrun.Request{
		Source: agentrun.SourceIdentity{TenantID: a.TenantId}, LegalEntity: a.LegalEntityId,
		Agent: agentrun.VersionRef{AgentID: a.Agent.AgentId, Version: a.Agent.Version, Digest: a.Agent.Digest}, InstallationID: a.InstallationId,
		Principal: agentrun.PrincipalChain{Mode: agentrun.RunMode(a.Principal.Mode), AgentPrincipalID: a.Principal.AgentPrincipalId, SponsorID: a.Principal.SponsorId, InvokerID: a.Principal.InvokerId, DelegatedCredentialRef: a.Principal.DelegatedCredentialRef},
		Purpose:   a.Purpose, Audience: agentrun.AudienceScope{ID: a.Audience.Id, SnapshotID: a.Audience.SnapshotId, Digest: a.Audience.Digest}, Context: agentrun.ContextScope{ID: a.ContextScope.Id, SnapshotID: a.ContextScope.SnapshotId, Digest: a.ContextScope.Digest},
		Deadline: a.Deadline.AsTime().UTC(), Budget: agentrun.Budget{MaxCostMicros: a.Budget.MaxCostMicros, MaxInputTokens: a.Budget.MaxInputTokens, MaxOutputTokens: a.Budget.MaxOutputTokens}, CauseID: a.CauseId,
	}}, nil
}

package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	agentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/agent/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestTodo_AGENT_033(t *testing.T) {
	runtime, _, _, now := commonAgentTestRuntime(t)
	service := AgentWorkflowService{Admission: runtime}
	request := AgentWorkflowInvokeRequest{InstanceID: uuid.New(), NodeID: "analyze", Admission: commonAgentTestRequest(*now)}
	request.Admission.Source.Key = "forged-key"
	first, err := service.Invoke(context.Background(), request)
	if err != nil || first.RunID == "" || first.Decision != agentrun.DecisionAccepted || first.Replay {
		t.Fatalf("invoke=%+v err=%v", first, err)
	}
	record, err := runtime.GetAdmission(context.Background(), "common-tenant", first.RunID)
	if err != nil || record.Request.Source.Kind != agentrun.SourceWorkflow || record.Request.Source.Key == "forged-key" || record.Request.Source.Ref != "workflow:"+request.InstanceID.String()+":analyze" {
		t.Fatalf("source=%+v err=%v", record.Request.Source, err)
	}
	run, err := runtime.GetRun(context.Background(), "common-tenant", first.RunID)
	if err != nil || run.State != runstate.StateReady || run.Lease != nil {
		t.Fatalf("invoke retained model lease: %+v err=%v", run, err)
	}
	replay, err := service.Invoke(context.Background(), request)
	if err != nil || replay.RunID != first.RunID || !replay.Replay {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	registry := capability.NewRegistry()
	if err = RegisterAgentWorkflowCapabilities(registry, service); err != nil {
		t.Fatal(err)
	}
	registered, ok := registry.Lookup(capability.Key{ID: "agents.invoke", Version: 1})
	if !ok || registered.Definition.EffectClass != capability.EffectInternalMutation || registered.Definition.AgentEligible {
		t.Fatalf("registered definition=%+v", registered)
	}
}

func TestAgentWorkflowPinnedEdge(t *testing.T) {
	m, _ := manifestAdapterFixture(t)
	plan, err := workflow.DecodeCanonicalPlan([]byte(`{"workflow_id":"allowed-workflow","version":2}`))
	if err != nil {
		t.Fatal(err)
	}
	m.ToolCeiling = []agentmanifest.Reference{{ID: "workflows.request_start:allowed-workflow", Version: 2, SchemaVersion: 1, Digest: "sha256:" + plan.Digest()}}
	digest, err := m.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if !agentWorkflowEdgeAllowed(m, digest, "allowed-workflow", plan) {
		t.Fatal("exact immutable edge refused")
	}
	if agentWorkflowEdgeAllowed(m, digest, "other-workflow", plan) || agentWorkflowEdgeAllowed(m, "sha256:changed", "allowed-workflow", plan) || agentWorkflowEdgeAllowed(m, digest, "allowed-workflow", nil) {
		t.Fatal("unrelated edge admitted")
	}
	changed, err := workflow.DecodeCanonicalPlan([]byte(`{"workflow_id":"allowed-workflow","version":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if agentWorkflowEdgeAllowed(m, digest, "allowed-workflow", changed) {
		t.Fatal("unpinned workflow version admitted")
	}
}

func TestAgentWorkflowTypedBoundary(t *testing.T) {
	_, _, _, now := commonAgentTestRuntime(t)
	r := commonAgentTestRequest(*now)
	a := &agentv1.WorkflowAgentAdmission{TenantId: r.Source.TenantID, LegalEntityId: r.LegalEntity, Agent: &agentv1.WorkflowAgentVersion{AgentId: r.Agent.AgentID, Version: r.Agent.Version, Digest: r.Agent.Digest}, InstallationId: r.InstallationID,
		Principal: &agentv1.WorkflowAgentPrincipal{Mode: string(r.Principal.Mode), AgentPrincipalId: r.Principal.AgentPrincipalID, SponsorId: r.Principal.SponsorID}, Purpose: r.Purpose,
		Audience: &agentv1.WorkflowAgentScope{Id: r.Audience.ID, SnapshotId: r.Audience.SnapshotID, Digest: r.Audience.Digest}, ContextScope: &agentv1.WorkflowAgentScope{Id: r.Context.ID, SnapshotId: r.Context.SnapshotID, Digest: r.Context.Digest}, Deadline: timestamppb.New(r.Deadline), Budget: &agentv1.WorkflowAgentBudget{MaxCostMicros: r.Budget.MaxCostMicros, MaxInputTokens: r.Budget.MaxInputTokens, MaxOutputTokens: r.Budget.MaxOutputTokens}, CauseId: r.CauseID}
	id := uuid.New()
	converted, err := agentWorkflowInvokeFromProto(&agentv1.WorkflowInvokeRequest{InstanceId: id.String(), NodeId: "analyze", Admission: a})
	if err != nil || converted.InstanceID != id || converted.Admission.Context != r.Context || converted.Admission.Budget != r.Budget {
		t.Fatalf("typed request=%+v err=%v", converted, err)
	}
	if _, err := agentWorkflowInvokeFromProto(nil); !errors.Is(err, ErrAgentWorkflowInput) {
		t.Fatalf("nil typed request=%v", err)
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var decoded agentrun.Request
	if err := agentWorkflowDecodeRequest(string(encoded), &decoded); err != nil {
		t.Fatal(err)
	}
	if err := agentWorkflowDecodeRequest(string(encoded)+` {"unexpected":true}`, &decoded); err == nil {
		t.Fatal("multiple request values accepted")
	}
	if err := agentWorkflowDecodeRequest(`{"unexpected":true}`, &decoded); err == nil {
		t.Fatal("unknown authority fields accepted")
	}
}

func TestTodo_AGENT_033_InvokeFault(t *testing.T) {
	runtime, _, authority, now := commonAgentTestRuntime(t)
	service := AgentWorkflowService{Admission: runtime}
	request := AgentWorkflowInvokeRequest{InstanceID: uuid.New(), NodeID: "analyze", Admission: commonAgentTestRequest(*now)}
	if _, err := (AgentWorkflowService{}).Invoke(context.Background(), request); !errors.Is(err, ErrAgentWorkflowInput) {
		t.Fatalf("missing owner=%v", err)
	}
	invalid := request
	invalid.NodeID = "foreign:node"
	if _, err := service.Invoke(context.Background(), invalid); !errors.Is(err, ErrAgentWorkflowInput) {
		t.Fatalf("malformed node=%v", err)
	}
	authority.revoked = true
	result, err := service.Invoke(context.Background(), request)
	if err != nil || result.Decision != agentrun.DecisionRefused || result.RefusalCode != "SPONSOR_REVOKED" {
		t.Fatalf("durable refusal=%+v err=%v", result, err)
	}
	if _, err := runtime.GetRun(context.Background(), "common-tenant", result.RunID); !errors.Is(err, runstate.ErrNotFound) {
		t.Fatalf("refused run started execution: %v", err)
	}
	if err := RegisterAgentWorkflowCapabilities(nil, service); !errors.Is(err, ErrAgentWorkflowInput) {
		t.Fatalf("nil registry=%v", err)
	}
}

func TestTodo_AGENT_033_SourceAuthorityFault(t *testing.T) {
	if err := (AgentWorkflowSourceAuthority{}).CheckRequest(context.Background(), agentrun.Request{}); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
		t.Fatalf("unconfigured source authority=%v", err)
	}
}

func TestTodo_AGENT_034_Fault(t *testing.T) {
	service := AgentGovernedWorkflowService{}
	if _, err := service.RequestStart(context.Background(), AgentWorkflowStartRequest{}); !errors.Is(err, ErrAgentWorkflowStartDenied) {
		t.Fatalf("unconfigured start=%v", err)
	}
	if _, err := service.Inspect(context.Background(), AgentWorkflowStartRequest{}, uuid.New()); !errors.Is(err, ErrAgentWorkflowStartDenied) {
		t.Fatalf("unconfigured inspect=%v", err)
	}
	if err := RegisterAgentGovernedWorkflowCapabilities(capability.NewRegistry(), service); !errors.Is(err, ErrAgentWorkflowStartDenied) {
		t.Fatalf("unconfigured start registry=%v", err)
	}
	if _, err := agentWorkflowStartFromProto(nil); !errors.Is(err, ErrAgentWorkflowStartDenied) {
		t.Fatalf("missing typed authority=%v", err)
	}
}

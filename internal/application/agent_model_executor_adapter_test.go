package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

func TestTodo_AGENT_019_020_024_ExecutorAdapterRequiresGateway(t *testing.T) {
	if _, err := NewAgentModelExecutorAdapter(nil); !errors.Is(err, ErrAgentModelExecutorNotConfigured) {
		t.Fatalf("nil gateway error = %v, want ErrAgentModelExecutorNotConfigured", err)
	}
	var adapter *AgentModelExecutorAdapter
	if _, err := adapter.Execute(context.Background(), AgentModelExecutorRequest{}); !errors.Is(err, ErrAgentModelExecutorNotConfigured) {
		t.Fatalf("nil adapter error = %v, want ErrAgentModelExecutorNotConfigured", err)
	}
}

func TestTodo_AGENT_019_020_024_ExecutorAdapterRejectsUntrustedPins(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*AgentModelExecutorRequest)
	}{
		{name: "untrusted task", mutate: func(req *AgentModelExecutorRequest) {
			req.Task = TrustedModelTask{TenantID: "tenant", TaskID: "task", AgentID: "agent", Workload: "worker"}
		}},
		{name: "missing tenant", mutate: func(req *AgentModelExecutorRequest) { req.Task.TenantID = "" }},
		{name: "missing model budget", mutate: func(req *AgentModelExecutorRequest) { req.Model.Limits.MaxCostMicros = 0 }},
		{name: "missing route pin", mutate: func(req *AgentModelExecutorRequest) { req.Route.Pin.AgentVersionDigest = "" }},
		{name: "tenant mismatch", mutate: func(req *AgentModelExecutorRequest) { req.Outbound.Tenant = "foreign" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := executorAdapterRequest(t)
			tc.mutate(&req)
			if err := validateExecutorRequest(req); !errors.Is(err, ErrAgentModelExecutorBinding) {
				t.Fatalf("validateExecutorRequest() = %v, want ErrAgentModelExecutorBinding", err)
			}
		})
	}
}

func TestTodo_AGENT_019_020_024_ExecutorAdapterBindsUniqueStepWithinDurableTask(t *testing.T) {
	req := executorAdapterRequest(t)
	req.StepID = "persona-model-step-2"
	req.Route.TraceID = req.StepID
	req.Model.TraceID = req.StepID
	if err := validateExecutorRequest(req); err != nil {
		t.Fatalf("validateExecutorRequest() rejected trusted continuation: %v", err)
	}
	if req.Outbound.TaskID != req.Task.TaskID || req.Task.TaskID != "task-1" {
		t.Fatalf("continuation escaped durable task: task=%q outbound=%q", req.Task.TaskID, req.Outbound.TaskID)
	}
	req.Route.TraceID = req.Task.TaskID
	if err := validateExecutorRequest(req); !errors.Is(err, ErrAgentModelExecutorBinding) {
		t.Fatalf("mismatched route step error=%v, want binding refusal", err)
	}
}

func executorAdapterRequest(t *testing.T) AgentModelExecutorRequest {
	t.Helper()
	task, err := NewTrustedModelTask("tenant-1", "task-1", "agent-1", "workload-1")
	if err != nil {
		t.Fatal(err)
	}
	return AgentModelExecutorRequest{
		Task:   task,
		StepID: "task-1",
		Lease:  lease.CredentialLease{Tenant: "tenant-1", Purpose: "agent.inference", Destination: "model-profile"},
		Route: agentmodel.RouteRequest{
			TraceID: "task-1",
			Pin:     agentmodel.ModelPin{AgentVersionDigest: "agent-1", TaskProfileID: "profile-1", Primary: agentmodel.ModelSelection{ProfileID: "model-profile"}},
			Task:    agentmodel.TaskProfile{ID: "profile-1", AgentVersionDigest: "agent-1", MaxCostMicros: 1000},
		},
		Model:    agentmodel.ModelRequest{TraceID: "task-1", TaskProfile: "profile-1", ModelProfile: "model-profile", Limits: agentmodel.ModelLimits{MaxCostMicros: 1000}},
		Outbound: agentegress.OutboundRequest{TaskID: "task-1", Tenant: "tenant-1", Purpose: "agent.inference", Region: "us-east", Profile: agentegress.Profile{ID: "model-profile", Kind: agentegress.TargetModel}},
	}
}

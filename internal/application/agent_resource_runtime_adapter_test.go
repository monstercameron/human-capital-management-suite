package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/resources"
)

func TestTodo_AGENT_038_RuntimeDurableIdentity(t *testing.T) {
	runtime, stores, _, now := commonAgentTestRuntime(t)
	ctx := context.Background()
	request := commonAgentTestRequest(*now)
	admitted, _, err := runtime.Admit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	resolve := CommonAgentResourceIdentityResolver(runtime)
	if _, err := resolve(ctx, request.Source.TenantID, admitted.ID); !errors.Is(err, ErrAgentResourceIdentity) {
		t.Fatalf("unclaimed run admitted = %v", err)
	}
	run, err := runtime.Claim(ctx, request.Source.TenantID, admitted.ID, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := resolve(ctx, request.Source.TenantID, admitted.ID)
	if err != nil || identity.TenantID != request.Source.TenantID || identity.UserID != request.Principal.SponsorID || identity.TaskID != admitted.ID || identity.Lane != resources.LaneAutonomous {
		t.Fatalf("durable identity = %+v %v", identity, err)
	}
	if _, err := resolve(ctx, "foreign", admitted.ID); err == nil {
		t.Fatal("cross tenant identity resolved")
	}
	run.CancelRequested = true
	run.Version++
	if err := stores.state.Save(ctx, run, run.Version-1); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve(ctx, request.Source.TenantID, admitted.ID); !errors.Is(err, ErrAgentResourceIdentity) {
		t.Fatalf("cancelled run admitted = %v", err)
	}
	if _, err := CommonAgentResourceIdentityResolver(nil)(ctx, "tenant", "task"); !errors.Is(err, ErrAgentResourceIdentity) {
		t.Fatalf("nil runtime = %v", err)
	}
}

func TestTodo_AGENT_038_RuntimeObservation(t *testing.T) {
	var observations []AgentResourceObservation
	runtime, err := NewAgentResourceRuntime(AgentResourceRuntimeConfig{Policy: agentResourceTestPolicy(), ObserveAdmission: func(observation AgentResourceObservation) { observations = append(observations, observation) }})
	if err != nil {
		t.Fatal(err)
	}
	identity := AgentResourceIdentity{TenantID: "tenant", UserID: "user", TaskID: "task", Lane: resources.LaneAutonomous}
	lease, err := runtime.Acquire(context.Background(), identity, "openai")
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	if err := runtime.ApplyPressure(resources.PressureShed); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Acquire(context.Background(), identity, "openai"); !errors.Is(err, resources.ErrPressureShed) {
		t.Fatal(err)
	}
	if len(observations) != 2 || observations[0].Identity != identity || observations[0].ProviderID != "openai" || observations[0].Outcome != "ADMITTED" || observations[0].PoolWait <= 0 || observations[1].Outcome != "SHED" {
		t.Fatalf("actual observations = %+v", observations)
	}
}

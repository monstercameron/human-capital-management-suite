package application

import (
	"context"
	"errors"
	"sync"
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
	var mu sync.Mutex
	var observations []AgentResourceObservation
	runtime, err := NewAgentResourceRuntime(AgentResourceRuntimeConfig{Policy: agentResourceTestPolicy(), ObserveAdmission: func(observation AgentResourceObservation) {
		mu.Lock()
		observations = append(observations, observation)
		mu.Unlock()
	}})
	if err != nil {
		t.Fatal(err)
	}
	identity := AgentResourceIdentity{TenantID: "tenant", UserID: "user", TaskID: "task", Lane: resources.LaneAutonomous}
	holder, err := runtime.Acquire(context.Background(), identity, "openai")
	if err != nil {
		t.Fatal(err)
	}
	// An admission that did not wait may measure zero on a coarse clock, so the
	// measured wait is proven with a real one: the task's one slot is held while
	// a second admission for the same task queues behind it.
	const held = 60 * time.Millisecond
	type admission struct {
		lease *AgentResourceLease
		err   error
	}
	waited := make(chan admission, 1)
	go func() {
		lease, err := runtime.Acquire(context.Background(), identity, "openai")
		waited <- admission{lease, err}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for runtime.SnapshotForTenant(identity.TenantID).Queued != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := runtime.SnapshotForTenant(identity.TenantID); got.Queued != 1 {
		t.Fatalf("second admission did not queue behind the held task slot: %+v", got)
	}
	time.Sleep(held)
	holder.Release()
	second := <-waited
	if second.err != nil {
		t.Fatal(second.err)
	}
	if second.lease.PoolWait < held/2 {
		t.Fatalf("lease pool wait = %s after waiting %s for the slot", second.lease.PoolWait, held)
	}
	second.lease.Release()
	if err := runtime.ApplyPressure(resources.PressureShed); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Acquire(context.Background(), identity, "openai"); !errors.Is(err, resources.ErrPressureShed) {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(observations) != 3 || observations[0].Identity != identity || observations[0].ProviderID != "openai" || observations[0].Outcome != "ADMITTED" || observations[0].PoolWait < 0 ||
		observations[1].Identity != identity || observations[1].Outcome != "ADMITTED" || observations[1].PoolWait < held/2 || observations[2].Outcome != "SHED" {
		t.Fatalf("actual observations = %+v", observations)
	}
}

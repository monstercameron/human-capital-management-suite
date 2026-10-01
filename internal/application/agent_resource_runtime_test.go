package application

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/resources"
)

func agentResourceTestPolicy() resources.Policy {
	return resources.Policy{CellID: "cell-a", MaxConcurrent: 4, MaxConcurrentPerTenant: 2, MaxConcurrentPerUser: 1, MaxConcurrentPerTask: 1,
		MaxQueued: 64, MaxQueuedPerTenant: 32, MaxQueuedPerUser: 16, MaxQueuedPerTask: 2, ReservedInteractive: 1,
		LaneConcurrency: map[resources.Lane]int{resources.LaneInteractive: 3, resources.LaneAutonomous: 3}, ProviderConcurrency: map[string]int{"openai": 2, "other": 1}}
}

func agentResourceTestRuntime(t *testing.T, resolver func(context.Context, string, string) (AgentResourceIdentity, error)) *AgentResourceRuntime {
	t.Helper()
	r, err := NewAgentResourceRuntime(AgentResourceRuntimeConfig{Policy: agentResourceTestPolicy(), ResolveIdentity: resolver})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestTodo_AGENT_038_Runtime(t *testing.T) {
	identity := AgentResourceIdentity{TenantID: "a", UserID: "user", TaskID: "task", Lane: resources.LaneInteractive}
	r := agentResourceTestRuntime(t, func(context.Context, string, string) (AgentResourceIdentity, error) { return identity, nil })
	request := AgentModelResourceRequest{TenantID: "a", UserID: "user", TaskID: "task", ProviderID: "openai", ModelProfileID: "profile"}
	for _, field := range []string{"tenant", "user", "task"} {
		forged := request
		switch field {
		case "tenant":
			forged.TenantID = "foreign"
		case "user":
			forged.UserID = "foreign"
		case "task":
			forged.TaskID = "foreign"
		}
		if release, err := r.AcquireAgentModelResources(context.Background(), forged); !errors.Is(err, ErrAgentResourceIdentity) || release != nil {
			t.Fatalf("%s forgery: %v", field, err)
		}
	}
	request.ProviderID = "unknown"
	if _, err := r.AcquireAgentModelResources(context.Background(), request); !errors.Is(err, resources.ErrUnknownProvider) {
		t.Fatalf("unknown provider = %v", err)
	}
	if got := r.SnapshotForTenant("a"); got.Active != 0 || got.Queued != 0 {
		t.Fatalf("failed provider reservation leaked work: %+v", got)
	}
	request.ProviderID = "openai"
	release, err := r.AcquireAgentModelResources(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.ProviderSnapshotForTenant("a"); got.Active != 1 || got.ByProvider["openai"] != 1 {
		t.Fatalf("selected provider not reserved: %+v", got)
	}
	release()
	release()
	if got := r.SnapshotForTenant("a"); got.Active != 0 || got.Queued != 0 {
		t.Fatalf("release leaked: %+v", got)
	}
	unbound := agentResourceTestRuntime(t, nil)
	if _, err := unbound.AcquireAgentModelResources(context.Background(), request); !errors.Is(err, ErrAgentResourceIdentity) {
		t.Fatalf("missing durable resolver = %v", err)
	}
}

func TestTodo_AGENT_038_RuntimeTaskStep(t *testing.T) {
	r := agentResourceTestRuntime(t, nil)
	adapter := AgentTaskStepAdmission{Runtime: r}
	ctx, release, err := adapter.AcquireTaskStep(context.Background(), agentsystem.TaskWorkIdentity{TenantID: "a", UserID: "user", TaskID: "task", Mode: agentsystem.ModeOnBehalfOf})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	modelRelease, err := r.AcquireAgentModelResources(ctx, AgentModelResourceRequest{TenantID: "a", UserID: "user", TaskID: "task", ProviderID: "openai"})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.SnapshotForTenant("a"); got.Active != 1 || got.ByLane[resources.LaneAutonomous] != 1 {
		t.Fatalf("nested model reserved duplicate worker: %+v", got)
	}
	modelRelease()
	if got := r.SnapshotForTenant("a"); got.Active != 1 {
		t.Fatalf("nested model released outer worker: %+v", got)
	}
	release()
	if _, err := r.AcquireAgentModelResources(ctx, AgentModelResourceRequest{TenantID: "a", UserID: "user", TaskID: "task", ProviderID: "openai"}); !errors.Is(err, ErrAgentResourceIdentity) {
		t.Fatalf("released context reused: %v", err)
	}
	if _, _, err := adapter.AcquireTaskStep(context.Background(), agentsystem.TaskWorkIdentity{TenantID: "a", UserID: "user", TaskID: "task", Mode: "forged"}); !errors.Is(err, ErrAgentResourceIdentity) {
		t.Fatalf("forged task mode = %v", err)
	}
	providerAdapter := AgentTaskStepAdmission{Runtime: r, ProviderID: "openai"}
	modelContext, outerRelease, err := providerAdapter.AcquireTaskStep(context.Background(), agentsystem.TaskWorkIdentity{TenantID: "a", UserID: "user", TaskID: "model-task", Mode: agentsystem.ModeOnBehalfOf, StepType: agentrun.StepAnalyze})
	if err != nil {
		t.Fatal(err)
	}
	defer outerRelease()
	innerRelease, err := r.AcquireAgentModelResources(modelContext, AgentModelResourceRequest{TenantID: "a", UserID: "user", TaskID: "model-task", ProviderID: "openai"})
	if err != nil {
		t.Fatal(err)
	}
	innerRelease()
	if got := r.ProviderSnapshotForTenant("a"); got.Active != 1 {
		t.Fatalf("nested model released or duplicated outer provider: %+v", got)
	}
	if _, err := r.AcquireAgentModelResources(modelContext, AgentModelResourceRequest{TenantID: "a", UserID: "user", TaskID: "model-task", ProviderID: "other"}); !errors.Is(err, ErrAgentResourceIdentity) {
		t.Fatalf("mismatched nested provider = %v", err)
	}
	canceled, cancel := context.WithCancel(modelContext)
	cancel()
	if _, err := r.AcquireAgentModelResources(canceled, AgentModelResourceRequest{TenantID: "a", UserID: "user", TaskID: "model-task", ProviderID: "openai"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled inherited context = %v", err)
	}
	outerRelease()
}

func TestTodo_AGENT_038_RuntimeProviderCancellation(t *testing.T) {
	r := agentResourceTestRuntime(t, nil)
	var held []*AgentResourceLease
	for i := 0; i < 2; i++ {
		lease, err := r.Acquire(context.Background(), AgentResourceIdentity{TenantID: fmt.Sprintf("held-%d", i), UserID: "user", TaskID: "task", Lane: resources.LaneInteractive}, "openai")
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, lease)
		defer lease.Release()
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		lease, err := r.Acquire(ctx, AgentResourceIdentity{TenantID: "waiting", UserID: "user", TaskID: "task", Lane: resources.LaneInteractive}, "openai")
		if lease != nil {
			lease.Release()
		}
		result <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for r.ProviderSnapshotForTenant("waiting").Queued != 1 && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if got := r.ProviderSnapshotForTenant("waiting"); got.Queued != 1 {
		cancel()
		t.Fatalf("provider queue = %+v", got)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled provider wait = %v", err)
	}
	if got := r.SnapshotForTenant("waiting"); got.Active != 0 || got.Queued != 0 {
		t.Fatalf("canceled provider wait leaked worker: %+v", got)
	}
	if got := r.ProviderSnapshotForTenant("waiting"); got.Active != 0 || got.Queued != 0 {
		t.Fatalf("canceled provider wait leaked queue: %+v", got)
	}
}

func TestTodo_AGENT_038_Runtime_Race(t *testing.T) {
	r := agentResourceTestRuntime(t, nil)
	var active atomic.Int32
	var done sync.WaitGroup
	start := make(chan struct{})
	errorsCh := make(chan error, 24)
	for i := 0; i < 24; i++ {
		done.Add(1)
		go func(index int) {
			defer done.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			identity := AgentResourceIdentity{TenantID: fmt.Sprintf("tenant-%d", index%3), UserID: fmt.Sprintf("user-%d", index), TaskID: fmt.Sprintf("task-%d", index), Lane: resources.LaneInteractive}
			lease, err := r.Acquire(ctx, identity, "openai")
			if err != nil {
				errorsCh <- err
				return
			}
			if n := active.Add(1); n > 2 {
				errorsCh <- fmt.Errorf("provider exceeded quota: %d", n)
			}
			if got := r.SnapshotForTenant(identity.TenantID); got.Active > 2 {
				errorsCh <- fmt.Errorf("tenant exceeded quota: %+v", got)
			}
			if lease.PoolWait <= 0 {
				errorsCh <- errors.New("pool wait was not measured")
			}
			runtime.Gosched()
			active.Add(-1)
			lease.Release()
		}(i)
	}
	close(start)
	done.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Error(err)
	}
	for i := 0; i < 3; i++ {
		tenant := fmt.Sprintf("tenant-%d", i)
		if got := r.SnapshotForTenant(tenant); got.Active != 0 || got.Queued != 0 {
			t.Errorf("worker leaked: %+v", got)
		}
		if got := r.ProviderSnapshotForTenant(tenant); got.Active != 0 || got.Queued != 0 {
			t.Errorf("provider leaked: %+v", got)
		}
	}
}

func TestTodo_AGENT_038_RuntimePressure(t *testing.T) {
	r := agentResourceTestRuntime(t, nil)
	if err := r.ApplyPressure(resources.PressureShed); err != nil {
		t.Fatal(err)
	}
	id := AgentResourceIdentity{TenantID: "a", UserID: "user", TaskID: "task", Lane: resources.LaneAutonomous}
	if _, err := r.Acquire(context.Background(), id, "openai"); !errors.Is(err, resources.ErrPressureShed) {
		t.Fatalf("background pressure = %v", err)
	}
	id.Lane = resources.LaneInteractive
	lease, err := r.Acquire(context.Background(), id, "openai")
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	if err := r.ApplyPressure(resources.PressureNormal); err != nil {
		t.Fatal(err)
	}
	id.Lane = resources.LaneAutonomous
	lease, err = r.Acquire(context.Background(), id, "openai")
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	if _, err := NewAgentResourceRuntime(AgentResourceRuntimeConfig{}); !errors.Is(err, resources.ErrInvalid) {
		t.Fatalf("missing capacity = %v", err)
	}
}

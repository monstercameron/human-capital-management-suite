package resources

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
)

func TestTodo_AGENT_038(t *testing.T) {
	c := newCoordinator(t, 2, map[Lane]int{LaneInteractive: 1, LaneAutonomous: 1}, map[string]int{"model-a": 1, "model-b": 1})
	interactive, err := c.Acquire(context.Background(), request("tenant-a", LaneInteractive, "model-a"))
	if err != nil {
		t.Fatal(err)
	}
	defer interactive.Release()
	queued := startAcquire(t, c, context.Background(), request("tenant-b", LaneInteractive, "model-a"))
	waitQueued(t, c, "tenant-b", 1)

	background, err := c.Acquire(context.Background(), request("tenant-c", LaneAutonomous, "model-b"))
	if err != nil {
		t.Fatalf("isolated autonomous lane was starved by interactive lane: %v", err)
	}
	background.Release()
	if got := c.SnapshotForTenant("tenant-b"); got.Queued != 1 || got.Active != 0 {
		t.Fatalf("queued tenant snapshot = %+v", got)
	}
	interactive.Release()
	if got := receiveAcquire(t, queued); got.err != nil {
		t.Fatalf("queued acquisition failed: %v", got.err)
	} else {
		got.lease.Release()
	}
	if snapshot := c.SnapshotForTenant("tenant-b"); snapshot.Active != 0 || snapshot.Queued != 0 {
		t.Fatalf("tenant capacity leaked after release: %+v", snapshot)
	}

	fair := newCoordinator(t, 1, map[Lane]int{LaneInteractive: 1}, nil)
	blocker, err := fair.Acquire(context.Background(), request("tenant-blocker", LaneInteractive, ""))
	if err != nil {
		t.Fatal(err)
	}
	firstNoisy := startAcquire(t, fair, context.Background(), request("tenant-noisy", LaneInteractive, ""))
	waitQueued(t, fair, "tenant-noisy", 1)
	quiet := startAcquire(t, fair, context.Background(), request("tenant-quiet", LaneInteractive, ""))
	waitQueued(t, fair, "tenant-quiet", 1)
	secondNoisy := startAcquire(t, fair, context.Background(), request("tenant-noisy", LaneInteractive, ""))
	waitQueued(t, fair, "tenant-noisy", 2)
	blocker.Release()
	noisyResult := receiveAcquire(t, firstNoisy)
	if noisyResult.err != nil {
		t.Fatal(noisyResult.err)
	}
	noisyResult.lease.Release()
	quietResult := receiveAcquire(t, quiet)
	if quietResult.err != nil {
		t.Fatalf("quiet tenant starved behind a burst from another tenant: %v", quietResult.err)
	}
	quietResult.lease.Release()
	lastNoisy := receiveAcquire(t, secondNoisy)
	if lastNoisy.err != nil {
		t.Fatal(lastNoisy.err)
	}
	lastNoisy.lease.Release()

	providerPool := newCoordinator(t, 3, map[Lane]int{LaneInteractive: 3}, map[string]int{"model-a": 1, "model-b": 1})
	providerA, err := providerPool.Acquire(context.Background(), request("tenant-provider-a", LaneInteractive, "model-a"))
	if err != nil {
		t.Fatal(err)
	}
	providerWaiter := startAcquire(t, providerPool, context.Background(), request("tenant-provider-b", LaneInteractive, "model-a"))
	waitQueued(t, providerPool, "tenant-provider-b", 1)
	otherProvider, err := providerPool.Acquire(context.Background(), request("tenant-provider-c", LaneInteractive, "model-b"))
	if err != nil {
		t.Fatalf("one provider quota blocked an unrelated provider: %v", err)
	}
	otherProvider.Release()
	providerA.Release()
	providerResult := receiveAcquire(t, providerWaiter)
	if providerResult.err != nil {
		t.Fatalf("released provider quota did not admit its waiter: %v", providerResult.err)
	}
	providerResult.lease.Release()
}

func TestTodo_AGENT_038_Fault(t *testing.T) {
	c := newCoordinator(t, 1, map[Lane]int{LaneInteractive: 1}, map[string]int{"model-a": 1})
	first, err := c.Acquire(context.Background(), request("tenant-a", LaneInteractive, "model-a"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	queued := startAcquire(t, c, ctx, request("tenant-b", LaneInteractive, "model-a"))
	waitQueued(t, c, "tenant-b", 1)
	cancel()
	if result := receiveAcquire(t, queued); !errors.Is(result.err, context.Canceled) || result.lease != nil {
		t.Fatalf("canceled acquisition = %+v, want canceled without a lease", result)
	}
	if snapshot := c.SnapshotForTenant("tenant-b"); snapshot.Queued != 0 || snapshot.Active != 0 {
		t.Fatalf("canceled waiter remained in the pool: %+v", snapshot)
	}
	first.Release()
	second, err := c.Acquire(context.Background(), request("tenant-c", LaneInteractive, "model-a"))
	if err != nil {
		t.Fatalf("canceled waiter leaked admission capacity: %v", err)
	}
	second.Release()
	second.Release()

	bounded, err := New(Policy{
		CellID: "cell-a", MaxConcurrent: 1, MaxConcurrentPerTenant: 1,
		MaxQueued: 1, MaxQueuedPerTenant: 1,
		LaneConcurrency: map[Lane]int{LaneInteractive: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	occupied, err := bounded.Acquire(context.Background(), request("tenant-held", LaneInteractive, ""))
	if err != nil {
		t.Fatal(err)
	}
	waiting := startAcquire(t, bounded, context.Background(), request("tenant-waiting", LaneInteractive, ""))
	waitQueued(t, bounded, "tenant-waiting", 1)
	if _, err := bounded.Acquire(context.Background(), request("tenant-shed", LaneInteractive, "")); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("overload error = %v, want bounded shedding", err)
	}
	occupied.Release()
	waitingResult := receiveAcquire(t, waiting)
	if waitingResult.err != nil {
		t.Fatal(waitingResult.err)
	}
	waitingResult.lease.Release()
}

func TestTodo_AGENT_038_Race(t *testing.T) {
	const workers = 24
	c := newCoordinator(t, 4, map[Lane]int{LaneInteractive: 3, LaneAutonomous: 1}, map[string]int{"model-a": 2})
	start := make(chan struct{})
	var ready sync.WaitGroup
	var done sync.WaitGroup
	ready.Add(workers)
	done.Add(workers)
	errCh := make(chan error, workers)
	for index := 0; index < workers; index++ {
		go func(index int) {
			defer done.Done()
			ready.Done()
			<-start
			lane := LaneInteractive
			if index%4 == 0 {
				lane = LaneAutonomous
			}
			lease, err := c.Acquire(context.Background(), request(fmt.Sprintf("tenant-%d", index%7), lane, "model-a"))
			if err != nil {
				errCh <- err
				return
			}
			snapshot := c.SnapshotForTenant(fmt.Sprintf("tenant-%d", index%7))
			if snapshot.Active < 1 || snapshot.Active > 2 {
				errCh <- fmt.Errorf("tenant active count = %d", snapshot.Active)
			}
			lease.Release()
		}(index)
	}
	ready.Wait()
	close(start)
	done.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent acquire failed: %v", err)
	}
	for _, tenant := range []string{"tenant-0", "tenant-1", "tenant-2", "tenant-3", "tenant-4", "tenant-5", "tenant-6"} {
		if snapshot := c.SnapshotForTenant(tenant); snapshot.Active != 0 || snapshot.Queued != 0 {
			t.Errorf("capacity leaked for %s: %+v", tenant, snapshot)
		}
	}
}

func TestTodo_AGENT_038_Integration(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) }
	ledger, err := agentbudget.NewWithClock(agentbudget.Policy{
		TaskDefault:   agentbudget.Limits{Steps: 5, Tokens: 100, WallClock: time.Minute, SpendMicros: 1000},
		UserDaily:     agentbudget.Limits{Steps: 10, Tokens: 500, WallClock: 5 * time.Minute, SpendMicros: 5000},
		TenantMonthly: agentbudget.Limits{Steps: 20, Tokens: 1000, WallClock: 10 * time.Minute, SpendMicros: 10000},
	}, clock)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.OpenTask(agentbudget.TaskSpec{ID: "task-a", TenantID: "tenant-a", UserID: "user-a"}); err != nil {
		t.Fatal(err)
	}
	coordinator := newCoordinator(t, 1, map[Lane]int{LaneInteractive: 1}, map[string]int{"model-a": 1})
	reservation, err := ledger.Reserve(context.Background(), agentbudget.Request{
		TaskID: "task-a", StepID: "step-a", Fingerprint: "request-digest",
		Estimate: agentbudget.Usage{Steps: 1, Tokens: 80, WallClock: time.Second, SpendMicros: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := coordinator.Acquire(context.Background(), request("tenant-a", LaneInteractive, "model-a"))
	if err != nil {
		_ = reservation.Release()
		t.Fatalf("resource admission failed after budget reservation: %v", err)
	}
	if err := reservation.Settle(agentbudget.Usage{Steps: 1, Tokens: 50, WallClock: time.Second, SpendMicros: 60}); err != nil {
		t.Fatal(err)
	}
	lease.Release()
	if got := ledger.Snapshot().Tasks[0].Used.Tokens; got != 50 {
		t.Fatalf("settled tokens = %d, want 50", got)
	}
	if got := coordinator.SnapshotForTenant("tenant-a"); got.Active != 0 || got.Queued != 0 {
		t.Fatalf("resource lease remained after budget settlement: %+v", got)
	}
}

func BenchmarkTodo_AGENT_038_Benchmark(b *testing.B) {
	c, err := New(Policy{
		CellID: "cell-a", MaxConcurrent: 32, MaxConcurrentPerTenant: 8, MaxQueued: 1024, MaxQueuedPerTenant: 64,
		LaneConcurrency:     map[Lane]int{LaneInteractive: 24, LaneAutonomous: 8},
		ProviderConcurrency: map[string]int{"model-a": 32},
	})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		lease, err := c.Acquire(context.Background(), request("tenant-a", LaneInteractive, "model-a"))
		if err != nil {
			b.Fatal(err)
		}
		lease.Release()
	}
}

type acquireResult struct {
	lease *Lease
	err   error
}

func newCoordinator(t testing.TB, max int, lanes map[Lane]int, providers map[string]int) *Coordinator {
	t.Helper()
	coordinator, err := New(Policy{
		CellID: "cell-a", MaxConcurrent: max, MaxConcurrentPerTenant: max,
		MaxQueued: 128, MaxQueuedPerTenant: 16,
		LaneConcurrency: lanes, ProviderConcurrency: providers,
	})
	if err != nil {
		t.Fatal(err)
	}
	return coordinator
}

func request(tenant string, lane Lane, provider string) Request {
	return Request{TenantID: tenant, CellID: "cell-a", Lane: lane, Provider: provider}
}

func startAcquire(t *testing.T, coordinator *Coordinator, ctx context.Context, request Request) <-chan acquireResult {
	t.Helper()
	result := make(chan acquireResult, 1)
	go func() {
		lease, err := coordinator.Acquire(ctx, request)
		result <- acquireResult{lease: lease, err: err}
	}()
	return result
}

func receiveAcquire(t *testing.T, result <-chan acquireResult) acquireResult {
	t.Helper()
	select {
	case acquired := <-result:
		return acquired
	case <-time.After(3 * time.Second):
		t.Fatal("acquisition did not resolve")
		return acquireResult{}
	}
}

func waitQueued(t *testing.T, coordinator *Coordinator, tenant string, want int) {
	t.Helper()
	for attempts := 0; attempts < 100_000; attempts++ {
		if coordinator.SnapshotForTenant(tenant).Queued == want {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("tenant %s queue did not reach %d", tenant, want)
}

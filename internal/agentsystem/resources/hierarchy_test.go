package resources

import (
	"context"
	"errors"
	"testing"
)

func TestTodo_AGENT_038_UserTaskAndInteractiveReserve(t *testing.T) {
	c, err := New(Policy{CellID: "cell-a", MaxConcurrent: 3, MaxConcurrentPerTenant: 3,
		MaxConcurrentPerUser: 1, MaxConcurrentPerTask: 1, ReservedInteractive: 1,
		MaxQueued: 8, MaxQueuedPerTenant: 8, MaxQueuedPerUser: 2, MaxQueuedPerTask: 1,
		LaneConcurrency: map[Lane]int{LaneInteractive: 3, LaneAutonomous: 3}})
	if err != nil {
		t.Fatal(err)
	}
	first := Request{TenantID: "a", UserID: "user", TaskID: "task", CellID: "cell-a", Lane: LaneAutonomous}
	held, err := c.Acquire(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queued := startAcquire(t, c, ctx, first)
	waitQueued(t, c, "a", 1)
	if _, err := c.Acquire(context.Background(), first); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("task queue overflow = %v", err)
	}
	other := first
	other.TaskID = "other-task"
	otherWait := startAcquire(t, c, ctx, other)
	waitQueued(t, c, "a", 2)
	other.UserID = "other-user"
	other.TaskID = "third-task"
	second, err := c.Acquire(context.Background(), other)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release()
	bulk := Request{TenantID: "b", UserID: "user", TaskID: "task", CellID: "cell-a", Lane: LaneAutonomous}
	bulkWait := startAcquire(t, c, ctx, bulk)
	waitQueued(t, c, "b", 1)
	interactive := bulk
	interactive.TenantID = "interactive"
	interactive.Lane = LaneInteractive
	live, err := c.Acquire(context.Background(), interactive)
	if err != nil {
		t.Fatal(err)
	}
	live.Release()
	if got := c.SnapshotForTenant("b"); got.Active != 0 || got.Queued != 1 {
		t.Fatalf("bulk stole reserved slot: %+v", got)
	}
	cancel()
	for _, result := range []<-chan acquireResult{queued, otherWait, bulkWait} {
		if got := receiveAcquire(t, result); !errors.Is(got.err, context.Canceled) {
			t.Fatalf("cancel = %v", got.err)
		}
	}
	held.Release()
	second.Release()
	if got := c.SnapshotForTenant("a"); got.Active != 0 || got.Queued != 0 {
		t.Fatalf("hierarchy leaked: %+v", got)
	}
}

func TestTodo_AGENT_038_PressureRecovery(t *testing.T) {
	c := newCoordinator(t, 2, map[Lane]int{LaneInteractive: 2, LaneAutonomous: 2}, nil)
	if err := c.ApplyPressure(PressureShed); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Acquire(context.Background(), request("bulk", LaneAutonomous, "")); !errors.Is(err, ErrPressureShed) {
		t.Fatalf("shed = %v", err)
	}
	live, err := c.Acquire(context.Background(), request("live", LaneInteractive, ""))
	if err != nil {
		t.Fatal(err)
	}
	live.Release()
	if err := c.ApplyPressure(PressureNormal); err != nil {
		t.Fatal(err)
	}
	bulk, err := c.Acquire(context.Background(), request("bulk", LaneAutonomous, ""))
	if err != nil {
		t.Fatal(err)
	}
	bulk.Release()
	if err := c.ApplyPressure(Pressure("invalid")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid pressure = %v", err)
	}
}

func TestTodo_AGENT_038_SlowPressureBoundsAndRestores(t *testing.T) {
	c := newCoordinator(t, 2, map[Lane]int{LaneInteractive: 2}, nil)
	if err := c.ApplyPressure(PressureSlow); err != nil {
		t.Fatal(err)
	}
	first, err := c.Acquire(context.Background(), request("first", LaneInteractive, ""))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	second := startAcquire(t, c, context.Background(), request("second", LaneInteractive, ""))
	waitQueued(t, c, "second", 1)
	if got := c.SnapshotForTenant("second"); got.Active != 0 {
		t.Fatalf("slow pressure exceeded reduced capacity: %+v", got)
	}
	if err := c.ApplyPressure(PressureNormal); err != nil {
		t.Fatal(err)
	}
	got := receiveAcquire(t, second)
	if got.err != nil {
		t.Fatal(got.err)
	}
	got.lease.Release()
	first.Release()
	if got := c.SnapshotForTenant("first"); got.Active != 0 || got.Queued != 0 {
		t.Fatalf("pressure recovery leaked capacity: %+v", got)
	}
}

func TestTodo_AGENT_038_RejectMissingHierarchy(t *testing.T) {
	policy := Policy{CellID: "cell-a", MaxConcurrent: 2, MaxConcurrentPerTenant: 2, MaxQueued: 8, MaxQueuedPerTenant: 4, MaxConcurrentPerUser: 1, MaxConcurrentPerTask: 1, LaneConcurrency: map[Lane]int{LaneInteractive: 2}}
	c, err := New(policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []Request{{TenantID: "tenant", CellID: "cell-a", Lane: LaneInteractive}, {TenantID: "tenant", CellID: "cell-a", Lane: LaneInteractive, UserID: "user"}, {TenantID: "tenant", CellID: "cell-a", Lane: LaneInteractive, TaskID: "task"}} {
		if _, err := c.Acquire(context.Background(), request); !errors.Is(err, ErrInvalid) {
			t.Fatalf("missing hierarchy accepted: %+v %v", request, err)
		}
	}
	policy.MaxConcurrentPerUser = -1
	if _, err := New(policy); !errors.Is(err, ErrInvalid) {
		t.Fatalf("negative hierarchy = %v", err)
	}
	policy.MaxConcurrentPerUser = 1
	policy.ReservedInteractive = 2
	if _, err := New(policy); !errors.Is(err, ErrInvalid) {
		t.Fatalf("reserve consumes whole pool = %v", err)
	}
}

func TestTodo_AGENT_038_InteractiveTenantUserReserve(t *testing.T) {
	c, err := New(Policy{CellID: "cell-a", MaxConcurrent: 4, MaxConcurrentPerTenant: 3, MaxConcurrentPerUser: 2, MaxConcurrentPerTask: 1,
		MaxQueued: 8, MaxQueuedPerTenant: 8, ReservedInteractivePerTenant: 1, ReservedInteractivePerUser: 1,
		LaneConcurrency: map[Lane]int{LaneInteractive: 4, LaneAutonomous: 4}})
	if err != nil {
		t.Fatal(err)
	}
	first := Request{TenantID: "tenant", UserID: "user-a", TaskID: "first", CellID: "cell-a", Lane: LaneAutonomous}
	held, err := c.Acquire(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	userBurst := first
	userBurst.TaskID = "second"
	userWait := startAcquire(t, c, ctx, userBurst)
	waitQueued(t, c, "tenant", 1)
	other := first
	other.UserID = "user-b"
	other.TaskID = "third"
	second, err := c.Acquire(context.Background(), other)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release()
	tenantBurst := first
	tenantBurst.UserID = "user-c"
	tenantBurst.TaskID = "fourth"
	tenantWait := startAcquire(t, c, ctx, tenantBurst)
	waitQueued(t, c, "tenant", 2)
	live := first
	live.TaskID = "live"
	live.Lane = LaneInteractive
	interactive, err := c.Acquire(context.Background(), live)
	if err != nil {
		t.Fatal(err)
	}
	interactive.Release()
	if got := c.SnapshotForTenant("tenant"); got.Active != 2 || got.Queued != 2 {
		t.Fatalf("interactive reserves failed: %+v", got)
	}
	cancel()
	for _, result := range []<-chan acquireResult{userWait, tenantWait} {
		if got := receiveAcquire(t, result); !errors.Is(got.err, context.Canceled) {
			t.Fatalf("cancel = %v", got.err)
		}
	}
}

package chatrouting

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func routeReq() ReserveRequest {
	return ReserveRequest{ConversationID: "c1", HostTenantID: "t1", ShardID: "s1", IdempotencyKey: "k1", PlacementPolicy: "tenant-default", PlacementPolicyVersion: 1}
}

func TestTodo_CHAT_002(t *testing.T) {
	d := NewMemoryDirectory()
	r, err := d.Reserve(context.Background(), routeReq())
	if err != nil || r.State != StatePending || r.Epoch != 1 {
		t.Fatalf("reserve = %+v, %v", r, err)
	}
	if _, err = d.Lookup(context.Background(), "c1", "other"); !errors.Is(err, ErrTenant) {
		t.Fatalf("foreign lookup = %v", err)
	}
	if _, err = d.Lookup(context.Background(), "guess", "t1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown lookup = %v", err)
	}
	active, err := d.Activate(context.Background(), "c1", "t1", 1)
	if err != nil || active.State != StateActive {
		t.Fatalf("activate = %+v, %v", active, err)
	}
	if _, err = d.Activate(context.Background(), "c1", "t1", 1); err != nil {
		t.Fatalf("idempotent activate = %v", err)
	}
}

func TestTodo_CHAT_002_Security(t *testing.T) {
	d := NewMemoryDirectory()
	_, _ = d.Reserve(context.Background(), routeReq())
	if _, err := d.Reserve(context.Background(), ReserveRequest{ConversationID: "c1", HostTenantID: "t2", ShardID: "s2", IdempotencyKey: "k2"}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("collision = %v", err)
	}
	if _, err := d.Reserve(context.Background(), routeReq()); err != nil {
		t.Fatalf("same retry = %v", err)
	}
}

func TestTodo_CHAT_002_Integration(t *testing.T) {
	d := NewMemoryDirectory()
	r, _ := d.Reserve(context.Background(), routeReq())
	if _, err := d.Activate(context.Background(), "c1", "t1", r.Epoch+1); !errors.Is(err, ErrStaleEpoch) {
		t.Fatalf("stale activation = %v", err)
	}
}

func TestTodo_CHAT_005(t *testing.T) {
	d := NewMemoryDirectory()
	_, _ = d.Reserve(context.Background(), routeReq())
	_, _ = d.Activate(context.Background(), "c1", "t1", 1)
	c := NewRouteCache(time.Minute)
	lease, err := c.Resolve(context.Background(), d, "c1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := d.BeginMove(context.Background(), "c1", "t1", 1, "s2")
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckWrite(context.Background(), d, lease, "t1", time.Now()); !errors.Is(err, ErrStaleEpoch) {
		t.Fatalf("stale write = %v", err)
	}
	c.Invalidate("c1", plan.MoveEpoch)
}

func TestTodo_CHAT_005_Fault(t *testing.T) {
	d := NewMemoryDirectory()
	_, _ = d.Reserve(context.Background(), routeReq())
	_, _ = d.Activate(context.Background(), "c1", "t1", 1)
	c := NewRouteCache(10 * time.Millisecond)
	c.now = func() time.Time { return time.Unix(0, 0) }
	lease, _ := c.Resolve(context.Background(), d, "c1", "t1")
	c.now = func() time.Time { return time.Unix(1, 0) }
	if _, err := c.Resolve(context.Background(), d, "c1", "t1"); err != nil {
		t.Fatal(err)
	}
	if lease.Route.Epoch == 0 {
		t.Fatal("empty lease")
	}
}

func TestTodo_CHAT_005_Security(t *testing.T) {
	d := NewMemoryDirectory()
	_, _ = d.Reserve(context.Background(), routeReq())
	_, _ = d.Activate(context.Background(), "c1", "t1", 1)
	c := NewRouteCache(time.Minute)
	if _, err := c.Resolve(context.Background(), d, "c1", "t2"); !errors.Is(err, ErrTenant) {
		t.Fatalf("cache foreign tenant = %v", err)
	}
}

func TestTodo_CHAT_006(t *testing.T) {
	d := NewMemoryDirectory()
	chat := &fakeCreator{}
	c := CreateCoordinator{Directory: d, Chat: chat}
	req := CreateRequest{ConversationID: "c1", HostTenantID: "t1", ShardID: "s1", IdempotencyKey: "k1"}
	r, err := c.Create(context.Background(), req)
	if err != nil || r.State != StateActive {
		t.Fatalf("create = %+v, %v", r, err)
	}
	r, err = c.Create(context.Background(), req)
	if err != nil || r.State != StateActive || chat.calls != 1 {
		t.Fatalf("retry = %+v, %v calls=%d", r, err, chat.calls)
	}
}

func TestTodo_CHAT_006_Recovery(t *testing.T) {
	d := NewMemoryDirectory()
	chat := &fakeCreator{fail: true}
	c := CreateCoordinator{Directory: d, Chat: chat}
	req := CreateRequest{ConversationID: "c1", HostTenantID: "t1", ShardID: "s1", IdempotencyKey: "k1"}
	r, err := c.Create(context.Background(), req)
	if err == nil || r.State != StatePending {
		t.Fatalf("failed create = %+v, %v", r, err)
	}
	chat.fail = false
	r, err = c.Reconcile(context.Background(), req)
	if err != nil || r.State != StateActive {
		t.Fatalf("reconcile = %+v, %v", r, err)
	}
}

func TestTodo_CHAT_006_Integration(t *testing.T) {
	d := NewMemoryDirectory()
	chat := &fakeCreator{}
	c := CreateCoordinator{Directory: d, Chat: chat}
	req := CreateRequest{ConversationID: "integration-conversation", HostTenantID: "tenant-integration", ShardID: "shard-integration", IdempotencyKey: "integration-key"}
	result, err := c.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	route, err := d.Lookup(context.Background(), req.ConversationID, req.HostTenantID)
	if err != nil {
		t.Fatalf("lookup created route: %v", err)
	}
	if result.State != StateActive || route.State != StateActive || route.ShardID != req.ShardID || chat.calls != 1 {
		t.Fatalf("create result=%+v route=%+v creator calls=%d", result, route, chat.calls)
	}
}

func TestTodo_CHAT_007(t *testing.T) {
	d := NewMemoryDirectory()
	_, _ = d.Reserve(context.Background(), routeReq())
	_, _ = d.Activate(context.Background(), "c1", "t1", 1)
	mover := &fakeMover{}
	r, err := (MoveCoordinator{Directory: d, Shards: mover}).Move(context.Background(), "c1", "t1", 1, "s2")
	if err != nil || r.State != StateActive || r.ShardID != "s2" || r.Epoch != 3 {
		t.Fatalf("move = %+v, %v", r, err)
	}
	if len(mover.events) != 3 {
		t.Fatalf("events = %v", mover.events)
	}
}

func TestTodo_CHAT_007_Recovery(t *testing.T) {
	d := NewMemoryDirectory()
	_, _ = d.Reserve(context.Background(), routeReq())
	_, _ = d.Activate(context.Background(), "c1", "t1", 1)
	mover := &fakeMover{verifyErr: errors.New("checksum")}
	if _, err := (MoveCoordinator{Directory: d, Shards: mover}).Move(context.Background(), "c1", "t1", 1, "s2"); err == nil {
		t.Fatal("verify failure accepted")
	}
	r, _ := d.Lookup(context.Background(), "c1", "t1")
	if r.State != StateActive || r.ShardID != "s1" {
		t.Fatalf("rollback route = %+v", r)
	}
}

func TestTodo_CHAT_007_Integration(t *testing.T) {
	d := NewMemoryDirectory()
	_, err := d.Reserve(context.Background(), routeReq())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Activate(context.Background(), "c1", "t1", 1); err != nil {
		t.Fatal(err)
	}
	mover := &fakeMover{}
	result, err := (MoveCoordinator{Directory: d, Shards: mover}).Move(context.Background(), "c1", "t1", 1, "s2")
	if err != nil {
		t.Fatalf("move route: %v", err)
	}
	if result.State != StateActive || result.ShardID != "s2" || result.Epoch != 3 {
		t.Fatalf("move result=%+v", result)
	}
	if len(mover.events) != 3 || mover.events[0] != "copy" || mover.events[1] != "verify" || mover.events[2] != "drain" {
		t.Fatalf("move sequence=%v", mover.events)
	}
}

func TestTodo_CHAT_007_Race(t *testing.T) {
	d := NewMemoryDirectory()
	_, _ = d.Reserve(context.Background(), routeReq())
	_, _ = d.Activate(context.Background(), "c1", "t1", 1)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := d.BeginMove(context.Background(), "c1", "t1", 1, "s2"); err == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful moves = %d", successes.Load())
	}
}

type fakeCreator struct {
	calls int
	fail  bool
}

func (f *fakeCreator) EnsureConversation(context.Context, CreateRequest) error {
	f.calls++
	if f.fail {
		return errors.New("chat unavailable")
	}
	return nil
}

type fakeMover struct {
	events    []string
	verifyErr error
}

func (*fakeMover) FenceWrites(context.Context, MovePlan) error         { return nil }
func (*fakeMover) AbortWrites(context.Context, MovePlan, uint64) error { return nil }

func (f *fakeMover) CopyConversation(context.Context, MovePlan) error {
	f.events = append(f.events, "copy")
	return nil
}
func (f *fakeMover) VerifyConversation(context.Context, MovePlan) error {
	f.events = append(f.events, "verify")
	return f.verifyErr
}
func (f *fakeMover) DrainConversation(context.Context, MovePlan) error {
	f.events = append(f.events, "drain")
	return nil
}

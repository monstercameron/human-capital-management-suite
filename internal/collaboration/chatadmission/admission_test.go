package chatadmission

import (
	"context"
	"errors"
	"testing"
)

func TestTodo_CHAT_046(t *testing.T) {
	a, err := New(Config{TenantConcurrent: 2, ConversationConcurrent: 2, SendConcurrent: 1, WatchConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	l, err := a.Acquire(context.Background(), Request{TenantID: "t", ConversationID: "c", Lane: LaneSend})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	if _, err := a.Acquire(context.Background(), Request{TenantID: "t", ConversationID: "c", Lane: LaneSend}); !errors.Is(err, ErrOverloaded) {
		t.Fatalf("second send=%v", err)
	}
	w, err := a.Acquire(context.Background(), Request{TenantID: "t", ConversationID: "c", Lane: LaneWatch})
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Release()
}
func TestTodo_CHAT_046_Fault(t *testing.T) {
	a, err := New(Config{TenantConcurrent: 1, ConversationConcurrent: 1, SendConcurrent: 1, WatchConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	l, err := a.Acquire(context.Background(), Request{TenantID: "t", ConversationID: "c", Lane: LaneSend})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(l.Release(), ErrReleased) {
		t.Fatal("double release accepted")
	}
	if _, err := a.Acquire(context.Background(), Request{TenantID: "", ConversationID: "c", Lane: LaneSend}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal(err)
	}
}

// TestTodo_CHAT_046_Eviction proves the per-tenant and per-conversation pools
// are evicted once their last lease is released, so a process that has served
// many conversations does not retain one pool per conversation forever.
func TestTodo_CHAT_046_Eviction(t *testing.T) {
	a, err := New(Config{TenantConcurrent: 4, ConversationConcurrent: 4, SendConcurrent: 4, WatchConcurrent: 4})
	if err != nil {
		t.Fatal(err)
	}
	leases := make([]*Lease, 0, 6)
	for i := 0; i < 3; i++ {
		l, err := a.Acquire(context.Background(), Request{TenantID: "t", ConversationID: string(rune('a' + i)), Lane: LaneSend})
		if err != nil {
			t.Fatal(err)
		}
		leases = append(leases, l)
	}
	if tenants, conversations := a.ScopeCounts(); tenants != 1 || conversations != 3 {
		t.Fatalf("held pools tenants=%d conversations=%d, want 1 and 3", tenants, conversations)
	}
	for _, l := range leases {
		if err := l.Release(); err != nil {
			t.Fatal(err)
		}
	}
	if tenants, conversations := a.ScopeCounts(); tenants != 0 || conversations != 0 {
		t.Fatalf("idle pools tenants=%d conversations=%d, want both evicted", tenants, conversations)
	}
	// A rejected acquisition must not leak a reservation either.
	full, err := New(Config{TenantConcurrent: 1, ConversationConcurrent: 1, SendConcurrent: 1, WatchConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	held, err := full.Acquire(context.Background(), Request{TenantID: "t", ConversationID: "c", Lane: LaneSend})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := full.Acquire(context.Background(), Request{TenantID: "t", ConversationID: "c", Lane: LaneSend}); !errors.Is(err, ErrOverloaded) {
		t.Fatalf("saturated send=%v", err)
	}
	if err := held.Release(); err != nil {
		t.Fatal(err)
	}
	if tenants, conversations := full.ScopeCounts(); tenants != 0 || conversations != 0 {
		t.Fatalf("after refusal tenants=%d conversations=%d, want both evicted", tenants, conversations)
	}
}

// TestTodo_CHAT_046_ShedOrder pins the spec's shed order: derived work is
// refused first, then reads, and a send is admitted until its own bound is
// full.
func TestTodo_CHAT_046_ShedOrder(t *testing.T) {
	a, err := New(Config{TenantConcurrent: 4, ConversationConcurrent: 4, SendConcurrent: 4, WatchConcurrent: 4, ReadConcurrent: 4, DerivedConcurrent: 4, DerivedShedFraction: 0.5, ReadShedFraction: 0.75})
	if err != nil {
		t.Fatal(err)
	}
	req := func(l Lane) Request { return Request{TenantID: "t", ConversationID: "c", Lane: l} }
	first, err := a.Acquire(context.Background(), req(LaneSend))
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.Acquire(context.Background(), req(LaneSend))
	if err != nil {
		t.Fatal(err)
	}
	// Half of the conversation scope is in flight: derived work is shed, read
	// and send work is still admitted.
	if _, err := a.Acquire(context.Background(), req(LaneDerived)); !errors.Is(err, ErrOverloaded) {
		t.Fatalf("derived under pressure=%v, want ErrOverloaded", err)
	}
	read, err := a.Acquire(context.Background(), req(LaneRead))
	if err != nil {
		t.Fatalf("read under derived-shed pressure=%v", err)
	}
	// Three of four in flight crosses the read fraction; a send still lands.
	if _, err := a.Acquire(context.Background(), req(LaneRead)); !errors.Is(err, ErrOverloaded) {
		t.Fatalf("read above read fraction=%v, want ErrOverloaded", err)
	}
	last, err := a.Acquire(context.Background(), req(LaneSend))
	if err != nil {
		t.Fatalf("send above read fraction=%v, want admitted", err)
	}
	for _, l := range []*Lease{first, second, read, last} {
		if err := l.Release(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.Acquire(context.Background(), req("bogus")); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unknown lane=%v", err)
	}
	if _, err := New(Config{TenantConcurrent: 1, ConversationConcurrent: 1, SendConcurrent: 1, WatchConcurrent: 1, ReadShedFraction: 0.2, DerivedShedFraction: 0.9}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatal("derived fraction above read fraction accepted")
	}
	if a.Available(req(LaneDerived)) != 4 {
		t.Fatalf("derived available=%d", a.Available(req(LaneDerived)))
	}
}

// TestWatchScopesAreSeparateFromRequestScopes proves a live subscription does
// not spend the per-conversation and per-tenant budgets that gate requests. It
// used to, so a handful of long-lived watches on one conversation pushed that
// conversation past the read shed point and every read on it was refused.
func TestWatchScopesAreSeparateFromRequestScopes(t *testing.T) {
	a, err := New(Config{TenantConcurrent: 2, ConversationConcurrent: 2, SendConcurrent: 4, WatchConcurrent: 16, ReadConcurrent: 4, DerivedConcurrent: 4, ReadShedFraction: 0.9, DerivedShedFraction: 0.75})
	if err != nil {
		t.Fatal(err)
	}
	watches := make([]*Lease, 0, 8)
	for i := 0; i < 8; i++ {
		lease, acquireErr := a.Acquire(context.Background(), Request{TenantID: "t", ConversationID: "c", Lane: LaneWatch})
		if acquireErr != nil {
			t.Fatalf("watch %d beyond the request scope bound = %v", i, acquireErr)
		}
		watches = append(watches, lease)
	}
	read, err := a.Acquire(context.Background(), Request{TenantID: "t", ConversationID: "c", Lane: LaneRead})
	if err != nil {
		t.Fatalf("read starved by open watches = %v", err)
	}
	if err := read.Release(); err != nil {
		t.Fatal(err)
	}
	for _, lease := range watches {
		if err := lease.Release(); err != nil {
			t.Fatal(err)
		}
	}
	if tenants, conversations := a.ScopeCounts(); tenants != 0 || conversations != 0 {
		t.Fatalf("watch scopes retained after release: tenants=%d conversations=%d", tenants, conversations)
	}
	// The watch lane itself is still bounded, on its own budget.
	full := make([]*Lease, 0, 16)
	for i := 0; i < 16; i++ {
		lease, acquireErr := a.Acquire(context.Background(), Request{TenantID: "t", ConversationID: "c", Lane: LaneWatch})
		if acquireErr != nil {
			t.Fatalf("watch %d = %v", i, acquireErr)
		}
		full = append(full, lease)
	}
	if _, err := a.Acquire(context.Background(), Request{TenantID: "t", ConversationID: "c", Lane: LaneWatch}); !errors.Is(err, ErrOverloaded) {
		t.Fatalf("watch above its own bound = %v", err)
	}
	for _, lease := range full {
		if err := lease.Release(); err != nil {
			t.Fatal(err)
		}
	}
}

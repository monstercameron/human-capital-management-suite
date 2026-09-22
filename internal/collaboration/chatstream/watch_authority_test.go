package chatstream

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type watcherKey struct{}
type watcherAuthority struct{ revoked atomic.Bool }

func (a *watcherAuthority) Authorize(ctx context.Context, _ Access) error {
	if ctx.Value(watcherKey{}) != "watcher" || a.revoked.Load() {
		return ErrUnauthorized
	}
	return nil
}

func TestTodo_CHAT_011_Security_WatcherContext(t *testing.T) {
	a := &watcherAuthority{}
	s, err := New(Config{Key: []byte("key"), Reader: testReader{}, Authorizer: a, QueueSize: 2, ReplayLimit: 2, CursorTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), watcherKey{}, "watcher"))
	defer cancel()
	sub, err := s.Watch(ctx, req())
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	if err := s.Publish(context.Background(), Event{TenantID: "tenant", ConversationID: "conversation", Sequence: 1, MembershipEpoch: 1}); err != nil {
		t.Fatal(err)
	}
	if event, err := sub.Next(ctx); err != nil || event.Sequence != 1 {
		t.Fatalf("watcher delivery=%+v err=%v", event, err)
	}
	if err := s.Publish(context.Background(), Event{TenantID: "tenant", ConversationID: "conversation", Sequence: 2, MembershipEpoch: 1}); err != nil {
		t.Fatal(err)
	}
	a.revoked.Store(true)
	if _, err := sub.Next(ctx); !errors.Is(err, ErrRevoked) {
		t.Fatalf("queued event after revoke: %v", err)
	}
}

// TestTodo_CHAT_011_Fault_IdleRevocation covers the safety-net recheck. The
// production cadence is DefaultRecheckInterval because revocation is now event
// driven (Stream.Revoke/RevokeTenant); this test configures a short interval so
// it still proves the periodic path closes an idle revoked watch.
func TestTodo_CHAT_011_Fault_IdleRevocation(t *testing.T) {
	a := &watcherAuthority{}
	s, err := New(Config{Key: []byte("key"), Reader: testReader{}, Authorizer: a, QueueSize: 1, ReplayLimit: 1, CursorTTL: time.Minute, RecheckInterval: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), watcherKey{}, "watcher"))
	defer cancel()
	sub, err := s.Watch(ctx, req())
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	a.revoked.Store(true)
	select {
	case <-sub.Done():
		if _, err := sub.Next(ctx); !errors.Is(err, ErrRevoked) {
			t.Fatalf("idle revoke error=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("idle revoked watch stayed open")
	}
}

func TestTodo_CHAT_020_Security_CanceledWatch(t *testing.T) {
	a := &watcherAuthority{}
	s, err := New(Config{Key: []byte("key"), Reader: testReader{}, Authorizer: a, QueueSize: 1, ReplayLimit: 1, CursorTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), watcherKey{}, "watcher"))
	sub, err := s.Watch(ctx, req())
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	if err := s.Publish(context.Background(), Event{TenantID: "tenant", ConversationID: "conversation", Sequence: 1, MembershipEpoch: 1}); err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := sub.Next(context.Background()); !errors.Is(err, ErrRevoked) {
		t.Fatalf("canceled watch returned queued data: %v", err)
	}
}

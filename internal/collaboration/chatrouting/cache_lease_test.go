package chatrouting

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type countingDirectory struct {
	Directory
	lookups atomic.Int32
}

func (d *countingDirectory) Lookup(ctx context.Context, conversationID, tenantID string) (Route, error) {
	d.lookups.Add(1)
	return d.Directory.Lookup(ctx, conversationID, tenantID)
}

func activeRouteDirectory(t *testing.T) *MemoryDirectory {
	t.Helper()
	d := NewMemoryDirectory()
	r, err := d.Reserve(context.Background(), routeReq())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Activate(context.Background(), r.ConversationID, r.HostTenantID, r.Epoch); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestRouteCacheReturnsSignedVersionedLease(t *testing.T) {
	d := &countingDirectory{Directory: activeRouteDirectory(t)}
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	c := NewRouteCache(time.Minute)
	c.now = func() time.Time { return now }

	lease, err := c.Resolve(context.Background(), d, "c1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if lease.Version != writeLeaseVersion || len(lease.Signature) == 0 {
		t.Fatalf("lease version/signature = %d/%x", lease.Version, lease.Signature)
	}
	if err := c.VerifyLease(lease); err != nil {
		t.Fatalf("valid lease rejected: %v", err)
	}
	if err := c.CheckWrite(context.Background(), d, lease, "t1", now); err != nil {
		t.Fatalf("valid lease failed live route check: %v", err)
	}

	for name, tamper := range map[string]func(*WriteLease){
		"route":     func(l *WriteLease) { l.Route.ShardID = "attacker-shard" },
		"expiry":    func(l *WriteLease) { l.ExpiresAt = l.ExpiresAt.Add(time.Second) },
		"version":   func(l *WriteLease) { l.Version++ },
		"signature": func(l *WriteLease) { l.Signature[0] ^= 0xff },
	} {
		t.Run(name, func(t *testing.T) {
			forged := lease
			forged.Signature = append([]byte(nil), lease.Signature...)
			tamper(&forged)
			if err := c.CheckWrite(context.Background(), d, forged, "t1", now); !errors.Is(err, ErrLeaseInvalid) {
				t.Fatalf("tampered lease check = %v, want ErrLeaseInvalid", err)
			}
		})
	}
}

func TestRouteCacheLeaseExpiresAndRefreshes(t *testing.T) {
	d := &countingDirectory{Directory: activeRouteDirectory(t)}
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	c := NewRouteCache(10 * time.Second)
	c.now = func() time.Time { return now }

	lease, err := c.Resolve(context.Background(), d, "c1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if d.lookups.Load() != 1 {
		t.Fatalf("initial lookups = %d, want 1", d.lookups.Load())
	}
	now = lease.ExpiresAt
	if err := c.CheckWrite(context.Background(), d, lease, "t1", now); !errors.Is(err, ErrStaleEpoch) {
		t.Fatalf("expired lease check = %v, want ErrStaleEpoch", err)
	}
	refreshed, err := c.Resolve(context.Background(), d, "c1", "t1")
	if err != nil {
		t.Fatalf("resolve after expiry: %v", err)
	}
	if d.lookups.Load() != 2 || !refreshed.ExpiresAt.After(now) {
		t.Fatalf("refresh lookups=%d expiry=%s, want lookup 2 and future expiry", d.lookups.Load(), refreshed.ExpiresAt)
	}
}

func TestRouteCacheLeaseSignatureDoesNotAliasCachedBytes(t *testing.T) {
	d := &countingDirectory{Directory: activeRouteDirectory(t)}
	c := NewRouteCache(time.Minute)
	first, err := c.Resolve(context.Background(), d, "c1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	first.Signature[0] ^= 0xff
	second, err := c.Resolve(context.Background(), d, "c1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.VerifyLease(second); err != nil {
		t.Fatalf("caller mutation corrupted cached signature: %v", err)
	}
	if d.lookups.Load() != 1 {
		t.Fatalf("mutated cache hit caused %d lookups, want 1", d.lookups.Load())
	}
}

func TestRouteCacheInvalidatesMovedEpochWithoutEvictingNewerLease(t *testing.T) {
	directory := activeRouteDirectory(t)
	d := &countingDirectory{Directory: directory}
	c := NewRouteCache(time.Minute)
	old, err := c.Resolve(context.Background(), d, "c1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := directory.BeginMove(context.Background(), "c1", "t1", old.Route.Epoch, "s2"); err != nil {
		t.Fatal(err)
	}
	if err := c.CheckWrite(context.Background(), d, old, "t1", time.Now()); !errors.Is(err, ErrStaleEpoch) {
		t.Fatalf("old epoch write = %v, want ErrStaleEpoch", err)
	}
	c.Invalidate("c1", old.Route.Epoch)
	current, err := c.Resolve(context.Background(), d, "c1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if current.Route.Epoch != old.Route.Epoch+1 || current.Route.State != StateMoving {
		t.Fatalf("route after move = %+v", current.Route)
	}
	lookups := d.lookups.Load()
	c.Invalidate("c1", old.Route.Epoch)
	again, err := c.Resolve(context.Background(), d, "c1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if again.Route.Epoch != current.Route.Epoch || d.lookups.Load() != lookups {
		t.Fatalf("stale invalidation evicted newer lease: epoch=%d lookups=%d, want epoch=%d lookups=%d", again.Route.Epoch, d.lookups.Load(), current.Route.Epoch, lookups)
	}
}

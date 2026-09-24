package documenthubstore

import (
	"context"
	"errors"
	"testing"
)

// TestTodo_HUB_002 is the PRIMARY test for HUB-002: a document ID resolves
// to exactly the tenant, shard and epoch core registered for it, and a
// reshard advances the epoch so any earlier route becomes stale.
func TestTodo_HUB_002(t *testing.T) {
	d := NewMemoryDocumentRoutes()
	ctx := context.Background()
	r, err := d.Register(ctx, "doc-1", "tenant-a", "shard-1", "key-1")
	if err != nil || r.Epoch != 1 || r.ShardID != "shard-1" {
		t.Fatalf("register = %+v, %v", r, err)
	}
	got, err := d.Lookup(ctx, "doc-1", "tenant-a")
	if err != nil || got != r {
		t.Fatalf("lookup = %+v, %v", got, err)
	}
	moved, err := d.Reshard(ctx, "doc-1", "tenant-a", "shard-2", 1)
	if err != nil || moved.ShardID != "shard-2" || moved.Epoch != 2 {
		t.Fatalf("reshard = %+v, %v", moved, err)
	}
	if err := RequireCurrentRoute(ctx, d, r, "tenant-a"); !errors.Is(err, ErrRouteStaleEpoch) {
		t.Fatalf("stale migrated copy accepted: %v", err)
	}
	if err := RequireCurrentRoute(ctx, d, moved, "tenant-a"); err != nil {
		t.Fatalf("current route refused: %v", err)
	}
}

// TestTodo_HUB_002_Security is the SECURITY test for HUB-002: a route
// resolved or reshard requested under a foreign tenant is refused, and an
// idempotency collision under a different tenant never silently overwrites
// an existing route.
func TestTodo_HUB_002_Security(t *testing.T) {
	d := NewMemoryDocumentRoutes()
	ctx := context.Background()
	if _, err := d.Register(ctx, "doc-1", "tenant-a", "shard-1", "key-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Register(ctx, "doc-1", "tenant-b", "shard-9", "key-2"); !errors.Is(err, ErrRouteExists) {
		t.Fatalf("foreign tenant registration collision = %v", err)
	}
	if _, err := d.Lookup(ctx, "doc-1", "tenant-b"); !errors.Is(err, ErrRouteTenant) {
		t.Fatalf("foreign lookup = %v", err)
	}
	if _, err := d.Reshard(ctx, "doc-1", "tenant-b", "shard-2", 1); !errors.Is(err, ErrRouteTenant) {
		t.Fatalf("foreign reshard = %v", err)
	}
	r, _ := d.Lookup(ctx, "doc-1", "tenant-a")
	if err := RequireCurrentRoute(ctx, d, r, "tenant-b"); !errors.Is(err, ErrRouteTenant) {
		t.Fatalf("foreign read refused route = %v", err)
	}
	if err := RequireCurrentRoute(ctx, nil, r, "tenant-a"); !errors.Is(err, ErrRouteInvalid) {
		t.Fatalf("nil directory accepted: %v", err)
	}
}

// TestTodo_HUB_002_Integration is the INTEGRATION test for HUB-002: a
// document ID unknown to the directory, or resolved under a stale epoch
// after a completed reshard, both refuse before any content read, and
// registration is idempotent under a retried request.
func TestTodo_HUB_002_Integration(t *testing.T) {
	d := NewMemoryDocumentRoutes()
	ctx := context.Background()
	if _, err := d.Lookup(ctx, "doc-missing", "tenant-a"); !errors.Is(err, ErrRouteNotFound) {
		t.Fatalf("unknown document resolved: %v", err)
	}
	first, err := d.Register(ctx, "doc-1", "tenant-a", "shard-1", "key-1")
	if err != nil {
		t.Fatal(err)
	}
	retry, err := d.Register(ctx, "doc-1", "tenant-a", "shard-1", "key-1")
	if err != nil || retry != first {
		t.Fatalf("idempotent retry = %+v, %v", retry, err)
	}
	if _, err := d.Reshard(ctx, "doc-1", "tenant-a", "shard-2", 99); !errors.Is(err, ErrRouteStaleEpoch) {
		t.Fatalf("reshard with wrong expected epoch = %v", err)
	}
	moved, err := d.Reshard(ctx, "doc-1", "tenant-a", "shard-2", first.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	// A reader who resolved the route before the migration must be refused,
	// not silently served the old shard's stale copy.
	if err := RequireCurrentRoute(ctx, d, first, "tenant-a"); !errors.Is(err, ErrRouteStaleEpoch) {
		t.Fatalf("stale pre-migration route accepted: %v", err)
	}
	if err := RequireCurrentRoute(ctx, d, moved, "tenant-a"); err != nil {
		t.Fatalf("post-migration route refused: %v", err)
	}
}

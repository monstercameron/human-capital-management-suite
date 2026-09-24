package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

// TestTodo_HUB_002 is the PRIMARY test for HUB-002 at the application
// boundary: documentService registers and enforces core document routes
// through the store's directory port.
func TestTodo_HUB_002(t *testing.T) {
	svc := documentService{}
	ctx := context.Background()
	dir := documenthubstore.NewMemoryDocumentRoutes()
	route, err := svc.RegisterDocumentRoute(ctx, dir, "doc-1", "tenant-a", "shard-1", "key-1")
	if err != nil || route.Epoch != 1 {
		t.Fatalf("register = %+v, %v", route, err)
	}
	if err := svc.RequireDocumentRoute(ctx, dir, route, "tenant-a"); err != nil {
		t.Fatalf("current route refused: %v", err)
	}
	if _, err := dir.Reshard(ctx, "doc-1", "tenant-a", "shard-2", route.Epoch); err != nil {
		t.Fatal(err)
	}
	if err := svc.RequireDocumentRoute(ctx, dir, route, "tenant-a"); !errors.Is(err, documenthubstore.ErrRouteStaleEpoch) {
		t.Fatalf("stale migrated copy accepted: %v", err)
	}
}

// TestTodo_HUB_002_Security is the SECURITY test for HUB-002 at the
// application boundary: registration and enforcement refuse without a
// configured directory, and a foreign tenant is refused.
func TestTodo_HUB_002_Security(t *testing.T) {
	svc := documentService{}
	ctx := context.Background()
	if _, err := svc.RegisterDocumentRoute(ctx, nil, "doc-1", "tenant-a", "shard-1", "key-1"); !errors.Is(err, ErrNoRouteDirectory) {
		t.Fatalf("nil directory registration accepted: %v", err)
	}
	dir := documenthubstore.NewMemoryDocumentRoutes()
	route, err := svc.RegisterDocumentRoute(ctx, dir, "doc-1", "tenant-a", "shard-1", "key-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RequireDocumentRoute(ctx, dir, route, "tenant-b"); !errors.Is(err, documenthubstore.ErrRouteTenant) {
		t.Fatalf("foreign tenant route accepted: %v", err)
	}
}

// TestTodo_HUB_002_Integration is the INTEGRATION test for HUB-002 at the
// application boundary: a route registered through documentService is
// visible to a fresh lookup on the same directory, and a retried
// registration under the same idempotency key returns the same route
// rather than a conflict.
func TestTodo_HUB_002_Integration(t *testing.T) {
	svc := documentService{}
	ctx := context.Background()
	dir := documenthubstore.NewMemoryDocumentRoutes()
	first, err := svc.RegisterDocumentRoute(ctx, dir, "doc-1", "tenant-a", "shard-1", "key-1")
	if err != nil {
		t.Fatal(err)
	}
	got, err := dir.Lookup(ctx, "doc-1", "tenant-a")
	if err != nil || got != first {
		t.Fatalf("registered route not visible to lookup: %+v, %v", got, err)
	}
	retry, err := svc.RegisterDocumentRoute(ctx, dir, "doc-1", "tenant-a", "shard-1", "key-1")
	if err != nil || retry != first {
		t.Fatalf("idempotent retry diverged: %+v, %v", retry, err)
	}
}

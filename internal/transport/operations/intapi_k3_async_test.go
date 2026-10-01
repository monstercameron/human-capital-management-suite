package operations

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/streaming"
)

func TestTodo_INTAPI_014_OperationResource(t *testing.T) {
	registry := NewAsyncRegistry(func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) })
	started, err := registry.Start(context.Background(), AsyncRequest{TenantID: "tenant-a", Owner: "actor-a", RequestType: "report.run", IdempotencyKey: "run-1"})
	if err != nil || started.State != streaming.OperationPending || started.OperationID == "" {
		t.Fatalf("start = %+v, err=%v", started, err)
	}
	running, err := registry.SetState(context.Background(), "tenant-a", started.OperationID, streaming.OperationRunning)
	if err != nil || running.State != streaming.OperationRunning {
		t.Fatalf("running = %+v, err=%v", running, err)
	}
	done, err := registry.SetState(context.Background(), "tenant-a", started.OperationID, streaming.OperationSucceeded)
	if err != nil || done.State != streaming.OperationSucceeded {
		t.Fatalf("done = %+v, err=%v", done, err)
	}
}

func TestTodo_INTAPI_014_Integration(t *testing.T) {
	registry := NewAsyncRegistry(nil)
	request := AsyncRequest{TenantID: "tenant-a", Owner: "actor-a", RequestType: "export.run", IdempotencyKey: "export-1"}
	first, err := registry.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := registry.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.OperationID != second.OperationID {
		t.Fatalf("idempotent start = %q and %q", first.OperationID, second.OperationID)
	}
	if _, err := registry.Get(context.Background(), "tenant-b", first.OperationID); !errors.Is(err, ErrAsyncNotFound) {
		t.Fatalf("cross-tenant operation = %v", err)
	}
}

func TestTodo_INTAPI_014_Security(t *testing.T) {
	registry := NewAsyncRegistry(nil)
	if _, err := registry.Start(context.Background(), AsyncRequest{TenantID: "tenant-a"}); !errors.Is(err, ErrAsyncInvalid) {
		t.Fatalf("invalid operation = %v", err)
	}
	started, err := registry.Start(context.Background(), AsyncRequest{TenantID: "tenant-a", Owner: "actor-a", RequestType: "import.run", IdempotencyKey: "import-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SetState(context.Background(), "tenant-a", started.OperationID, streaming.OperationCancelled); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SetState(context.Background(), "tenant-a", started.OperationID, streaming.OperationSucceeded); !errors.Is(err, ErrAsyncConflict) {
		t.Fatalf("terminal mutation = %v", err)
	}
}

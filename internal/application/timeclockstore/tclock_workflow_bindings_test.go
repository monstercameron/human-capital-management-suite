package timeclockstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
)

func TestTodo_FTIME_003_WorkflowBindingPersistsImmutableTenantScopedIdentity(t *testing.T) {
	store, tenant := adapterFixture(t)
	ctx := context.Background()
	tenantID, instanceID := uuid.New(), uuid.New()
	binding := clockservice.TimeClockRunBinding{TenantID: tenantID, TenantKey: tenant, SessionID: "session-1", InstanceID: instanceID, WorkflowID: "clock.workflow", PlanDigest: "sha256:plan", StartKey: "start-1", CorrelationID: "corr-1", CreatedAt: time.Date(2026, 9, 29, 15, 0, 0, 0, time.UTC)}
	resolve := func(id uuid.UUID) (string, error) {
		if id == tenantID {
			return tenant, nil
		}
		return "", errors.New("unknown tenant")
	}
	adapter := WorkflowBindingAdapter{Store: store, ResolveTenant: resolve}
	if err := adapter.Save(ctx, binding); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Save(ctx, binding); err != nil {
		t.Fatalf("exact idempotent replay: %v", err)
	}
	got, err := adapter.Load(ctx, tenantID, binding.SessionID)
	if err != nil || got.TenantID != binding.TenantID || got.TenantKey != binding.TenantKey || got.SessionID != binding.SessionID || got.InstanceID != binding.InstanceID || got.WorkflowID != binding.WorkflowID || got.PlanDigest != binding.PlanDigest || got.StartKey != binding.StartKey || got.CorrelationID != binding.CorrelationID || !got.CreatedAt.Equal(binding.CreatedAt) {
		t.Fatalf("load=%+v err=%v", got, err)
	}
	changed := binding
	changed.PlanDigest = "sha256:changed"
	if err := adapter.Save(ctx, changed); err == nil {
		t.Fatal("binding overwrite accepted")
	}
	if err := (WorkflowBindingAdapter{Store: store, ResolveTenant: func(uuid.UUID) (string, error) { return "other-tenant", nil }}).Save(ctx, binding); err == nil {
		t.Fatal("tenant mapping mismatch accepted")
	}
	invalid := binding
	invalid.TenantKey = ""
	if err := (WorkflowBindingAdapter{Store: store}).Save(ctx, invalid); err == nil {
		t.Fatal("incomplete binding accepted")
	}
	if _, err := (WorkflowBindingAdapter{Store: store}).Load(ctx, tenantID, binding.SessionID); err == nil {
		t.Fatal("load without tenant resolver accepted")
	}
	if _, err := adapter.Load(ctx, uuid.New(), binding.SessionID); err == nil {
		t.Fatal("unknown tenant accepted")
	}
	if _, err := adapter.Load(ctx, tenantID, "missing-session"); err == nil {
		t.Fatal("missing binding accepted")
	}
	if err := (WorkflowBindingAdapter{}).Save(ctx, binding); !errors.Is(err, clockservice.ErrUnavailable) {
		t.Fatalf("missing store error=%v", err)
	}
	if _, err := (WorkflowBindingAdapter{}).Load(ctx, tenantID, binding.SessionID); !errors.Is(err, clockservice.ErrUnavailable) {
		t.Fatalf("missing store error=%v", err)
	}
}

func TestTodo_FTIME_003_LegacyBatchEntryPointsFailClosed(t *testing.T) {
	ctx := context.Background()
	adapter := Adapter{}
	if _, err := adapter.Punch(ctx, "tenant", clockservice.PunchWork{}); !errors.Is(err, clockservice.ErrUnavailable) {
		t.Fatalf("single punch error=%v", err)
	}
	if _, err := adapter.Batch(ctx, "tenant", nil); !errors.Is(err, clockservice.ErrUnavailable) {
		t.Fatalf("batch error=%v", err)
	}
}

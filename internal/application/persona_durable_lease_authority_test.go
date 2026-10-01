package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type personaDurableStepStoreFake struct {
	tenant uuid.UUID
	lease  string
	step   string
	at     time.Time
	ctx    context.Context
	err    error
	calls  int
}

func (f *personaDurableStepStoreFake) RunPersonaSecurityStep(ctx context.Context, tenant uuid.UUID, lease, step string, at time.Time, work func(context.Context) error) error {
	f.calls++
	f.tenant, f.lease, f.step, f.at, f.ctx = tenant, lease, step, at, ctx
	if f.err != nil {
		return f.err
	}
	return work(ctx)
}

type personaLeaseFenceFake struct {
	boundRun   agentsecurity.PersonaRunID
	boundLease agentsecurity.KillSwitchLeaseID
	steps      int
	err        error
}

func (f *personaLeaseFenceFake) Bind(run agentsecurity.PersonaRunID, lease agentsecurity.KillSwitchLeaseID) error {
	f.boundRun, f.boundLease = run, lease
	return f.err
}

func (f *personaLeaseFenceFake) RunStep(_ agentsecurity.PersonaRunID, work func() error) (agentsecurity.Fallback, error) {
	f.steps++
	if f.err != nil {
		return agentsecurity.Fallback{}, f.err
	}
	return agentsecurity.Fallback{}, work()
}

func TestDatabasePersonaRunSecurityFence_BindsAndRunsDurableTenantStep(t *testing.T) {
	tenantID := uuid.MustParse("8f6f8477-f4d2-4ba6-8d3e-493277638ef0")
	store := &personaDurableStepStoreFake{}
	leaseFence := &personaLeaseFenceFake{}
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	fence, err := NewDatabasePersonaRunSecurityFence(DatabasePersonaRunSecurityFenceConfig{
		Steps: store, LeaseFence: leaseFence,
		TenantUUID: func(tenant values.TenantId) uuid.UUID {
			if tenant == "tenant-a" {
				return tenantID
			}
			return uuid.Nil
		},
		Now: func() time.Time { return at },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fence.Bind("run-a", "tenant-a", "lease-a"); err != nil {
		t.Fatal(err)
	}
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "request-context")
	called := false
	if _, err := fence.RunStep(ctx, "run-a", "persona-model-e4a12c", func(stepCtx context.Context) error {
		called = stepCtx.Value(contextKey{}) == "request-context"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !called || store.calls != 1 || store.tenant != tenantID || store.lease != "lease-a" || store.step != "persona-model-e4a12c" || !store.at.Equal(at) || store.ctx != ctx {
		t.Fatalf("callback=%v store calls=%d tenant=%s lease=%q step=%q at=%s context-preserved=%v", called, store.calls, store.tenant, store.lease, store.step, store.at, store.ctx == ctx)
	}
	if leaseFence.boundRun != "run-a" || leaseFence.boundLease != "lease-a" || leaseFence.steps != 1 {
		t.Fatalf("process fence run=%q lease=%q steps=%d", leaseFence.boundRun, leaseFence.boundLease, leaseFence.steps)
	}
}

func TestDatabasePersonaRunSecurityFence_DoesNotRunAfterDurableDenial(t *testing.T) {
	denied := errors.New("revoked lease")
	store := &personaDurableStepStoreFake{err: denied}
	fence, err := NewDatabasePersonaRunSecurityFence(DatabasePersonaRunSecurityFenceConfig{
		Steps: store, LeaseFence: &personaLeaseFenceFake{},
		TenantUUID: func(values.TenantId) uuid.UUID { return uuid.MustParse("8f6f8477-f4d2-4ba6-8d3e-493277638ef0") },
		Now:        func() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fence.Bind("run-a", "tenant-a", "lease-a"); err != nil {
		t.Fatal(err)
	}
	called := false
	_, err = fence.RunStep(context.Background(), "run-a", "persona-context-b9f201", func(context.Context) error {
		called = true
		return nil
	})
	if !errors.Is(err, denied) || called || store.calls != 1 {
		t.Fatalf("error=%v callback=%v store calls=%d, want durable denial before callback", err, called, store.calls)
	}
}

func TestDatabasePersonaRunSecurityFence_RejectsInvalidTenantAndUnknownRun(t *testing.T) {
	fence, err := NewDatabasePersonaRunSecurityFence(DatabasePersonaRunSecurityFenceConfig{
		Steps: &personaDurableStepStoreFake{}, LeaseFence: &personaLeaseFenceFake{},
		TenantUUID: func(values.TenantId) uuid.UUID { return uuid.Nil }, Now: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fence.Bind("run-a", "tenant-a", "lease-a"); !errors.Is(err, errPersonaDurableSecurityFence) {
		t.Fatalf("invalid tenant bind error=%v", err)
	}
	called := false
	if _, err := fence.RunStep(context.Background(), "run-a", "step-a", func(context.Context) error { called = true; return nil }); !errors.Is(err, errPersonaDurableSecurityFence) || called {
		t.Fatalf("unknown run error=%v callback=%v", err, called)
	}
}

package agentrunstate

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// The failure gate, owner and location chosen by the executor must survive a
// save and a reload; before this the columns existed and the store never
// wrote or read them, so every stored failure lost its cause.
func TestTodo_AGENTUX_036_Integration_FailureFieldsPersist(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatalf("agent migrations: %v", err)
	}
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	tenantID := uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES ($1)`, tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	store := openAgentStore(t, db)
	defer store.Close()
	repository, err := New(store, func(tenant string) uuid.UUID {
		if tenant == "tenant-a" {
			return tenantID
		}
		return uuid.Nil
	})
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := repository.ForTenant("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	admission := testAcceptedAdmission(now)
	if err := insertAcceptedAdmission(ctx, store, tenantID, admission); err != nil {
		t.Fatalf("insert accepted admission fixture: %v", err)
	}
	service, err := runstate.New(tenant, allowRecheck{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.Start(ctx, admission)
	if err != nil {
		t.Fatalf("persist admitted run: %v", err)
	}
	created, err := tenant.Get(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if created.FailureGate != "" || created.FailureOwner != "" || created.FailureLocation != "" {
		t.Fatalf("a new run carries failure fields: %+v", created)
	}
	next := created
	next.Version++
	next.UpdatedAt = now.Add(time.Second)
	next.FailureGate, next.FailureOwner, next.FailureLocation = string(runstate.FailureGateAuthority), "application", "persona_run_executor.go:1"
	if err := tenant.Save(ctx, next, created.Version); err != nil {
		t.Fatalf("save failure fields: %v", err)
	}
	reloaded, err := tenant.Get(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.FailureGate != next.FailureGate || reloaded.FailureOwner != next.FailureOwner || reloaded.FailureLocation != next.FailureLocation {
		t.Fatalf("failure fields were not persisted: gate=%q owner=%q location=%q", reloaded.FailureGate, reloaded.FailureOwner, reloaded.FailureLocation)
	}
	bad := reloaded
	bad.Version++
	bad.FailureGate = "the user's question text"
	if err := tenant.Save(ctx, bad, reloaded.Version); err == nil {
		t.Fatal("a failure gate outside the closed list was stored")
	}
}

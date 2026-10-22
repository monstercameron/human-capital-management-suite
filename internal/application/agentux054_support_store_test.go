package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// The desk on the real agent database: the inbox receipts, the plan receipts and
// the effect reservations are the product's own tables, the objects are real
// files, and only the project service and the incident channel are fakes. The
// seeded inbox waits unprocessed; simulating the same email later is a replay
// that makes its ticket once.
func TestTodo_AGENTUX_054_SupportStore_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	agents := commonAgentOpenIntegrationStore(t, db)
	mapper := tenantKeyMapper[values.TenantId](pgstore.TenantID)
	tenantID := mapper(localAgentDemoTenant)
	db.Exec(t, "INSERT INTO tenant(tenant_id) VALUES($1)", tenantID)
	inbox, err := agentstore.NewSupportInboxStore(agents)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	admin := mustAdmin(t, now)
	adminCtx := trust.WithPrincipal(ctx, admin)
	objects := AgentUXDemoLocalSupportObjects{Root: t.TempDir()}
	seed := AgentUXDemoInboxSurface{Inbox: inbox, Objects: objects, Queue: AgentUXDemoSupportDeferredQueue{}, Owners: AgentUXDemoSupportOwner{}, TenantUUID: mapper, Now: func() time.Time { return now }}
	for run := 0; run < 2; run++ {
		receipts, err := AgentUXDemoSeedSupportEmails(adminCtx, seed)
		if err != nil || len(receipts) != 5 {
			t.Fatalf("run %d: %v receipts=%d", run, err, len(receipts))
		}
		for _, receipt := range receipts {
			if receipt.Replayed != (run == 1) {
				t.Fatalf("run %d receipt %+v", run, receipt)
			}
		}
	}

	world, _ := newAgentUXSupportDeskWorld(t)
	world.tenantID = tenantID
	world.now = now
	desk := &AgentUXDemoSupportDesk{Inbox: inbox, Journal: inbox, Objects: objects, Planner: AgentUXDemoLocalSupportPlanner{}, Projects: world, Alerts: world, Targets: world, Now: func() time.Time { return now }}
	simulate := AgentUXDemoInboxSurface{Inbox: inbox, Objects: objects, Queue: AgentUXDemoSupportQueue{Desk: desk}, Owners: AgentUXDemoSupportOwner{}, TenantUUID: mapper, Now: func() time.Time { return now }}
	email := AgentUXDemoSupportEmails()[2]
	for run := 0; run < 2; run++ {
		receipt, err := simulate.SimulateCustomerEmail(adminCtx, email)
		if err != nil || !receipt.Replayed || receipt.State != "QUEUED" {
			t.Fatalf("simulation %d: %+v %v", run, receipt, err)
		}
	}
	if world.effects["ticket"] != 1 || world.effects["alert"] != 1 {
		t.Fatalf("the seeded email made %v effects, want one ticket and one alert", world.effects)
	}
	for _, task := range world.tasks {
		if task.Priority != "HIGH" || task.Title != "The service is down" {
			t.Fatalf("ticket %+v", task)
		}
	}
	// A different tenant's identifier is not this tenant's email.
	if _, err := inbox.Get(ctx, uuid.New(), "support-email:"+uuid.NewString()); err == nil {
		t.Fatal("an unknown tenant read the inbox")
	}
}

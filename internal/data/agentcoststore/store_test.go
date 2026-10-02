package agentcoststore

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentcost"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

type tenantRunner struct{ db dbport.Beginner }

func (r tenantRunner) RunTenantTx(ctx context.Context, tenant uuid.UUID, fn func(dbport.Tx) error) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type owners struct{ store *Store }

func (o owners) IsOwner(tenant, agent, actor string) bool {
	return o.store.IsBusinessOwner(tenant, agent, actor)
}
func (o owners) IsAdmin(tenant, actor string) bool { return actor == "admin" }

// The limits, the audit rows and the run ledger are written by the runtime role
// only inside the tenant, one tenant never reads another's, and a restart (a new
// gate over the same database) refuses what the old gate would have refused.
func TestTodo_AGENTCOST_006_StoreIntegration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenant, other := uuid.New(), uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES($1),($2)`, tenant, other); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO persona_owners(tenant_id,persona_id,owner_role,principal_id,assigned_by,assigned_at) VALUES($1,'policy','BUSINESS_OWNER','olive','admin',now()),($1,'policy','TECHNICAL_STEWARD','sam','admin',now())`, tenant); err != nil {
		t.Fatal(err)
	}
	appPool, err := pgxadapter.NewPool(ctx, db.URL, map[string]string{"search_path": db.Schema, "role": agentstore.AppRole})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(appPool.Close)
	mapper := func(key string) uuid.UUID {
		switch key {
		case "t1":
			return tenant
		case "t2":
			return other
		}
		return uuid.Nil
	}
	store, err := New(tenantRunner{db: appPool}, mapper)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	gate, err := agentcost.NewGate(owners{store}, nil, time.UTC, clock)
	if err != nil {
		t.Fatal(err)
	}
	gate.WithStore(store)
	ledger := (&agentcost.Ledger{}).WithStore(store)
	meter := agentcost.Meter{Gate: gate, Ledger: ledger}

	if err := gate.Set("sam", agentcost.Limit{TenantID: "t1", AgentID: "policy", MaxRuns: 3}); err == nil {
		t.Fatal("the technical steward set a limit that only the business owner may")
	}
	if err := gate.Set("olive", agentcost.Limit{TenantID: "t1", AgentID: "policy", MaxRuns: 2, MaxSpendMicros: 1_000_000}); err != nil {
		t.Fatal(err)
	}
	subject := agentcost.Subject{TenantID: "t1", AgentID: "policy", AgentName: "Policy Helper", ConversationID: "general", ConversationLabel: "#general", EstimateMicros: 10_000}
	for _, id := range []string{"run-1", "run-2"} {
		if decision := meter.Admit(subject); !decision.Allowed {
			t.Fatalf("%s refused: %+v", id, decision)
		}
		if err := meter.Finish(subject, agentcost.Run{TenantID: "t1", AgentID: "policy", ConversationID: "general", RunID: id, At: now, Kind: agentcost.KindAnswer, SpendMicros: 40_000, Answered: true}, "olive"); err != nil {
			t.Fatal(err)
		}
	}
	// A repeat of a finished run's id is not counted twice.
	if err := ledger.Append(agentcost.Run{TenantID: "t1", AgentID: "policy", ConversationID: "general", RunID: "run-2", At: now, Kind: agentcost.KindAnswer, SpendMicros: 40_000}); err != nil {
		t.Fatal(err)
	}
	if runs, micros, err := store.Usage("t1", "policy", "", now.Truncate(24*time.Hour)); err != nil || runs != 2 || micros != 80_000 {
		t.Fatalf("usage = %d %d %v", runs, micros, err)
	}

	// A new gate over the same database (a restart) refuses the third run with
	// the sentence the asker sees.
	restarted, _ := agentcost.NewGate(owners{store}, nil, time.UTC, clock)
	restarted.WithStore(store)
	decision := restarted.Admit(subject)
	if decision.Allowed || decision.Reached != "Policy Helper reached today's limit. It resets at 00:00." {
		t.Fatalf("after a restart: %+v", decision)
	}
	// Lowering is audited with both sides; the trail survives the restart.
	if err := restarted.Set("admin", agentcost.Limit{TenantID: "t1", AgentID: "policy", MaxRuns: 1, MaxSpendMicros: 500_000}); err != nil {
		t.Fatalf("an administrator could not lower the limit: %v", err)
	}
	if err := restarted.Set("admin", agentcost.Limit{TenantID: "t1", AgentID: "policy", MaxRuns: 9, MaxSpendMicros: 500_000}); err == nil {
		t.Fatal("an administrator raised a limit")
	}
	trail, err := restarted.AuditTrail("t1", "policy", "olive")
	if err != nil || len(trail) != 2 || trail[1].Actor != "admin" || trail[1].Before == nil || trail[1].Before.MaxRuns != 2 || trail[1].After == nil || trail[1].After.MaxRuns != 1 {
		t.Fatalf("audit = %+v %v", trail, err)
	}

	// The report counts only the viewer's agents, from the stored ledger.
	if err := ledger.Append(agentcost.Run{TenantID: "t1", AgentID: "benefits", RunID: "run-b", At: now, Kind: agentcost.KindScreening, SpendMicros: 999_000}); err != nil {
		t.Fatal(err)
	}
	report, err := ledger.ReportFor(owners{store}, "t1", "olive", now, time.UTC)
	if err != nil || len(report.RecentRuns) != 2 || report.MonthToDateMicros != 80_000 {
		t.Fatalf("report = %+v %v", report, err)
	}

	// Tenant rules: another tenant sees none of it, and cannot write into this
	// one's rows; the ledger and the audit cannot be rewritten.
	if limits, err := store.LoadLimits("t2"); err != nil || len(limits) != 0 {
		t.Fatalf("t2 limits = %+v %v", limits, err)
	}
	if runs, err := store.RunsSince("t2", now.Add(-time.Hour)); err != nil || len(runs) != 0 {
		t.Fatalf("t2 runs = %+v %v", runs, err)
	}
	if trail, err := store.LimitAudit("t2", "policy"); err != nil || len(trail) != 0 {
		t.Fatalf("t2 audit = %+v %v", trail, err)
	}
	for _, statement := range []string{
		`UPDATE agent_run_costs SET spend_micros=0`,
		`DELETE FROM agent_run_costs`,
		`UPDATE agent_spend_limit_audit SET actor='someone'`,
		`DELETE FROM agent_spend_limits`,
	} {
		err := tenantRunner{db: appPool}.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
			_, execErr := tx.Exec(ctx, statement)
			return execErr
		})
		if err == nil {
			t.Fatalf("the runtime role could run %q", statement)
		}
	}
	err = tenantRunner{db: appPool}.RunTenantTx(ctx, other, func(tx dbport.Tx) error {
		_, execErr := tx.Exec(ctx, `INSERT INTO agent_run_costs (tenant_id,agent_id,run_id,at,kind,spend_micros) VALUES ($1,'policy','x',now(),'answer',1)`, tenant)
		return execErr
	})
	if err == nil || !strings.Contains(err.Error(), "row-level security") {
		t.Fatalf("a run was written into another tenant: %v", err)
	}
}

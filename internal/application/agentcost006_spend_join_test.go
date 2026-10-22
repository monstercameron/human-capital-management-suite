package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/ownerops"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_AGENTCOST_006_SpendJoinOnRealRows runs the owner's run list against a
// real PostgreSQL with real run, request and cost rows, as the runtime role. A
// run's spend is the sum of the cost rows whose run id is the run's id, or the
// admission request's id, followed by a slash and a step; a run whose id merely
// starts with the same letters, a cost row with no step, and another tenant's
// row with the same id are not added. The join had only been written, never run.
func TestTodo_AGENTCOST_006_SpendJoinOnRealRows(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenant := values.TenantId("tenant-a")
	tenantID, otherID := uuid.New(), uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1),($2)`, tenantID, otherID)
	agents := commonAgentOpenIntegrationStore(t, db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	createdAt, deadline, leaseUntil, updatedAt := now.Add(-2*time.Hour), now.Add(-time.Hour), now.Add(-90*time.Minute), now.Add(-45*time.Minute)
	digest := func(letter string) string { return strings.Repeat(letter, 64) }
	seed := func(tx dbport.Tx, tenantID uuid.UUID) error {
		inserts := []struct {
			statement string
			args      []any
		}{
			{`INSERT INTO persona_versions(tenant_id,persona_id,version,agent_version,handle,display_name,profile,content_digest,created_at) VALUES($1,'policy-helper',6,'agent.policy@6','policy-helper','Policy Helper','{}','sha256:` + digest("a") + `',$2)`, []any{tenantID, createdAt}},
			{`INSERT INTO persona_owners(tenant_id,persona_id,owner_role,principal_id,assigned_by,assigned_at) VALUES($1,'policy-helper','BUSINESS_OWNER','owner-a','system',$2)`, []any{tenantID, createdAt}},
			{`INSERT INTO persona_invocations(tenant_id,invocation_id,conversation_id,thread_id,post_id,invoker_id,persona_id,persona_version,installation_id,mode,skills,actor,owner_id,admission_id,run_id,task_id,context_digest,state,created_at,started_at) VALUES($1,'invoke-a','direct-a','thread-a','post-a','walt','policy-helper','6','install-a','ON_BEHALF_OF','{}','{}','owner-a','request-a','run-a','task-a','sha256:` + digest("b") + `','STARTED',$2,$2)`, []any{tenantID, createdAt}},
			{`INSERT INTO agent_run_request(tenant_id,request_id,source_kind,source_key_digest,source_ref,request_digest,request_payload,decision,authority_snapshot,admitted_at,deadline,agent_id,agent_version,agent_digest,installation_id,principal_chain,purpose,audience,context_scope,budget,cause_id) VALUES($1,'request-a','PERSONA_MENTION','` + digest("c") + `','post-a','` + digest("d") + `','{}','ACCEPTED','{}',$2,$3,'policy-helper','6','sha256:` + digest("e") + `','install-a','{"mode":"ON_BEHALF_OF"}','answer policy','[]','[]','{}','invoke-a')`, []any{tenantID, createdAt, deadline}},
			{`INSERT INTO agent_run_execution(tenant_id,run_id,admission_id,request_digest,agent_id,agent_version,agent_digest,context_digest,deadline,state,revision,fence,lease_owner,lease_until,created_at,updated_at) VALUES($1,'run-a','request-a','` + digest("d") + `','policy-helper','6','sha256:` + digest("e") + `','sha256:` + digest("b") + `',$2,'RUNNING',4,1,'worker-a',$3,$4,$5)`, []any{tenantID, deadline, leaseUntil, createdAt, updatedAt}},
		}
		for _, insert := range inserts {
			if _, err := tx.Exec(ctx, insert.statement, insert.args...); err != nil {
				return err
			}
		}
		return nil
	}
	cost := func(tx dbport.Tx, tenantID uuid.UUID, runID string, micros int64) error {
		_, err := tx.Exec(ctx, `INSERT INTO agent_run_costs (tenant_id,agent_id,conversation_id,run_id,at,kind,spend_micros,answered,messages_read) VALUES ($1,'policy-helper','direct-a',$2,$3,'answer',$4,true,0)`, tenantID, runID, now.Add(-time.Hour), micros)
		return err
	}
	if err := agents.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := seed(tx, tenantID); err != nil {
			return err
		}
		for run, micros := range map[string]int64{
			"run-a/persona-model-1":     600_000, // the run's own id, then a step
			"request-a/persona-model-2": 150_000, // the admission request's id, then a step
			"run-ab/persona-model-1":    999_999, // a different run that starts with the same letters
			"run-a":                     888_888, // no step: not a step of the run
			"xrun-a/persona-model-1":    777_777, // the run's id inside another's
		} {
			if err := cost(tx, tenantID, run, micros); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Another tenant has a cost row under the very same run id.
	if err := agents.RunTenantTx(ctx, otherID, func(tx dbport.Tx) error { return cost(tx, otherID, "run-a/persona-model-1", 555_555) }); err != nil {
		t.Fatal(err)
	}
	// And the runtime role cannot rewrite a cost once written.
	if err := agents.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE agent_run_costs SET spend_micros=0 WHERE tenant_id=$1`, tenantID)
		return err
	}); err == nil {
		t.Fatal("the runtime role rewrote a cost row")
	}

	source := AgentPersonaRunSource{
		DB: agents,
		TenantUUID: func(value values.TenantId) uuid.UUID {
			switch value {
			case tenant:
				return tenantID
			case "tenant-b":
				return otherID
			}
			return uuid.Nil
		},
		Directory: agentUXR6OpsDirectory{"owner-a": "Maya Chen", "walt": "Walt Brennan"},
	}
	rows, err := source.DashboardProjection(ctx, agentUXR6OpsPrincipal(t, tenant, "owner-a"), ownerops.AudienceOwner)
	if err != nil || len(rows) != 1 || rows[0].View.TaskID != "run-a" {
		t.Fatalf("owner rows = %+v, %v", rows, err)
	}
	if got, want := rows[0].View.SpendMicros, int64(750_000); got != want {
		t.Fatalf("the run's spend = %d micro-dollars, want %d (its own steps and its request's, nothing else)", got, want)
	}
	// The person who asked sees the same figure for their own run.
	asked, err := source.DashboardProjection(ctx, agentUXR6OpsPrincipal(t, tenant, "walt"), ownerops.AudienceMember)
	if err != nil || len(asked) != 1 || asked[0].View.SpendMicros != 750_000 {
		t.Fatalf("invoker rows = %+v, %v", asked, err)
	}
	// A run with no cost rows is zero, not an error and not another run's spend.
	if err := agents.RunTenantTx(ctx, otherID, func(tx dbport.Tx) error { return seed(tx, otherID) }); err != nil {
		t.Fatal(err)
	}
	other, err := source.DashboardProjection(ctx, agentUXR6OpsPrincipal(t, "tenant-b", "owner-a"), ownerops.AudienceOwner)
	if err != nil || len(other) != 1 || other[0].View.SpendMicros != 555_555 {
		t.Fatalf("the other tenant's run = %+v, %v; want only its own cost row", other, err)
	}
}

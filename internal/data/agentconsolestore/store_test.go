package agentconsolestore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/agentconnect"
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

type admins map[string]bool

func (a admins) CanAdminister(tenant, actor string) bool { return a[tenant+"/"+actor] }

type livePublisher struct{ published []agentaccess.Revision }

func (p *livePublisher) Publish(revision agentaccess.Revision) error {
	p.published = append(p.published, revision)
	return nil
}

// The console keeps its revisions in the database: a restart shows the same
// revisions, a published revision cannot be rewritten even by the runtime role,
// another tenant sees nothing, and the audit trail survives.
func TestTodo_AGENT2_019_StoreIntegration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenant, other := uuid.New(), uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES($1),($2)`, tenant, other); err != nil {
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
	clock := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	allowed := admins{"t1/ada": true, "t1/ben": true, "t2/ada": true}
	newConsole := func() (*agentaccess.Console, *livePublisher) {
		publisher := &livePublisher{}
		console, err := agentaccess.NewConsole(allowed, publisher, nil, func() time.Time { return clock })
		if err != nil {
			t.Fatal(err)
		}
		return console.WithStore(store), publisher
	}
	console, publisher := newConsole()
	draft, err := console.Create("t1", "ada", agentaccess.Revision{
		ConnectionID: "hris", Provider: "HRIS", CredentialMode: agentconnect.UserDelegated,
		Skills: []agentaccess.Skill{{ID: "read-profile", Tier: agentconnect.TierT0}, {ID: "change-salary", Tier: agentconnect.TierT3}},
		Grants: []agentconnect.GrantScope{{ID: "g1", Roles: []string{"manager"}, Population: "direct-reports", OrganizationScopes: []string{"*"}, Skills: []string{"read-profile", "change-salary"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := console.Publish("t1", "ada", draft.ID()); err == nil {
		t.Fatal("a T3 grant was published by one administrator")
	}
	if _, err := console.RequestApproval("t1", "ada", draft.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := console.Approve("t1", "ada", draft.ID(), true); err == nil {
		t.Fatal("the requester approved their own revision")
	}
	if _, err := console.Approve("t1", "ben", draft.ID(), true); err != nil {
		t.Fatal(err)
	}
	if _, err := console.Publish("t1", "ben", draft.ID()); err != nil {
		t.Fatal(err)
	}
	if len(publisher.published) != 1 {
		t.Fatalf("published %d times", len(publisher.published))
	}

	// A restart: a new console over the same database sees the same revision in
	// the same state, with the approval that covers its content.
	restarted, _ := newConsole()
	list, err := restarted.Revisions("t1", "ada")
	if err != nil || len(list) != 1 || list[0].Status != agentaccess.StatusPublished || list[0].ApprovedBy != "ben" || list[0].RequestedBy != "ada" || !list[0].StepUp || list[0].Digest != draft.Digest || len(list[0].Skills) != 2 || len(list[0].Grants) != 1 {
		t.Fatalf("after a restart: %+v %v", list, err)
	}
	// A second revision, then a rollback to the first: history only grows.
	second, err := restarted.Create("t1", "ada", agentaccess.Revision{ConnectionID: "hris", Provider: "HRIS", CredentialMode: agentconnect.UserDelegated,
		Skills: []agentaccess.Skill{{ID: "read-profile", Tier: agentconnect.TierT0}},
		Grants: []agentconnect.GrantScope{{ID: "g1", Roles: []string{"*"}, Population: "*", OrganizationScopes: []string{"*"}, Skills: []string{"read-profile"}}}})
	if err != nil || second.Number != 2 {
		t.Fatalf("second revision = %+v %v", second, err)
	}
	if _, err := restarted.Publish("t1", "ada", second.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Rollback("t1", "ben", draft.ID()); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	again, _ := newConsole()
	list, _ = again.Revisions("t1", "ada")
	if len(list) != 3 || list[0].Number != 3 || list[0].Status != agentaccess.StatusPublished || list[1].Status != agentaccess.StatusSuperseded || list[2].Status != agentaccess.StatusSuperseded {
		t.Fatalf("history = %+v", list)
	}
	trail := again.AuditTrail("t1")
	actions := ""
	for _, event := range trail {
		actions += event.Actor + ":" + event.Action + " "
	}
	if want := "ada:create ada:request-approval ben:approve ben:publish ada:create ada:publish ben:rollback "; actions != want {
		t.Fatalf("audit = %q, want %q", actions, want)
	}

	// Tenant rules, and a published revision is history even for the runtime role.
	if other, err := store.LoadRevisions("t2"); err != nil || len(other) != 0 {
		t.Fatalf("t2 sees %+v %v", other, err)
	}
	if events, err := store.Audit("t2"); err != nil || len(events) != 0 {
		t.Fatalf("t2 audit %+v %v", events, err)
	}
	for _, statement := range []string{
		`UPDATE agent_connection_drafts SET body='{"provider":"x"}'::jsonb WHERE number=1`,
		`UPDATE agent_connection_drafts SET status='DRAFT' WHERE number=1`,
		`DELETE FROM agent_connection_drafts`,
		`UPDATE agent_connection_console_audit SET actor='x'`,
	} {
		err := tenantRunner{db: appPool}.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
			_, execErr := tx.Exec(ctx, statement)
			return execErr
		})
		if err == nil {
			t.Fatalf("the runtime role could run %q", statement)
		}
	}
	if err := restartedWrite(store, "t2", clock); err == nil {
		t.Fatal("a revision was written for a tenant the caller's rows do not cover")
	}
}

// restartedWrite writes into tenant t2 through t1's identity by naming t1's
// revision under t2: the store binds the transaction to the named tenant and
// refuses a revision that belongs to another.
func restartedWrite(store *Store, tenant string, at time.Time) error {
	revision := agentaccess.Revision{TenantID: "t1", ConnectionID: "hris", Number: 9, Status: agentaccess.StatusDraft, CreatedBy: "ada", Digest: "sha256:x", CredentialMode: agentconnect.UserDelegated}
	return store.SaveRevisions(tenant, at, agentaccess.AuditEvent{At: at, Tenant: tenant, Actor: "ada", Action: "create", Revision: "hris#9"}, []agentaccess.Revision{revision})
}

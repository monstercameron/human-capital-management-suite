package agentstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

type agentuxDemoGrantOwner struct{ db dbport.Beginner }

func (s agentuxDemoGrantOwner) RunTenantTx(ctx context.Context, tenant uuid.UUID, fn func(dbport.Tx) error) error {
	tx, err := s.db.Begin(ctx)
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

func TestAgentUXDemo_ProjectGrant_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenant, other := uuid.New(), uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES($1),($2)`, tenant, other); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxadapter.NewPool(ctx, db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	owner, err := NewSupportInboxStore(agentuxDemoGrantOwner{db: pool})
	if err != nil {
		t.Fatal(err)
	}
	r := SupportProjectGrant{TenantID: tenant, ServiceSubject: "service:support-desk", ProjectID: "customer-support", Revision: 1, Active: true, GrantedBy: "owner", RecordedAt: time.Now().UTC()}
	if err := owner.AppendSupportProjectGrant(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := owner.AppendSupportProjectGrant(ctx, r); !errors.Is(err, ErrSupportInboxReplay) {
		t.Fatalf("grant replay: %v", err)
	}
	for _, tc := range []struct {
		tenant           uuid.UUID
		subject, project string
		allowed          bool
	}{{tenant, r.ServiceSubject, r.ProjectID, true}, {other, r.ServiceSubject, r.ProjectID, false}, {tenant, "service:other", r.ProjectID, false}, {tenant, r.ServiceSubject, "another-project", false}} {
		allowed, err := owner.SupportProjectCreateAllowed(ctx, tc.tenant, tc.subject, tc.project)
		if err != nil || allowed != tc.allowed {
			t.Fatalf("scoped grant: %+v %v %v", tc, allowed, err)
		}
	}
	r.Revision++
	r.Active = false
	if err := owner.AppendSupportProjectGrant(ctx, r); err != nil {
		t.Fatal(err)
	}
	if allowed, err := owner.SupportProjectCreateAllowed(ctx, tenant, r.ServiceSubject, r.ProjectID); err != nil || allowed {
		t.Fatal("revocation ignored")
	}
	appPool, err := pgxadapter.NewPool(ctx, db.URL, map[string]string{"search_path": db.Schema, "role": AppRole})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(appPool.Close)
	app, _ := NewSupportInboxStore(agentuxDemoGrantOwner{db: appPool})
	r.Revision++
	r.Active = true
	if err := app.AppendSupportProjectGrant(ctx, r); err == nil {
		t.Fatal("serving agent self-granted authority")
	}
	if err := (agentuxDemoGrantOwner{db: appPool}).RunTenantTx(ctx, other, func(tx dbport.Tx) error {
		var n int
		err := tx.QueryRow(ctx, `SELECT count(*) FROM support_project_grant WHERE tenant_id=$1`, tenant).Scan(&n)
		if n != 0 {
			t.Error("grant RLS leaked")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.ExecContext(ctx, `UPDATE support_project_grant SET active=true WHERE tenant_id=$1`, tenant); err == nil {
		t.Fatal("grant history mutable")
	}
}

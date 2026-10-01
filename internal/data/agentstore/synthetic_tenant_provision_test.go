package agentstore

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestTodo_AGENT2_025_SyntheticProvisionRLSRevocationRestart(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := Migrate(ctx, db.SQL); err != nil {
		t.Fatalf("apply agent migrations: %v", err)
	}
	tenantA, tenantB := uuid.New(), uuid.New()
	for _, tenantID := range []uuid.UUID{tenantA, tenantB} {
		if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES ($1)`, tenantID); err != nil {
			t.Fatalf("seed tenant projection: %v", err)
		}
	}
	agentLogin, issuerLogin := roleName("synthetic_reader"), roleName("synthetic_issuer")
	for _, tc := range []struct{ login, role string }{{agentLogin, AppRole}, {issuerLogin, SyntheticTenantProvisionerRole}} {
		if _, err := db.SQL.ExecContext(ctx, fmt.Sprintf("CREATE ROLE %s LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS PASSWORD 'test-password'", tc.login)); err != nil {
			t.Fatalf("create scoped login: %v", err)
		}
		if _, err := db.SQL.ExecContext(ctx, "GRANT "+tc.role+" TO "+tc.login); err != nil {
			t.Fatalf("grant scoped role: %v", err)
		}
		login := tc.login
		t.Cleanup(func() {
			if _, err := db.SQL.ExecContext(context.Background(), "DROP ROLE "+login); err != nil {
				t.Errorf("drop scoped login: %v", err)
			}
		})
	}
	password := "test-password"
	agentDSN := testDSN(t, db.URL, db.Schema, agentLogin, password, "postgres")
	issuerDSN := testDSN(t, db.URL, db.Schema, issuerLogin, password, "postgres")
	coreDSN := testDSN(t, db.URL, "", "core", "unused", "core")
	reader, err := New(ctx, Config{DSN: agentDSN, CoreDSN: coreDSN, MaxConns: 1})
	if err != nil {
		t.Fatalf("open read-only agent pool: %v", err)
	}
	t.Cleanup(reader.Close)
	issuer, err := NewSyntheticTenantProvisionIssuerStore(ctx, SyntheticTenantProvisionIssuerConfig{
		DSN: issuerDSN, AgentDSN: agentDSN, CoreDSN: coreDSN, MaxConns: 1,
	})
	if err != nil {
		t.Fatalf("open evaluator issuer pool: %v", err)
	}
	t.Cleanup(issuer.Close)
	now := time.Date(2026, 9, 30, 16, 0, 0, 0, time.UTC)
	provision := func(tenantID uuid.UUID, suffix string) SyntheticTenantProvisionRecord {
		return SyntheticTenantProvisionRecord{
			TenantID: tenantID, SuiteTenantID: "synthetic-" + suffix, ProvisionID: "provision-" + suffix, MarkerID: "marker-" + suffix,
			Purpose: "agent-evaluation", Status: "ACTIVE", IssuedAt: now, ExpiresAt: now.Add(time.Hour),
			GrantStoreID: "grant-" + suffix, TaskStoreID: "task-" + suffix, BudgetLedgerID: "budget-" + suffix,
			AuditStoreID: "audit-" + suffix, ToolOwnerID: "tool-owner-" + suffix, ToolOwnerProfile: "fixture-only/v1",
		}
	}
	issuedA, issuedB := provision(tenantA, "a"), provision(tenantB, "b")
	for _, record := range []SyntheticTenantProvisionRecord{issuedA, issuedB} {
		if err := issuer.IssueSyntheticTenantProvision(ctx, SyntheticTenantProvisionIssue{Record: record, ActorID: "evaluation-service"}); err != nil {
			t.Fatalf("issue explicit synthetic provision: %v", err)
		}
	}
	if err := reader.RunTenantTx(ctx, tenantA, func(tx dbport.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM synthetic_tenant_provision`).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("tenant A sees %d marker rows, want 1", count)
		}
		return nil
	}); err != nil {
		t.Fatalf("enforce marker RLS: %v", err)
	}
	if err := reader.RunTenantTx(ctx, tenantA, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO synthetic_tenant_provision
			(tenant_id,provision_id,marker_id,purpose,status,expires_at,grant_store_id,task_store_id,budget_ledger_id,
			 audit_store_id,tool_owner_id,tool_owner_profile,issued_at)
			VALUES ($1,'forged','forged','agent-evaluation','ACTIVE',$2,'g','t','b','a','o','fixture-only/v1',$3)`,
			tenantA, now.Add(time.Hour), now); err == nil {
			return errors.New("agent application role issued a synthetic tenant marker")
		} else {
			return err
		}
	}); err == nil {
		t.Fatal("agent application role issued a synthetic tenant marker")
	}
	if _, err := reader.SyntheticTenantProvision(ctx, tenantB, issuedB.SuiteTenantID); err != nil {
		t.Fatalf("tenant B cannot read its marker: %v", err)
	}
	if _, err := reader.SyntheticTenantProvision(ctx, tenantA, issuedB.SuiteTenantID); !errors.Is(err, ErrSyntheticProvisionAbsent) {
		t.Fatalf("tenant A alias lookup returned another suite marker: %v", err)
	}
	if err := issuer.RevokeSyntheticTenantProvision(ctx, tenantA, issuedA.ProvisionID, "evaluation-service", "test revocation", now.Add(time.Minute)); err != nil {
		t.Fatalf("revoke synthetic marker: %v", err)
	}
	if err := reader.RunTenantTx(ctx, tenantA, func(tx dbport.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM synthetic_tenant_provision_audit WHERE provision_id=$1`, issuedA.ProvisionID).Scan(&count); err != nil {
			return err
		}
		if count != 2 {
			return fmt.Errorf("tenant A sees %d audit facts after revocation, want issuance and revocation", count)
		}
		return nil
	}); err != nil {
		t.Fatalf("verify immutable provision audit chain: %v", err)
	}
	if err := issuer.RevokeSyntheticTenantProvision(ctx, tenantA, issuedA.ProvisionID, "evaluation-service", "duplicate revocation", now.Add(2*time.Minute)); !errors.Is(err, ErrSyntheticProvisionRevoked) {
		t.Fatalf("second revocation = %v, want revoked error", err)
	}
	reader.Close()
	reader, err = New(ctx, Config{DSN: agentDSN, CoreDSN: coreDSN, MaxConns: 1})
	if err != nil {
		t.Fatalf("reopen agent pool: %v", err)
	}
	reopened, err := reader.SyntheticTenantProvision(ctx, tenantA, issuedA.SuiteTenantID)
	if err != nil || reopened.Status != "REVOKED" || reopened.RevokedAt == nil || reopened.RevokeReason != "test revocation" {
		t.Fatalf("reopened revoked provision = %+v, %v", reopened, err)
	}
	if reopened.MarkerID != issuedA.MarkerID || reopened.ToolOwnerID != issuedA.ToolOwnerID {
		t.Fatalf("reopened provision lost its isolation marker: %+v", reopened)
	}
	other, err := reader.SyntheticTenantProvision(ctx, tenantB, issuedB.SuiteTenantID)
	if err != nil || other.Status != "ACTIVE" || other.TenantID != tenantB {
		t.Fatalf("tenant B provision after tenant A revoke = %+v, %v", other, err)
	}
}

func TestSyntheticTenantProvisionIssuerRequiresDedicatedCredential(t *testing.T) {
	core := "postgres://core:pw@db.example:5432/core"
	agent := "postgres://agent:pw@db.example:5432/agent"
	if _, err := NewSyntheticTenantProvisionIssuerStore(context.Background(), SyntheticTenantProvisionIssuerConfig{
		DSN: agent, AgentDSN: agent, CoreDSN: core,
	}); !errors.Is(err, ErrSharedCredential) {
		t.Fatalf("issuer accepted the serving credential: %v", err)
	}
	if sameDatabase(agent, "postgres://issuer:pw@db.example:5432/other") {
		t.Fatal("database identity check accepted a different evaluator database")
	}
}

func TestSyntheticTenantProvisionIssueRejectsInvalidBindings(t *testing.T) {
	base := SyntheticTenantProvisionRecord{
		TenantID: uuid.New(), ProvisionID: "p1", MarkerID: "m1", Purpose: "agent-evaluation", Status: "ACTIVE",
		IssuedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour), GrantStoreID: "g", TaskStoreID: "t",
		BudgetLedgerID: "b", AuditStoreID: "a", ToolOwnerID: "o", ToolOwnerProfile: "fixture-only/v1",
	}
	for _, tc := range []struct {
		name string
		edit func(*SyntheticTenantProvisionRecord)
	}{
		{name: "missing tenant", edit: func(r *SyntheticTenantProvisionRecord) { r.TenantID = uuid.Nil }},
		{name: "wrong purpose", edit: func(r *SyntheticTenantProvisionRecord) { r.Purpose = "production" }},
		{name: "shared stores", edit: func(r *SyntheticTenantProvisionRecord) { r.TaskStoreID = r.GrantStoreID }},
		{name: "nonfixture owner", edit: func(r *SyntheticTenantProvisionRecord) { r.ToolOwnerProfile = "production/v1" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := base
			tc.edit(&record)
			if validSyntheticTenantProvision(record) {
				t.Fatalf("accepted invalid record: %+v", record)
			}
		})
	}
}

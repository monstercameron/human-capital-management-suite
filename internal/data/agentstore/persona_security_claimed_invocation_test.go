package agentstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestTodo_AGENTP_016_Integration_ClaimedInvocationLeaseRequiresDurableAcceptedExecution(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES ($1)`, tenant); err != nil {
		t.Fatal(err)
	}
	login, password := roleName("persona_claimed"), uuid.NewString()
	if err := createAgentLogin(ctx, db.SQL, login, password); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.SQL.ExecContext(context.Background(), "DROP ROLE "+login) })
	store, err := New(ctx, Config{DSN: testDSN(t, db.URL, db.Schema, login, password, "postgres"), CoreDSN: "postgres://core:pw@127.0.0.1:5433/core", MaxConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	seedPersonaSecurityRunWithState(t, db, tenant, "CLAIMED")
	at := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	lease, err := store.IssuePersonaSecurityLease(ctx, PersonaSecurityLeaseRequest{TenantID: tenant, AdmissionID: "admission-persona-1", IssuedAt: at, ExpiresAt: at.Add(time.Minute)})
	if err != nil || lease.InvocationID != "invocation-1" || lease.RunID != "run-persona-1" {
		t.Fatalf("claimed accepted execution lease=%+v err=%v", lease, err)
	}
	if _, err := store.IssuePersonaSecurityLease(ctx, PersonaSecurityLeaseRequest{TenantID: uuid.New(), AdmissionID: "admission-persona-1", IssuedAt: at, ExpiresAt: at.Add(time.Minute)}); !errors.Is(err, ErrPersonaSecurityLeaseDenied) {
		t.Fatalf("foreign tenant lease err=%v", err)
	}
	if _, err := store.IssuePersonaSecurityLease(ctx, PersonaSecurityLeaseRequest{TenantID: tenant, AdmissionID: "missing-admission", IssuedAt: at, ExpiresAt: at.Add(time.Minute)}); !errors.Is(err, ErrPersonaSecurityLeaseDenied) {
		t.Fatalf("missing admission lease err=%v", err)
	}
}

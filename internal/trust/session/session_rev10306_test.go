package session_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/session"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/session/pgstore"
)

// failEvidenceStore decorates a session.Store and fails every denial
// evidence write, so the test can prove the denial still stands and the
// failure is reported instead of dropped.
type failEvidenceStore struct {
	session.Store
	err error
}

func (f failEvidenceStore) RecordEvidence(ctx context.Context, id session.ID, kind session.EvidenceKind, reason string, at time.Time) error {
	return f.err
}

// TestTodo_REV_103_06 proves a tenant-mismatch denial is still denied when
// its audit row cannot be written, and the evidence failure travels with
// the denial instead of vanishing into `_ =`.
func TestTodo_REV_103_06(t *testing.T) {
	db := pgtest.New(t)
	tenant := uuid.New()
	when := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'REV-103-06 tenant', 'ACTIVE', $3)`,
		tenant, "rev10306-ok-"+tenant.String(), when.Add(-time.Hour))
	sessionConn := db.NewConn(t)
	if _, err := sessionConn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("SET ROLE: %v", err)
	}
	good, err := session.NewPersistentManager(session.PersistentManagerConfig{
		Now: func() time.Time { return when }, Store: pgstore.New(sessionConn),
	})
	if err != nil {
		t.Fatalf("NewPersistentManager: %v", err)
	}
	rec, _, err := good.Create(context.Background(), session.CreateSpec{
		Tenant: values.TenantId(tenant.String()), Subject: "user:operator",
		PrincipalFingerprint: "fp:rev10306", Assurance: trust.AssuranceHigh,
		IdleTimeout: time.Hour, AbsoluteTimeout: 4 * time.Hour,
	})
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	// The healthy path still denies with the typed sentinel.
	if _, err := good.Validate(context.Background(), rec.ID(), session.Claim{Tenant: "tenant:other"}); !errors.Is(err, session.ErrTenantMismatch) {
		t.Fatalf("healthy denial = %v, want ErrTenantMismatch", err)
	}

	// FAULT: the evidence write fails. The caller is still refused with
	// the same sentinel, and the evidence failure is joined to it.
	broken, err := session.NewPersistentManager(session.PersistentManagerConfig{
		Now:   func() time.Time { return when },
		Store: failEvidenceStore{Store: pgstore.New(sessionConn), err: errors.New("evidence table unavailable")},
	})
	if err != nil {
		t.Fatalf("NewPersistentManager: %v", err)
	}
	_, err = broken.Validate(context.Background(), rec.ID(), session.Claim{Tenant: "tenant:other"})
	if !errors.Is(err, session.ErrTenantMismatch) {
		t.Fatalf("denial with failed evidence write = %v, want ErrTenantMismatch to stand", err)
	}
	if err == nil || !strings.Contains(err.Error(), "record denial evidence") {
		t.Fatalf("denial error = %v, want the evidence failure joined to it", err)
	}
}

// TestTodo_REV_103_06_Security proves the assurance-mismatch path fails
// closed the same way: no session record is returned as valid, and the
// evidence failure is reported.
func TestTodo_REV_103_06_Security(t *testing.T) {
	db := pgtest.New(t)
	tenant := uuid.New()
	when := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'REV-103-06 assurance', 'ACTIVE', $3)`,
		tenant, "rev10306-sec-"+tenant.String(), when.Add(-time.Hour))
	sessionConn := db.NewConn(t)
	if _, err := sessionConn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("SET ROLE: %v", err)
	}
	good, err := session.NewPersistentManager(session.PersistentManagerConfig{
		Now: func() time.Time { return when }, Store: pgstore.New(sessionConn),
	})
	if err != nil {
		t.Fatalf("NewPersistentManager: %v", err)
	}
	rec, _, err := good.Create(context.Background(), session.CreateSpec{
		Tenant: values.TenantId(tenant.String()), Subject: "user:operator",
		PrincipalFingerprint: "fp:rev10306-sec", Assurance: trust.AssuranceHigh,
		IdleTimeout: time.Hour, AbsoluteTimeout: 4 * time.Hour,
	})
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	broken, err := session.NewPersistentManager(session.PersistentManagerConfig{
		Now:   func() time.Time { return when },
		Store: failEvidenceStore{Store: pgstore.New(sessionConn), err: errors.New("evidence table unavailable")},
	})
	if err != nil {
		t.Fatalf("NewPersistentManager: %v", err)
	}
	got, err := broken.Validate(context.Background(), rec.ID(), session.Claim{Assurance: trust.AssuranceLow})
	if !errors.Is(err, session.ErrAssuranceMismatch) {
		t.Fatalf("assurance denial = %v (record %+v), want ErrAssuranceMismatch", err, got)
	}
	if err == nil || !strings.Contains(err.Error(), "record denial evidence") {
		t.Fatalf("assurance denial error = %v, want the evidence failure joined to it", err)
	}
}

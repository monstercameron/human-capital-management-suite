package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// promotionGuardWireFixture composes the smallest journeyEngine that can
// actually reach a real database through admitPromotionWindow and
// confirmPromotionWindow: a tenant mapping, an id source, and the pgtest
// connection itself as the execution database. This is the file
// promotionguard_wire.go's own package-local test, exercising the exact
// tenant-scoping (e.svc.tenantUUID, e.beginTenant) and error-projection
// (journeyError) glue those two methods add -- internal/data/promotionguard's
// own suite proves the admission decision itself; this proves the wiring
// that hands it its tenant, worker and window.
func promotionGuardWireFixture(t *testing.T) (*journeyEngine, *trust.Principal, uuid.UUID) {
	t.Helper()
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenantID, "promotionguard-wire-it", "promotionguard wire test tenant")

	mint := 0
	svc := &IntentService{
		ids: func() (string, error) {
			mint++
			id, err := uuid.NewV7()
			if err != nil {
				return "", err
			}
			return id.String(), nil
		},
		tenantUUID: func(values.TenantId) uuid.UUID { return tenantID },
	}
	engine := newJourneyEngine(svc, db.Conn, "", func() time.Time {
		return time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	}, nil)

	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: fixtures.Tenant, Subject: "user-promotionguard-wire", SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "org:promotionguard-wire-it", Roles: []string{"intent_author"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-promotionguard-wire", Purposes: []string{"hcm_operations"},
		IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour),
		CredentialDigest: "digest:promotionguard-wire-fixture",
	})
	if err != nil {
		t.Fatalf("trust.NewPrincipal: %v", err)
	}
	return engine, principal, tenantID
}

// TestAdmitPromotionWindowGrantsThenRefusesAConflict proves
// admitPromotionWindow's own contribution on top of
// internal/data/promotionguard.Admit: it opens a real tenant-scoped
// transaction against the engine's own database handle, commits a successful
// admission, and projects a refused one onto workspace.ErrJourneyActiveConflict
// -- the sentinel journeyError and, downstream, internal/transport/journey's
// ownedError both switch on -- rather than leaking the internal envelope or a
// bare promotionguard.ErrActiveConflict a caller of the workspace.JourneyEngine
// port could not usefully match on.
func TestAdmitPromotionWindowGrantsThenRefusesAConflict(t *testing.T) {
	e, principal, _ := promotionGuardWireFixture(t)
	ctx := trust.WithPrincipal(context.Background(), principal)
	const worker = "EMPLOYMENT:wire-test-worker"

	guardID, replayed, err := e.admitPromotionWindow(ctx, principal, worker, "2027-04-01", "journey:propose:key-one")
	if err != nil {
		t.Fatalf("admitPromotionWindow(first) = %v, want nil", err)
	}
	if guardID == uuid.Nil {
		t.Fatal("admitPromotionWindow returned a nil guard id on success")
	}
	if replayed {
		t.Fatal("admitPromotionWindow(first) reported a replay for a new window")
	}

	if _, _, err := e.admitPromotionWindow(ctx, principal, worker, "2027-04-01", "journey:propose:key-two"); !errors.Is(err, workspace.ErrJourneyActiveConflict) {
		t.Fatalf("admitPromotionWindow(conflicting) = %v, want workspace.ErrJourneyActiveConflict", err)
	}

	// A different, non-overlapping window for the same worker is still
	// admitted -- the same GREEN carve-out internal/data/promotionguard's own
	// suite proves, now proven through this engine's own wiring.
	if _, _, err := e.admitPromotionWindow(ctx, principal, worker, "2028-01-01", "journey:propose:key-three"); err != nil {
		t.Fatalf("admitPromotionWindow(non-overlapping) = %v, want nil", err)
	}

	// confirmPromotionWindow attaches a real intent id and never fails the
	// caller even if it were to encounter an already-confirmed row (it is
	// documented as best-effort); calling it twice with the same arguments
	// must not error.
	intentID := uuid.New().String()
	if err := e.confirmPromotionWindow(ctx, principal, guardID, "journey:propose:key-one", intentID); err != nil {
		t.Fatalf("confirmPromotionWindow: %v", err)
	}
	if err := e.confirmPromotionWindow(ctx, principal, guardID, "journey:propose:key-one", intentID); err != nil {
		t.Fatalf("confirmPromotionWindow (repeat): %v", err)
	}
}

// TestAdmitPromotionWindowFailsClosedWithNoExecutionDatabase proves a cell
// composed with no execution database refuses to admit rather than silently
// skipping the guard: a promotion.propose contract this lane runs on has no
// weaker mode where the database-enforced exclusivity guarantee simply does
// not apply.
func TestAdmitPromotionWindowFailsClosedWithNoExecutionDatabase(t *testing.T) {
	svc := &IntentService{
		ids:        func() (string, error) { return uuid.NewString(), nil },
		tenantUUID: func(values.TenantId) uuid.UUID { return uuid.New() },
	}
	engine := newJourneyEngine(svc, nil, "", nil, nil)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: fixtures.Tenant, Subject: "user-no-db", SubjectKind: trust.SubjectKindHuman,
		Roles: []string{"intent_author"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceHigh, SessionRef: "session-no-db", Purposes: []string{"hcm_operations"},
		IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour),
		CredentialDigest: "digest:no-db-fixture",
	})
	if err != nil {
		t.Fatalf("trust.NewPrincipal: %v", err)
	}
	if _, _, err := engine.admitPromotionWindow(context.Background(), principal, "EMPLOYMENT:x", "2027-01-01", "key"); !errors.Is(err, workspace.ErrJourneyUnavailable) {
		t.Fatalf("admitPromotionWindow(no db) = %v, want workspace.ErrJourneyUnavailable", err)
	}
}

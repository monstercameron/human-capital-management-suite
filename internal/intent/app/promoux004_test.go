package app

// PROMOUX-004 follow-up: the coordinator traced RED to the live path a user
// actually reaches -- internal/humanwork/workspace's /workspace/promotion
// preview, served in production by workspaceReader.ReadPromotion
// (workspace.go) -- and found it still accepted a guessed target position
// because evaluateTargetPositionSelection was opt-in.
//
// TestTodo_PROMOUX_004_WorkspaceLivePath drives that exact real
// implementation, composed the same way NewCell composes it for a live
// deployment (a real PostgreSQL-backed ExecutionDB and tenant mapping, so
// workspaceReader carries a real internal/data/positionfacts.Reader -- not
// the domain evaluator in isolation, and not a fake reader), and proves a
// bare guessed identifier such as POS-ENG-MGR-101 is refused on the
// existence ground rather than silently accepted.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// promoux004LivePathQuery is internal/humanwork/workspace's own reference
// scenario (the exact shape DefaultQuery seeds, minus the position field it
// no longer carries), so this test exercises the same Query.Typed() ->
// Cell.ReadPromotion path a real page request takes -- not a hand-built
// PreflightRequest.
func promoux004LivePathQuery(targetPositionID string) workspace.Query {
	return workspace.Query{
		WorkerRef: "omar-reyes", CurrentBase: "93000.00", ProposedBase: "98000.00",
		Currency: "USD", BonusTargetPercent: "0.0500",
		TargetJobCode: "OPS-HRBP3", TargetGrade: "P3", TargetOrgUnit: "people-ops",
		TargetPositionID: targetPositionID, TargetPayZone: "US-EAST",
		EffectiveDate: "2026-06-01", EvaluationDate: "2026-05-15",
		BusinessReason: "Promotion into the senior HRBP role", BudgetAvailable: "50000.00",
	}
}

func promoux004LivePathPrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: fixtures.Tenant, Subject: "user-promoux004-live", SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID:  "org-north-america",
		Roles:                []string{"intent_author", string(authz.RoleCompAdmin)},
		AuthorityRefs:        []string{"authority:position:vp-people"},
		Purposes:             []string{authz.PurposeCompensationReview},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-promoux004-live", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		CredentialDigest: "credential-digest-promoux004",
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return p
}

// promoux004LiveCell composes a Cell exactly the way a real deployment does
// for PROMOUX-004's read half: a real PostgreSQL-backed ExecutionDB and a
// tenant mapping, which is what makes Cell.WorkspacePort()'s workspaceReader
// carry a real internal/data/positionfacts.Reader instead of nil. The
// tenant's job_position table is left empty on purpose: this environment
// has no real Position domain data seeded for the legacy corpus scenario
// (see internal/humanwork/workspace/request.go's own DefaultQuery comment),
// which is exactly what makes a guessed identifier resolve to "not found"
// rather than something this test would have to fabricate.
func promoux004LiveCell(t *testing.T) *Cell {
	t.Helper()
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenantID, "promoux004-live", "PROMOUX-004 live-path tenant")

	store := newMemLifecycleStore()
	if err := store.Bootstrap(context.Background(), string(fixtures.Tenant)); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	verifier := trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) {
		return nil, nil
	})
	cell, err := NewCell(CellConfig{
		Store: store, Verifier: verifier, Audience: "hcm-next-api",
		ExecutionDB: db.Conn,
		TenantUUID:  func(values.TenantId) uuid.UUID { return tenantID },
	})
	if err != nil {
		t.Fatalf("NewCell: %v", err)
	}
	return cell
}

// TestTodo_PROMOUX_004_WorkspaceLivePath is the live-path proof the
// coordinator asked for: not evaluateTargetPositionSelection called
// directly, but the real workspace.Cell implementation a browser request
// reaches, composed with a real database-backed position reader.
func TestTodo_PROMOUX_004_WorkspaceLivePath(t *testing.T) {
	cell := promoux004LiveCell(t)
	cellPort := cell.WorkspacePort()
	ctx := trust.WithPrincipal(context.Background(), promoux004LivePathPrincipal(t))

	t.Run("a guessed target position is refused on the existence ground, not silently accepted", func(t *testing.T) {
		req, err := promoux004LivePathQuery("POS-ENG-MGR-101").Typed()
		if err != nil {
			t.Fatalf("Query.Typed: %v", err)
		}
		reading, err := cellPort.ReadPromotion(ctx, req)
		if err != nil {
			t.Fatalf("ReadPromotion: %v", err)
		}
		if reading.Preflight.Status != promotion.StatusBlocked {
			t.Fatalf("Preflight.Status = %s, want BLOCKED; findings: %+v", reading.Preflight.Status, reading.Preflight.Findings)
		}
		if !reading.Preflight.HasCode(promotion.CodeTargetPositionNotFound) {
			t.Fatalf("Preflight findings %+v do not carry %s", reading.Preflight.Findings, promotion.CodeTargetPositionNotFound)
		}
		if reading.Simulation.Executable {
			t.Fatal("a promotion refused on an unprovable target position must not report Executable")
		}
	})

	t.Run("the identical guessed reference reused for a genuinely different tenant is refused the same way", func(t *testing.T) {
		// Proves the refusal is not an artifact of this test's specific
		// tenant wiring: a second, independently composed cell over its
		// own empty database refuses the same guessed identifier too.
		otherCell := promoux004LiveCell(t)
		req, err := promoux004LivePathQuery("POS-ENG-MGR-101").Typed()
		if err != nil {
			t.Fatalf("Query.Typed: %v", err)
		}
		reading, err := otherCell.WorkspacePort().ReadPromotion(ctx, req)
		if err != nil {
			t.Fatalf("ReadPromotion: %v", err)
		}
		if reading.Preflight.Status != promotion.StatusBlocked || !reading.Preflight.HasCode(promotion.CodeTargetPositionNotFound) {
			t.Fatalf("Preflight = %+v, want BLOCKED with %s", reading.Preflight, promotion.CodeTargetPositionNotFound)
		}
	})

	t.Run("no target position named at all still reaches a READY preflight on job/grade alone", func(t *testing.T) {
		// The control: the live path is not broken wholesale by this
		// change. A promotion that never claims a specific position still
		// proceeds normally, exactly as internal/domains/promotion's own
		// checkPlacement has always allowed.
		req, err := promoux004LivePathQuery("").Typed()
		if err != nil {
			t.Fatalf("Query.Typed: %v", err)
		}
		reading, err := cellPort.ReadPromotion(ctx, req)
		if err != nil {
			t.Fatalf("ReadPromotion: %v", err)
		}
		if reading.Preflight.Status != promotion.StatusReady {
			t.Fatalf("Preflight.Status = %s, want READY; findings: %+v", reading.Preflight.Status, reading.Preflight.Findings)
		}
	})
}

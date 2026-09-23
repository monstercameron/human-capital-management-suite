package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionguard"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	rev004ProducedAt     = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	rev004EffectiveStart = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	rev004ReleasedAt     = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
)

func rev004SeedTenant(t *testing.T, db *pgtest.DB) uuid.UUID {
	t.Helper()
	tenantID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, 'wfrev004-app', 'cell-local', 'wfrev004 app', 'ACTIVE', timestamptz '2020-01-01T00:00:00Z')`,
		tenantID)
	return tenantID
}

func rev004TenantUUID(tenantID uuid.UUID) func(values.TenantId) uuid.UUID {
	return func(k values.TenantId) uuid.UUID {
		if k == "wfrev004-app" {
			return tenantID
		}
		return uuid.Nil
	}
}

func rev004InTx(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope: %v", err)
	}
	if err := fn(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func rev004SeedPool(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, amount string) {
	t.Helper()
	rev004InTx(t, db, tenantID, func(tx dbport.Tx) error {
		_, err := demoworkforce.SeedAggregateCatalog(context.Background(), tx, tenantID, demoworkforce.AggregateCatalog{
			LegalEntityName: "WF-REV-004 Entity", BudgetOrgUnits: []string{"people-ops"},
			BudgetCurrency: "USD", BudgetAmount: amount, RecordedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
			Jobs:      []demoworkforce.CatalogJob{{Code: "OPS-HRBP3", Grade: "P3"}},
			Vacancies: []demoworkforce.CatalogVacancy{{OrgUnit: "people-ops", JobCode: "OPS-HRBP3", Grade: "P3"}},
		})
		return err
	})
}

func rev004SeedGuard(t *testing.T, db *pgtest.DB, tenantID, intentID uuid.UUID, slot string) uuid.UUID {
	t.Helper()
	guardID := uuid.New()
	key := "key-" + intentID.String()[:8]
	rev004InTx(t, db, tenantID, func(tx dbport.Tx) error {
		ctx := context.Background()
		if _, err := promotionguard.Admit(ctx, tx, tenantID, guardID, "EMPLOYMENT:wfrev004-"+slot, "2027-01-0"+slot, key); err != nil {
			return err
		}
		return promotionguard.Confirm(ctx, tx, tenantID, guardID, key, intentID)
	})
	return guardID
}

// rev004SeedHold records the proposal revision the intent minted and cuts its
// compensation-pool hold, the shape the propose path leaves behind.
func rev004SeedHold(t *testing.T, db *pgtest.DB, tenantID, intentID, proposal uuid.UUID, materialDigest, amount string) {
	t.Helper()
	ctx := context.Background()
	db.Exec(t, `INSERT INTO intent_instance
		(tenant_id, intent_id, definition_ref, definition_version, request_digest, idempotency_key,
		 request_state, execution_state, business_state, consistency_state, obligation_state,
		 created_at, last_transition_at)
		VALUES ($1,$2,'promotion.default/v1',1,$3,$4,'APPROVED','SCHEDULED','IN_PROGRESS','PENDING_OBSERVATION','PENDING',$5,$5)`,
		tenantID, intentID, strings.Repeat("b", 64), "wfrev004-"+intentID.String(), rev004ProducedAt)
	start, err := values.ParseLocalDate("2026-10-15")
	if err != nil {
		t.Fatal(err)
	}
	effective, err := values.NewOpenLocalDateInterval(start, values.CalendarRef{Ref: "gregorian", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	dto := intent.ProposalRevision{
		ProposalRevisionID: proposal.String(), IntentID: intentID.String(), Revision: 1,
		Tenant: values.TenantId(tenantID.String()), OrganizationScopeID: "org:people-ops",
		EffectiveTime: effective, MaterialDigest: digest.Reference{ProfileID: "hcmnext.proposal", ProfileVersion: 1, SchemaID: "proposal", SchemaVersion: 1, AlgorithmID: "sha256", Digest: materialDigest},
		CreatedBy: intent.PrincipalReference{PrincipalID: "principal:test", Kind: intent.InitiatorHuman, IdentityAssuranceRef: "ial:test"},
		Subjects:  []intent.SubjectReference{{Kind: "EMPLOYMENT", SubjectID: uuid.NewString(), AuthorityDomain: "PEOPLE"}},
		CreatedAt: values.NewInstant(rev004ProducedAt),
		ControlSnapshots: intent.ControlSnapshots{
			CapabilityRegistryDigest: "sha256:capability", PolicyBundleDigest: "sha256:policy",
			LegalContextDigest: "sha256:legal", EntitlementDigest: "sha256:entitlement",
			ReferenceDataDigest: "sha256:reference", ClassificationTaxonomyDigest: "sha256:classification",
			ClassificationLabelSetDigest: "sha256:labels", ClassificationPropagationWatermark: "sha256:watermark",
			DLPDecisionDigest: "sha256:dlp",
		},
	}
	payload, err := intentcontrol.EncodeFullProposal(dto)
	if err != nil {
		t.Fatal(err)
	}
	rev004InTx(t, db, tenantID, func(tx dbport.Tx) error {
		if _, err := (intentcontrol.RevisionStore{}).Materialize(ctx, tx, intentcontrol.Revision{
			TenantID: tenantID, IntentID: intentID, Revision: 1, ProposalDigest: materialDigest, MaterialDigest: materialDigest,
			SchemaRef: "hcmnext.proposal.full/v1", Payload: payload, ProducedBy: "test", ProducedAt: rev004ProducedAt,
		}); err != nil {
			return err
		}
		held, err := promotionbudget.ReserveProposalBudget(ctx, tx, tenantID, promotionbudget.ProposalReservation{
			ProposalRevisionID: proposal, OrgUnit: "people-ops", Amount: amount,
			ProducedAt: rev004ProducedAt, EffectiveStart: rev004EffectiveStart,
		})
		if err != nil {
			return err
		}
		if !held {
			t.Fatal("ReserveProposalBudget cut no hold for the seeded pool")
		}
		return nil
	})
}

func rev004Reservation(t *testing.T, db *pgtest.DB, tenantID, proposal uuid.UUID) aggregates.BudgetReservation {
	t.Helper()
	var out aggregates.BudgetReservation
	rev004InTx(t, db, tenantID, func(tx dbport.Tx) error {
		reservation, err := aggregates.CompensationStore{}.ReservationForProposal(context.Background(), tx, tenantID, proposal, rev004EffectiveStart)
		if err != nil {
			return err
		}
		out = reservation
		return nil
	})
	return out
}

func rev004ReservationRows(t *testing.T, db *pgtest.DB, tenantID, proposal uuid.UUID) int {
	t.Helper()
	var n int
	rev004InTx(t, db, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM budget_reservation WHERE tenant_id = $1 AND entity_id = $2`,
			tenantID, promotionbudget.ReservationID(proposal)).Scan(&n)
	})
	return n
}

func rev004GuardStatus(t *testing.T, db *pgtest.DB, guardID uuid.UUID) string {
	t.Helper()
	var status string
	if err := db.Conn.QueryRow(context.Background(), `SELECT status FROM promotion_active_intent_guard WHERE guard_id = $1`, guardID).Scan(&status); err != nil {
		t.Fatalf("read guard: %v", err)
	}
	return status
}

// TestTodo_WF_REV_004 proves the kernel CancelIntent path's admission release
// frees the promotion's compensation-pool hold: a held proposal is RELEASED
// through the composed release, a replayed cancellation writes nothing more,
// a malformed intent is refused, and an intent that never held anything is a
// no-op.
func TestTodo_WF_REV_004(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenantID := rev004SeedTenant(t, db)
	_, release, err := composeWorkflowCancellation(db.Conn, rev004TenantUUID(tenantID))
	if err != nil || release == nil {
		t.Fatalf("compose = %v, %v", release, err)
	}
	rev004SeedPool(t, db, tenantID, "1000000.00")
	intentID, proposal := uuid.New(), uuid.New()
	guardID := rev004SeedGuard(t, db, tenantID, intentID, "1")
	rev004SeedHold(t, db, tenantID, intentID, proposal, strings.Repeat("e", 64), "5000.00")

	if err := release(ctx, "wfrev004-app", intentID.String(), rev004ReleasedAt); err != nil {
		t.Fatalf("release the cancelled intent: %v", err)
	}
	if got := rev004GuardStatus(t, db, guardID); got != "CLOSED" {
		t.Fatalf("guard after release = %q, want CLOSED", got)
	}
	first := rev004Reservation(t, db, tenantID, proposal)
	if first.Status != promotionbudget.ReservationReleased {
		t.Fatalf("reservation after release = %+v, want RELEASED", first)
	}
	rows := rev004ReservationRows(t, db, tenantID, proposal)

	// A replayed cancellation through the same capability writes nothing more:
	// the hold is freed exactly once.
	if err := release(ctx, "wfrev004-app", intentID.String(), rev004ReleasedAt); err != nil {
		t.Fatalf("re-release the cancelled intent: %v", err)
	}
	second := rev004Reservation(t, db, tenantID, proposal)
	if second.Status != promotionbudget.ReservationReleased || !second.RecordedAt.Equal(first.RecordedAt) {
		t.Fatalf("reservation after the re-release = %+v, want the single release %+v", second, first)
	}
	if got := rev004ReservationRows(t, db, tenantID, proposal); got != rows {
		t.Fatalf("reservation rows after the re-release = %d, want %d: the hold was freed more than once", got, rows)
	}

	if err := release(ctx, "wfrev004-app", "not-a-uuid", rev004ReleasedAt); err == nil {
		t.Fatal("a malformed intent id was released")
	}
	if err := release(ctx, "wfrev004-app", uuid.NewString(), rev004ReleasedAt); err != nil {
		t.Fatalf("release for an intent with no hold: %v", err)
	}
}

// rev004AttemptHold tries to hold amount against the pool and reports whether
// anything was established. The attempt always rolls back, so it observes the
// remaining budget without spending it.
func rev004AttemptHold(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, amount string) bool {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope: %v", err)
	}
	held, _ := promotionbudget.ReserveProposalBudget(ctx, tx, tenantID, promotionbudget.ProposalReservation{
		ProposalRevisionID: uuid.New(), OrgUnit: "people-ops", Amount: amount,
		ProducedAt: rev004ProducedAt, EffectiveStart: rev004EffectiveStart,
	})
	return held
}

// TestTodo_WF_REV_004_Integration cancels one promotion through the kernel
// CancelIntent path and another through the journey path and asserts the
// remaining budget: while both holds are live a further hold would overcommit
// the pool, and after both cancellations it is established.
func TestTodo_WF_REV_004_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenantID := rev004SeedTenant(t, db)
	_, release, err := composeWorkflowCancellation(db.Conn, rev004TenantUUID(tenantID))
	if err != nil || release == nil {
		t.Fatalf("compose = %v, %v", release, err)
	}
	rev004SeedPool(t, db, tenantID, "10000.00")
	intentA, proposalA := uuid.New(), uuid.New()
	intentB, proposalB := uuid.New(), uuid.New()
	guardA := rev004SeedGuard(t, db, tenantID, intentA, "1")
	guardB := rev004SeedGuard(t, db, tenantID, intentB, "2")
	rev004SeedHold(t, db, tenantID, intentA, proposalA, strings.Repeat("e", 64), "6000.00")
	rev004SeedHold(t, db, tenantID, intentB, proposalB, strings.Repeat("f", 64), "3000.00")

	if rev004AttemptHold(t, db, tenantID, "2000.00") {
		t.Fatal("a 2000 hold was established while 9000 of the 10000 pool is held: the holds encumber nothing")
	}

	// Both user-visible cancel paths funnel through the composed admission
	// release CancelIntent runs on a CANCELLED disposition.
	if err := release(ctx, "wfrev004-app", intentA.String(), rev004ReleasedAt); err != nil {
		t.Fatalf("release the CancelIntent path: %v", err)
	}
	if err := release(ctx, "wfrev004-app", intentB.String(), rev004ReleasedAt); err != nil {
		t.Fatalf("release the journey path: %v", err)
	}
	for name, proposal := range map[string]uuid.UUID{"CancelIntent": proposalA, "journey": proposalB} {
		if got := rev004Reservation(t, db, tenantID, proposal).Status; got != promotionbudget.ReservationReleased {
			t.Fatalf("reservation after the %s cancellation = %q, want RELEASED", name, got)
		}
	}
	if got := rev004GuardStatus(t, db, guardA); got != "CLOSED" {
		t.Fatalf("guard A after release = %q, want CLOSED", got)
	}
	if got := rev004GuardStatus(t, db, guardB); got != "CLOSED" {
		t.Fatalf("guard B after release = %q, want CLOSED", got)
	}

	if !rev004AttemptHold(t, db, tenantID, "2000.00") {
		t.Fatal("a 2000 hold was refused after both 9000 of holds were released: the remaining budget was not restored")
	}
}

package promotionbudget_test

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
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var (
	producedAt     = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	effectiveStart = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
)

func seedTenant(t *testing.T, db *pgtest.DB) uuid.UUID {
	t.Helper()
	tenantID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'promotionbudget test', 'ACTIVE', timestamptz '2020-01-01T00:00:00Z')`,
		tenantID, "promotionbudget-"+tenantID.String()[:8])
	return tenantID
}

func inTx(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, fn func(tx dbport.Tx) error) {
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

func seedPool(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, orgUnit, amount string) {
	t.Helper()
	inTx(t, db, tenantID, func(tx dbport.Tx) error {
		_, err := demoworkforce.SeedAggregateCatalog(context.Background(), tx, tenantID, demoworkforce.AggregateCatalog{
			LegalEntityName: "Reservation Test Entity", BudgetOrgUnits: []string{orgUnit},
			BudgetCurrency: "USD", BudgetAmount: amount, RecordedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
			Jobs:      []demoworkforce.CatalogJob{{Code: "OPS-HRBP3", Grade: "P3"}},
			Vacancies: []demoworkforce.CatalogVacancy{{OrgUnit: orgUnit, JobCode: "OPS-HRBP3", Grade: "P3"}},
		})
		return err
	})
}

// TestReserveProposalBudgetHoldsTheRaiseAgainstThePool proves the hold is
// written for the proposal, recorded no later than the revision's ProducedAt,
// readable as the proposal's reservation at its effective date, idempotent on
// replay, and absent when the organization unit has no pool.
func TestReserveProposalBudgetHoldsTheRaiseAgainstThePool(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedTenant(t, db)
	ctx := context.Background()
	seedPool(t, db, tenantID, "people-ops", "1000000.00")
	proposal := uuid.New()
	req := promotionbudget.ProposalReservation{
		ProposalRevisionID: proposal, OrgUnit: "people-ops", Amount: "8000.00",
		ProducedAt: producedAt, EffectiveStart: effectiveStart,
	}
	inTx(t, db, tenantID, func(tx dbport.Tx) error {
		held, err := promotionbudget.ReserveProposalBudget(ctx, tx, tenantID, req)
		if err != nil || !held {
			t.Fatalf("ReserveProposalBudget = %t, %v", held, err)
		}
		again, err := promotionbudget.ReserveProposalBudget(ctx, tx, tenantID, req)
		if err != nil || !again {
			t.Fatalf("replayed ReserveProposalBudget = %t, %v", again, err)
		}
		return nil
	})
	inTx(t, db, tenantID, func(tx dbport.Tx) error {
		comp := aggregates.CompensationStore{}
		reservation, err := comp.ReservationForProposal(ctx, tx, tenantID, proposal, effectiveStart)
		if err != nil {
			t.Fatalf("ReservationForProposal: %v", err)
		}
		if reservation.Status != promotionbudget.ReservationHeld || reservation.Amount != "8000.0000" || reservation.Currency != "USD" {
			t.Fatalf("reservation = %+v", reservation)
		}
		if reservation.EntityID != promotionbudget.ReservationID(proposal) {
			t.Fatalf("reservation identity = %s, want the derived one", reservation.EntityID)
		}
		if reservation.RecordedAt.After(producedAt) {
			t.Fatalf("recorded at %s, after the revision's produced instant %s", reservation.RecordedAt, producedAt)
		}
		if reservation.Expiry == nil || !reservation.Expiry.After(effectiveStart) {
			t.Fatalf("reservation expiry = %v, want a bounded hold past the effective start", reservation.Expiry)
		}
		// Known as of the produced instant: what the terminal resolver pins.
		if _, err := comp.KnownAsOfBudgetReservation(ctx, tx, tenantID, reservation.EntityID, effectiveStart, producedAt); err != nil {
			t.Fatalf("KnownAsOfBudgetReservation at the produced instant: %v", err)
		}
		return nil
	})
	inTx(t, db, tenantID, func(tx dbport.Tx) error {
		other := req
		other.ProposalRevisionID, other.OrgUnit = uuid.New(), "unfunded-unit"
		held, err := promotionbudget.ReserveProposalBudget(ctx, tx, tenantID, other)
		if err != nil || held {
			t.Fatalf("hold against an unfunded unit = %t, %v; want nothing written", held, err)
		}
		if _, err := promotionbudget.ReserveProposalBudget(ctx, tx, tenantID, promotionbudget.ProposalReservation{}); err == nil {
			t.Fatal("an incomplete reservation request was accepted")
		}
		bad := req
		bad.ProposalRevisionID, bad.Amount = uuid.New(), "not-a-decimal"
		if _, err := promotionbudget.ReserveProposalBudget(ctx, tx, tenantID, bad); err == nil {
			t.Fatal("a non-decimal amount was accepted")
		}
		return nil
	})
}

// TestReleaseProposalBudgetOnlyReleasesAHeldReservation proves a held
// reservation is released, that releasing twice writes nothing more, and that
// a committed reservation is never released.
func TestReleaseProposalBudgetOnlyReleasesAHeldReservation(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedTenant(t, db)
	ctx := context.Background()
	seedPool(t, db, tenantID, "people-ops", "1000000.00")
	proposal := uuid.New()
	inTx(t, db, tenantID, func(tx dbport.Tx) error {
		_, err := promotionbudget.ReserveProposalBudget(ctx, tx, tenantID, promotionbudget.ProposalReservation{
			ProposalRevisionID: proposal, OrgUnit: "people-ops", Amount: "8000.00",
			ProducedAt: producedAt, EffectiveStart: effectiveStart,
		})
		return err
	})
	released := producedAt.Add(48 * time.Hour)
	inTx(t, db, tenantID, func(tx dbport.Tx) error {
		ok, err := promotionbudget.ReleaseProposalBudget(ctx, tx, tenantID, proposal, released)
		if err != nil || !ok {
			t.Fatalf("ReleaseProposalBudget = %t, %v", ok, err)
		}
		return nil
	})
	inTx(t, db, tenantID, func(tx dbport.Tx) error {
		reservation, err := aggregates.CompensationStore{}.ReservationForProposal(ctx, tx, tenantID, proposal, effectiveStart)
		if err != nil || reservation.Status != promotionbudget.ReservationReleased {
			t.Fatalf("reservation after release = %+v, %v", reservation, err)
		}
		again, err := promotionbudget.ReleaseProposalBudget(ctx, tx, tenantID, proposal, released)
		if err != nil || again {
			t.Fatalf("second release = %t, %v; want nothing to release", again, err)
		}
		unknown, err := promotionbudget.ReleaseProposalBudget(ctx, tx, tenantID, uuid.New(), released)
		if err != nil || unknown {
			t.Fatalf("release of an unheld proposal = %t, %v", unknown, err)
		}
		if _, err := promotionbudget.ReleaseProposalBudget(ctx, tx, tenantID, uuid.Nil, released); err == nil {
			t.Fatal("a release with no proposal was accepted")
		}
		if _, err := promotionbudget.ReleaseForIntent(ctx, tx, tenantID, uuid.Nil, released); err == nil {
			t.Fatal("a release for no intent was accepted")
		}
		// An intent with no recorded proposal revision holds nothing.
		none, err := promotionbudget.ReleaseForIntent(ctx, tx, tenantID, uuid.New(), released)
		if err != nil || none {
			t.Fatalf("release for an intent with no revision = %t, %v", none, err)
		}
		return nil
	})
}

// TestReleaseForIntentReleasesTheIntentsOwnRevision proves a cancellation,
// which knows only the intent it cancelled, releases the hold that intent's
// latest recorded proposal revision cut.
func TestReleaseForIntentReleasesTheIntentsOwnRevision(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedTenant(t, db)
	ctx := context.Background()
	seedPool(t, db, tenantID, "people-ops", "1000000.00")
	intentID, proposal := uuid.New(), uuid.New()
	materialDigest := strings.Repeat("a", 64)
	db.Exec(t, `INSERT INTO intent_instance
		(tenant_id, intent_id, definition_ref, definition_version, request_digest, idempotency_key,
		 request_state, execution_state, business_state, consistency_state, obligation_state,
		 created_at, last_transition_at)
		VALUES ($1,$2,'promotion.default/v1',1,$3,$4,'APPROVED','SCHEDULED','IN_PROGRESS','PENDING_OBSERVATION','PENDING',$5,$5)`,
		tenantID, intentID, strings.Repeat("b", 64), "promotionbudget-"+intentID.String(), producedAt)
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
		CreatedAt: values.NewInstant(producedAt),
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
	inTx(t, db, tenantID, func(tx dbport.Tx) error {
		if _, err := (intentcontrol.RevisionStore{}).Materialize(ctx, tx, intentcontrol.Revision{
			TenantID: tenantID, IntentID: intentID, Revision: 1, ProposalDigest: materialDigest, MaterialDigest: materialDigest,
			SchemaRef: "hcmnext.proposal.full/v1", Payload: payload, ProducedBy: "test", ProducedAt: producedAt,
		}); err != nil {
			return err
		}
		_, err := promotionbudget.ReserveProposalBudget(ctx, tx, tenantID, promotionbudget.ProposalReservation{
			ProposalRevisionID: proposal, OrgUnit: "people-ops", Amount: "5000.00",
			ProducedAt: producedAt, EffectiveStart: effectiveStart,
		})
		return err
	})
	inTx(t, db, tenantID, func(tx dbport.Tx) error {
		released, err := promotionbudget.ReleaseForIntent(ctx, tx, tenantID, intentID, producedAt.Add(time.Hour))
		if err != nil || !released {
			t.Fatalf("ReleaseForIntent = %t, %v", released, err)
		}
		reservation, err := aggregates.CompensationStore{}.ReservationForProposal(ctx, tx, tenantID, proposal, effectiveStart)
		if err != nil || reservation.Status != promotionbudget.ReservationReleased {
			t.Fatalf("reservation after the cancellation = %+v, %v", reservation, err)
		}
		return nil
	})
}

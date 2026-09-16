package promotionbudget

// WF-RUN-034: the proposal's DB-010 budget reservation.
//
// A served promotion holds its raise against its organization unit's
// compensation pool from the moment its proposal revision is produced: the
// reservation is written in the proposal's own candidate transaction and
// recorded no later than the revision's ProducedAt, because the PROMOUX-016
// resolver pins the reservation's baseline digest known as of that instant.
// The commit transitions it HELD -> COMMITTED (internal/data/promotioncommit);
// a promotion that ends any other way releases it (HELD -> RELEASED). The
// budget_reservation_forbid_overcommit trigger is the over-reservation fence.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// reservationNamespace is the fixed namespace reservation identities are
// derived under: one stable identity per proposal revision, so a replayed
// proposal finds the hold it already cut instead of cutting a second one.
var reservationNamespace = uuid.MustParse("1c7c9f34-6a2f-4e0f-9f3a-9d0f3a7d8b21")

// ReservationHoldPeriod bounds a held reservation past the promotion's
// effective start. A hold that never expires is a leak.
const ReservationHoldPeriod = 30 * 24 * time.Hour

// Reservation statuses this file writes.
const (
	ReservationHeld     = "HELD"
	ReservationReleased = "RELEASED"
)

// ProposalReservation is one proposal's budget hold.
type ProposalReservation struct {
	// ProposalRevisionID binds the reservation to the proposal revision.
	ProposalRevisionID uuid.UUID
	// OrgUnit selects the organization unit's compensation pool.
	OrgUnit string
	// Amount is the annualized raise, exact decimal text.
	Amount string
	// ProducedAt is the proposal revision's produced instant.
	ProducedAt time.Time
	// EffectiveStart is the promotion's effective start (UTC midnight).
	EffectiveStart time.Time
}

// ReservationID is the deterministic reservation identity for a proposal.
func ReservationID(proposalRevisionID uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(reservationNamespace, []byte("budget-reservation:"+proposalRevisionID.String()))
}

// ReserveProposalBudget writes the proposal's HELD reservation inside tx
// (already tenant scoped). It reports false, writing nothing, when the
// tenant records no pool for the organization unit; it is idempotent for a
// proposal that already holds one.
func ReserveProposalBudget(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, req ProposalReservation) (bool, error) {
	if tx == nil || tenant == uuid.Nil || req.ProposalRevisionID == uuid.Nil || req.ProducedAt.IsZero() || req.EffectiveStart.IsZero() {
		return false, fmt.Errorf("promotionbudget: reserve budget: transaction, tenant, proposal and instants are required")
	}
	amount, err := values.NewDecimal(req.Amount, 4, values.RoundingExactRequired)
	if err != nil || amount.Sign() < 0 {
		return false, fmt.Errorf("promotionbudget: reserve budget: amount %q is not a non-negative exact decimal", req.Amount)
	}
	comp := aggregates.CompensationStore{}
	id := ReservationID(req.ProposalRevisionID)
	if _, err := comp.CurrentBudgetReservation(ctx, tx, tenant, id, req.EffectiveStart); err == nil {
		return true, nil
	} else if !errors.Is(err, aggregates.ErrNotFound) {
		return false, fmt.Errorf("promotionbudget: read budget reservation: %w", err)
	}
	budget, err := comp.CurrentWorkforceBudget(ctx, tx, tenant, demoworkforce.BudgetID(req.OrgUnit), req.EffectiveStart)
	if errors.Is(err, aggregates.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("promotionbudget: read budget %s: %w", req.OrgUnit, err)
	}
	// Recorded no later than ProducedAt, at the database's own microsecond
	// precision, so a read known as of ProducedAt always sees the hold.
	recorded := req.ProducedAt.UTC().Truncate(time.Microsecond)
	day := time.Date(recorded.Year(), recorded.Month(), recorded.Day(), 0, 0, 0, 0, time.UTC)
	if day.After(req.EffectiveStart) {
		day = req.EffectiveStart
	}
	expiry := req.EffectiveStart.Add(ReservationHoldPeriod)
	proposal := req.ProposalRevisionID
	reservation, err := aggregates.NewBudgetReservation(tenant, id, budget.EntityID, &proposal, day, nil, recorded,
		amount.String(), budget.Currency, ReservationHeld, &expiry)
	if err != nil {
		return false, fmt.Errorf("promotionbudget: budget reservation: %w", err)
	}
	if _, err := comp.PutBudgetReservation(ctx, tx, reservation); err != nil {
		return false, fmt.Errorf("promotionbudget: hold budget for proposal %s: %w", proposal, err)
	}
	return true, nil
}

// ReleaseProposalBudget releases the proposal's HELD reservation inside tx at
// recordedAt. It reports false when the proposal holds nothing to release
// (no reservation, or one already committed or released).
func ReleaseProposalBudget(ctx context.Context, tx dbport.Tx, tenant, proposalRevisionID uuid.UUID, recordedAt time.Time) (bool, error) {
	if tx == nil || tenant == uuid.Nil || proposalRevisionID == uuid.Nil {
		return false, fmt.Errorf("promotionbudget: release budget: transaction, tenant and proposal are required")
	}
	comp := aggregates.CompensationStore{}
	held, err := comp.ReservationForProposal(ctx, tx, tenant, proposalRevisionID, farFuture)
	if errors.Is(err, aggregates.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("promotionbudget: read budget reservation: %w", err)
	}
	if held.Status != ReservationHeld {
		return false, nil
	}
	recorded := recordedAt.UTC()
	if !recorded.After(held.RecordedAt) {
		recorded = held.RecordedAt.Add(time.Microsecond)
	}
	released, err := aggregates.NewBudgetReservation(tenant, held.EntityID, held.BudgetRef, held.ProposalRef, held.EffectiveFrom, held.EffectiveTo,
		recorded, held.Amount, held.Currency, ReservationReleased, held.Expiry)
	if err != nil {
		return false, fmt.Errorf("promotionbudget: release budget reservation: %w", err)
	}
	if _, err := comp.PutBudgetReservation(ctx, tx, released); err != nil {
		return false, fmt.Errorf("promotionbudget: release budget for proposal %s: %w", proposalRevisionID, err)
	}
	return true, nil
}

// farFuture is a business instant past every hold's effective start, used to
// find a proposal's open-ended reservation regardless of when it started.
var farFuture = time.Date(9999, time.January, 1, 0, 0, 0, 0, time.UTC)

// ReleaseForIntent releases the hold of an intent's own latest proposal
// revision: the shape a cancellation has, which knows the intent it cancelled
// and not the revision that intent minted. An intent with no recorded
// revision holds nothing and reports false.
func ReleaseForIntent(ctx context.Context, tx dbport.Tx, tenant, intentID uuid.UUID, recordedAt time.Time) (bool, error) {
	if tx == nil || tenant == uuid.Nil || intentID == uuid.Nil {
		return false, fmt.Errorf("promotionbudget: release for intent: transaction, tenant and intent are required")
	}
	var latest *int64
	if err := tx.QueryRow(ctx, `SELECT max(revision) FROM proposal_revision WHERE tenant_id = $1 AND intent_id = $2`,
		tenant, intentID).Scan(&latest); err != nil {
		return false, fmt.Errorf("promotionbudget: read the intent's proposal revisions: %w", err)
	}
	if latest == nil || *latest <= 0 {
		return false, nil
	}
	row, err := (intentcontrol.RevisionStore{}).Load(ctx, tx, tenant, intentID, uint64(*latest))
	if err != nil {
		return false, fmt.Errorf("promotionbudget: load the intent's proposal revision: %w", err)
	}
	dto, err := intentcontrol.DecodeFullProposal(row.Payload, storedDigest{expected: row.MaterialDigest})
	if err != nil {
		return false, fmt.Errorf("promotionbudget: decode the intent's proposal revision: %w", err)
	}
	proposalID, err := uuid.Parse(dto.ProposalRevisionID)
	if err != nil {
		return false, nil
	}
	return ReleaseProposalBudget(ctx, tx, tenant, proposalID, recordedAt)
}

// storedDigest verifies a decoded revision against the digest its own row
// records, so a release never acts on a payload that is not the stored one.
type storedDigest struct{ expected string }

func (v storedDigest) VerifyProposalDigest(rev intent.ProposalRevision) error {
	if rev.MaterialDigest.Digest != v.expected {
		return fmt.Errorf("promotionbudget: proposal digest %q does not match stored %q", rev.MaterialDigest.Digest, v.expected)
	}
	return nil
}

var _ intentcontrol.FullProposalVerifier = storedDigest{}

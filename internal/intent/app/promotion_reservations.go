package app

// REV-006-02: the candidate path's half of the capacity fence.
//
// The fence itself lives in internal/domains/promotion/localcommit
// (PromotionReservationFence over the pure position and budget holds); this
// file derives the position head a minted proposal revision implies and
// holds it at candidate materialization, next to the durable budget hold.
// The served budget half stays durable: internal/data/promotionbudget holds
// the raise and the terminal writer verifies it HELD, bound and unexpired
// before committing. The position half had no equivalent, so the admitted
// proposal holds it here -- before the commit is attempted -- and the
// committer refuses without it.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/localcommit"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// reservationFenceOf answers the cell's capacity fence, defaulting to a
// fresh one when the engine was built outside NewCell (unit fixtures). The
// fence is process-local either way; a nil map never panics, it fails
// closed.
func (e *journeyEngine) reservationFenceOf() *localcommit.PromotionReservationFence {
	if e != nil && e.reservations != nil {
		return e.reservations
	}
	return localcommit.NewPromotionReservationFence()
}

// holdProposalPosition is the candidate path's half of the fence: read the
// revision's target seat as the fence's own reader sees it now, and hold one
// head at the seat's capacity FTE keyed to the proposal digest. The seat is
// read here, not trusted from the request: a hold cut for a seat nobody can
// still see would fence nothing. A missing seat refuses admission closed --
// the terminal resolver cannot commit a position-less promotion, so
// materializing its candidates would be evidence pointing at nothing.
func (e *journeyEngine) holdProposalPosition(
	ctx context.Context, inst intent.Instance, rev intent.ProposalRevision, observed time.Time,
) error {
	positionID, start, ok := promotionPositionTarget(rev)
	if !ok {
		return nil
	}
	if e.positionReader == nil {
		return fmt.Errorf("%w: this cell cannot verify the proposal's target seat", workspace.ErrJourneyUnavailable)
	}
	effective, err := values.ParseLocalDate(start.Format(time.DateOnly))
	if err != nil {
		return nil
	}
	known, err := values.NewKnownAt(values.NewInstant(observed.UTC()))
	if err != nil {
		return fmt.Errorf("app: target seat known-at: %w", err)
	}
	ref := values.EntityRef{Tenant: inst.Tenant, Kind: position.KindPosition, Id: positionID.String()}
	seat, exists, err := e.positionReader.PositionRevisionAt(ctx, position.PositionQuery{
		Tenant: inst.Tenant, Position: ref,
		AsOf: position.AsOf{EffectiveOn: effective, KnownAt: known},
	})
	if err != nil {
		return fmt.Errorf("app: read the proposal's target seat: %w", err)
	}
	if !exists {
		return fmt.Errorf("app: the proposal's target seat does not resolve")
	}
	req, ok, err := positionHoldRequest(rev, seat, controlSnapshotDigest(rev.ControlSnapshots))
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	// The seat read and the hold race by design: the fence serializes
	// competing acquisitions, and MinRevision pins this hold to the seat
	// revision read above, so a seat that moved underneath loses with a
	// typed conflict instead of holding a head it never saw.
	if _, err := e.reservationFenceOf().AcquirePosition(ctx, e.positionReader, req, observed); err != nil {
		return fmt.Errorf("app: hold the proposal's position: %w", err)
	}
	return nil
}

// positionHoldTTL mirrors the durable budget hold horizon
// (internal/data/promotionbudget.ReservationHoldPeriod): one horizon for the
// raise and the head it pays for, so neither outlives the other. The expiry
// derives from the effective start, never from the acquisition instant, so a
// re-materialized revision replays to the identical hold instead of
// conflicting with its own earlier self.
const positionHoldTTL = 30 * 24 * time.Hour

// promotionPositionTarget names the seat a minted revision implies: the
// POSITION subject and the revision's effective start. A revision with no
// POSITION subject or no effective date names none, mirroring
// proposalReservationFor's graceful skip.
func promotionPositionTarget(rev intent.ProposalRevision) (uuid.UUID, time.Time, bool) {
	var positionID uuid.UUID
	for _, s := range rev.Subjects {
		if s.Kind == "POSITION" {
			id, err := uuid.Parse(s.SubjectID)
			if err != nil || id == uuid.Nil {
				return uuid.Nil, time.Time{}, false
			}
			positionID = id
		}
	}
	if positionID == uuid.Nil {
		return uuid.Nil, time.Time{}, false
	}
	start, ok := effectiveStartOf(rev.EffectiveTime)
	if !ok {
		return uuid.Nil, time.Time{}, false
	}
	return positionID, start, true
}

// positionHoldRequest derives the position head a minted revision implies:
// the target seat at the revision's effective date, one head at the seat's
// own capacity FTE, keyed to the proposal digest. A revision with no usable
// target implies no hold.
func positionHoldRequest(
	rev intent.ProposalRevision, seat position.PositionRevision, controlDigest string,
) (position.PositionReservationRequest, bool, error) {
	positionID, start, ok := promotionPositionTarget(rev)
	if !ok {
		return position.PositionReservationRequest{}, false, nil
	}
	effective, err := values.ParseLocalDate(start.Format(time.DateOnly))
	if err != nil {
		return position.PositionReservationRequest{}, false, nil
	}
	known, err := values.NewKnownAt(values.NewInstant(rev.CreatedAt.Time()))
	if err != nil {
		return position.PositionReservationRequest{}, false, nil
	}
	digest := strings.TrimSpace(rev.MaterialDigest.Digest)
	if digest == "" || strings.TrimSpace(rev.ProposalRevisionID) == "" || strings.TrimSpace(controlDigest) == "" {
		return position.PositionReservationRequest{}, false, nil
	}
	if id := strings.TrimSpace(seat.Position.Id); id != "" && id != positionID.String() {
		return position.PositionReservationRequest{}, false, fmt.Errorf("app: seat revision is for position %q, want %q", id, positionID)
	}
	var jobCode, orgUnit string
	for _, s := range rev.ProposedState {
		switch s.FieldPath {
		case PlacementFieldPrefix + "job_code":
			jobCode = s.CanonicalText
		case PlacementFieldPrefix + "org_unit":
			orgUnit = s.CanonicalText
		}
	}
	return position.PositionReservationRequest{
		Tenant:             rev.Tenant,
		Position:           values.EntityRef{Tenant: rev.Tenant, Kind: position.KindPosition, Id: positionID.String()},
		AsOf:               position.AsOf{EffectiveOn: effective, KnownAt: known},
		ProposalRevisionID: rev.ProposalRevisionID,
		ProposalDigest:     digest,
		EffectiveDate:      effective,
		FTE:                seat.Capacity.CapacityFTE,
		Heads:              1,
		DesiredJobCode:     jobCode,
		DesiredOrgUnit:     orgUnit,
		MinRevision:        seat.Revision,
		AuthorityDigest:    controlDigest,
		IdempotencyKey:     digest,
		ExpiresAt:          start.Add(positionHoldTTL),
	}, true, nil
}

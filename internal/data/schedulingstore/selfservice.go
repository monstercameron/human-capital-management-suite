package schedulingstore

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/schedopt"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var _ schedopt.ShiftSelfServiceRepository = (*Store)(nil)

// LoadShiftScheduleFence returns the latest durable fence bound to digest.
// An absent row is the first-use state; a different digest fails closed.
func (s *Store) LoadShiftScheduleFence(ctx context.Context, tenant values.TenantId, scheduleID, publicationDigest string) (uint64, error) {
	if scheduleID == "" || publicationDigest == "" {
		return 0, coded(CodeInvalid, ErrInvalid, "schedule and publication digest are required")
	}
	fence := uint64(1)
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var digest string
		var stored int64
		err := tx.QueryRow(ctx, `SELECT publication_digest,fence FROM schedopt_self_service_state WHERE tenant_id=$1 AND schedule_id=$2`, tenantID, scheduleID).Scan(&digest, &stored)
		if errors.Is(err, dbport.ErrNoRows) {
			return nil
		}
		if err != nil {
			return coded(CodeDatabase, err, "load schedopt self-service state")
		}
		if stored < 1 || domainDigest(digest) != publicationDigest {
			return coded(CodeVersionConflict, ErrVersionConflict, "published schedule digest is stale")
		}
		fence = uint64(stored)
		return nil
	})
	return fence, err
}

// LoadShiftOffer rehydrates the offer payload for another service instance.
func (s *Store) LoadShiftOffer(ctx context.Context, tenant values.TenantId, scheduleID, offerID string) (schedopt.ShiftOffer, error) {
	var offer schedopt.ShiftOffer
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var state string
		var fence int64
		var digest string
		var payload []byte
		err := tx.QueryRow(ctx, `SELECT offer_state,offer_fence,publication_digest,payload FROM schedopt_self_service_offer WHERE tenant_id=$1 AND schedule_id=$2 AND offer_id=$3`, tenantID, scheduleID, offerID).Scan(&state, &fence, &digest, &payload)
		if errors.Is(err, dbport.ErrNoRows) {
			return coded(CodeNotFound, ErrNotFound, "schedopt shift offer")
		}
		if err != nil {
			return coded(CodeDatabase, err, "load schedopt shift offer")
		}
		if err := json.Unmarshal(payload, &offer); err != nil {
			return coded(CodeDatabase, err, "decode schedopt shift offer")
		}
		if fence < 1 {
			return coded(CodeDatabase, ErrInvalid, "stored schedopt shift offer fence is invalid")
		}
		if offer.ID != offerID || offer.FencingToken != uint64(fence) || offer.PublicationDigest != domainDigest(digest) {
			return coded(CodeDatabase, ErrInvalid, "stored schedopt shift offer does not match its fence row")
		}
		offer.State = schedopt.ShiftOfferState(state)
		offer.FencingToken = uint64(fence)
		offer.PublicationDigest = domainDigest(digest)
		return nil
	})
	return offer, err
}

// CreateShiftOffer persists an offer and advances the durable schedule fence.
// The first writer initializes the state from the publication bound into the
// offer; subsequent writers must present the latest schedule fence/digest.
func (s *Store) CreateShiftOffer(ctx context.Context, tenant values.TenantId, scheduleID string, expectedFence uint64, offer schedopt.ShiftOffer) (schedopt.ShiftOffer, error) {
	if expectedFence == 0 || expectedFence > uint64(^uint64(0)>>1) || scheduleID == "" || offer.ID == "" || offer.PublicationDigest == "" || offer.FencingToken == 0 {
		return schedopt.ShiftOffer{}, coded(CodeInvalid, ErrInvalid, "shift offer or expected fence is incomplete")
	}
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		if _, err := tx.Exec(ctx, `INSERT INTO schedopt_self_service_state (tenant_id,schedule_id,publication_digest,fence) VALUES ($1,$2,$3,1) ON CONFLICT (tenant_id,schedule_id) DO NOTHING`, tenantID, scheduleID, storageDigest(offer.PublicationDigest)); err != nil {
			return coded(CodeDatabase, err, "initialize schedopt self-service state")
		}
		var digest string
		var fence int64
		if err := tx.QueryRow(ctx, `SELECT publication_digest,fence FROM schedopt_self_service_state WHERE tenant_id=$1 AND schedule_id=$2 FOR UPDATE`, tenantID, scheduleID).Scan(&digest, &fence); err != nil {
			return coded(CodeDatabase, err, "lock schedopt self-service state")
		}
		if uint64(fence) != expectedFence || domainDigest(digest) != offer.PublicationDigest {
			return coded(CodeVersionConflict, ErrVersionConflict, "schedule publication or fence changed")
		}
		newFence := expectedFence + 1
		if newFence > uint64(^uint64(0)>>1) {
			return coded(CodeInvalid, ErrInvalid, "shift fence overflow")
		}
		offer.FencingToken = newFence
		payload, err := json.Marshal(offer)
		if err != nil {
			return coded(CodeInvalid, ErrInvalid, "encode shift offer")
		}
		if _, err := tx.Exec(ctx, `UPDATE schedopt_self_service_state SET fence=$3,updated_at=now() WHERE tenant_id=$1 AND schedule_id=$2 AND fence=$4`, tenantID, scheduleID, int64(newFence), int64(expectedFence)); err != nil {
			return coded(CodeDatabase, err, "advance schedopt schedule fence")
		}
		if _, err := tx.Exec(ctx, `UPDATE schedopt_published_schedule_current SET fence=$3,updated_at=now() WHERE tenant_id=$1 AND schedule_id=$2 AND publication_digest=$4`, tenantID, scheduleID, int64(newFence), digest); err != nil {
			return coded(CodeDatabase, err, "advance published schedule pointer fence")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schedopt_self_service_offer (tenant_id,schedule_id,offer_id,offer_state,offer_fence,publication_digest,payload) VALUES ($1,$2,$3,'OPEN',$4,$5,$6::jsonb)`, tenantID, scheduleID, offer.ID, int64(newFence), storageDigest(offer.PublicationDigest), payload); err != nil {
			return mapWriteError(CodeDuplicate, "schedopt_self_service_offer", err)
		}
		return nil
	})
	return offer, err
}

// ClaimShiftOffer atomically consumes one open offer only while the published
// digest it names is still current. The offer transition and schedule digest
// advance commit in one tenant-scoped transaction.
func (s *Store) ClaimShiftOffer(ctx context.Context, tenant values.TenantId, scheduleID, offerID string, expectedOfferFence uint64, newPublicationDigest string) (uint64, error) {
	if expectedOfferFence == 0 || expectedOfferFence > uint64(^uint64(0)>>1) || scheduleID == "" || offerID == "" || newPublicationDigest == "" {
		return 0, coded(CodeInvalid, ErrInvalid, "shift claim fence or identity is incomplete")
	}
	var nextFence uint64
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var currentDigest string
		var currentFence int64
		if err := tx.QueryRow(ctx, `SELECT publication_digest,fence FROM schedopt_self_service_state WHERE tenant_id=$1 AND schedule_id=$2 FOR UPDATE`, tenantID, scheduleID).Scan(&currentDigest, &currentFence); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return coded(CodeVersionConflict, ErrVersionConflict, "schedule state is missing")
			}
			return coded(CodeDatabase, err, "lock schedopt self-service state")
		}
		var state, offerDigest string
		var offerFence int64
		var payload []byte
		if err := tx.QueryRow(ctx, `SELECT offer_state,offer_fence,publication_digest,payload FROM schedopt_self_service_offer WHERE tenant_id=$1 AND schedule_id=$2 AND offer_id=$3 FOR UPDATE`, tenantID, scheduleID, offerID).Scan(&state, &offerFence, &offerDigest, &payload); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return coded(CodeVersionConflict, ErrVersionConflict, "shift offer is missing")
			}
			return coded(CodeDatabase, err, "lock schedopt shift offer")
		}
		if state != "OPEN" || uint64(offerFence) != expectedOfferFence || offerDigest != currentDigest {
			return coded(CodeVersionConflict, ErrVersionConflict, "offer is closed, stale, or bound to an older publication")
		}
		var offer schedopt.ShiftOffer
		if err := json.Unmarshal(payload, &offer); err != nil {
			return coded(CodeDatabase, err, "decode schedopt shift offer")
		}
		if offer.ID != offerID || offer.FencingToken != expectedOfferFence || offer.PublicationDigest != domainDigest(offerDigest) || offer.Kind != schedopt.ShiftOfferOpenClaim && offer.Kind != schedopt.ShiftOfferTrade {
			return coded(CodeDatabase, ErrInvalid, "stored schedopt shift offer does not match its fence row")
		}
		newState := "CLAIMED"
		if offer.Kind == schedopt.ShiftOfferTrade {
			newState = "TRADED"
		}
		if currentFence < 1 || currentFence >= int64(^uint64(0)>>1) {
			return coded(CodeInvalid, ErrInvalid, "shift fence overflow")
		}
		nextFence = uint64(currentFence + 1)
		if _, err := tx.Exec(ctx, `UPDATE schedopt_self_service_offer SET offer_state=$4,updated_at=now() WHERE tenant_id=$1 AND schedule_id=$2 AND offer_id=$3 AND offer_state='OPEN' AND offer_fence=$5`, tenantID, scheduleID, offerID, newState, int64(expectedOfferFence)); err != nil {
			return coded(CodeDatabase, err, "consume schedopt shift offer")
		}
		if _, err := tx.Exec(ctx, `UPDATE schedopt_self_service_state SET publication_digest=$3,fence=$4,updated_at=now() WHERE tenant_id=$1 AND schedule_id=$2 AND publication_digest=$5 AND fence=$6`, tenantID, scheduleID, storageDigest(newPublicationDigest), int64(nextFence), currentDigest, currentFence); err != nil {
			return coded(CodeDatabase, err, "advance schedopt publication fence")
		}
		return nil
	})
	return nextFence, err
}
